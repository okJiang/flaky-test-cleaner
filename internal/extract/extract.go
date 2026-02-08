package extract

import (
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/domain"
)

type Input struct {
	Repo       string
	Workflow   string
	RunID      int64
	RunURL     string
	HeadSHA    string
	JobID      int64
	JobName    string
	RunnerOS   string
	OccurredAt time.Time

	RawLogText string
}

type Extractor interface {
	Extract(in Input) []domain.Occurrence
}

type GoTestExtractor struct{}

func NewGoTestExtractor() *GoTestExtractor { return &GoTestExtractor{} }

func (e *GoTestExtractor) Extract(in Input) []domain.Occurrence {
	if in.RawLogText == "" {
		return nil
	}
	if in.OccurredAt.IsZero() {
		in.OccurredAt = time.Now()
	}

	lines := strings.Split(in.RawLogText, "\n")
	reGoFileLine := regexp.MustCompile(`[A-Za-z0-9_./-]+\.go:\d+`)
	patterns := []struct {
		re   *regexp.Regexp
		kind string
	}{
		{regexp.MustCompile(`--- FAIL: ([^\s]+)`), "go-test-fail"},
		{regexp.MustCompile(`^\[FAIL\]\s+(.+)$`), "ginkgo-fail"},
		{regexp.MustCompile(`panic:`), "panic"},
		{regexp.MustCompile(`DATA RACE`), "race"},
		{regexp.MustCompile(`(?i)(panic: test timed out after|test timed out after|context deadline exceeded|deadline exceeded)`), "timeout"},
	}

	var out []domain.Occurrence
	seen := map[string]struct{}{}
	for i, line := range lines {
		for _, p := range patterns {
			if !p.re.MatchString(line) {
				continue
			}

			name := ""
			switch p.kind {
			case "go-test-fail":
				if matches := p.re.FindStringSubmatch(line); len(matches) > 1 {
					name = matches[1]
				}
			case "ginkgo-fail":
				if matches := p.re.FindStringSubmatch(line); len(matches) > 1 {
					name = parseGinkgoFailTestName(matches[1])
				}
			}
			if name == "" {
				name = inferTestName(lines, i)
			}
			if strings.TrimSpace(name) == "" {
				continue
			}

			win := excerptWindowForKind(p.kind)
			excerpt := extractExcerpt(lines, i, win.before, win.after, win.max)
			errorSig := line
			switch p.kind {
			case "go-test-fail", "ginkgo-fail":
				if detail := findFailureDetailLine(lines, i+1, reGoFileLine); detail != "" {
					errorSig = detail + "\n" + line
				}
			case "panic", "timeout", "race":
				if i+1 < len(lines) {
					errorSig = line + "\n" + lines[i+1]
				}
			}

			key := name + "|" + errorSig
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, domain.Occurrence{
				Repo:           in.Repo,
				Workflow:       in.Workflow,
				RunID:          in.RunID,
				RunURL:         in.RunURL,
				HeadSHA:        in.HeadSHA,
				JobID:          in.JobID,
				JobName:        in.JobName,
				RunnerOS:       in.RunnerOS,
				OccurredAt:     in.OccurredAt,
				Framework:      "go test",
				TestName:       name,
				ErrorSignature: errorSig,
				Excerpt:        excerpt,
			})
		}
	}
	return dropParentTests(out)
}

func dropParentTests(in []domain.Occurrence) []domain.Occurrence {
	if len(in) == 0 {
		return in
	}
	parents := map[string]struct{}{}
	for _, o := range in {
		name := strings.TrimSpace(o.TestName)
		for strings.Contains(name, "/") {
			parent := name[:strings.LastIndex(name, "/")]
			parents[parent] = struct{}{}
			name = parent
		}
	}
	if len(parents) == 0 {
		return in
	}
	out := make([]domain.Occurrence, 0, len(in))
	for _, o := range in {
		if _, ok := parents[strings.TrimSpace(o.TestName)]; ok {
			continue
		}
		out = append(out, o)
	}
	return out
}

func parseGinkgoFailTestName(rest string) string {
	fields := strings.Fields(rest)
	for _, f := range fields {
		if strings.HasPrefix(f, "Test") {
			return f
		}
	}
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func inferTestName(lines []string, from int) string {
	maxBack := 200
	reFail := regexp.MustCompile(`--- FAIL: ([^\s]+)`)
	reRun := regexp.MustCompile(`^=== RUN\s+([^\s]+)`)
	reGinkgoFail := regexp.MustCompile(`^\[FAIL\]\s+(.+)$`)

	for i := from; i >= 0 && from-i <= maxBack; i-- {
		line := lines[i]
		if m := reFail.FindStringSubmatch(line); len(m) > 1 {
			return m[1]
		}
		if m := reRun.FindStringSubmatch(line); len(m) > 1 {
			return m[1]
		}
		if m := reGinkgoFail.FindStringSubmatch(line); len(m) > 1 {
			if name := parseGinkgoFailTestName(m[1]); strings.TrimSpace(name) != "" {
				return name
			}
		}
	}
	return ""
}

type excerptWindow struct {
	before int
	after  int
	max    int
}

func excerptWindowForKind(kind string) excerptWindow {
	switch kind {
	case "panic", "timeout":
		return excerptWindow{before: 40, after: 159, max: 200}
	case "race":
		return excerptWindow{before: 60, after: 139, max: 200}
	case "go-test-fail", "ginkgo-fail":
		return excerptWindow{before: 159, after: 40, max: 200}
	default:
		return excerptWindow{before: 100, after: 99, max: 200}
	}
}

var reGitHubActionsTS = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z\s+`)

func stripGitHubActionsPrefix(line string) string {
	return reGitHubActionsTS.ReplaceAllString(line, "")
}

func isGitHubActionsGroupStart(line string) bool {
	return strings.Contains(stripGitHubActionsPrefix(line), "##[group]")
}

func isGitHubActionsEndGroup(line string) bool {
	return strings.Contains(stripGitHubActionsPrefix(line), "##[endgroup]")
}

func findActionsGroupBounds(lines []string, center int) (int, int) {
	if center < 0 {
		center = 0
	}
	if center >= len(lines) {
		center = len(lines) - 1
	}
	start := -1
	for i := center; i >= 0; i-- {
		if isGitHubActionsEndGroup(lines[i]) {
			return -1, -1
		}
		if isGitHubActionsGroupStart(lines[i]) {
			start = i
			break
		}
	}
	if start == -1 {
		return -1, -1
	}
	end := -1
	for i := center; i < len(lines); i++ {
		if isGitHubActionsEndGroup(lines[i]) {
			end = i
			break
		}
	}
	return start, end
}

func extractExcerpt(lines []string, center, before, after, max int) string {
	if len(lines) == 0 {
		return ""
	}
	if center < 0 {
		center = 0
	}
	if center >= len(lines) {
		center = len(lines) - 1
	}

	start := center - before
	if start < 0 {
		start = 0
	}
	end := center + after + 1
	if end > len(lines) {
		end = len(lines)
	}

	if gs, ge := findActionsGroupBounds(lines, center); gs >= 0 {
		if gs > start {
			start = gs
		}
		if ge >= 0 && ge+1 < end {
			end = ge + 1
		}
	}

	if max > 0 && end-start > max {
		start, end = trimWindow(lines, center, start, end, before, after, max)
	}
	return strings.Join(lines[start:end], "\n")
}

func trimWindow(lines []string, center, start, end, before, after, max int) (int, int) {
	if max <= 0 || end-start <= max {
		return start, end
	}
	if center < start {
		center = start
	}
	if center >= end {
		center = end - 1
	}

	availBefore := center - start
	availAfter := end - center - 1
	totalBudget := max - 1
	if totalBudget < 0 {
		totalBudget = 0
	}

	desiredBefore, desiredAfter := before, after
	sum := desiredBefore + desiredAfter
	if sum <= 0 {
		desiredBefore = totalBudget
		desiredAfter = 0
	} else if sum > totalBudget {
		scale := float64(totalBudget) / float64(sum)
		desiredBefore = int(math.Round(float64(desiredBefore) * scale))
		if desiredBefore < 0 {
			desiredBefore = 0
		}
		if desiredBefore > totalBudget {
			desiredBefore = totalBudget
		}
		desiredAfter = totalBudget - desiredBefore
	}

	beforeBudget := desiredBefore
	if beforeBudget > availBefore {
		beforeBudget = availBefore
	}
	afterBudget := desiredAfter
	if afterBudget > availAfter {
		afterBudget = availAfter
	}

	remaining := totalBudget - (beforeBudget + afterBudget)
	if remaining > 0 {
		roomBefore := availBefore - beforeBudget
		roomAfter := availAfter - afterBudget
		if roomAfter > roomBefore {
			add := minInt(remaining, roomAfter)
			afterBudget += add
			remaining -= add
		}
		if remaining > 0 {
			add := minInt(remaining, roomBefore)
			beforeBudget += add
			remaining -= add
		}
		if remaining > 0 {
			add := minInt(remaining, availAfter-afterBudget)
			afterBudget += add
		}
	}

	newStart := center - beforeBudget
	newEnd := center + afterBudget + 1
	if newStart < start {
		newStart = start
	}
	if newEnd > end {
		newEnd = end
	}
	return newStart, newEnd
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func findFailureDetailLine(lines []string, from int, reGoFileLine *regexp.Regexp) string {
	const maxLookahead = 20
	reStop := regexp.MustCompile(`^(?:=== RUN|--- FAIL:|\[FAIL\]|panic:|FAIL\b|PASS\b)`)

	for i := from; i < len(lines) && i-from <= maxLookahead; i++ {
		raw := lines[i]
		noTS := stripGitHubActionsPrefix(raw)
		trim := strings.TrimSpace(noTS)
		if trim == "" {
			continue
		}
		if reStop.MatchString(trim) {
			return ""
		}
		if strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.HasPrefix(noTS, "    ") || reGoFileLine.MatchString(trim) {
			return raw
		}
	}
	return ""
}

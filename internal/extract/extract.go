package extract

import (
	"regexp"
	"strings"
	"time"
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

type Occurrence struct {
	Repo           string
	Workflow       string
	RunID          int64
	RunURL         string
	HeadSHA        string
	JobID          int64
	JobName        string
	RunnerOS       string
	OccurredAt     time.Time
	Framework      string
	TestName       string
	ErrorSignature string
	Excerpt        string
	Fingerprint    string
}

func (o Occurrence) PlatformBucket() string {
	if o.RunnerOS == "" {
		return ""
	}
	return o.RunnerOS
}

type Extractor interface {
	Extract(in Input) []Occurrence
}

type GoTestExtractor struct{}

func NewGoTestExtractor() *GoTestExtractor { return &GoTestExtractor{} }

func (e *GoTestExtractor) Extract(in Input) []Occurrence {
	if in.RawLogText == "" {
		return nil
	}
	if in.OccurredAt.IsZero() {
		in.OccurredAt = time.Now()
	}

	lines := strings.Split(in.RawLogText, "\n")
	patterns := []struct {
		re   *regexp.Regexp
		kind string
	}{
		{regexp.MustCompile(`--- FAIL: ([^\s]+)`), "go-test-fail"},
		{regexp.MustCompile(`\[FAIL\]`), "ginkgo-fail"},
		{regexp.MustCompile(`panic:`), "panic"},
		{regexp.MustCompile(`DATA RACE`), "race"},
		{regexp.MustCompile(`timeout`), "timeout"},
	}

	var out []Occurrence
	seen := map[string]struct{}{}
	for i, line := range lines {
		for _, p := range patterns {
			if !p.re.MatchString(line) {
				continue
			}
			name := ""
			if matches := p.re.FindStringSubmatch(line); len(matches) > 1 {
				name = matches[1]
			}
			excerpt := extractExcerpt(lines, i, 40, 40, 120)
			errorSig := line
			if i+1 < len(lines) {
				errorSig = line + "\n" + lines[i+1]
			}
			key := name + "|" + errorSig
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, Occurrence{
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
	return out
}

func extractExcerpt(lines []string, center, before, after, max int) string {
	start := center - before
	if start < 0 {
		start = 0
	}
	end := center + after + 1
	if end > len(lines) {
		end = len(lines)
	}
	excerpt := lines[start:end]
	if max > 0 && len(excerpt) > max {
		trimStart := center - max/2
		if trimStart < 0 {
			trimStart = 0
		}
		trimEnd := trimStart + max
		if trimEnd > len(lines) {
			trimEnd = len(lines)
			trimStart = trimEnd - max
			if trimStart < 0 {
				trimStart = 0
			}
		}
		excerpt = lines[trimStart:trimEnd]
	}
	return strings.Join(excerpt, "\n")
}

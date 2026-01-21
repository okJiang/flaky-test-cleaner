package issueagent

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/classify"
	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
)

type Agent struct{}

func New() *Agent { return &Agent{} }

type Input struct {
	Fingerprint    store.FingerprintRecord
	Occurrences    []extract.Occurrence
	Classification classify.Result
}

type Comment struct {
	Body string
}

func (a *Agent) BuildInitialComment(in Input) Comment {
	body := renderInitialComment(in)
	return Comment{Body: body}
}

func renderInitialComment(in Input) string {
	var b strings.Builder
	occ := in.Occurrences
	runs := describeRuns(occ)
	testName := pickTestName(in)
	firstSeen, lastSeen := timeline(in, occ)
	hypotheses := generateHypotheses(occ)
	repro := reproductionIdeas(testName, occ)
	nextSteps := suggestedNextSteps(in.Classification)
	risk := riskNotes(in.Classification)

	b.WriteString("<!-- FTC:ISSUE_AGENT_START -->\n")
	b.WriteString("## AI Analysis Summary\n\n")
	fmt.Fprintf(&b, "- Fingerprint: `%s`\n", safe(in.Fingerprint.Fingerprint))
	fmt.Fprintf(&b, "- Classification: **%s** (confidence %.2f)\n", in.Classification.Class, in.Classification.Confidence)
	if strings.TrimSpace(in.Classification.Explanation) != "" {
		fmt.Fprintf(&b, "- Classifier notes: %s\n", strings.TrimSpace(in.Classification.Explanation))
	}
	fmt.Fprintf(&b, "- Test focus: %s\n", safe(testName))
	fmt.Fprintf(&b, "- Runs analyzed: %s\n", runs)
	fmt.Fprintf(&b, "- Evidence window: %s → %s\n", formatTime(firstSeen), formatTime(lastSeen))

	b.WriteString("\n## Hypotheses\n")
	for i, h := range hypotheses {
		fmt.Fprintf(&b, "%d. %s\n", i+1, h)
	}

	b.WriteString("\n## Reproduction Ideas\n")
	for _, step := range repro {
		fmt.Fprintf(&b, "- %s\n", step)
	}

	b.WriteString("\n## Suggested Fix Directions\n")
	for _, step := range nextSteps {
		fmt.Fprintf(&b, "- %s\n", step)
	}

	b.WriteString("\n## Risk Notes\n")
	for _, note := range risk {
		fmt.Fprintf(&b, "- %s\n", note)
	}

	b.WriteString("\n## Evidence Highlights\n")
	for _, line := range evidenceHighlights(occ) {
		fmt.Fprintf(&b, "- %s\n", line)
	}

	b.WriteString("\n<!-- FTC:ISSUE_AGENT_END -->\n")
	return b.String()
}

func pickTestName(in Input) string {
	if strings.TrimSpace(in.Fingerprint.TestName) != "" {
		return in.Fingerprint.TestName
	}
	for _, occ := range in.Occurrences {
		if strings.TrimSpace(occ.TestName) != "" {
			return occ.TestName
		}
	}
	return "unknown-test"
}

func describeRuns(occ []extract.Occurrence) string {
	if len(occ) == 0 {
		return "n/a"
	}
	set := map[int64]string{}
	for _, o := range occ {
		set[o.RunID] = o.RunURL
	}
	var ids []int64
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	if len(ids) > 5 {
		ids = ids[:5]
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if url := set[id]; url != "" {
			parts = append(parts, fmt.Sprintf("[%d](%s)", id, url))
		} else {
			parts = append(parts, fmt.Sprintf("%d", id))
		}
	}
	return strings.Join(parts, ", ")
}

func timeline(in Input, occ []extract.Occurrence) (time.Time, time.Time) {
	first := in.Fingerprint.FirstSeenAt
	last := in.Fingerprint.LastSeenAt
	for _, o := range occ {
		if first.IsZero() || o.OccurredAt.Before(first) {
			first = o.OccurredAt
		}
		if o.OccurredAt.After(last) {
			last = o.OccurredAt
		}
	}
	return first, last
}

type hypothesisRule struct {
	keywords []string
	message  string
}

var hypothesisRules = []hypothesisRule{
	{keywords: []string{"data race", "race detected"}, message: "Logs show race detector warnings; run with `go test -race` and audit shared state around the failing test."},
	{keywords: []string{"panic", "fatal error"}, message: "A panic occurred in the failing test; inspect the stack trace and recent changes touching the reported function."},
	{keywords: []string{"timeout", "timed out", "deadline exceeded"}, message: "Timeout keywords found; the test likely hangs or exceeds time budget. Verify cleanup and consider instrumentation around network calls."},
	{keywords: []string{"connection reset", "broken pipe", "dial tcp"}, message: "Network/connectivity issues detected; confirm TiKV/TiDB clusters are reachable during the test and add retries/backoff."},
	{keywords: []string{"assert", "expected", "mismatch"}, message: "Assertion mismatch hints at logic regression; compare the observed vs expected values in the excerpt."},
}

func generateHypotheses(occ []extract.Occurrence) []string {
	textBuckets := make([]string, len(occ))
	for i, o := range occ {
		textBuckets[i] = strings.ToLower(o.ErrorSignature + "\n" + o.Excerpt)
	}
	var out []string
	for _, rule := range hypothesisRules {
		if matchAny(rule.keywords, textBuckets) {
			out = append(out, rule.message)
		}
	}
	if len(out) == 0 {
		out = append(out, "No dominant signal detected; review the log excerpts above and gather additional context from recent commits touching the test.")
	}
	return out
}

func matchAny(keywords []string, texts []string) bool {
	for _, text := range texts {
		for _, kw := range keywords {
			if strings.Contains(text, kw) {
				return true
			}
		}
	}
	return false
}

func reproductionIdeas(testName string, occ []extract.Occurrence) []string {
	regexName := regexp.QuoteMeta(testName)
	if regexName == "" || regexName == "unknown-test" {
		regexName = "Test.*"
	}
	var head string
	for _, o := range occ {
		if strings.TrimSpace(o.HeadSHA) != "" {
			head = o.HeadSHA
			break
		}
	}
	short := shortSHA(head)
	var steps []string
	if short != "" {
		steps = append(steps, fmt.Sprintf("Checkout commit `%s` (or the latest ancestor) locally to mirror the failing CI context.", short))
	}
	steps = append(steps, fmt.Sprintf("Stress the suspected test: `go test ./... -run '^%s$' -count=30 -race`.", regexName))
	steps = append(steps, "Capture verbose logs (`GO111MODULE=on GODEBUG=gctrace=1`) to identify stalls or resource starvation.")
	return steps
}

func suggestedNextSteps(res classify.Result) []string {
	switch res.Class {
	case classify.ClassInfraFlake:
		return []string{
			"Correlate failure timestamp with infra metrics (network, runners) to confirm whether it is safe to auto-ignore.",
			"Add defensive retries or health checks around external services used by the test.",
		}
	case classify.ClassLikelyRegression:
		return []string{
			"Diff the commits between first failure and last passing run to locate candidate changes.",
			"Add focused assertions around the failing code path to narrow down incorrect behavior.",
		}
	default:
		return []string{
			"Audit the test for shared global state or timing assumptions; convert to isolated setup if possible.",
			"Add diagnostics (logging, metrics) around the failing assertions to capture additional evidence in future runs.",
		}
	}
}

func riskNotes(res classify.Result) []string {
	switch res.Class {
	case classify.ClassInfraFlake:
		return []string{
			"Noise can hide real regressions; keep infra flakes from blocking merges by routing to metrics-only pipeline.",
		}
	case classify.ClassLikelyRegression:
		return []string{
			"Potential correctness regression: prioritize manual confirmation before promoting automated fixes.",
		}
	default:
		return []string{
			"Flaky tests erode CI signal; each recurrence costs reruns and review time. Prioritize stabilization before enabling auto-fix.",
		}
	}
}

func evidenceHighlights(occ []extract.Occurrence) []string {
	if len(occ) == 0 {
		return []string{"No occurrences available for evidence."}
	}
	limit := len(occ)
	if limit > 3 {
		limit = 3
	}
	var lines []string
	for i := 0; i < limit; i++ {
		o := occ[i]
		line := fmt.Sprintf("Run [%d](%s) · Job %s · Commit %s · Test %s — %s",
			o.RunID,
			firstOrDefault(o.RunURL, "#"),
			safe(o.JobName),
			shortSHA(o.HeadSHA),
			safe(o.TestName),
			summarize(o.ErrorSignature),
		)
		lines = append(lines, line)
	}
	return lines
}

func summarize(sig string) string {
	line := strings.TrimSpace(strings.SplitN(sig, "\n", 2)[0])
	if len(line) > 120 {
		return line[:120] + "..."
	}
	if line == "" {
		return "unknown error"
	}
	return line
}

func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return strings.TrimSpace(sha)
	}
	return sha[:7]
}

func safe(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format(time.RFC3339)
}

func firstOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

package issue

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/classify"
	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/github"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
)

type Options struct {
	Owner  string
	Repo   string
	DryRun bool
}

type Manager struct {
	opts Options
}

func NewManager(opts Options) *Manager { return &Manager{opts: opts} }

type PlanInput struct {
	Fingerprint    store.FingerprintRecord
	Occurrences    []extract.Occurrence
	Classification classify.Result
}

type PlannedChange struct {
	Noop        bool
	Create      bool
	IssueNumber int
	Title       string
	Body        string
	Labels      []string
}

func (m *Manager) PlanIssueUpdate(in PlanInput) (PlannedChange, error) {
	if len(in.Occurrences) == 0 {
		return PlannedChange{Noop: true}, nil
	}
	shortSig := summarizeSignature(in.Occurrences[0].ErrorSignature)
	name := in.Fingerprint.TestName
	if name == "" {
		name = in.Occurrences[0].TestName
	}
	if name == "" {
		name = "unknown-test"
	}
	if shortSig == "" {
		shortSig = "unknown-error"
	}
	title := fmt.Sprintf("[flaky] %s — %s", name, shortSig)
	labels := defaultLabels(in.Classification)
	body := buildBody(in, labels)

	if in.Fingerprint.IssueNumber == 0 {
		return PlannedChange{
			Create: true,
			Title:  title,
			Body:   body,
			Labels: labels,
		}, nil
	}
	return PlannedChange{
		IssueNumber: in.Fingerprint.IssueNumber,
		Title:       title,
		Body:        body,
		Labels:      labels,
	}, nil
}

func (m *Manager) Apply(ctx context.Context, gh *github.Client, ch PlannedChange) (int, error) {
	if ch.Noop {
		return 0, nil
	}
	if m.opts.DryRun {
		return 0, nil
	}
	if err := gh.EnsureLabels(ctx, m.opts.Owner, m.opts.Repo, ch.Labels); err != nil {
		return 0, err
	}
	if ch.Create {
		created, err := gh.CreateIssue(ctx, m.opts.Owner, m.opts.Repo, github.CreateIssueInput{
			Title:  ch.Title,
			Body:   ch.Body,
			Labels: ch.Labels,
		})
		if err != nil {
			return 0, err
		}
		return created.Number, nil
	}
	_, err := gh.UpdateIssue(ctx, m.opts.Owner, m.opts.Repo, ch.IssueNumber, github.UpdateIssueInput{
		Title:  &ch.Title,
		Body:   &ch.Body,
		Labels: ch.Labels,
	})
	if err != nil {
		return 0, err
	}
	return ch.IssueNumber, nil
}

func defaultLabels(res classify.Result) []string {
	labels := []string{
		"flaky-test-cleaner/ai-managed",
	}
	switch res.Class {
	case classify.ClassFlakyTest:
		labels = append(labels, "flaky-test-cleaner/flaky-test")
	case classify.ClassUnknown:
		labels = append(labels, "flaky-test-cleaner/needs-triage")
	case classify.ClassLikelyRegression:
		labels = append(labels, "flaky-test-cleaner/needs-triage")
	}
	return labels
}

func buildBody(in PlanInput, labels []string) string {
	firstSeen := in.Fingerprint.FirstSeenAt
	lastSeen := in.Fingerprint.LastSeenAt
	if firstSeen.IsZero() || lastSeen.IsZero() {
		firstSeen, lastSeen = occurrenceRange(in.Occurrences)
	}

	summary := fmt.Sprintf("## Summary\n\n- Classification: **%s** (confidence %.2f)\n- First seen: %s\n- Last seen: %s\n",
		in.Classification.Class,
		in.Classification.Confidence,
		formatTime(firstSeen),
		formatTime(lastSeen),
	)

	evidence := "## Evidence\n\n| Run | Workflow | Job | Commit | Test | Error Signature |\n| --- | --- | --- | --- | --- | --- |\n"
	for _, occ := range in.Occurrences {
		evidence += fmt.Sprintf("| [%d](%s) | %s | %s | %s | %s | %s |\n",
			occ.RunID, occ.RunURL, occ.Workflow, occ.JobName, shortSHA(occ.HeadSHA), safe(occ.TestName), summarizeSignature(occ.ErrorSignature),
		)
	}

	excerpts := "## Log Excerpts\n"
	for _, occ := range in.Occurrences {
		if occ.Excerpt == "" {
			continue
		}
		excerpts += fmt.Sprintf("\n<details>\n<summary>Run %d — %s</summary>\n\n````\n%s\n````\n</details>\n",
			occ.RunID, safe(occ.JobName), occ.Excerpt,
		)
	}

	nextActions := "## Next Actions\n\n- [ ] Re-run the failing test to confirm reproducibility\n- [ ] Check recent changes around the failing test\n- [ ] Consider adding retry/timeout stabilization if flaky\n"

	automation := fmt.Sprintf("## Automation\n\n- Fingerprint: `%s`\n- Labels: %s\n- Last scan: %s\n",
		in.Fingerprint.Fingerprint,
		strings.Join(labels, ", "),
		formatTime(time.Now()),
	)

	return joinBlocks(
		wrapBlock("SUMMARY", summary),
		wrapBlock("EVIDENCE", evidence),
		wrapBlock("EXCERPTS", excerpts),
		wrapBlock("NEXT_ACTIONS", nextActions),
		wrapBlock("AUTOMATION", automation),
	)
}

func wrapBlock(name, content string) string {
	return fmt.Sprintf("<!-- FTC:%s_START -->\n%s\n<!-- FTC:%s_END -->", name, strings.TrimSpace(content), name)
}

func joinBlocks(blocks ...string) string {
	return strings.Join(blocks, "\n\n") + "\n"
}

func summarizeSignature(sig string) string {
	line := strings.TrimSpace(strings.SplitN(sig, "\n", 2)[0])
	if len(line) > 120 {
		return line[:120] + "..."
	}
	return line
}

func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}

func safe(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func occurrenceRange(list []extract.Occurrence) (time.Time, time.Time) {
	if len(list) == 0 {
		return time.Time{}, time.Time{}
	}
	first := list[0].OccurredAt
	last := list[0].OccurredAt
	for _, occ := range list[1:] {
		if occ.OccurredAt.Before(first) {
			first = occ.OccurredAt
		}
		if occ.OccurredAt.After(last) {
			last = occ.OccurredAt
		}
	}
	return first, last
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format(time.RFC3339)
}

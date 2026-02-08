package issue

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/domain"
	"github.com/okJiang/flaky-test-cleaner/internal/ports"
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
	Fingerprint    domain.FingerprintRecord
	Occurrences    []domain.Occurrence
	Classification domain.Classification
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
	title := fmt.Sprintf("[flaky] %s", name)
	if name == "unknown-test" {
		title = fmt.Sprintf("[flaky] %s — %s", name, shortSig)
	}
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

func (m *Manager) Apply(ctx context.Context, gh ports.IssueService, ch PlannedChange) (int, error) {
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
		created, err := gh.CreateIssue(ctx, m.opts.Owner, m.opts.Repo, domain.CreateIssueInput{
			Title:  ch.Title,
			Body:   ch.Body,
			Labels: ch.Labels,
		})
		if err != nil {
			return 0, err
		}
		return created.Number, nil
	}
	_, err := gh.UpdateIssue(ctx, m.opts.Owner, m.opts.Repo, ch.IssueNumber, domain.UpdateIssueInput{
		Title:  &ch.Title,
		Body:   &ch.Body,
		Labels: ch.Labels,
	})
	if err != nil {
		return 0, err
	}
	return ch.IssueNumber, nil
}

func defaultLabels(res domain.Classification) []string {
	labels := []string{"flaky-test-cleaner/ai-managed"}
	switch res.Class {
	case domain.ClassFlakyTest:
		labels = append(labels, "flaky-test-cleaner/flaky-test")
	case domain.ClassUnknown, domain.ClassLikelyRegression:
		labels = append(labels, "flaky-test-cleaner/needs-triage")
	}
	return labels
}

func buildBody(in PlanInput, labels []string) string {
	occ := make([]domain.Occurrence, len(in.Occurrences))
	copy(occ, in.Occurrences)
	sort.SliceStable(occ, func(i, j int) bool {
		if occ[i].OccurredAt.Equal(occ[j].OccurredAt) {
			return occ[i].RunID > occ[j].RunID
		}
		return occ[i].OccurredAt.After(occ[j].OccurredAt)
	})

	firstSeen := in.Fingerprint.FirstSeenAt
	lastSeen := in.Fingerprint.LastSeenAt
	if firstSeen.IsZero() || lastSeen.IsZero() {
		firstSeen, lastSeen = occurrenceRange(occ)
	}

	summary := fmt.Sprintf("## Summary\n\n- Classification: **%s** (confidence %.2f)\n- First seen: %s\n- Last seen: %s\n",
		in.Classification.Class,
		in.Classification.Confidence,
		formatTime(firstSeen),
		formatTime(lastSeen),
	)

	evidence := "## Evidence\n\n| Run | Commit | Test | Error Signature |\n| --- | --- | --- | --- |\n"
	for _, o := range occ {
		evidence += fmt.Sprintf("| [%d](%s) | %s | %s | %s |\n",
			o.RunID, o.RunURL, shortSHA(o.HeadSHA), safe(o.TestName), summarizeSignature(o.ErrorSignature),
		)
	}

	excerpts := "## Log Excerpts\n"
	const maxExcerptRuns = 2
	for i, o := range occ {
		if i >= maxExcerptRuns {
			break
		}
		if o.Excerpt == "" {
			continue
		}
		excerpts += fmt.Sprintf("\n<details>\n<summary>Run %d — %s</summary>\n\n````\n%s\n````\n</details>\n",
			o.RunID, safe(o.JobName), o.Excerpt,
		)
	}

	automation := fmt.Sprintf("<details>\n<summary>Automation</summary>\n\n- Fingerprint: `%s`\n- Labels: %s\n- Last scan: %s\n</details>\n",
		in.Fingerprint.Fingerprint,
		strings.Join(labels, ", "),
		formatTime(time.Now()),
	)

	return joinBlocks(
		wrapBlock("SUMMARY", summary),
		wrapBlock("EVIDENCE", evidence),
		wrapBlock("EXCERPTS", excerpts),
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
	reTS := regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z\s+`)
	line = strings.TrimSpace(reTS.ReplaceAllString(line, ""))
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

func occurrenceRange(list []domain.Occurrence) (time.Time, time.Time) {
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

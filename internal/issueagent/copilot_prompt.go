package issueagent

import (
	"fmt"
	"strings"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/domain"
)

func BuildCopilotSystemMessage() string {
	return strings.TrimSpace(`You are an assistant that writes a GitHub Issue comment for flaky test triage.

Rules:
- DO NOT call any tools.
- DO NOT modify files.
- Output MUST be GitHub-flavored Markdown.
- Output MUST contain exactly one block delimited by these markers:
  <!-- FTC:ISSUE_AGENT_START -->
  ...content...
  <!-- FTC:ISSUE_AGENT_END -->
- If RepoContextSnippets are provided, you MUST ground your reasoning in them and cite snippet IDs (e.g. "S1") and the file+line ranges.
- Provide concrete reproduction commands.
- Provide a concrete patch plan (what to change, where, and why). If feasible, include a small diff sketch.
- End with a short "Maintainer approval checklist".
- Keep it concise and actionable.`)
}

func BuildCopilotPrompt(fp domain.FingerprintRecord, occ []domain.Occurrence, c domain.Classification) string {
	return BuildCopilotPromptWithRepoContext(fp, occ, c, "")
}

func BuildCopilotPromptWithRepoContext(fp domain.FingerprintRecord, occ []domain.Occurrence, c domain.Classification, repoContext string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fingerprint: %s\n", fp.Fingerprint)
	fmt.Fprintf(&b, "Repo: %s\n", fp.Repo)
	fmt.Fprintf(&b, "TestName: %s\n", fp.TestName)
	fmt.Fprintf(&b, "Framework: %s\n", fp.Framework)
	fmt.Fprintf(&b, "Class: %s\n", c.Class)
	fmt.Fprintf(&b, "Confidence: %.2f\n", c.Confidence)
	if strings.TrimSpace(c.Explanation) != "" {
		fmt.Fprintf(&b, "ClassifierNotes: %s\n", strings.TrimSpace(c.Explanation))
	}
	fmt.Fprintf(&b, "FirstSeenAt: %s\n", fp.FirstSeenAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "LastSeenAt: %s\n", fp.LastSeenAt.UTC().Format(time.RFC3339))

	b.WriteString("\nRecentOccurrences:\n")
	limit := len(occ)
	if limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		o := occ[i]
		fmt.Fprintf(&b, "- run_id=%d job=%q sha=%s test=%q os=%q\n  error=%q\n  excerpt=\n%s\n",
			o.RunID, o.JobName, shortSHA(o.HeadSHA), o.TestName, o.RunnerOS, o.ErrorSignature, o.Excerpt)
	}

	if strings.TrimSpace(repoContext) != "" {
		b.WriteString("\n")
		b.WriteString(repoContext)
		b.WriteString("\n")
	}

	b.WriteString("\nWrite the issue comment now following the marker rules.\n")
	return b.String()
}

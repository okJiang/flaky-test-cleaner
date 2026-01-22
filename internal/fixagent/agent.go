package fixagent

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/github"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
	"github.com/okJiang/flaky-test-cleaner/internal/workspace"
)

type Options struct {
	Owner      string
	Repo       string
	DryRun     bool
	GitHub     *github.Client
	Workspace  *workspace.Manager
	Store      store.Store
	BaseBranch string
}

type Agent struct {
	opts Options
}

func New(opts Options) (*Agent, error) {
	if opts.GitHub == nil {
		return nil, fmt.Errorf("fixagent requires github client")
	}
	if opts.Workspace == nil {
		return nil, fmt.Errorf("fixagent requires workspace manager")
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("fixagent requires store")
	}
	if strings.TrimSpace(opts.BaseBranch) == "" {
		return nil, fmt.Errorf("fixagent requires base branch")
	}
	return &Agent{opts: opts}, nil
}

type AttemptResult struct {
	CommentBody string
	BranchName  string
	PRNumber    int
}

type FollowUpResult struct {
	CommentBody string
	BranchName  string
}

func (a *Agent) Attempt(ctx context.Context, fp store.FingerprintRecord, occ []extract.Occurrence) (AttemptResult, error) {
	if fp.IssueNumber == 0 {
		return AttemptResult{}, fmt.Errorf("fingerprint %s missing issue number", fp.Fingerprint)
	}
	if len(occ) == 0 {
		return AttemptResult{}, fmt.Errorf("no occurrences available for fingerprint %s", fp.Fingerprint)
	}
	targetSHA := occ[0].HeadSHA
	if strings.TrimSpace(targetSHA) == "" {
		return AttemptResult{}, fmt.Errorf("occurrence missing head sha for fingerprint %s", fp.Fingerprint)
	}
	leaseName := fp.Fingerprint
	if len(leaseName) > 16 {
		leaseName = leaseName[:16]
	}
	lease, err := a.opts.Workspace.Acquire(ctx, fmt.Sprintf("fix-%s", leaseName), targetSHA)
	if err != nil {
		return AttemptResult{}, fmt.Errorf("acquire workspace: %w", err)
	}
	defer lease.Release(context.Background())

	if err := writeTodoFile(lease.Path, fp, occ[0]); err != nil {
		return AttemptResult{}, fmt.Errorf("write todo: %w", err)
	}
	testSummary := "go test ./... skipped"
	if summary, err := runGoTest(ctx, lease.Path); err != nil {
		testSummary = fmt.Sprintf("go test ./... failed: %v\n%s", err, summary)
		log.Printf("fixagent go test failed: %v", err)
	} else {
		testSummary = fmt.Sprintf("go test ./... succeeded:\n%s", summary)
	}

	branch := fmt.Sprintf("ai/fix/%s", shortBranch(fp.Fingerprint))
	body := buildPreparationComment(fp, occ, lease.Path, testSummary)
	if a.opts.DryRun {
		return AttemptResult{CommentBody: body, BranchName: branch}, nil
	}
	if err := a.opts.GitHub.CreateIssueComment(ctx, a.opts.Owner, a.opts.Repo, fp.IssueNumber, body); err != nil {
		return AttemptResult{}, err
	}
	if err := a.opts.Store.UpdateFingerprintState(ctx, fp.Fingerprint, store.StatePROpen); err != nil {
		return AttemptResult{}, err
	}
	if err := a.opts.Store.RecordAudit(ctx, "fixagent.prepare", fmt.Sprintf("issue/%d", fp.IssueNumber), "success", lease.Path); err != nil {
		return AttemptResult{}, err
	}
	if err := createBranch(ctx, lease.Path, branch); err != nil {
		return AttemptResult{}, err
	}
	if err := commitAll(ctx, lease.Path, fmt.Sprintf("fix flaky test %s", safe(fp.TestName))); err != nil {
		return AttemptResult{}, err
	}
	if err := pushBranch(ctx, lease.Path, branch); err != nil {
		return AttemptResult{}, err
	}
	pr, err := a.opts.GitHub.CreatePullRequest(ctx, a.opts.Owner, a.opts.Repo, github.CreatePullRequestInput{
		Title: fmt.Sprintf("[AI] Stabilize %s", safe(fp.TestName)),
		Head:  branch,
		Base:  a.opts.BaseBranch,
		Body:  body,
	})
	if err != nil {
		return AttemptResult{}, err
	}
	if err := a.opts.GitHub.AddIssueLabels(ctx, a.opts.Owner, a.opts.Repo, fp.IssueNumber, []string{"flaky-test-cleaner/ai-pr-open"}); err != nil {
		return AttemptResult{}, err
	}
	fpUpdate := fp
	fpUpdate.PRNumber = pr.Number
	fpUpdate.State = store.StatePROpen
	fpUpdate.StateChangedAt = time.Now()
	if err := a.opts.Store.UpsertFingerprint(ctx, fpUpdate); err != nil {
		return AttemptResult{}, err
	}
	if err := a.opts.Store.RecordAudit(ctx, "fixagent.pr_create", fmt.Sprintf("issue/%d", fp.IssueNumber), "success", fmt.Sprintf("pr#%d", pr.Number)); err != nil {
		return AttemptResult{}, err
	}
	return AttemptResult{CommentBody: body, BranchName: branch, PRNumber: pr.Number}, nil
}

type PRFeedback struct {
	PRNumber         int
	PRURL            string
	HeadSHA          string
	ChangesRequested []github.PullRequestReview
	CombinedStatus   github.CombinedStatus
}

func (f PRFeedback) NeedsUpdate() bool {
	if len(f.ChangesRequested) > 0 {
		return true
	}
	s := strings.ToLower(strings.TrimSpace(f.CombinedStatus.State))
	return s == "failure" || s == "error"
}

func (a *Agent) FollowUp(ctx context.Context, fp store.FingerprintRecord, fb PRFeedback) (FollowUpResult, error) {
	if fp.IssueNumber == 0 {
		return FollowUpResult{}, fmt.Errorf("fingerprint %s missing issue number", fp.Fingerprint)
	}
	if fb.PRNumber == 0 {
		return FollowUpResult{}, fmt.Errorf("fingerprint %s missing PR number", fp.Fingerprint)
	}
	if strings.TrimSpace(fb.HeadSHA) == "" {
		return FollowUpResult{}, fmt.Errorf("missing PR head sha for fingerprint %s", fp.Fingerprint)
	}

	leaseName := fp.Fingerprint
	if len(leaseName) > 16 {
		leaseName = leaseName[:16]
	}
	lease, err := a.opts.Workspace.Acquire(ctx, fmt.Sprintf("fix-update-%s", leaseName), fb.HeadSHA)
	if err != nil {
		return FollowUpResult{}, fmt.Errorf("acquire workspace: %w", err)
	}
	defer lease.Release(context.Background())

	branch := fmt.Sprintf("ai/fix/%s", shortBranch(fp.Fingerprint))
	if err := checkoutBranchFromOrigin(ctx, lease.Path, branch); err != nil {
		return FollowUpResult{}, err
	}

	if err := updateTodoForFeedback(lease.Path, fp, fb); err != nil {
		return FollowUpResult{}, fmt.Errorf("update todo: %w", err)
	}

	comment := buildFollowUpComment(fp, fb)
	if a.opts.DryRun {
		return FollowUpResult{CommentBody: comment, BranchName: branch}, nil
	}

	if err := commitAll(ctx, lease.Path, fmt.Sprintf("chore: follow up on PR #%d feedback", fb.PRNumber)); err != nil {
		return FollowUpResult{}, err
	}
	if err := pushBranch(ctx, lease.Path, branch); err != nil {
		return FollowUpResult{}, err
	}
	if err := a.opts.GitHub.CreateIssueComment(ctx, a.opts.Owner, a.opts.Repo, fb.PRNumber, comment); err != nil {
		return FollowUpResult{}, err
	}
	if err := a.opts.Store.RecordAudit(ctx, "fixagent.review_followup", fmt.Sprintf("pr/%d", fb.PRNumber), "success", ""); err != nil {
		return FollowUpResult{}, err
	}
	return FollowUpResult{CommentBody: comment, BranchName: branch}, nil
}

func buildPreparationComment(fp store.FingerprintRecord, occ []extract.Occurrence, path string, testSummary string) string {
	target := occ[0]
	return fmt.Sprintf("<!-- FTC:FIX_AGENT_START -->\n"+
		"FixAgent is preparing an automated stabilization patch for fingerprint `%s`.\n\n"+
		"- Workspace: `%s`\n"+
		"- Commit: %s\n"+
		"- Test: %s\n"+
		"- Last occurrence: [run %d](%s)\n"+
		"- Verification: %s\n"+
		"- Next Steps:\n"+
		"  1. Reproduce failure locally within the workspace.\n"+
		"  2. Craft a stabilization patch focused on the failing test.\n"+
		"  3. Run targeted suites and prepare a PR for review.\n\n"+
		"_This is an automated preparation comment emitted at %s._\n"+
		"<!-- FTC:FIX_AGENT_END -->",
		fp.Fingerprint,
		filepath.Base(path),
		shortSHA(target.HeadSHA),
		safe(target.TestName),
		target.RunID,
		target.RunURL,
		testSummary,
		time.Now().UTC().Format(time.RFC3339),
	)
}

func writeTodoFile(path string, fp store.FingerprintRecord, occ extract.Occurrence) error {
	content := fmt.Sprintf("# FixAgent TODO\n\n- Fingerprint: `%s`\n- Test: `%s`\n- Latest run: %s\n- Commit: %s\n\nDescribe the stabilization strategy here.\n",
		fp.Fingerprint, safe(occ.TestName), occ.RunURL, occ.HeadSHA)
	return os.WriteFile(filepath.Join(path, "FIX_AGENT_TODO.md"), []byte(content), 0o644)
}

func updateTodoForFeedback(path string, fp store.FingerprintRecord, fb PRFeedback) error {
	todoPath := filepath.Join(path, "FIX_AGENT_TODO.md")
	existing, _ := os.ReadFile(todoPath)
	content := string(existing)
	if strings.TrimSpace(content) == "" {
		content = "# FixAgent TODO\n\n"
	}
	content = strings.TrimRight(content, "\n") + "\n\n## Review Follow-up\n\n" + renderFeedbackChecklist(fb)
	return os.WriteFile(todoPath, []byte(content), 0o644)
}

func renderFeedbackChecklist(fb PRFeedback) string {
	var b strings.Builder
	now := time.Now().UTC().Format(time.RFC3339)
	b.WriteString(fmt.Sprintf("_Generated at %s_\n\n", now))
	if len(fb.ChangesRequested) > 0 {
		b.WriteString("### Changes requested\n\n")
		for _, r := range fb.ChangesRequested {
			who := r.User.Login
			if strings.TrimSpace(who) == "" {
				who = "unknown"
			}
			snippet := strings.TrimSpace(r.Body)
			if snippet == "" {
				snippet = "(no body)"
			}
			if len(snippet) > 240 {
				snippet = snippet[:240] + "…"
			}
			b.WriteString(fmt.Sprintf("- [ ] %s: %s\n", who, snippet))
		}
		b.WriteString("\n")
	}

	state := strings.TrimSpace(fb.CombinedStatus.State)
	if state != "" {
		b.WriteString("### CI status\n\n")
		b.WriteString(fmt.Sprintf("- Combined state: `%s`\n", state))
		for _, s := range fb.CombinedStatus.Statuses {
			st := strings.ToLower(strings.TrimSpace(s.State))
			if st != "failure" && st != "error" {
				continue
			}
			ctx := s.Context
			if strings.TrimSpace(ctx) == "" {
				ctx = "(unknown)"
			}
			desc := strings.TrimSpace(s.Description)
			if desc == "" {
				desc = "(no description)"
			}
			b.WriteString(fmt.Sprintf("- [ ] %s: %s\n", ctx, desc))
		}
		b.WriteString("\n")
	}

	if b.Len() == 0 {
		return "- [ ] No actionable feedback detected.\n"
	}
	return b.String()
}

func buildFollowUpComment(fp store.FingerprintRecord, fb PRFeedback) string {
	var b strings.Builder
	b.WriteString("<!-- FTC:REVIEW_RESPONSE_START -->\n")
	b.WriteString("FixAgent detected review feedback / CI signals and prepared a follow-up plan.\n\n")
	b.WriteString(fmt.Sprintf("- Fingerprint: `%s`\n", fp.Fingerprint))
	b.WriteString(fmt.Sprintf("- PR: #%d\n", fb.PRNumber))
	if strings.TrimSpace(fb.PRURL) != "" {
		b.WriteString(fmt.Sprintf("- URL: %s\n", fb.PRURL))
	}
	if strings.TrimSpace(fb.HeadSHA) != "" {
		b.WriteString(fmt.Sprintf("- Head: %s\n", shortSHA(fb.HeadSHA)))
	}
	if state := strings.TrimSpace(fb.CombinedStatus.State); state != "" {
		b.WriteString(fmt.Sprintf("- CI: `%s`\n", state))
	}
	if len(fb.ChangesRequested) > 0 {
		b.WriteString(fmt.Sprintf("- Changes requested: %d review(s)\n", len(fb.ChangesRequested)))
	}
	b.WriteString("\nA checklist has been appended to `FIX_AGENT_TODO.md` in the FixAgent worktree.\n")
	b.WriteString(fmt.Sprintf("_Emitted at %s._\n", time.Now().UTC().Format(time.RFC3339)))
	b.WriteString("<!-- FTC:REVIEW_RESPONSE_END -->")
	return b.String()
}

func runGoTest(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func createBranch(ctx context.Context, dir, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", "-B", branch)
	cmd.Dir = dir
	return cmd.Run()
}

func commitAll(ctx context.Context, dir, message string) error {
	cmd := exec.CommandContext(ctx, "git", "add", ".")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return err
	}
	cmd = exec.CommandContext(ctx, "git", "commit", "--allow-empty", "-m", message)
	cmd.Dir = dir
	return cmd.Run()
}

func pushBranch(ctx context.Context, dir, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "push", "--set-upstream", "origin", branch)
	cmd.Dir = dir
	return cmd.Run()
}

func checkoutBranchFromOrigin(ctx context.Context, dir, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin", branch)
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.CommandContext(ctx, "git", "checkout", "-B", branch, fmt.Sprintf("origin/%s", branch))
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		return nil
	}
	cmd = exec.CommandContext(ctx, "git", "checkout", "-B", branch)
	cmd.Dir = dir
	return cmd.Run()
}

func shortBranch(fp string) string {
	if len(fp) <= 12 {
		return fp
	}
	return fp[:12]
}

func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}

func safe(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

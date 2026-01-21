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
	Owner     string
	Repo      string
	DryRun    bool
	GitHub    *github.Client
	Workspace *workspace.Manager
	Store     store.Store
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
	return &Agent{opts: opts}, nil
}

type AttemptResult struct {
	CommentBody string
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

	body := buildPreparationComment(fp, occ, lease.Path, testSummary)
	if a.opts.DryRun {
		return AttemptResult{CommentBody: body}, nil
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
	return AttemptResult{CommentBody: body}, nil
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

func runGoTest(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	out, err := cmd.CombinedOutput()
	return string(out), err
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

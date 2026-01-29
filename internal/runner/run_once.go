package runner

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/classify"
	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/copilotsdk"
	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/fixagent"
	"github.com/okJiang/flaky-test-cleaner/internal/github"
	"github.com/okJiang/flaky-test-cleaner/internal/issueagent"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
	"github.com/okJiang/flaky-test-cleaner/internal/workspace"
)

func firstLine(s string) string {
	line := strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if len(line) > 160 {
		return line[:160] + "..."
	}
	return line
}

func countLines(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return len(strings.Split(strings.TrimRight(s, "\n"), "\n"))
}

func isFTCManagedBody(body string) bool {
	return strings.Contains(body, "<!-- FTC:")
}

type RunOnceDeps struct {
	Store store.Store

	// GitHub clients can be injected for tests to avoid binding local ports.
	GitHubRead  *github.Client
	GitHubIssue *github.Client
}

func RunOnce(ctx context.Context, cfg config.Config) error {
	return RunOnceWithDeps(ctx, cfg, RunOnceDeps{})
}

func RunOnceWithDeps(ctx context.Context, cfg config.Config, deps RunOnceDeps) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	rt, cleanup, err := newRuntime(ctx, cfg, deps)
	if err != nil {
		return err
	}
	defer func() { _ = cleanup() }()

	if err := rt.DiscoveryOnce(ctx); err != nil {
		return err
	}
	return rt.InteractionOnce(ctx)
}

func needsFixAgent(ctx context.Context, st store.Store) (bool, error) {
	states := []store.FingerprintState{
		store.StateApprovedToFix,
		store.StatePROpen,
		store.StatePRNeedsChanges,
		store.StatePRUpdating,
	}
	for _, state := range states {
		fps, err := st.ListFingerprintsByState(ctx, state, 1)
		if err != nil {
			return false, err
		}
		if len(fps) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func shouldRunInitialAnalysis(state store.FingerprintState) bool {
	return state == store.StateDiscovered
}

func runInitialAnalysis(
	ctx context.Context,
	cfg config.Config,
	agent *issueagent.Agent,
	copilotClient *copilotsdk.Client,
	gh *github.Client,
	st store.Store,
	issueNumber int,
	fingerprint string,
	fpRec store.FingerprintRecord,
	occ []extract.Occurrence,
	classification classify.Result,
) error {
	var repoContext string
	// Avoid network access in tests by only attempting repo-context fetching when using the default GitHub API.
	if cfg.GitHubAPIBaseURL == "https://api.github.com" {
		repoWS, err := workspace.NewManager(workspace.Options{
			RemoteURL:    cfg.RepoRemoteURL(),
			MirrorDir:    cfg.WorkspaceMirrorDir + ".src",
			WorktreesDir: cfg.WorkspaceWorktreesDir + "/src",
			MaxWorktrees: 0,
		})
		if err == nil {
			repoContext = buildIssueAgentRepoContext(ctx, repoWS, occ)
		}
	}

	input := issueagent.Input{
		Fingerprint:         fpRec,
		Occurrences:         occ,
		Classification:      classification,
		RepoContextSnippets: repoContext,
	}
	comment := agent.BuildInitialComment(input)
	body := comment.Body

	if copilotClient != nil {
		systemMsg := issueagent.BuildCopilotSystemMessage()
		prompt := issueagent.BuildCopilotPromptWithRepoContext(fpRec, occ, classification, repoContext)
		if out, err := copilotClient.GenerateIssueAgentComment(ctx, systemMsg, prompt); err == nil && issueagent.IsValidIssueAgentBlock(out) {
			body = out
		} else if err != nil {
			_ = st.RecordAudit(ctx, "copilot_sdk.issueagent", fmt.Sprintf("issue/%d", issueNumber), "error", err.Error())
		}
	}

	if cfg.DryRun {
		log.Printf("dry-run issueagent comment issue=%d fingerprint=%s\n%s", issueNumber, fingerprint, body)
		return nil
	}
	if err := gh.CreateIssueComment(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, issueNumber, body); err != nil {
		_ = st.RecordAudit(ctx, "issueagent.initial_analysis", fmt.Sprintf("issue/%d", issueNumber), "error", err.Error())
		return err
	}
	if err := st.UpdateFingerprintState(ctx, fingerprint, store.StateTriaged); err != nil {
		return err
	}
	if err := st.UpdateFingerprintState(ctx, fingerprint, store.StateWaitingForSignal); err != nil {
		return err
	}
	return st.RecordAudit(ctx, "issueagent.initial_analysis", fmt.Sprintf("issue/%d", issueNumber), "success", "")
}

func checkApprovalSignals(ctx context.Context, cfg config.Config, gh *github.Client, st store.Store) error {
	const batchSize = 20
	fps, err := st.ListFingerprintsByState(ctx, store.StateWaitingForSignal, batchSize)
	if err != nil {
		return err
	}
	for _, fp := range fps {
		if fp.IssueNumber == 0 {
			continue
		}
		issue, err := gh.GetIssue(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, fp.IssueNumber)
		if err != nil {
			return err
		}
		approved := false
		reason := ""
		for _, lbl := range issue.Labels {
			if strings.EqualFold(lbl.Name, "flaky-test-cleaner/ai-fix-approved") {
				approved = true
				reason = "label flaky-test-cleaner/ai-fix-approved present"
				break
			}
		}
		comments, err := gh.ListIssueComments(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, fp.IssueNumber, github.ListIssueCommentsOptions{
			PerPage: 50,
		})
		if err != nil {
			return err
		}
		var maxCommentID int64
		var hasNewHumanComment bool
		for _, comment := range comments {
			if comment.ID > maxCommentID {
				maxCommentID = comment.ID
			}
			bodyLower := strings.ToLower(comment.Body)
			if strings.Contains(bodyLower, "/ai-fix") {
				approved = true
				reason = fmt.Sprintf("comment by %s triggered /ai-fix", comment.User.Login)
			}
			if comment.ID <= fp.LastIssueCommentID {
				continue
			}
			if isFTCManagedBody(comment.Body) {
				continue
			}
			if strings.Contains(bodyLower, "/ai-fix") {
				continue
			}
			hasNewHumanComment = true
		}
		if maxCommentID > fp.LastIssueCommentID {
			fpUpdate := fp
			fpUpdate.LastIssueCommentID = maxCommentID
			if err := st.UpsertFingerprint(ctx, fpUpdate); err != nil {
				return err
			}
			if hasNewHumanComment {
				_ = st.RecordAudit(ctx, "signal.issue_comment", fmt.Sprintf("issue/%d", fp.IssueNumber), "success", "")
			}
		}
		if !approved {
			continue
		}
		log.Printf("approval detected for issue %d (fingerprint %s): %s", fp.IssueNumber, fp.Fingerprint, reason)
		if err := st.UpdateFingerprintState(ctx, fp.Fingerprint, store.StateApprovedToFix); err != nil {
			return err
		}
		if err := st.RecordAudit(ctx, "signal.approval", fmt.Sprintf("issue/%d", fp.IssueNumber), "success", reason); err != nil {
			return err
		}
	}
	return nil
}

func runFixAgent(ctx context.Context, agent *fixagent.Agent, st store.Store, gh *github.Client) error {
	const batchSize = 5
	fps, err := st.ListFingerprintsByState(ctx, store.StateApprovedToFix, batchSize)
	if err != nil {
		return err
	}
	for _, fp := range fps {
		occ, err := st.ListRecentOccurrences(ctx, fp.Fingerprint, 1)
		if err != nil {
			return err
		}
		if len(occ) == 0 {
			continue
		}
		res, err := agent.Attempt(ctx, fp, occ)
		if err != nil {
			return err
		}
		if res.CommentBody != "" {
			log.Printf("fixagent prepared fingerprint %s issue #%d", fp.Fingerprint, fp.IssueNumber)
		}
	}
	return nil
}

func checkPRStatus(ctx context.Context, cfg config.Config, gh *github.Client, st store.Store) error {
	states := []store.FingerprintState{
		store.StatePROpen,
		store.StatePRNeedsChanges,
		store.StatePRUpdating,
	}
	for _, state := range states {
		fps, err := st.ListFingerprintsByState(ctx, state, 10)
		if err != nil {
			return err
		}
		for _, fp := range fps {
			if fp.PRNumber == 0 {
				continue
			}
			pr, err := gh.GetPullRequest(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, fp.PRNumber)
			if err != nil {
				return err
			}
			if isMerged(pr) {
				if err := finalizeMergedPR(ctx, cfg, gh, st, fp, pr); err != nil {
					return err
				}
				continue
			}
			if pr.State == "closed" {
				if err := handleClosedPR(ctx, cfg, gh, st, fp, pr); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func isMerged(pr github.PullRequest) bool {
	if pr.Merged {
		return true
	}
	return pr.MergedAt != nil && !pr.MergedAt.IsZero()
}

func finalizeMergedPR(ctx context.Context, cfg config.Config, gh *github.Client, st store.Store, fp store.FingerprintRecord, pr github.PullRequest) error {
	comment := fmt.Sprintf("PR #%d has been merged. Closing this issue and marking the fingerprint as resolved.", pr.Number)
	if err := gh.CreateIssueComment(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, fp.IssueNumber, comment); err != nil {
		return err
	}
	stateClosed := "closed"
	if _, err := gh.UpdateIssue(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, fp.IssueNumber, github.UpdateIssueInput{State: &stateClosed}); err != nil {
		return err
	}
	if err := st.UpdateFingerprintState(ctx, fp.Fingerprint, store.StateMerged); err != nil {
		return err
	}
	return st.RecordAudit(ctx, "fixagent.pr_merged", fmt.Sprintf("issue/%d", fp.IssueNumber), "success", fmt.Sprintf("pr#%d", pr.Number))
}

func handleClosedPR(ctx context.Context, cfg config.Config, gh *github.Client, st store.Store, fp store.FingerprintRecord, pr github.PullRequest) error {
	comment := fmt.Sprintf("PR #%d was closed without merge. Marking this fingerprint as CLOSED_WONTFIX.", pr.Number)
	if err := gh.CreateIssueComment(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, fp.IssueNumber, comment); err != nil {
		return err
	}
	if err := st.UpdateFingerprintState(ctx, fp.Fingerprint, store.StateClosedWontFix); err != nil {
		return err
	}
	return st.RecordAudit(ctx, "fixagent.pr_closed", fmt.Sprintf("issue/%d", fp.IssueNumber), "success", fmt.Sprintf("pr#%d", pr.Number))
}

func handlePRFeedbackLoop(ctx context.Context, cfg config.Config, gh *github.Client, st store.Store, agent *fixagent.Agent) error {
	if cfg.DryRun {
		return nil
	}

	// 1) Detect feedback signals on PR_OPEN and move to PR_NEEDS_CHANGES.
	fps, err := st.ListFingerprintsByState(ctx, store.StatePROpen, 10)
	if err != nil {
		return err
	}
	for _, fp := range fps {
		if fp.PRNumber == 0 {
			continue
		}
		fb, err := buildPRFeedback(ctx, cfg, gh, fp.PRNumber, fp.LastPRCommentID)
		if err != nil {
			return err
		}
		if !fb.NeedsUpdate() {
			continue
		}
		log.Printf("pr feedback detected for pr#%d fingerprint=%s", fp.PRNumber, fp.Fingerprint)
		if err := st.UpdateFingerprintState(ctx, fp.Fingerprint, store.StatePRNeedsChanges); err != nil {
			return err
		}
		_ = st.RecordAudit(ctx, "signal.pr_feedback", fmt.Sprintf("pr/%d", fp.PRNumber), "success", "")
	}

	// 2) For PR_NEEDS_CHANGES, run FixAgent follow-up and cycle PR_UPDATING -> PR_OPEN.
	fps, err = st.ListFingerprintsByState(ctx, store.StatePRNeedsChanges, 10)
	if err != nil {
		return err
	}
	for _, fp := range fps {
		if fp.PRNumber == 0 {
			continue
		}
		fb, err := buildPRFeedback(ctx, cfg, gh, fp.PRNumber, fp.LastPRCommentID)
		if err != nil {
			return err
		}
		if err := st.UpdateFingerprintState(ctx, fp.Fingerprint, store.StatePRUpdating); err != nil {
			return err
		}
		if _, err := agent.FollowUp(ctx, fp, fb); err != nil {
			_ = st.RecordAudit(ctx, "fixagent.review_followup", fmt.Sprintf("pr/%d", fp.PRNumber), "error", err.Error())
			return err
		}
		if fb.LatestIssueCommentID > fp.LastPRCommentID {
			fpUpdate := fp
			fpUpdate.LastPRCommentID = fb.LatestIssueCommentID
			if err := st.UpsertFingerprint(ctx, fpUpdate); err != nil {
				return err
			}
		}
		if err := st.UpdateFingerprintState(ctx, fp.Fingerprint, store.StatePROpen); err != nil {
			return err
		}
		_ = st.RecordAudit(ctx, "fixagent.review_followup", fmt.Sprintf("pr/%d", fp.PRNumber), "success", "")
	}
	return nil
}

func buildPRFeedback(ctx context.Context, cfg config.Config, gh *github.Client, prNumber int, sinceCommentID int64) (fixagent.PRFeedback, error) {
	pr, err := gh.GetPullRequest(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, prNumber)
	if err != nil {
		return fixagent.PRFeedback{}, err
	}
	reviews, err := gh.ListPullRequestReviews(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, prNumber)
	if err != nil {
		return fixagent.PRFeedback{}, err
	}
	var changesRequested []github.PullRequestReview
	for _, r := range reviews {
		if strings.EqualFold(strings.TrimSpace(r.State), "CHANGES_REQUESTED") {
			changesRequested = append(changesRequested, r)
		}
	}
	status := github.CombinedStatus{}
	if strings.TrimSpace(pr.Head.SHA) != "" {
		st, err := gh.GetCombinedStatus(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, pr.Head.SHA)
		if err != nil {
			return fixagent.PRFeedback{}, err
		}
		status = st
	}

	issueComments, err := gh.ListIssueComments(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, prNumber, github.ListIssueCommentsOptions{
		PerPage: 50,
	})
	if err != nil {
		return fixagent.PRFeedback{}, err
	}
	var latestCommentID int64
	var newComments []github.IssueComment
	for _, c := range issueComments {
		if c.ID > latestCommentID {
			latestCommentID = c.ID
		}
		if c.ID <= sinceCommentID {
			continue
		}
		if isFTCManagedBody(c.Body) {
			continue
		}
		newComments = append(newComments, c)
	}
	return fixagent.PRFeedback{
		PRNumber:             pr.Number,
		PRURL:                pr.HTMLURL,
		HeadSHA:              pr.Head.SHA,
		ChangesRequested:     changesRequested,
		CombinedStatus:       status,
		LatestIssueCommentID: latestCommentID,
		NewIssueComments:     newComments,
	}, nil
}

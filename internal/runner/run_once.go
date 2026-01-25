package runner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/classify"
	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/copilotsdk"
	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/fingerprint"
	"github.com/okJiang/flaky-test-cleaner/internal/fixagent"
	"github.com/okJiang/flaky-test-cleaner/internal/github"
	"github.com/okJiang/flaky-test-cleaner/internal/issue"
	"github.com/okJiang/flaky-test-cleaner/internal/issueagent"
	"github.com/okJiang/flaky-test-cleaner/internal/sanitize"
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

type RunOnceDeps struct {
	Store store.Store
}

func RunOnce(ctx context.Context, cfg config.Config) error {
	return RunOnceWithDeps(ctx, cfg, RunOnceDeps{})
}

func RunOnceWithDeps(ctx context.Context, cfg config.Config, deps RunOnceDeps) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	// Backward-compatible default: if write repo is not set (e.g. tests constructing Config directly),
	// fall back to the source repo.
	if strings.TrimSpace(cfg.GitHubWriteOwner) == "" {
		cfg.GitHubWriteOwner = cfg.GitHubOwner
	}
	if strings.TrimSpace(cfg.GitHubWriteRepo) == "" {
		cfg.GitHubWriteRepo = cfg.GitHubRepo
	}

	ghRead := github.NewClientWithBaseURL(cfg.GitHubReadToken, cfg.RequestTimeout, cfg.GitHubAPIBaseURL)
	ghIssue := ghRead
	if !cfg.DryRun {
		ghIssue = github.NewClientWithBaseURL(cfg.GitHubIssueToken, cfg.RequestTimeout, cfg.GitHubAPIBaseURL)
	}

	st := deps.Store
	closeStore := func() error { return nil }
	if st == nil {
		st = store.NewMemory()
		if cfg.TiDBEnabled {
			tidb, err := store.NewTiDBStore(cfg)
			if err != nil {
				return err
			}
			st = tidb
			closeStore = tidb.Close
		}
		defer func() { _ = closeStore() }()
	}

	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	wf, err := ghRead.FindWorkflowByName(ctx, cfg.GitHubOwner, cfg.GitHubRepo, cfg.WorkflowName)
	if err != nil {
		return err
	}

	runs, err := ghRead.ListWorkflowRuns(ctx, cfg.GitHubOwner, cfg.GitHubRepo, wf.ID, github.ListWorkflowRunsOptions{
		Status:  "failure",
		Branch:  cfg.GitHubBaseBranch,
		Event:   "push",
		PerPage: cfg.MaxRuns,
	})
	if err != nil {
		return err
	}

	extractor := extract.NewGoTestExtractor()
	classifier := classify.NewHeuristic(cfg.ConfidenceThreshold)
	issueMgr := issue.NewManager(issue.Options{
		Owner:  cfg.GitHubWriteOwner,
		Repo:   cfg.GitHubWriteRepo,
		DryRun: cfg.DryRun,
	})
	analysisAgent := issueagent.New()
	copilotClient := copilotsdk.New(copilotsdk.Options{
		Enabled:  cfg.CopilotSDKEnabled,
		Model:    cfg.CopilotModel,
		Timeout:  cfg.CopilotTimeout,
		LogLevel: cfg.CopilotLogLevel,
	})
	if err := copilotClient.Start(); err != nil {
		log.Printf("copilot sdk start failed; falling back to heuristic issueagent: %v", err)
		copilotClient = nil
	} else {
		defer copilotClient.Stop()
	}

	for _, run := range runs {
		jobs, err := ghRead.ListRunJobs(ctx, cfg.GitHubOwner, cfg.GitHubRepo, run.ID, github.ListRunJobsOptions{PerPage: cfg.MaxJobs})
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.Conclusion != "failure" {
				continue
			}
			log.Printf("scanning run=%d job=%d %q", run.ID, job.ID, job.Name)
			raw, err := ghRead.DownloadJobLogs(ctx, cfg.GitHubOwner, cfg.GitHubRepo, job.ID)
			if err != nil {
				return err
			}
			failures := extractor.Extract(extract.Input{
				Repo:       cfg.GitHubOwner + "/" + cfg.GitHubRepo,
				Workflow:   wf.Name,
				RunID:      run.ID,
				RunURL:     run.HTMLURL,
				HeadSHA:    run.HeadSHA,
				JobID:      job.ID,
				JobName:    job.Name,
				RunnerOS:   job.RunnerOS,
				OccurredAt: time.Now(),
				RawLogText: string(raw),
			})
			if len(failures) == 0 {
				continue
			}

			for _, occ := range failures {
				occ.Excerpt = sanitize.Scrub(occ.Excerpt)
				occ.ErrorSignature = fingerprint.NormalizeErrorSignature(occ.ErrorSignature)
				fp := fingerprint.V1(fingerprint.V1Input{
					Repo:         cfg.GitHubOwner + "/" + cfg.GitHubRepo,
					Framework:    occ.Framework,
					TestName:     occ.TestName,
					ErrorSigNorm: occ.ErrorSignature,
					Platform:     occ.PlatformBucket(),
				})
				occ.Fingerprint = fp

				if err := st.UpsertOccurrence(ctx, occ); err != nil {
					return err
				}

				c, err := classifier.Classify(ctx, st, occ)
				if err != nil {
					return err
				}

				if err := st.UpsertFingerprint(ctx, store.FingerprintRecord{
					Fingerprint:    fp,
					Repo:           cfg.GitHubOwner + "/" + cfg.GitHubRepo,
					TestName:       occ.TestName,
					Framework:      occ.Framework,
					Class:          string(c.Class),
					Confidence:     c.Confidence,
					State:          store.StateDiscovered,
					StateChangedAt: occ.OccurredAt,
					FirstSeenAt:    occ.OccurredAt,
					LastSeenAt:     occ.OccurredAt,
				}); err != nil {
					return err
				}

				if c.Class == classify.ClassInfraFlake || c.Class == classify.ClassLikelyRegression {
					continue
				}

				fpRec, err := st.GetFingerprint(ctx, fp)
				if err != nil {
					return err
				}
				if fpRec == nil {
					return errors.New("fingerprint record missing after upsert")
				}

				recent, err := st.ListRecentOccurrences(ctx, fp, 5)
				if err != nil {
					return err
				}

				change, err := issueMgr.PlanIssueUpdate(issue.PlanInput{
					Fingerprint:    *fpRec,
					Occurrences:    recent,
					Classification: c,
				})
				if err != nil {
					return err
				}

				if change.Noop {
					continue
				}

				if cfg.DryRun {
					log.Printf("dry-run issue update fingerprint=%s class=%s confidence=%.2f title=%q labels=%v", fp, c.Class, c.Confidence, change.Title, change.Labels)
					for i, o := range recent {
						if i >= 2 {
							break
						}
						log.Printf("dry-run evidence[%d] run=%d url=%s job=%q sha=%s test=%q sig=%q excerpt_lines=%d", i, o.RunID, o.RunURL, o.JobName, o.HeadSHA, o.TestName, firstLine(o.ErrorSignature), countLines(o.Excerpt))
					}
				}

				issueNumber, err := issueMgr.Apply(ctx, ghIssue, change)
				if err != nil {
					return err
				}
				if issueNumber != 0 {
					if err := st.LinkIssue(ctx, fp, issueNumber); err != nil {
						return err
					}
					fpRecUpdated, err := st.GetFingerprint(ctx, fp)
					if err != nil {
						return err
					}
					if fpRecUpdated == nil {
						return errors.New("fingerprint record missing after linking issue")
					}
					if shouldRunInitialAnalysis(fpRecUpdated.State) {
						if err := st.UpdateFingerprintState(ctx, fp, store.StateIssueOpen); err != nil {
							return err
						}
						if err := runInitialAnalysis(ctx, cfg, analysisAgent, copilotClient, ghIssue, st, issueNumber, fp, *fpRecUpdated, recent, c); err != nil {
							return err
						}
					}
				}
			}
		}
	}

	if err := checkApprovalSignals(ctx, cfg, ghIssue, st); err != nil {
		return err
	}

	if !cfg.DryRun {
		needsFix, err := needsFixAgent(ctx, st)
		if err != nil {
			return err
		}
		var fx *fixagent.Agent
		if needsFix {
			wsManager, err := workspace.NewManager(workspace.Options{
				RemoteURL:    cfg.WriteRepoRemoteURL(),
				MirrorDir:    cfg.WorkspaceMirrorDir,
				WorktreesDir: cfg.WorkspaceWorktreesDir,
				MaxWorktrees: cfg.WorkspaceMaxWorktrees,
			})
			if err != nil {
				return err
			}
			if err := wsManager.Ensure(ctx); err != nil {
				return err
			}
			fx, err = fixagent.New(fixagent.Options{
				Owner:      cfg.GitHubWriteOwner,
				Repo:       cfg.GitHubWriteRepo,
				DryRun:     cfg.DryRun,
				GitHub:     ghIssue,
				Workspace:  wsManager,
				Store:      st,
				BaseBranch: cfg.GitHubBaseBranch,
			})
			if err != nil {
				return err
			}
		}
		if fx != nil {
			if err := runFixAgent(ctx, fx, st, ghIssue); err != nil {
				return err
			}
			if err := handlePRFeedbackLoop(ctx, cfg, ghIssue, st, fx); err != nil {
				return err
			}
		}
		if err := checkPRStatus(ctx, cfg, ghIssue, st); err != nil {
			return err
		}
	}

	return nil
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
	input := issueagent.Input{
		Fingerprint:    fpRec,
		Occurrences:    occ,
		Classification: classification,
	}
	comment := agent.BuildInitialComment(input)
	body := comment.Body

	if cfg.CopilotSDKEnabled && copilotClient != nil {
		systemMsg := issueagent.BuildCopilotSystemMessage()
		prompt := issueagent.BuildCopilotPrompt(fpRec, occ, classification)
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
		approved, reason, err := issueHasApproval(ctx, cfg, gh, fp.IssueNumber)
		if err != nil {
			return err
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

func issueHasApproval(ctx context.Context, cfg config.Config, gh *github.Client, issueNumber int) (bool, string, error) {
	issue, err := gh.GetIssue(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, issueNumber)
	if err != nil {
		return false, "", err
	}
	for _, lbl := range issue.Labels {
		if strings.EqualFold(lbl.Name, "flaky-test-cleaner/ai-fix-approved") {
			return true, "label flaky-test-cleaner/ai-fix-approved present", nil
		}
	}
	comments, err := gh.ListIssueComments(ctx, cfg.GitHubWriteOwner, cfg.GitHubWriteRepo, issueNumber, github.ListIssueCommentsOptions{
		PerPage: 50,
	})
	if err != nil {
		return false, "", err
	}
	for _, comment := range comments {
		if strings.Contains(strings.ToLower(comment.Body), "/ai-fix") {
			return true, fmt.Sprintf("comment by %s triggered /ai-fix", comment.User.Login), nil
		}
	}
	return false, "", nil
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
		fb, err := buildPRFeedback(ctx, cfg, gh, fp.PRNumber)
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
		fb, err := buildPRFeedback(ctx, cfg, gh, fp.PRNumber)
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
		if err := st.UpdateFingerprintState(ctx, fp.Fingerprint, store.StatePROpen); err != nil {
			return err
		}
		_ = st.RecordAudit(ctx, "fixagent.review_followup", fmt.Sprintf("pr/%d", fp.PRNumber), "success", "")
	}
	return nil
}

func buildPRFeedback(ctx context.Context, cfg config.Config, gh *github.Client, prNumber int) (fixagent.PRFeedback, error) {
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
	return fixagent.PRFeedback{
		PRNumber:         pr.Number,
		PRURL:            pr.HTMLURL,
		HeadSHA:          pr.Head.SHA,
		ChangesRequested: changesRequested,
		CombinedStatus:   status,
	}, nil
}

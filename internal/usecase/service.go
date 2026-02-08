package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	githubadapter "github.com/okJiang/flaky-test-cleaner/internal/adapters/github"
	storeadapter "github.com/okJiang/flaky-test-cleaner/internal/adapters/store"
	"github.com/okJiang/flaky-test-cleaner/internal/classify"
	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/copilotsdk"
	"github.com/okJiang/flaky-test-cleaner/internal/domain"
	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/fingerprint"
	"github.com/okJiang/flaky-test-cleaner/internal/fixagent"
	"github.com/okJiang/flaky-test-cleaner/internal/issue"
	"github.com/okJiang/flaky-test-cleaner/internal/issueagent"
	"github.com/okJiang/flaky-test-cleaner/internal/ports"
	"github.com/okJiang/flaky-test-cleaner/internal/sanitize"
	"github.com/okJiang/flaky-test-cleaner/internal/workspace"
)

type ServiceDeps struct {
	Store       ports.Store
	GitHubRead  *githubadapter.Client
	GitHubIssue *githubadapter.Client
}

type Service struct {
	cfg config.Config

	ghRead  *githubadapter.Client
	ghIssue *githubadapter.Client
	store   ports.Store

	extractor     *extract.GoTestExtractor
	classifier    *classify.Heuristic
	issueMgr      *issue.Manager
	analysisAgent *issueagent.Agent
	copilotClient *copilotsdk.Client

	wsWrite *workspace.Manager
}

func NewService(ctx context.Context, cfg config.Config, deps ServiceDeps) (*Service, func() error, error) {
	ghRead := deps.GitHubRead
	ghIssue := deps.GitHubIssue
	if ghRead == nil {
		ghRead = githubadapter.NewClientWithBaseURL(cfg.GitHubReadToken, cfg.RequestTimeout, cfg.GitHubAPIBaseURL)
	}
	if ghIssue == nil {
		ghIssue = ghRead
		if !cfg.DryRun {
			ghIssue = githubadapter.NewClientWithBaseURL(cfg.GitHubIssueToken, cfg.RequestTimeout, cfg.GitHubAPIBaseURL)
		}
	}

	st := deps.Store
	closeStore := func() error { return nil }
	if st == nil {
		st = storeadapter.NewMemory()
		if cfg.TiDBEnabled {
			tidb, err := storeadapter.NewTiDBStore(cfg)
			if err != nil {
				return nil, nil, err
			}
			st = tidb
			closeStore = tidb.Close
		}
	}
	if err := st.Migrate(ctx); err != nil {
		_ = closeStore()
		return nil, nil, fmt.Errorf("migrate: %w", err)
	}

	issueMgr := issue.NewManager(issue.Options{Owner: cfg.GitHubWriteOwner, Repo: cfg.GitHubWriteRepo, DryRun: cfg.DryRun})
	analysisAgent := issueagent.New()

	copilotEnabled := strings.TrimSpace(cfg.CopilotModel) != ""
	copilotClient := copilotsdk.New(copilotsdk.Options{
		Enabled:  copilotEnabled,
		Model:    cfg.CopilotModel,
		Timeout:  cfg.CopilotTimeout,
		LogLevel: cfg.CopilotLogLevel,
	})
	if copilotEnabled {
		if err := copilotClient.Start(); err != nil {
			log.Printf("copilot sdk start failed; falling back to heuristic issueagent: %v", err)
			copilotClient = nil
		}
	} else {
		copilotClient = nil
	}

	cleanup := func() error {
		if copilotClient != nil {
			copilotClient.Stop()
		}
		return closeStore()
	}

	return &Service{
		cfg:           cfg,
		ghRead:        ghRead,
		ghIssue:       ghIssue,
		store:         st,
		extractor:     extract.NewGoTestExtractor(),
		classifier:    classify.NewHeuristic(cfg.ConfidenceThreshold),
		issueMgr:      issueMgr,
		analysisAgent: analysisAgent,
		copilotClient: copilotClient,
	}, cleanup, nil
}

func (s *Service) DiscoveryOnce(ctx context.Context) error {
	wf, err := s.ghRead.FindWorkflowByName(ctx, s.cfg.GitHubOwner, s.cfg.GitHubRepo, s.cfg.WorkflowName)
	if err != nil {
		return err
	}

	runs, err := s.ghRead.ListWorkflowRuns(ctx, s.cfg.GitHubOwner, s.cfg.GitHubRepo, wf.ID, domain.ListWorkflowRunsOptions{
		Status:  "failure",
		Branch:  s.cfg.GitHubBaseBranch,
		PerPage: s.cfg.MaxRuns,
	})
	if err != nil {
		return err
	}

	for _, run := range runs {
		jobs, err := s.ghRead.ListRunJobs(ctx, s.cfg.GitHubOwner, s.cfg.GitHubRepo, run.ID, domain.ListRunJobsOptions{PerPage: s.cfg.MaxJobs})
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.Conclusion != "failure" {
				continue
			}
			log.Printf("scanning run=%d job=%d %q", run.ID, job.ID, job.Name)
			raw, err := s.ghRead.DownloadJobLogs(ctx, s.cfg.GitHubOwner, s.cfg.GitHubRepo, job.ID)
			if err != nil {
				return err
			}

			failures := s.extractor.Extract(extract.Input{
				Repo:       s.cfg.GitHubOwner + "/" + s.cfg.GitHubRepo,
				Workflow:   wf.Name,
				RunID:      run.ID,
				RunURL:     run.HTMLURL,
				HeadSHA:    run.HeadSHA,
				JobID:      job.ID,
				JobName:    job.Name,
				RunnerOS:   job.RunnerOS,
				OccurredAt: run.CreatedAt,
				RawLogText: string(raw),
			})
			if len(failures) == 0 {
				continue
			}

			for _, occ := range failures {
				occ.Excerpt = sanitize.Scrub(occ.Excerpt)
				normSig := fingerprint.NormalizeErrorSignature(occ.ErrorSignature)
				fp := fingerprint.V1(fingerprint.V1Input{
					Repo:         s.cfg.GitHubOwner + "/" + s.cfg.GitHubRepo,
					Framework:    occ.Framework,
					TestName:     occ.TestName,
					ErrorSigNorm: normSig,
					Platform:     occ.PlatformBucket(),
				})
				occ.Fingerprint = fp

				if err := s.store.UpsertOccurrence(ctx, occ); err != nil {
					return err
				}

				c, err := s.classifier.Classify(ctx, s.store, occ)
				if err != nil {
					return err
				}

				if err := s.store.UpsertFingerprint(ctx, domain.FingerprintRecord{
					Fingerprint:        fp,
					FingerprintVersion: fingerprint.VersionV1,
					Repo:               s.cfg.GitHubOwner + "/" + s.cfg.GitHubRepo,
					TestName:           occ.TestName,
					Framework:          occ.Framework,
					Class:              string(c.Class),
					Confidence:         c.Confidence,
					State:              domain.StateDiscovered,
					StateChangedAt:     occ.OccurredAt,
					FirstSeenAt:        occ.OccurredAt,
					LastSeenAt:         occ.OccurredAt,
				}); err != nil {
					return err
				}

				if c.Class == domain.ClassInfraFlake || c.Class == domain.ClassLikelyRegression {
					continue
				}

				fpRec, err := s.store.GetFingerprint(ctx, fp)
				if err != nil {
					return err
				}
				if fpRec == nil {
					return errors.New("fingerprint record missing after upsert")
				}

				recent, err := s.store.ListRecentOccurrences(ctx, fp, 5)
				if err != nil {
					return err
				}
				change, err := s.issueMgr.PlanIssueUpdate(issue.PlanInput{Fingerprint: *fpRec, Occurrences: recent, Classification: c})
				if err != nil {
					return err
				}
				if change.Noop {
					continue
				}

				if s.cfg.DryRun {
					log.Printf("dry-run issue update fingerprint=%s class=%s confidence=%.2f title=%q labels=%v", fp, c.Class, c.Confidence, change.Title, change.Labels)
					for i, o := range recent {
						if i >= 2 {
							break
						}
						log.Printf("dry-run evidence[%d] run=%d url=%s job=%q sha=%s test=%q sig=%q excerpt_lines=%d", i, o.RunID, o.RunURL, o.JobName, o.HeadSHA, o.TestName, firstLine(o.ErrorSignature), countLines(o.Excerpt))
					}
				}

				issueNumber, err := s.issueMgr.Apply(ctx, s.ghIssue, change)
				if err != nil {
					return err
				}
				if issueNumber == 0 {
					continue
				}

				if err := s.store.LinkIssue(ctx, fp, issueNumber); err != nil {
					return err
				}
				fpRecUpdated, err := s.store.GetFingerprint(ctx, fp)
				if err != nil {
					return err
				}
				if fpRecUpdated == nil {
					return errors.New("fingerprint record missing after linking issue")
				}
				if shouldRunInitialAnalysis(fpRecUpdated.State) {
					if err := s.store.UpdateFingerprintState(ctx, fp, domain.StateIssueOpen); err != nil {
						return err
					}
					if err := s.runInitialAnalysis(ctx, issueNumber, fp, *fpRecUpdated, recent, c); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (s *Service) InteractionOnce(ctx context.Context) error {
	if err := s.checkApprovalSignals(ctx); err != nil {
		return err
	}
	if s.cfg.DryRun {
		return nil
	}

	needsFix, err := s.needsFixAgent(ctx)
	if err != nil {
		return err
	}
	if !needsFix {
		return nil
	}

	if err := s.ensureWriteWorkspace(ctx); err != nil {
		return err
	}
	fx, err := fixagent.New(fixagent.Options{
		Owner:      s.cfg.GitHubWriteOwner,
		Repo:       s.cfg.GitHubWriteRepo,
		DryRun:     s.cfg.DryRun,
		GitHub:     s.ghIssue,
		Workspace:  s.wsWrite,
		Store:      s.store,
		BaseBranch: s.cfg.GitHubBaseBranch,
	})
	if err != nil {
		return err
	}
	if err := s.runFixAgent(ctx, fx); err != nil {
		return err
	}
	if err := s.handlePRFeedbackLoop(ctx, fx); err != nil {
		return err
	}
	return s.checkPRStatus(ctx)
}

func (s *Service) ensureWriteWorkspace(ctx context.Context) error {
	if s.wsWrite == nil {
		ws, err := workspace.NewManager(workspace.Options{
			RemoteURL:    s.cfg.WriteRepoRemoteURL(),
			MirrorDir:    s.cfg.WorkspaceMirrorDir,
			WorktreesDir: s.cfg.WorkspaceWorktreesDir,
			MaxWorktrees: s.cfg.WorkspaceMaxWorktrees,
		})
		if err != nil {
			return err
		}
		s.wsWrite = ws
	}
	return s.wsWrite.Ensure(ctx)
}

func (s *Service) needsFixAgent(ctx context.Context) (bool, error) {
	states := []domain.FingerprintState{domain.StateApprovedToFix, domain.StatePROpen, domain.StatePRNeedsChanges, domain.StatePRUpdating}
	for _, state := range states {
		fps, err := s.store.ListFingerprintsByState(ctx, state, 1)
		if err != nil {
			return false, err
		}
		if len(fps) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func shouldRunInitialAnalysis(state domain.FingerprintState) bool {
	return state == domain.StateDiscovered
}

func (s *Service) runInitialAnalysis(ctx context.Context, issueNumber int, fingerprintID string, fpRec domain.FingerprintRecord, occ []domain.Occurrence, classification domain.Classification) error {
	var repoContext string
	if s.cfg.GitHubAPIBaseURL == "https://api.github.com" {
		repoWS, err := workspace.NewManager(workspace.Options{
			RemoteURL:    s.cfg.RepoRemoteURL(),
			MirrorDir:    s.cfg.WorkspaceMirrorDir + ".src",
			WorktreesDir: s.cfg.WorkspaceWorktreesDir + "/src",
			MaxWorktrees: 0,
		})
		if err == nil {
			repoContext = buildIssueAgentRepoContext(ctx, repoWS, occ)
		}
	}

	input := issueagent.Input{Fingerprint: fpRec, Occurrences: occ, Classification: classification, RepoContextSnippets: repoContext}
	comment := s.analysisAgent.BuildInitialComment(input)
	body := comment.Body

	if s.copilotClient != nil {
		systemMsg := issueagent.BuildCopilotSystemMessage()
		prompt := issueagent.BuildCopilotPromptWithRepoContext(fpRec, occ, classification, repoContext)
		if out, err := s.copilotClient.GenerateIssueAgentComment(ctx, systemMsg, prompt); err == nil && issueagent.IsValidIssueAgentBlock(out) {
			body = out
		} else if err != nil {
			_ = s.store.RecordAudit(ctx, "copilot_sdk.issueagent", fmt.Sprintf("issue/%d", issueNumber), "error", err.Error())
		}
	}

	if s.cfg.DryRun {
		log.Printf("dry-run issueagent comment issue=%d fingerprint=%s\n%s", issueNumber, fingerprintID, body)
		return nil
	}
	if err := s.ghIssue.CreateIssueComment(ctx, s.cfg.GitHubWriteOwner, s.cfg.GitHubWriteRepo, issueNumber, body); err != nil {
		_ = s.store.RecordAudit(ctx, "issueagent.initial_analysis", fmt.Sprintf("issue/%d", issueNumber), "error", err.Error())
		return err
	}
	if err := s.store.UpdateFingerprintState(ctx, fingerprintID, domain.StateTriaged); err != nil {
		return err
	}
	if err := s.store.UpdateFingerprintState(ctx, fingerprintID, domain.StateWaitingForSignal); err != nil {
		return err
	}
	return s.store.RecordAudit(ctx, "issueagent.initial_analysis", fmt.Sprintf("issue/%d", issueNumber), "success", "")
}

func (s *Service) checkApprovalSignals(ctx context.Context) error {
	fps, err := s.store.ListFingerprintsByState(ctx, domain.StateWaitingForSignal, 20)
	if err != nil {
		return err
	}
	for _, fp := range fps {
		if fp.IssueNumber == 0 {
			continue
		}
		issue, err := s.ghIssue.GetIssue(ctx, s.cfg.GitHubWriteOwner, s.cfg.GitHubWriteRepo, fp.IssueNumber)
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
		comments, err := s.ghIssue.ListIssueComments(ctx, s.cfg.GitHubWriteOwner, s.cfg.GitHubWriteRepo, fp.IssueNumber, domain.ListIssueCommentsOptions{PerPage: 50})
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
			if isFTCManagedBody(comment.Body) || strings.Contains(bodyLower, "/ai-fix") {
				continue
			}
			hasNewHumanComment = true
		}
		if maxCommentID > fp.LastIssueCommentID {
			fpUpdate := fp
			fpUpdate.LastIssueCommentID = maxCommentID
			if err := s.store.UpsertFingerprint(ctx, fpUpdate); err != nil {
				return err
			}
			if hasNewHumanComment {
				_ = s.store.RecordAudit(ctx, "signal.issue_comment", fmt.Sprintf("issue/%d", fp.IssueNumber), "success", "")
			}
		}
		if !approved {
			continue
		}
		log.Printf("approval detected for issue %d (fingerprint %s): %s", fp.IssueNumber, fp.Fingerprint, reason)
		if err := s.store.UpdateFingerprintState(ctx, fp.Fingerprint, domain.StateApprovedToFix); err != nil {
			return err
		}
		if err := s.store.RecordAudit(ctx, "signal.approval", fmt.Sprintf("issue/%d", fp.IssueNumber), "success", reason); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) runFixAgent(ctx context.Context, agent *fixagent.Agent) error {
	fps, err := s.store.ListFingerprintsByState(ctx, domain.StateApprovedToFix, 5)
	if err != nil {
		return err
	}
	for _, fp := range fps {
		occ, err := s.store.ListRecentOccurrences(ctx, fp.Fingerprint, 1)
		if err != nil {
			return err
		}
		if len(occ) == 0 {
			continue
		}
		if _, err := agent.Attempt(ctx, fp, occ); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) checkPRStatus(ctx context.Context) error {
	states := []domain.FingerprintState{domain.StatePROpen, domain.StatePRNeedsChanges, domain.StatePRUpdating}
	for _, state := range states {
		fps, err := s.store.ListFingerprintsByState(ctx, state, 10)
		if err != nil {
			return err
		}
		for _, fp := range fps {
			if fp.PRNumber == 0 {
				continue
			}
			pr, err := s.ghIssue.GetPullRequest(ctx, s.cfg.GitHubWriteOwner, s.cfg.GitHubWriteRepo, fp.PRNumber)
			if err != nil {
				return err
			}
			if isMerged(pr) {
				if err := s.finalizeMergedPR(ctx, fp, pr); err != nil {
					return err
				}
				continue
			}
			if pr.State == "closed" {
				if err := s.handleClosedPR(ctx, fp, pr); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func isMerged(pr domain.PullRequest) bool {
	if pr.Merged {
		return true
	}
	return pr.MergedAt != nil && !pr.MergedAt.IsZero()
}

func (s *Service) finalizeMergedPR(ctx context.Context, fp domain.FingerprintRecord, pr domain.PullRequest) error {
	comment := fmt.Sprintf("PR #%d has been merged. Closing this issue and marking the fingerprint as resolved.", pr.Number)
	if err := s.ghIssue.CreateIssueComment(ctx, s.cfg.GitHubWriteOwner, s.cfg.GitHubWriteRepo, fp.IssueNumber, comment); err != nil {
		return err
	}
	stateClosed := "closed"
	if _, err := s.ghIssue.UpdateIssue(ctx, s.cfg.GitHubWriteOwner, s.cfg.GitHubWriteRepo, fp.IssueNumber, domain.UpdateIssueInput{State: &stateClosed}); err != nil {
		return err
	}
	if err := s.store.UpdateFingerprintState(ctx, fp.Fingerprint, domain.StateMerged); err != nil {
		return err
	}
	return s.store.RecordAudit(ctx, "fixagent.pr_merged", fmt.Sprintf("issue/%d", fp.IssueNumber), "success", fmt.Sprintf("pr#%d", pr.Number))
}

func (s *Service) handleClosedPR(ctx context.Context, fp domain.FingerprintRecord, pr domain.PullRequest) error {
	comment := fmt.Sprintf("PR #%d was closed without merge. Marking this fingerprint as CLOSED_WONTFIX.", pr.Number)
	if err := s.ghIssue.CreateIssueComment(ctx, s.cfg.GitHubWriteOwner, s.cfg.GitHubWriteRepo, fp.IssueNumber, comment); err != nil {
		return err
	}
	if err := s.store.UpdateFingerprintState(ctx, fp.Fingerprint, domain.StateClosedWontFix); err != nil {
		return err
	}
	return s.store.RecordAudit(ctx, "fixagent.pr_closed", fmt.Sprintf("issue/%d", fp.IssueNumber), "success", fmt.Sprintf("pr#%d", pr.Number))
}

func (s *Service) handlePRFeedbackLoop(ctx context.Context, agent *fixagent.Agent) error {
	if s.cfg.DryRun {
		return nil
	}
	fps, err := s.store.ListFingerprintsByState(ctx, domain.StatePROpen, 10)
	if err != nil {
		return err
	}
	for _, fp := range fps {
		if fp.PRNumber == 0 {
			continue
		}
		fb, err := s.buildPRFeedback(ctx, fp.PRNumber, fp.LastPRCommentID)
		if err != nil {
			return err
		}
		if !fb.NeedsUpdate() {
			continue
		}
		if err := s.store.UpdateFingerprintState(ctx, fp.Fingerprint, domain.StatePRNeedsChanges); err != nil {
			return err
		}
		_ = s.store.RecordAudit(ctx, "signal.pr_feedback", fmt.Sprintf("pr/%d", fp.PRNumber), "success", "")
	}

	fps, err = s.store.ListFingerprintsByState(ctx, domain.StatePRNeedsChanges, 10)
	if err != nil {
		return err
	}
	for _, fp := range fps {
		if fp.PRNumber == 0 {
			continue
		}
		fb, err := s.buildPRFeedback(ctx, fp.PRNumber, fp.LastPRCommentID)
		if err != nil {
			return err
		}
		if err := s.store.UpdateFingerprintState(ctx, fp.Fingerprint, domain.StatePRUpdating); err != nil {
			return err
		}
		if _, err := agent.FollowUp(ctx, fp, fb); err != nil {
			_ = s.store.RecordAudit(ctx, "fixagent.review_followup", fmt.Sprintf("pr/%d", fp.PRNumber), "error", err.Error())
			return err
		}
		if fb.LatestIssueCommentID > fp.LastPRCommentID {
			fpUpdate := fp
			fpUpdate.LastPRCommentID = fb.LatestIssueCommentID
			if err := s.store.UpsertFingerprint(ctx, fpUpdate); err != nil {
				return err
			}
		}
		if err := s.store.UpdateFingerprintState(ctx, fp.Fingerprint, domain.StatePROpen); err != nil {
			return err
		}
		_ = s.store.RecordAudit(ctx, "fixagent.review_followup", fmt.Sprintf("pr/%d", fp.PRNumber), "success", "")
	}
	return nil
}

func (s *Service) buildPRFeedback(ctx context.Context, prNumber int, sinceCommentID int64) (domain.PRFeedback, error) {
	pr, err := s.ghIssue.GetPullRequest(ctx, s.cfg.GitHubWriteOwner, s.cfg.GitHubWriteRepo, prNumber)
	if err != nil {
		return domain.PRFeedback{}, err
	}
	reviews, err := s.ghIssue.ListPullRequestReviews(ctx, s.cfg.GitHubWriteOwner, s.cfg.GitHubWriteRepo, prNumber)
	if err != nil {
		return domain.PRFeedback{}, err
	}
	var changesRequested []domain.PullRequestReview
	for _, r := range reviews {
		if strings.EqualFold(strings.TrimSpace(r.State), "CHANGES_REQUESTED") {
			changesRequested = append(changesRequested, r)
		}
	}
	status := domain.CombinedStatus{}
	if strings.TrimSpace(pr.Head.SHA) != "" {
		st, err := s.ghIssue.GetCombinedStatus(ctx, s.cfg.GitHubWriteOwner, s.cfg.GitHubWriteRepo, pr.Head.SHA)
		if err != nil {
			return domain.PRFeedback{}, err
		}
		status = st
	}

	issueComments, err := s.ghIssue.ListIssueComments(ctx, s.cfg.GitHubWriteOwner, s.cfg.GitHubWriteRepo, prNumber, domain.ListIssueCommentsOptions{PerPage: 50})
	if err != nil {
		return domain.PRFeedback{}, err
	}
	var latestCommentID int64
	var newComments []domain.IssueComment
	for _, c := range issueComments {
		if c.ID > latestCommentID {
			latestCommentID = c.ID
		}
		if c.ID <= sinceCommentID || isFTCManagedBody(c.Body) {
			continue
		}
		newComments = append(newComments, c)
	}
	return domain.PRFeedback{
		PRNumber:             pr.Number,
		PRURL:                pr.HTMLURL,
		HeadSHA:              pr.Head.SHA,
		ChangesRequested:     changesRequested,
		CombinedStatus:       status,
		LatestIssueCommentID: latestCommentID,
		NewIssueComments:     newComments,
	}, nil
}

func isFTCManagedBody(body string) bool {
	return strings.Contains(body, "<!-- FTC:")
}

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

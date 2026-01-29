package runner

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/okJiang/flaky-test-cleaner/internal/classify"
	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/fingerprint"
	"github.com/okJiang/flaky-test-cleaner/internal/fixagent"
	"github.com/okJiang/flaky-test-cleaner/internal/github"
	"github.com/okJiang/flaky-test-cleaner/internal/issue"
	"github.com/okJiang/flaky-test-cleaner/internal/sanitize"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
)

func (r *runtime) DiscoveryOnce(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("runtime not initialized")
	}

	cfg := r.cfg
	ghRead := r.ghRead
	ghIssue := r.ghIssue
	st := r.store

	wf, err := ghRead.FindWorkflowByName(ctx, cfg.GitHubOwner, cfg.GitHubRepo, cfg.WorkflowName)
	if err != nil {
		return err
	}

	runs, err := ghRead.ListWorkflowRuns(ctx, cfg.GitHubOwner, cfg.GitHubRepo, wf.ID, github.ListWorkflowRunsOptions{
		Status:  "failure",
		Branch:  cfg.GitHubBaseBranch,
		PerPage: cfg.MaxRuns,
	})
	if err != nil {
		return err
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
			failures := r.extractor.Extract(extract.Input{
				Repo:       cfg.GitHubOwner + "/" + cfg.GitHubRepo,
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
					Repo:         cfg.GitHubOwner + "/" + cfg.GitHubRepo,
					Framework:    occ.Framework,
					TestName:     occ.TestName,
					ErrorSigNorm: normSig,
					Platform:     occ.PlatformBucket(),
				})
				occ.Fingerprint = fp

				if err := st.UpsertOccurrence(ctx, occ); err != nil {
					return err
				}

				c, err := r.classifier.Classify(ctx, st, occ)
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

				change, err := r.issueMgr.PlanIssueUpdate(issue.PlanInput{
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

				issueNumber, err := r.issueMgr.Apply(ctx, ghIssue, change)
				if err != nil {
					return err
				}
				if issueNumber == 0 {
					continue
				}

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
					if err := runInitialAnalysis(ctx, cfg, r.analysisAgent, r.copilotClient, ghIssue, st, issueNumber, fp, *fpRecUpdated, recent, c); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func (r *runtime) InteractionOnce(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("runtime not initialized")
	}

	cfg := r.cfg
	st := r.store
	ghIssue := r.ghIssue

	if err := checkApprovalSignals(ctx, cfg, ghIssue, st); err != nil {
		return err
	}

	if cfg.DryRun {
		return nil
	}

	needsFix, err := needsFixAgent(ctx, st)
	if err != nil {
		return err
	}
	if !needsFix {
		return nil
	}

	if err := r.EnsureWorkspace(ctx); err != nil {
		return err
	}
	fx, err := fixagent.New(fixagent.Options{
		Owner:      cfg.GitHubWriteOwner,
		Repo:       cfg.GitHubWriteRepo,
		DryRun:     cfg.DryRun,
		GitHub:     ghIssue,
		Workspace:  r.wsManager,
		Store:      st,
		BaseBranch: cfg.GitHubBaseBranch,
	})
	if err != nil {
		return err
	}
	if err := runFixAgent(ctx, fx, st, ghIssue); err != nil {
		return err
	}
	if err := handlePRFeedbackLoop(ctx, cfg, ghIssue, st, fx); err != nil {
		return err
	}
	return checkPRStatus(ctx, cfg, ghIssue, st)
}

package runner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/classify"
	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/fingerprint"
	"github.com/okJiang/flaky-test-cleaner/internal/github"
	"github.com/okJiang/flaky-test-cleaner/internal/issue"
	"github.com/okJiang/flaky-test-cleaner/internal/issueagent"
	"github.com/okJiang/flaky-test-cleaner/internal/sanitize"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
)

func RunOnce(ctx context.Context, cfg config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	ghRead := github.NewClient(cfg.GitHubReadToken, cfg.RequestTimeout)
	ghIssue := ghRead
	if !cfg.DryRun {
		ghIssue = github.NewClient(cfg.GitHubIssueToken, cfg.RequestTimeout)
	}

	var st store.Store = store.NewMemory()
	if cfg.TiDBEnabled {
		tidb, err := store.NewTiDBStore(cfg)
		if err != nil {
			return err
		}
		defer tidb.Close()
		st = tidb
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
		PerPage: cfg.MaxRuns,
	})
	if err != nil {
		return err
	}

	extractor := extract.NewGoTestExtractor()
	classifier := classify.NewHeuristic(cfg.ConfidenceThreshold)
	issueMgr := issue.NewManager(issue.Options{
		Owner:  cfg.GitHubOwner,
		Repo:   cfg.GitHubRepo,
		DryRun: cfg.DryRun,
	})
	analysisAgent := issueagent.New()

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

				if c.Class == classify.ClassInfraFlake {
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
					log.Printf("dry-run issue update fingerprint=%s title=%q labels=%v", fp, change.Title, change.Labels)
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
						if err := runInitialAnalysis(ctx, cfg, analysisAgent, ghIssue, st, issueNumber, fp, *fpRecUpdated, recent, c); err != nil {
							return err
						}
					}
				}
			}
		}
	}

	return nil
}

func shouldRunInitialAnalysis(state store.FingerprintState) bool {
	return state == store.StateDiscovered
}

func runInitialAnalysis(
	ctx context.Context,
	cfg config.Config,
	agent *issueagent.Agent,
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
	if cfg.DryRun {
		log.Printf("dry-run issueagent comment issue=%d fingerprint=%s\n%s", issueNumber, fingerprint, comment.Body)
		return nil
	}
	if err := gh.CreateIssueComment(ctx, cfg.GitHubOwner, cfg.GitHubRepo, issueNumber, comment.Body); err != nil {
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

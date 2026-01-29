package runner

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/okJiang/flaky-test-cleaner/internal/classify"
	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/copilotsdk"
	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/github"
	"github.com/okJiang/flaky-test-cleaner/internal/issue"
	"github.com/okJiang/flaky-test-cleaner/internal/issueagent"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
	"github.com/okJiang/flaky-test-cleaner/internal/workspace"
)

type runtime struct {
	cfg config.Config

	ghRead  *github.Client
	ghIssue *github.Client

	store store.Store

	extractor     *extract.GoTestExtractor
	classifier    *classify.Heuristic
	issueMgr      *issue.Manager
	analysisAgent *issueagent.Agent

	copilotClient *copilotsdk.Client

	wsManager *workspace.Manager
}

func newRuntime(ctx context.Context, cfg config.Config, deps RunOnceDeps) (*runtime, func() error, error) {
	// Backward-compatible defaults: if write repo is not set (e.g. tests constructing Config directly),
	// fall back to the source repo.
	if strings.TrimSpace(cfg.GitHubWriteOwner) == "" {
		cfg.GitHubWriteOwner = cfg.GitHubOwner
	}
	if strings.TrimSpace(cfg.GitHubWriteRepo) == "" {
		cfg.GitHubWriteRepo = cfg.GitHubRepo
	}

	ghRead := deps.GitHubRead
	ghIssue := deps.GitHubIssue
	if ghRead == nil {
		ghRead = github.NewClientWithBaseURL(cfg.GitHubReadToken, cfg.RequestTimeout, cfg.GitHubAPIBaseURL)
	}
	if ghIssue == nil {
		ghIssue = ghRead
		if !cfg.DryRun {
			ghIssue = github.NewClientWithBaseURL(cfg.GitHubIssueToken, cfg.RequestTimeout, cfg.GitHubAPIBaseURL)
		}
	}
	if ghRead == nil {
		ghRead = ghIssue
	}

	st := deps.Store
	closeStore := func() error { return nil }
	if st == nil {
		st = store.NewMemory()
		if cfg.TiDBEnabled {
			tidb, err := store.NewTiDBStore(cfg)
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

	issueMgr := issue.NewManager(issue.Options{
		Owner:  cfg.GitHubWriteOwner,
		Repo:   cfg.GitHubWriteRepo,
		DryRun: cfg.DryRun,
	})

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
		if err := closeStore(); err != nil {
			return err
		}
		return nil
	}

	return &runtime{
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

func (r *runtime) EnsureWorkspace(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("runtime not initialized")
	}
	if r.wsManager == nil {
		ws, err := workspace.NewManager(workspace.Options{
			RemoteURL:    r.cfg.WriteRepoRemoteURL(),
			MirrorDir:    r.cfg.WorkspaceMirrorDir,
			WorktreesDir: r.cfg.WorkspaceWorktreesDir,
			MaxWorktrees: r.cfg.WorkspaceMaxWorktrees,
		})
		if err != nil {
			return err
		}
		r.wsManager = ws
	}
	return r.wsManager.Ensure(ctx)
}

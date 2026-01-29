package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Source repo (read-only): Actions runs/jobs/logs are fetched from here.
	GitHubOwner string
	GitHubRepo  string

	// Write repo (write): issues/labels/comments/PRs are created here.
	GitHubWriteOwner string
	GitHubWriteRepo  string

	GitHubBaseBranch string
	GitHubAPIBaseURL string

	GitHubReadToken  string
	GitHubIssueToken string

	WorkflowName string
	MaxRuns      int
	MaxJobs      int

	DryRun bool

	ConfidenceThreshold float64

	TiDBEnabled    bool
	TiDBHost       string
	TiDBPort       int
	TiDBUser       string
	TiDBPassword   string
	TiDBDatabase   string
	TiDBCACertPath string

	WorkspaceMirrorDir    string
	WorkspaceWorktreesDir string
	WorkspaceMaxWorktrees int

	RequestTimeout time.Duration

	// RunOnce forces a single cycle and exit.
	RunOnce bool

	// DiscoveryInterval controls how often the bot scans CI failures and updates issues.
	// Set to 0 to disable discovery.
	DiscoveryInterval time.Duration

	// InteractionInterval controls how often the bot polls issue/PR signals (approval/review/CI)
	// and drives FixAgent follow-ups. Set to 0 to disable interaction.
	InteractionInterval time.Duration

	CopilotModel    string
	CopilotTimeout  time.Duration
	CopilotLogLevel string
}

func FromEnvAndFlags(args []string) (Config, error) {
	fs := flag.NewFlagSet("flaky-test-cleaner", flag.ContinueOnError)

	var cfg Config
	cfg.GitHubOwner = envOr("FTC_GITHUB_OWNER", "tikv")
	cfg.GitHubRepo = envOr("FTC_GITHUB_REPO", "pd")
	cfg.GitHubWriteOwner = envOr("FTC_GITHUB_WRITE_OWNER", cfg.GitHubOwner)
	cfg.GitHubWriteRepo = envOr("FTC_GITHUB_WRITE_REPO", cfg.GitHubRepo)
	cfg.GitHubBaseBranch = envOr("FTC_BASE_BRANCH", "master")
	cfg.GitHubAPIBaseURL = envOr("FTC_GITHUB_API_BASE_URL", "https://api.github.com")
	cfg.GitHubReadToken = os.Getenv("FTC_GITHUB_READ_TOKEN")
	cfg.GitHubIssueToken = os.Getenv("FTC_GITHUB_ISSUE_TOKEN")

	cfg.WorkflowName = envOr("FTC_WORKFLOW_NAME", "PD Test")
	cfg.MaxRuns = envIntOr("FTC_MAX_RUNS", 20)
	cfg.MaxJobs = envIntOr("FTC_MAX_JOBS", 50)

	cfg.DryRun = envBoolOr("FTC_DRY_RUN", true)
	cfg.ConfidenceThreshold = envFloatOr("FTC_CONFIDENCE_THRESHOLD", 0.75)

	cfg.TiDBEnabled = envBoolOr("FTC_TIDB_ENABLED", false)
	cfg.TiDBHost = os.Getenv("TIDB_HOST")
	cfg.TiDBPort = envIntOr("TIDB_PORT", 4000)
	cfg.TiDBUser = os.Getenv("TIDB_USER")
	cfg.TiDBPassword = os.Getenv("TIDB_PASSWORD")
	cfg.TiDBDatabase = envOr("TIDB_DATABASE", "flaky_test_cleaner")
	cfg.TiDBCACertPath = os.Getenv("TIDB_CA_CERT_PATH")

	cfg.WorkspaceMirrorDir = envOr("FTC_WORKSPACE_MIRROR", "cache/tikv-pd.git")
	cfg.WorkspaceWorktreesDir = envOr("FTC_WORKSPACE_WORKTREES", "worktrees")
	cfg.WorkspaceMaxWorktrees = envIntOr("FTC_WORKSPACE_MAX", 2)

	cfg.RequestTimeout = envDurationOr("FTC_REQUEST_TIMEOUT", 30*time.Second)

	cfg.RunOnce = envBoolOr("FTC_RUN_ONCE", false)
	cfg.DiscoveryInterval = envDurationOr("FTC_DISCOVERY_INTERVAL", 72*time.Hour)
	cfg.InteractionInterval = envDurationOr("FTC_INTERACTION_INTERVAL", 10*time.Minute)

	cfg.CopilotModel = envOr("FTC_COPILOT_MODEL", "gpt-5")
	cfg.CopilotTimeout = envDurationOr("FTC_COPILOT_TIMEOUT", 60*time.Second)
	cfg.CopilotLogLevel = envOr("FTC_COPILOT_LOG_LEVEL", "error")

	fs.StringVar(&cfg.GitHubOwner, "owner", cfg.GitHubOwner, "GitHub repository owner (source for Actions logs)")
	fs.StringVar(&cfg.GitHubRepo, "repo", cfg.GitHubRepo, "GitHub repository name (source for Actions logs)")
	fs.StringVar(&cfg.GitHubWriteOwner, "write-owner", cfg.GitHubWriteOwner, "GitHub repository owner to write issues/PRs to")
	fs.StringVar(&cfg.GitHubWriteRepo, "write-repo", cfg.GitHubWriteRepo, "GitHub repository name to write issues/PRs to")
	fs.StringVar(&cfg.GitHubBaseBranch, "base-branch", cfg.GitHubBaseBranch, "Base branch used when opening PRs")
	fs.StringVar(&cfg.GitHubAPIBaseURL, "github-api-base-url", cfg.GitHubAPIBaseURL, "GitHub API base URL (for tests; default https://api.github.com)")
	fs.StringVar(&cfg.WorkflowName, "workflow", cfg.WorkflowName, "Workflow name to scan")
	fs.IntVar(&cfg.MaxRuns, "max-runs", cfg.MaxRuns, "Max failed runs to scan")
	fs.IntVar(&cfg.MaxJobs, "max-jobs", cfg.MaxJobs, "Max jobs per run to scan")
	fs.BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun, "Do not write to GitHub (issue create/update); still writes to TiDB if enabled")
	fs.Float64Var(&cfg.ConfidenceThreshold, "confidence-threshold", cfg.ConfidenceThreshold, "Classifier threshold to label as flaky")
	fs.BoolVar(&cfg.TiDBEnabled, "tidb", cfg.TiDBEnabled, "Enable TiDB state store")
	fs.StringVar(&cfg.WorkspaceMirrorDir, "workspace-mirror", cfg.WorkspaceMirrorDir, "Path to bare mirror used by RepoWorkspaceManager")
	fs.StringVar(&cfg.WorkspaceWorktreesDir, "workspace-dir", cfg.WorkspaceWorktreesDir, "Path holding RepoWorkspaceManager worktrees")
	fs.IntVar(&cfg.WorkspaceMaxWorktrees, "workspace-max", cfg.WorkspaceMaxWorktrees, "Maximum concurrent worktrees to lease")
	fs.BoolVar(&cfg.RunOnce, "once", cfg.RunOnce, "Run one cycle and exit (useful for local validation)")
	fs.DurationVar(&cfg.DiscoveryInterval, "discovery-interval", cfg.DiscoveryInterval, "Interval to scan CI failures and update issues (0 disables discovery)")
	fs.DurationVar(&cfg.InteractionInterval, "interaction-interval", cfg.InteractionInterval, "Interval to poll issue/PR signals and drive FixAgent (0 disables interaction)")
	fs.StringVar(&cfg.CopilotModel, "copilot-model", cfg.CopilotModel, "Copilot model ID (e.g. gpt-5, gpt-4.1)")
	fs.DurationVar(&cfg.CopilotTimeout, "copilot-timeout", cfg.CopilotTimeout, "Copilot SDK timeout per request")
	fs.StringVar(&cfg.CopilotLogLevel, "copilot-log-level", cfg.CopilotLogLevel, "Copilot CLI log level (error/info/debug)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if cfg.GitHubOwner == "" || cfg.GitHubRepo == "" {
		return Config{}, errors.New("owner/repo must be set")
	}
	if cfg.GitHubWriteOwner == "" || cfg.GitHubWriteRepo == "" {
		return Config{}, errors.New("write-owner/write-repo must be set")
	}
	if cfg.GitHubReadToken == "" {
		return Config{}, errors.New("FTC_GITHUB_READ_TOKEN is required")
	}
	if !cfg.DryRun && cfg.GitHubIssueToken == "" {
		return Config{}, errors.New("FTC_GITHUB_ISSUE_TOKEN is required unless --dry-run")
	}
	if cfg.TiDBEnabled {
		if cfg.TiDBHost == "" || cfg.TiDBUser == "" {
			return Config{}, errors.New("TiDB enabled but TIDB_HOST/TIDB_USER not set")
		}
		// Local TiDB deployments may not require TLS (no CA) and may allow empty passwords.
	}

	if cfg.RunOnce {
		if cfg.DiscoveryInterval <= 0 && cfg.InteractionInterval <= 0 {
			return Config{}, errors.New("--once requires at least one of discovery/interaction to be enabled")
		}
	} else {
		if cfg.DiscoveryInterval <= 0 && cfg.InteractionInterval <= 0 {
			return Config{}, errors.New("daemon mode requires at least one of discovery/interaction to be enabled (set an interval or use --once)")
		}
	}

	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func envBoolOr(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "t", "yes", "y", "on":
			return true
		case "0", "false", "f", "no", "n", "off":
			return false
		default:
			return def
		}
	}
	return def
}

func envFloatOr(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envDurationOr(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func (cfg Config) RepoRemoteURL() string {
	return fmt.Sprintf("https://github.com/%s/%s.git", cfg.GitHubOwner, cfg.GitHubRepo)
}

func (cfg Config) WriteRepoRemoteURL() string {
	return fmt.Sprintf("https://github.com/%s/%s.git", cfg.GitHubWriteOwner, cfg.GitHubWriteRepo)
}

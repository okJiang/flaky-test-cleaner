package config

import (
	"errors"
	"flag"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	GitHubOwner string
	GitHubRepo  string

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

	RequestTimeout time.Duration
}

func FromEnvAndFlags(args []string) (Config, error) {
	fs := flag.NewFlagSet("flaky-test-cleaner", flag.ContinueOnError)

	var cfg Config
	cfg.GitHubOwner = envOr("FTC_GITHUB_OWNER", "tikv")
	cfg.GitHubRepo = envOr("FTC_GITHUB_REPO", "pd")
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

	cfg.RequestTimeout = envDurationOr("FTC_REQUEST_TIMEOUT", 30*time.Second)

	fs.StringVar(&cfg.GitHubOwner, "owner", cfg.GitHubOwner, "GitHub repository owner")
	fs.StringVar(&cfg.GitHubRepo, "repo", cfg.GitHubRepo, "GitHub repository name")
	fs.StringVar(&cfg.WorkflowName, "workflow", cfg.WorkflowName, "Workflow name to scan")
	fs.IntVar(&cfg.MaxRuns, "max-runs", cfg.MaxRuns, "Max failed runs to scan")
	fs.IntVar(&cfg.MaxJobs, "max-jobs", cfg.MaxJobs, "Max jobs per run to scan")
	fs.BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun, "Do not write to GitHub (issue create/update); still writes to TiDB if enabled")
	fs.Float64Var(&cfg.ConfidenceThreshold, "confidence-threshold", cfg.ConfidenceThreshold, "Classifier threshold to label as flaky")
	fs.BoolVar(&cfg.TiDBEnabled, "tidb", cfg.TiDBEnabled, "Enable TiDB state store")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if cfg.GitHubOwner == "" || cfg.GitHubRepo == "" {
		return Config{}, errors.New("owner/repo must be set")
	}
	if cfg.GitHubReadToken == "" {
		return Config{}, errors.New("FTC_GITHUB_READ_TOKEN is required")
	}
	if !cfg.DryRun && cfg.GitHubIssueToken == "" {
		return Config{}, errors.New("FTC_GITHUB_ISSUE_TOKEN is required unless --dry-run")
	}
	if cfg.TiDBEnabled {
		if cfg.TiDBHost == "" || cfg.TiDBUser == "" || cfg.TiDBPassword == "" {
			return Config{}, errors.New("TiDB enabled but TIDB_HOST/TIDB_USER/TIDB_PASSWORD not set")
		}
		if strings.TrimSpace(cfg.TiDBCACertPath) == "" {
			return Config{}, errors.New("TiDB enabled but TIDB_CA_CERT_PATH not set")
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

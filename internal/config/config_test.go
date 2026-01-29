package config

import (
	"os"
	"testing"
	"time"
)

func withEnv(t *testing.T, key, val string) func() {
	t.Helper()
	old, ok := os.LookupEnv(key)
	if val == "" {
		_ = os.Unsetenv(key)
	} else {
		_ = os.Setenv(key, val)
	}
	return func() {
		if ok {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}

func TestFromEnvAndFlags_RequiresReadToken(t *testing.T) {
	undo1 := withEnv(t, "FTC_GITHUB_READ_TOKEN", "")
	undo2 := withEnv(t, "FTC_DRY_RUN", "true")
	defer undo1()
	defer undo2()

	_, err := FromEnvAndFlags([]string{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestFromEnvAndFlags_DryRunDoesNotRequireIssueToken(t *testing.T) {
	undo1 := withEnv(t, "FTC_GITHUB_READ_TOKEN", "read")
	undo2 := withEnv(t, "FTC_GITHUB_ISSUE_TOKEN", "")
	undo3 := withEnv(t, "FTC_DRY_RUN", "true")
	defer undo1()
	defer undo2()
	defer undo3()

	_, err := FromEnvAndFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFromEnvAndFlags_NonDryRunRequiresIssueToken(t *testing.T) {
	undo1 := withEnv(t, "FTC_GITHUB_READ_TOKEN", "read")
	undo2 := withEnv(t, "FTC_GITHUB_ISSUE_TOKEN", "")
	undo3 := withEnv(t, "FTC_DRY_RUN", "false")
	defer undo1()
	defer undo2()
	defer undo3()

	_, err := FromEnvAndFlags([]string{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestFromEnvAndFlags_TiDBEnabledRequiresHostAndUser(t *testing.T) {
	undo := []func(){
		withEnv(t, "FTC_GITHUB_READ_TOKEN", "read"),
		withEnv(t, "FTC_DRY_RUN", "true"),
		withEnv(t, "FTC_TIDB_ENABLED", "true"),
		withEnv(t, "TIDB_HOST", ""),
		withEnv(t, "TIDB_USER", ""),
	}
	defer func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}()

	_, err := FromEnvAndFlags([]string{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestFromEnvAndFlags_LegacyIntervalZeroDoesNotDisableLoopDefaults(t *testing.T) {
	undo := []func(){
		withEnv(t, "FTC_GITHUB_READ_TOKEN", "read"),
		withEnv(t, "FTC_DRY_RUN", "true"),
		withEnv(t, "FTC_RUN_ONCE", ""),
		withEnv(t, "FTC_RUN_INTERVAL", "0s"),
		withEnv(t, "FTC_DISCOVERY_INTERVAL", ""),
		withEnv(t, "FTC_INTERACTION_INTERVAL", ""),
	}
	defer func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}()

	cfg, err := FromEnvAndFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DiscoveryInterval <= 0 {
		t.Fatalf("expected discovery interval >0, got %s", cfg.DiscoveryInterval)
	}
	if cfg.InteractionInterval <= 0 {
		t.Fatalf("expected interaction interval >0, got %s", cfg.InteractionInterval)
	}
}

func TestFromEnvAndFlags_LegacyIntervalPositiveSetsBothLoops(t *testing.T) {
	undo := []func(){
		withEnv(t, "FTC_GITHUB_READ_TOKEN", "read"),
		withEnv(t, "FTC_DRY_RUN", "true"),
		withEnv(t, "FTC_RUN_ONCE", ""),
		withEnv(t, "FTC_RUN_INTERVAL", "1h"),
		withEnv(t, "FTC_DISCOVERY_INTERVAL", ""),
		withEnv(t, "FTC_INTERACTION_INTERVAL", ""),
	}
	defer func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}()

	cfg, err := FromEnvAndFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DiscoveryInterval != time.Hour {
		t.Fatalf("expected discovery interval 1h, got %s", cfg.DiscoveryInterval)
	}
	if cfg.InteractionInterval != time.Hour {
		t.Fatalf("expected interaction interval 1h, got %s", cfg.InteractionInterval)
	}
}

package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/domain"
)

func TestMemoryListFingerprintsByState(t *testing.T) {
	ctx := context.Background()
	mem := NewMemory()
	now := time.Now()
	if err := mem.UpsertFingerprint(ctx, domain.FingerprintRecord{
		Fingerprint: "fp-waiting",
		State:       domain.StateWaitingForSignal,
		FirstSeenAt: now.Add(-time.Hour),
		LastSeenAt:  now,
	}); err != nil {
		t.Fatalf("upsert waiting: %v", err)
	}
	if err := mem.UpsertFingerprint(ctx, domain.FingerprintRecord{
		Fingerprint: "fp-triaged",
		State:       domain.StateTriaged,
		FirstSeenAt: now,
		LastSeenAt:  now,
	}); err != nil {
		t.Fatalf("upsert triaged: %v", err)
	}
	res, err := mem.ListFingerprintsByState(ctx, domain.StateWaitingForSignal, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res) != 1 || res[0].Fingerprint != "fp-waiting" {
		t.Fatalf("unexpected results: %+v", res)
	}
}

func TestMySQLDSN_TLSOptional(t *testing.T) {
	cfg := config.Config{
		TiDBHost:     "127.0.0.1",
		TiDBPort:     4000,
		TiDBUser:     "root",
		TiDBPassword: "",
	}
	cfg.TiDBCACertPath = ""
	noTLS := mysqlDSN(cfg, "flaky_test_cleaner")
	if strings.Contains(noTLS, "tls=") {
		t.Fatalf("expected DSN without tls param, got %q", noTLS)
	}
	cfg.TiDBCACertPath = "/tmp/ca.pem"
	withTLS := mysqlDSN(cfg, "flaky_test_cleaner")
	if !strings.Contains(withTLS, "tls=tidb") {
		t.Fatalf("expected DSN with tls=tidb, got %q", withTLS)
	}
}

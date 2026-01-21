package store

import (
	"context"
	"testing"
	"time"
)

func TestMemoryListFingerprintsByState(t *testing.T) {
	ctx := context.Background()
	mem := NewMemory()
	now := time.Now()
	if err := mem.UpsertFingerprint(ctx, FingerprintRecord{
		Fingerprint: "fp-waiting",
		State:       StateWaitingForSignal,
		FirstSeenAt: now.Add(-time.Hour),
		LastSeenAt:  now,
	}); err != nil {
		t.Fatalf("upsert waiting: %v", err)
	}
	if err := mem.UpsertFingerprint(ctx, FingerprintRecord{
		Fingerprint: "fp-triaged",
		State:       StateTriaged,
		FirstSeenAt: now,
		LastSeenAt:  now,
	}); err != nil {
		t.Fatalf("upsert triaged: %v", err)
	}
	res, err := mem.ListFingerprintsByState(ctx, StateWaitingForSignal, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res) != 1 || res[0].Fingerprint != "fp-waiting" {
		t.Fatalf("unexpected results: %+v", res)
	}
}

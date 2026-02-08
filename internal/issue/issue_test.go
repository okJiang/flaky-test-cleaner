package issue

import (
	"strings"
	"testing"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/domain"
)

func TestPlanIssueUpdateCreatesBody(t *testing.T) {
	mgr := NewManager(Options{Owner: "tikv", Repo: "pd"})
	occ := []domain.Occurrence{{
		RunID:          1,
		RunURL:         "https://example.com/run/1",
		Workflow:       "PD Test",
		JobName:        "PD Test",
		HeadSHA:        "deadbeef",
		TestName:       "TestFoo",
		ErrorSignature: "panic: boom",
		Excerpt:        "panic: boom",
		OccurredAt:     time.Now(),
	}}
	occ[0].ErrorSignature = "2026-01-22T08:01:43.5719114Z --- FAIL: TestFoo (0.00s)"
	change, err := mgr.PlanIssueUpdate(PlanInput{
		Fingerprint: domain.FingerprintRecord{
			Fingerprint: "abc",
			TestName:    "TestFoo",
			FirstSeenAt: time.Now().Add(-time.Hour),
			LastSeenAt:  time.Now(),
		},
		Occurrences:    occ,
		Classification: domain.Classification{Class: domain.ClassFlakyTest, Confidence: 0.8},
	})
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if !change.Create {
		t.Fatalf("expected create change")
	}
	if !strings.Contains(change.Body, "FTC:SUMMARY_START") {
		t.Fatalf("expected summary block")
	}
	if strings.Contains(change.Title, "2026-") {
		t.Fatalf("expected title timestamp to be stripped, got %q", change.Title)
	}
}

package issue

import (
	"strings"
	"testing"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/classify"
	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
)

func TestPlanIssueUpdateCreatesBody(t *testing.T) {
	mgr := NewManager(Options{Owner: "tikv", Repo: "pd"})
	occ := []extract.Occurrence{{
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
	change, err := mgr.PlanIssueUpdate(PlanInput{
		Fingerprint: store.FingerprintRecord{
			Fingerprint: "abc",
			TestName:    "TestFoo",
			FirstSeenAt: time.Now().Add(-time.Hour),
			LastSeenAt:  time.Now(),
		},
		Occurrences:     occ,
		Classification: classify.Result{Class: classify.ClassFlakyTest, Confidence: 0.8},
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
}

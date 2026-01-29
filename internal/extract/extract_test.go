package extract

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestGoTestExtractorFindsFail(t *testing.T) {
	log := strings.Join([]string{
		"=== RUN   TestFoo",
		"--- FAIL: TestFoo (0.00s)",
		"    foo_test.go:12: expected true",
		"FAIL",
	}, "\n")

	extractor := NewGoTestExtractor()
	occ := extractor.Extract(Input{
		Repo:       "tikv/pd",
		Workflow:   "PD Test",
		RunID:      1,
		RunURL:     "https://example.com/run/1",
		HeadSHA:    "deadbeef",
		JobID:      2,
		JobName:    "PD Test",
		RunnerOS:   "ubuntu-latest",
		OccurredAt: time.Now(),
		RawLogText: log,
	})
	if len(occ) != 1 {
		t.Fatalf("expected 1 occurrence, got %d", len(occ))
	}
	if occ[0].TestName != "TestFoo" {
		t.Fatalf("expected TestFoo, got %q", occ[0].TestName)
	}
	if occ[0].Excerpt == "" {
		t.Fatalf("expected excerpt")
	}
}

func TestGoTestExtractorIgnoresTimeoutNoise(t *testing.T) {
	b, err := os.ReadFile("testdata/noise-timeout.log")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	extractor := NewGoTestExtractor()
	occ := extractor.Extract(Input{RawLogText: string(b)})
	if len(occ) != 0 {
		t.Fatalf("expected 0 occurrences, got %d", len(occ))
	}
}

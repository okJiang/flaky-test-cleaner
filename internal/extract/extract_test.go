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
	if !strings.Contains(occ[0].ErrorSignature, "foo_test.go:12: expected true") {
		t.Fatalf("expected error signature to include failure detail line, got %q", occ[0].ErrorSignature)
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

func TestGoTestExtractorPrefersContextAboveFailMarkerWithinActionsGroup(t *testing.T) {
	var lines []string
	lines = append(lines, "BEFORE_GROUP: setup noise")
	lines = append(lines, "2026-01-29T00:00:00Z ##[group]Run make test")
	lines = append(lines, "CONTEXT: cluster bootstrap complete")
	for i := 0; i < 140; i++ {
		lines = append(lines, "filler line")
	}
	lines = append(lines, "=== RUN   TestFoo")
	lines = append(lines, "--- FAIL: TestFoo (0.02s)")
	lines = append(lines, "    foo_test.go:12: expected true, got false")
	lines = append(lines, "FAIL")
	lines = append(lines, "2026-01-29T00:10:00Z ##[endgroup]")
	lines = append(lines, "2026-01-29T00:10:01Z ##[error]Process completed with exit code 1.")

	extractor := NewGoTestExtractor()
	occ := extractor.Extract(Input{RawLogText: strings.Join(lines, "\n")})
	if len(occ) != 1 {
		t.Fatalf("expected 1 occurrence, got %d", len(occ))
	}
	if strings.Contains(occ[0].Excerpt, "BEFORE_GROUP: setup noise") {
		t.Fatalf("expected excerpt to stay within the Actions group")
	}
	if !strings.Contains(occ[0].Excerpt, "CONTEXT: cluster bootstrap complete") {
		t.Fatalf("expected excerpt to include context above FAIL marker; excerpt=%q", occ[0].Excerpt)
	}
	if first := strings.SplitN(occ[0].ErrorSignature, "\n", 2)[0]; !strings.Contains(first, "foo_test.go:12: expected true, got false") {
		t.Fatalf("expected error signature first line to be failure detail, got %q", occ[0].ErrorSignature)
	}
}

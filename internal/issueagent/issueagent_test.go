package issueagent

import (
	"strings"
	"testing"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/classify"
	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
)

func TestBuildInitialComment(t *testing.T) {
	agent := New()
	now := time.Date(2026, 1, 21, 10, 0, 0, 0, time.UTC)
	input := Input{
		Fingerprint: store.FingerprintRecord{
			Fingerprint: "fp-123",
			TestName:    "TestLeaderTransfer",
			FirstSeenAt: now.Add(-24 * time.Hour),
			LastSeenAt:  now,
		},
		Occurrences: []extract.Occurrence{
			{
				RunID:          42,
				RunURL:         "https://github.com/tikv/pd/actions/runs/42",
				JobName:        "PD Test",
				HeadSHA:        "abcdef1234567890",
				TestName:       "TestLeaderTransfer",
				ErrorSignature: "panic: leader is nil",
				Excerpt:        "panic: leader is nil\nDATA RACE",
				OccurredAt:     now,
			},
		},
		Classification: classify.Result{
			Class:       classify.ClassFlakyTest,
			Confidence:  0.82,
			Explanation: "matched panic keyword",
		},
	}

	comment := agent.BuildInitialComment(input)
	body := comment.Body
	for _, section := range []string{
		"<!-- FTC:ISSUE_AGENT_START -->",
		"## AI Analysis Summary",
		"## Hypotheses",
		"## Reproduction Ideas",
		"## Suggested Fix Directions",
		"## Risk Notes",
		"## Evidence Highlights",
		"<!-- FTC:ISSUE_AGENT_END -->",
	} {
		if !strings.Contains(body, section) {
			t.Fatalf("expected commentary to contain section %q", section)
		}
	}

	if !strings.Contains(body, "panic occurred") {
		t.Fatalf("expected panic hypothesis, got:\n%s", body)
	}
	if !strings.Contains(body, "`go test ./... -run '^TestLeaderTransfer$' -count=30 -race`") {
		t.Fatalf("expected go test reproduction command, got:\n%s", body)
	}
	if !strings.Contains(body, "[42](https://github.com/tikv/pd/actions/runs/42)") {
		t.Fatalf("expected run link, got:\n%s", body)
	}
}

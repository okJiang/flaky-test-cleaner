package fixagent

import (
	"strings"
	"testing"

	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/github"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
)

func TestBuildPreparationComment(t *testing.T) {
	fp := store.FingerprintRecord{Fingerprint: "fp-123"}
	occ := []extract.Occurrence{{
		RunID:    101,
		RunURL:   "https://example.com/run/101",
		TestName: "TestBar",
		HeadSHA:  "abcdef1234567890",
	}}
	comment := buildPreparationComment(fp, occ, "/tmp/worktrees/fix-fp", "go test ok")
	for _, token := range []string{"FixAgent", "run 101", "abcdef1", "go test ok"} {
		if !strings.Contains(comment, token) {
			t.Fatalf("expected comment to contain %q:\n%s", token, comment)
		}
	}
}

func TestRenderFeedbackChecklist(t *testing.T) {
	fb := PRFeedback{
		PRNumber: 101,
		CombinedStatus: github.CombinedStatus{
			State: "failure",
			Statuses: []github.Status{{
				Context:     "ci/unit",
				State:       "failure",
				Description: "tests failed",
			}},
		},
		ChangesRequested: []github.PullRequestReview{{
			State: "CHANGES_REQUESTED",
			Body:  "please add a test",
			User:  github.User{Login: "reviewer"},
		}},
	}
	out := renderFeedbackChecklist(fb)
	for _, want := range []string{"Changes requested", "reviewer", "please add a test", "CI status", "ci/unit", "tests failed", "Combined state"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected checklist to contain %q:\n%s", want, out)
		}
	}
}

func TestBuildFollowUpComment(t *testing.T) {
	fp := store.FingerprintRecord{Fingerprint: "fp-123", TestName: "TestFoo"}
	fb := PRFeedback{PRNumber: 22, PRURL: "https://example.com/pr/22", HeadSHA: "abcdef123456"}
	comment := buildFollowUpComment(fp, fb)
	for _, want := range []string{"FTC:REVIEW_RESPONSE_START", "fp-123", "#22", "example.com/pr/22", "abcdef1"} {
		if !strings.Contains(comment, want) {
			t.Fatalf("expected comment to contain %q:\n%s", want, comment)
		}
	}
}

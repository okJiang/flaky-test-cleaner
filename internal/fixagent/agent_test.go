package fixagent

import (
	"strings"
	"testing"

	"github.com/okJiang/flaky-test-cleaner/internal/extract"
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
	comment := buildPreparationComment(fp, occ, "/tmp/worktrees/fix-fp")
	for _, token := range []string{"FixAgent", "run 101", "abcdef1"} {
		if !strings.Contains(comment, token) {
			t.Fatalf("expected comment to contain %q:\n%s", token, comment)
		}
	}
}

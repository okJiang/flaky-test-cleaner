package extract

import (
	"testing"

	"github.com/okJiang/flaky-test-cleaner/internal/domain"
)

func TestDropParentTestsKeepsLeaf(t *testing.T) {
	in := []domain.Occurrence{
		{TestName: "TestSwitchModeDuringWorkload"},
		{TestName: "TestSwitchModeDuringWorkload/pd-to-standalone"},
		{TestName: "TestSwitchModeDuringWorkload/pd-to-standalone/caseA"},
	}
	out := dropParentTests(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 occurrence, got %d", len(out))
	}
	if out[0].TestName != "TestSwitchModeDuringWorkload/pd-to-standalone/caseA" {
		t.Fatalf("unexpected kept test: %q", out[0].TestName)
	}
}

func TestDropParentTestsNoopWhenNoSubtests(t *testing.T) {
	in := []domain.Occurrence{{TestName: "TestA"}, {TestName: "TestB"}}
	out := dropParentTests(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 occurrences, got %d", len(out))
	}
}

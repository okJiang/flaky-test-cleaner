package classify

import (
	"context"
	"testing"

	"github.com/okJiang/flaky-test-cleaner/internal/domain"
)

func TestHeuristicClassify(t *testing.T) {
	h := NewHeuristic(0.75)
	cases := []struct {
		name string
		occ  domain.Occurrence
		want domain.Class
	}{
		{name: "infra", occ: domain.Occurrence{ErrorSignature: "dial tcp 1.1.1.1:443: i/o timeout"}, want: domain.ClassInfraFlake},
		{name: "regression", occ: domain.Occurrence{ErrorSignature: "undefined: foo"}, want: domain.ClassLikelyRegression},
		{name: "flaky", occ: domain.Occurrence{ErrorSignature: "panic: test timed out"}, want: domain.ClassFlakyTest},
		{name: "unknown", occ: domain.Occurrence{}, want: domain.ClassUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := h.Classify(context.Background(), nil, tc.occ)
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if got.Class != tc.want {
				t.Fatalf("want %s, got %s", tc.want, got.Class)
			}
		})
	}
}

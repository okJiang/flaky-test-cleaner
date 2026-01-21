package classify

import (
	"context"

	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
)

type Class string

const (
	ClassFlakyTest       Class = "flaky-test"
	ClassInfraFlake      Class = "infra-flake"
	ClassLikelyRegression Class = "likely-regression"
	ClassUnknown         Class = "unknown"
)

type Result struct {
	Class      Class
	Confidence float64
	Explanation string
}

type Classifier interface {
	Classify(ctx context.Context, st store.Store, occ extract.Occurrence) (Result, error)
}

type Heuristic struct {
	threshold float64
}

func NewHeuristic(threshold float64) *Heuristic { return &Heuristic{threshold: threshold} }

func (h *Heuristic) Classify(ctx context.Context, st store.Store, occ extract.Occurrence) (Result, error) {
	return Result{Class: ClassUnknown, Confidence: 0.5, Explanation: "heuristic classifier stub"}, nil
}








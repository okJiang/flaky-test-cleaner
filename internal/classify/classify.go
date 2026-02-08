package classify

import (
	"context"
	"strings"

	"github.com/okJiang/flaky-test-cleaner/internal/domain"
	"github.com/okJiang/flaky-test-cleaner/internal/ports"
)

type Classifier interface {
	Classify(ctx context.Context, st ports.Store, occ domain.Occurrence) (domain.Classification, error)
}

type Heuristic struct {
	threshold float64
}

func NewHeuristic(threshold float64) *Heuristic { return &Heuristic{threshold: threshold} }

func (h *Heuristic) Classify(ctx context.Context, st ports.Store, occ domain.Occurrence) (domain.Classification, error) {
	text := strings.ToLower(strings.TrimSpace(occ.ErrorSignature + "\n" + occ.Excerpt))
	if text == "" {
		return domain.Classification{Class: domain.ClassUnknown, Confidence: 0.4, Explanation: "no signal in logs"}, nil
	}
	if containsAny(text, infraKeywords) {
		return domain.Classification{Class: domain.ClassInfraFlake, Confidence: 0.9, Explanation: "matched infra/network keyword"}, nil
	}
	if containsAny(text, regressionKeywords) {
		return domain.Classification{Class: domain.ClassLikelyRegression, Confidence: 0.85, Explanation: "matched build/compile keyword"}, nil
	}
	if containsAny(text, flakyKeywords) {
		return domain.Classification{Class: domain.ClassFlakyTest, Confidence: 0.8, Explanation: "matched flaky/timeout/race keyword"}, nil
	}
	return domain.Classification{Class: domain.ClassUnknown, Confidence: 0.5, Explanation: "no strong heuristic match"}, nil
}

var infraKeywords = []string{
	"connection reset",
	"broken pipe",
	"dial tcp",
	"tls handshake timeout",
	"i/o timeout",
	"no space left on device",
	"network is unreachable",
	"temporary failure",
	"runner lost",
	"operation timed out",
}

var regressionKeywords = []string{
	"undefined:",
	"cannot find",
	"build failed",
	"compile",
	"syntax error",
	"missing module",
	"no required module provides package",
}

var flakyKeywords = []string{
	"data race",
	"panic:",
	"timeout",
	"test timed out",
	"race detected",
}

func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

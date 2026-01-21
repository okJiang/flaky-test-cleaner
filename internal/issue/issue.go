package issue

import (
	"context"

	"github.com/okJiang/flaky-test-cleaner/internal/classify"
	"github.com/okJiang/flaky-test-cleaner/internal/extract"
	"github.com/okJiang/flaky-test-cleaner/internal/github"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
)

type Options struct {
	Owner string
	Repo  string
	DryRun bool
}

type Manager struct {
	opts Options
}

func NewManager(opts Options) *Manager { return &Manager{opts: opts} }

type PlanInput struct {
	Fingerprint store.FingerprintRecord
	Occurrences []extract.Occurrence
	Classification classify.Result
}

type PlannedChange struct {
	Noop bool
	Create bool
	IssueNumber int
	Title string
	Body string
	Labels []string
}

func (m *Manager) PlanIssueUpdate(in PlanInput) (PlannedChange, error) {
	return PlannedChange{Noop: true}, nil
}

func (m *Manager) Apply(ctx context.Context, gh *github.Client, ch PlannedChange) (int, error) {
	return 0, nil
}

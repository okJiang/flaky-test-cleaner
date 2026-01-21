package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/extract"
)

type FingerprintRecord struct {
	Fingerprint string
	Repo        string
	TestName    string
	Framework   string
	Class       string
	Confidence  float64
	IssueNumber int
	PRNumber    int
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

type Store interface {
	Migrate(ctx context.Context) error
	UpsertOccurrence(ctx context.Context, occ extract.Occurrence) error
	UpsertFingerprint(ctx context.Context, rec FingerprintRecord) error
	GetFingerprint(ctx context.Context, fingerprint string) (*FingerprintRecord, error)
	ListRecentOccurrences(ctx context.Context, fingerprint string, limit int) ([]extract.Occurrence, error)
	LinkIssue(ctx context.Context, fingerprint string, issueNumber int) error
	Close() error
}

type Memory struct {
	mu          sync.Mutex
	fps         map[string]FingerprintRecord
	occurrences map[string][]extract.Occurrence

}




































































































func NewMemory() *Memory {
	return &Memory{fps: map[string]FingerprintRecord{}, occurrences: map[string][]extract.Occurrence{}}
}

func (m *Memory) Migrate(ctx context.Context) error { return nil }

func (m *Memory) UpsertOccurrence(ctx context.Context, occ extract.Occurrence) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.occurrences[occ.Fingerprint] = append(m.occurrences[occ.Fingerprint], occ)
	return nil
}

func (m *Memory) UpsertFingerprint(ctx context.Context, rec FingerprintRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev, ok := m.fps[rec.Fingerprint]
	if ok {
		if prev.FirstSeenAt.IsZero() || (!rec.FirstSeenAt.IsZero() && rec.FirstSeenAt.Before(prev.FirstSeenAt)) {
			prev.FirstSeenAt = rec.FirstSeenAt
		}
		if rec.LastSeenAt.After(prev.LastSeenAt) {
			prev.LastSeenAt = rec.LastSeenAt
		}
		prev.Class = rec.Class
		prev.Confidence = rec.Confidence
		if rec.TestName != "" {
			prev.TestName = rec.TestName
		}
		if rec.Framework != "" {
			prev.Framework = rec.Framework
		}
		if rec.Repo != "" {
			prev.Repo = rec.Repo
		}
		m.fps[rec.Fingerprint] = prev
		return nil
	}
	m.fps[rec.Fingerprint] = rec
	return nil
}

func (m *Memory) GetFingerprint(ctx context.Context, fingerprint string) (*FingerprintRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.fps[fingerprint]
	if !ok {
		return nil, nil
	}
	cpy := rec
	return &cpy, nil
}

func (m *Memory) ListRecentOccurrences(ctx context.Context, fingerprint string, limit int) ([]extract.Occurrence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.occurrences[fingerprint]
	if limit <= 0 || len(list) <= limit {
		out := make([]extract.Occurrence, len(list))
		copy(out, list)
		return out, nil
	}
	out := make([]extract.Occurrence, limit)
	copy(out, list[len(list)-limit:])
	return out, nil
}

func (m *Memory) LinkIssue(ctx context.Context, fingerprint string, issueNumber int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.fps[fingerprint]
	if !ok {
		return errors.New("fingerprint not found")
	}
	rec.IssueNumber = issueNumber
	m.fps[fingerprint] = rec
	return nil
}

func (m *Memory) Close() error { return nil }

type TiDBStore struct{}

func NewTiDBStore(cfg any) (*TiDBStore, error) {
	return nil, errors.New("tidb store not implemented")
}

func (t *TiDBStore) Migrate(ctx context.Context) error { return errors.New("tidb store not implemented") }
func (t *TiDBStore) UpsertOccurrence(ctx context.Context, occ extract.Occurrence) error {
	return errors.New("tidb store not implemented")
}
func (t *TiDBStore) UpsertFingerprint(ctx context.Context, rec FingerprintRecord) error {
	return errors.New("tidb store not implemented")
}
func (t *TiDBStore) GetFingerprint(ctx context.Context, fingerprint string) (*FingerprintRecord, error) {
	return nil, errors.New("tidb store not implemented")
}
func (t *TiDBStore) ListRecentOccurrences(ctx context.Context, fingerprint string, limit int) ([]extract.Occurrence, error) {
	return nil, errors.New("tidb store not implemented")
}
func (t *TiDBStore) LinkIssue(ctx context.Context, fingerprint string, issueNumber int) error {
	return errors.New("tidb store not implemented")
}
func (t *TiDBStore) Close() error { return nil }
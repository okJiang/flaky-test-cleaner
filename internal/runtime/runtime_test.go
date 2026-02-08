package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/config"
)

type fakeDiscovery struct {
	err   error
	calls int
}

func (f *fakeDiscovery) DiscoveryOnce(ctx context.Context) error {
	f.calls++
	return f.err
}

type fakeInteraction struct {
	err   error
	calls int
}

func (f *fakeInteraction) InteractionOnce(ctx context.Context) error {
	f.calls++
	return f.err
}

func TestRunOnce_ExecutesBothLoopsWhenEnabled(t *testing.T) {
	d := &fakeDiscovery{}
	i := &fakeInteraction{}
	rt, err := New(config.Config{RunOnce: true, DiscoveryInterval: time.Hour, InteractionInterval: time.Minute}, d, i)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d.calls != 1 {
		t.Fatalf("expected discovery once, got %d", d.calls)
	}
	if i.calls != 1 {
		t.Fatalf("expected interaction once, got %d", i.calls)
	}
}

func TestRunOnce_PropagatesDiscoveryError(t *testing.T) {
	want := errors.New("boom")
	d := &fakeDiscovery{err: want}
	i := &fakeInteraction{}
	rt, err := New(config.Config{RunOnce: true, DiscoveryInterval: time.Hour, InteractionInterval: time.Minute}, d, i)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = rt.Run(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
	if i.calls != 0 {
		t.Fatalf("interaction should not run after discovery error")
	}
}

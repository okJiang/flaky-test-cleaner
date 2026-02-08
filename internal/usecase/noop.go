package usecase

import "context"

// Noop is the minimal usecase used in phase A before real orchestration is wired.
type Noop struct{}

func NewNoop() *Noop { return &Noop{} }

func (n *Noop) DiscoveryOnce(ctx context.Context) error { return nil }

func (n *Noop) InteractionOnce(ctx context.Context) error { return nil }

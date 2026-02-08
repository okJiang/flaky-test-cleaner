package runtime

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/ports"
)

type Runtime struct {
	cfg         config.Config
	discovery   ports.DiscoveryUseCase
	interaction ports.InteractionUseCase
}

func New(cfg config.Config, discovery ports.DiscoveryUseCase, interaction ports.InteractionUseCase) (*Runtime, error) {
	if discovery == nil {
		return nil, fmt.Errorf("discovery use case is required")
	}
	if interaction == nil {
		return nil, fmt.Errorf("interaction use case is required")
	}
	return &Runtime{cfg: cfg, discovery: discovery, interaction: interaction}, nil
}

func (r *Runtime) Run(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("runtime is nil")
	}
	cfg := r.cfg
	if !cfg.RunOnce && cfg.DiscoveryInterval <= 0 && cfg.InteractionInterval <= 0 {
		return fmt.Errorf("daemon mode requires at least one of discovery/interaction to be enabled")
	}

	runDiscovery := func() error {
		if cfg.DiscoveryInterval <= 0 {
			return nil
		}
		cycleCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		return r.discovery.DiscoveryOnce(cycleCtx)
	}
	runInteraction := func() error {
		if cfg.InteractionInterval <= 0 {
			return nil
		}
		cycleCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		return r.interaction.InteractionOnce(cycleCtx)
	}

	if cfg.RunOnce {
		if err := runDiscovery(); err != nil {
			return err
		}
		if err := runInteraction(); err != nil {
			return err
		}
		return nil
	}

	var discoveryTicker *time.Ticker
	var interactionTicker *time.Ticker
	var discoveryCh <-chan time.Time
	var interactionCh <-chan time.Time

	if cfg.DiscoveryInterval > 0 {
		discoveryTicker = time.NewTicker(cfg.DiscoveryInterval)
		discoveryCh = discoveryTicker.C
		defer discoveryTicker.Stop()
		if err := runDiscovery(); err != nil {
			log.Printf("discovery cycle failed: %v", err)
		}
	}
	if cfg.InteractionInterval > 0 {
		interactionTicker = time.NewTicker(cfg.InteractionInterval)
		interactionCh = interactionTicker.C
		defer interactionTicker.Stop()
		if err := runInteraction(); err != nil {
			log.Printf("interaction cycle failed: %v", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-discoveryCh:
			if err := runDiscovery(); err != nil {
				log.Printf("discovery cycle failed: %v", err)
			}
		case <-interactionCh:
			if err := runInteraction(); err != nil {
				log.Printf("interaction cycle failed: %v", err)
			}
		}
	}
}

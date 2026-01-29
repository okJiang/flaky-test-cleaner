package runner

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/config"
)

func Run(ctx context.Context, cfg config.Config) error {
	if !cfg.RunOnce && cfg.DiscoveryInterval <= 0 && cfg.InteractionInterval <= 0 {
		return fmt.Errorf("daemon mode requires at least one of discovery/interaction to be enabled")
	}

	rt, cleanup, err := newRuntime(ctx, cfg, RunOnceDeps{})
	if err != nil {
		return err
	}
	defer func() { _ = cleanup() }()

	runDiscovery := func() error {
		cycleCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		return rt.DiscoveryOnce(cycleCtx)
	}
	runInteraction := func() error {
		cycleCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		return rt.InteractionOnce(cycleCtx)
	}

	if cfg.RunOnce {
		if cfg.DiscoveryInterval > 0 {
			if err := runDiscovery(); err != nil {
				return err
			}
		}
		if cfg.InteractionInterval > 0 {
			if err := runInteraction(); err != nil {
				return err
			}
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
		// Run immediately on startup.
		if err := runDiscovery(); err != nil {
			log.Printf("discovery cycle failed: %v", err)
		}
	}
	if cfg.InteractionInterval > 0 {
		interactionTicker = time.NewTicker(cfg.InteractionInterval)
		interactionCh = interactionTicker.C
		defer interactionTicker.Stop()
		// Run immediately on startup.
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

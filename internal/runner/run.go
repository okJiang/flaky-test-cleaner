package runner

import (
	"context"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/config"
)

func Run(ctx context.Context, cfg config.Config) error {
	if cfg.RunInterval <= 0 {
		return RunOnce(ctx, cfg)
	}

	ticker := time.NewTicker(cfg.RunInterval)
	defer ticker.Stop()

	for {
		if err := RunOnce(ctx, cfg); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

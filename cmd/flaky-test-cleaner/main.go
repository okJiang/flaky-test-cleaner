package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/runtime"
	"github.com/okJiang/flaky-test-cleaner/internal/usecase"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.FromEnvAndFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	rt, err := runtime.New(cfg, usecase.NewNoop(), usecase.NewNoop())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if err := rt.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("run failed: %v", err)
		os.Exit(1)
	}
}

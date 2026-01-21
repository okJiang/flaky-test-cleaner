package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/runner"
)

func main() {
	ctx := context.Background()
	cfg, err := config.FromEnvAndFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if err := runner.RunOnce(ctx, cfg); err != nil {
		log.Printf("run failed: %v", err)
		os.Exit(1)
	}
}

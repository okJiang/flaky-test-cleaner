SHELL := /bin/bash

GO ?= go
GOCACHE ?= /tmp/go-build-cache
FTC_MAIN ?= ./cmd/flaky-test-cleaner
FTC_BIN ?= bin/flaky-test-cleaner
FTC_ENV_FILE ?= .env

.PHONY: help check fmt fmt-check vet tidy test test/usecase test/race build \
	run run/once run/dry \
	clean clean/workspace clean/go-cache clean/issue

help:
	@echo "Targets:"
	@echo "  make check           # fmt-check + vet + test"
	@echo "  make test            # go test ./... -count=1"
	@echo "  make test/race       # go test -race ./... -count=1"
	@echo "  make test/usecase    # usecase package tests"
	@echo "  make fmt             # gofmt -w ."
	@echo "  make fmt-check       # fail if gofmt would change files"
	@echo "  make vet             # go vet ./..."
	@echo "  make tidy            # go mod tidy + download"
	@echo "  make build           # build binary to $(FTC_BIN)"
	@echo "  make run/dry         # run once in dry-run mode"
	@echo "  make run/once        # run once"
	@echo "  make run             # run as daemon"
	@echo "  make clean/workspace # delete local cache/worktrees dirs"
	@echo "  make clean/issue     # close validation issues in GitHub write repo"

check: fmt-check vet test

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)"

vet:
	GOCACHE="$(GOCACHE)" $(GO) vet ./...

tidy:
	$(GO) mod tidy
	$(GO) mod download

test:
	GOCACHE="$(GOCACHE)" $(GO) test ./... -count=1

test/race:
	GOCACHE="$(GOCACHE)" $(GO) test -race ./... -count=1

test/usecase:
	GOCACHE="$(GOCACHE)" $(GO) test ./internal/usecase -count=1 -run Test -v

build:
	@set -euo pipefail; \
	mkdir -p "$$(dirname "$(FTC_BIN)")"; \
	GOCACHE="$(GOCACHE)" $(GO) build -o "$(FTC_BIN)" "$(FTC_MAIN)"

run/dry:
	@set -euo pipefail; \
	if [ -f "$(FTC_ENV_FILE)" ]; then set -a; source "$(FTC_ENV_FILE)"; set +a; fi; \
	GOCACHE="$(GOCACHE)" $(GO) run "$(FTC_MAIN)" --once --dry-run

run/once:
	@set -euo pipefail; \
	if [ -f "$(FTC_ENV_FILE)" ]; then set -a; source "$(FTC_ENV_FILE)"; set +a; fi; \
	GOCACHE="$(GOCACHE)" $(GO) run "$(FTC_MAIN)" --once

run:
	@set -euo pipefail; \
	if [ -f "$(FTC_ENV_FILE)" ]; then set -a; source "$(FTC_ENV_FILE)"; set +a; fi; \
	GOCACHE="$(GOCACHE)" $(GO) run "$(FTC_MAIN)" --dry-run=false

clean:
	rm -rf bin

clean/workspace:
	rm -rf worktrees cache

clean/go-cache:
	@set -euo pipefail; \
	if [ -z "$(GOCACHE)" ] || [ "$(GOCACHE)" = "/" ] || [ "$(GOCACHE)" = "/tmp" ]; then \
		echo "refusing to remove unsafe GOCACHE=$(GOCACHE)"; \
		exit 1; \
	fi; \
	rm -rf "$(GOCACHE)"

clean/issue:
	@set -euo pipefail; \
	repo="$${FTC_CLEAN_REPO:-okJiang/pd}"; \
	ids=$$(gh issue list -R "$$repo" -l "flaky-test-cleaner/ai-managed" --state open -L 200 --json number --jq '.[].number'); \
	if [ -z "$$ids" ]; then echo "no issues to close in $$repo"; exit 0; fi; \
	for id in $$ids; do \
		echo "closing issue #$$id in $$repo"; \
		gh issue close "$$id" -R "$$repo" -c "cleanup: closing flaky-test-cleaner validation issue"; \
	done

# Makefile targets (2026-01-29)

## File
- `Makefile`

## Variables
- `GO` (default `go`)
- `GOCACHE` (default `/tmp/go-build-cache`)
- `FTC_MAIN` (default `./cmd/flaky-test-cleaner`)
- `FTC_BIN` (default `bin/flaky-test-cleaner`)
- `FTC_ENV_FILE` (default `.env`, loaded by `run/*` targets if present)

## Targets
- `make help`
- `make fmt` / `make fmt-check`
- `make vet`
- `make tidy`
- `make test` / `make test/race` / `make test/runner`
- `make build`
- `make run` / `make run/once` / `make run/dry`
- `make clean` / `make clean/workspace` / `make clean/go-cache`
- `make clean/issue` (uses `gh issue list/close`)

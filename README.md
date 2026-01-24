# flaky-test-cleaner

MVP implementation of the flaky test cleaner for `tikv/pd`.

## Quick start

```bash
export FTC_GITHUB_READ_TOKEN=...     # required
export FTC_GITHUB_ISSUE_TOKEN=...    # required unless --dry-run

# Scan upstream logs (read-only)
export FTC_GITHUB_OWNER=tikv
export FTC_GITHUB_REPO=pd

# Write issues/PRs to your fork (write)
export FTC_GITHUB_WRITE_OWNER=okjiang
export FTC_GITHUB_WRITE_REPO=pd

# Optional TiDB state store
export FTC_TIDB_ENABLED=true
export TIDB_HOST=127.0.0.1
export TIDB_PORT=4000
export TIDB_USER=root
export TIDB_PASSWORD=
export TIDB_DATABASE=flaky_test_cleaner
# Optional: when set, TLS is enabled and DSN uses tls=tidb
# export TIDB_CA_CERT_PATH=/path/to/ca.pem

go run ./cmd/flaky-test-cleaner --dry-run
```

## Configuration

Environment variables:
- `FTC_GITHUB_OWNER` (default `tikv`) — source repo for Actions logs (read-only)
- `FTC_GITHUB_REPO` (default `pd`)
- `FTC_GITHUB_WRITE_OWNER` (default: `FTC_GITHUB_OWNER`) — write repo for issues/PRs
- `FTC_GITHUB_WRITE_REPO` (default: `FTC_GITHUB_REPO`)
- `FTC_GITHUB_API_BASE_URL` (default `https://api.github.com`, useful for tests / GitHub Enterprise)
- `FTC_GITHUB_READ_TOKEN` (required)
- `FTC_GITHUB_ISSUE_TOKEN` (required unless `--dry-run`)
- `FTC_WORKFLOW_NAME` (default `PD Test`)
- `FTC_MAX_RUNS` (default `20`)
- `FTC_MAX_JOBS` (default `50`)
- `FTC_CONFIDENCE_THRESHOLD` (default `0.75`)
- `FTC_REQUEST_TIMEOUT` (default `30s`)
- `FTC_RUN_INTERVAL` (default `0`, run once)
- `FTC_TIDB_ENABLED` (default `false`)
- `FTC_BASE_BRANCH` (default `main`, branch used for opening PRs)
- `FTC_WORKSPACE_MIRROR` (default `cache/tikv-pd.git`, bare mirror path)
- `FTC_WORKSPACE_WORKTREES` (default `worktrees`, directory for git worktrees)
- `FTC_WORKSPACE_MAX` (default `2`, max concurrent worktrees)

# Optional: Copilot CLI SDK (Go)
- `FTC_COPILOT_SDK_ENABLED` (default `false`)
- `FTC_COPILOT_MODEL` (default `gpt-5`)
- `FTC_COPILOT_TIMEOUT` (default `60s`)
- `FTC_COPILOT_LOG_LEVEL` (default `error`)

Flags:
- `--dry-run` (default true)
- `--owner` / `--repo` (source repo for Actions logs)
- `--write-owner` / `--write-repo` (write repo for issues/PRs)
- `--interval`
- `--copilot-sdk`
- `--copilot-model`
- `--copilot-timeout`
- `--copilot-log-level`

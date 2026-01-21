# flaky-test-cleaner

MVP implementation of the flaky test cleaner for `tikv/pd`.

## Quick start

```bash
export FTC_GITHUB_READ_TOKEN=...     # required
export FTC_GITHUB_ISSUE_TOKEN=...    # required unless --dry-run

# Optional TiDB Cloud Starter
export FTC_TIDB_ENABLED=true
export TIDB_HOST=...
export TIDB_PORT=4000
export TIDB_USER=...
export TIDB_PASSWORD=...
export TIDB_DATABASE=flaky_test_cleaner
export TIDB_CA_CERT_PATH=/path/to/ca.pem

go run ./cmd/flaky-test-cleaner --dry-run
```

## Configuration

Environment variables:
- `FTC_GITHUB_OWNER` (default `tikv`)
- `FTC_GITHUB_REPO` (default `pd`)
- `FTC_GITHUB_READ_TOKEN` (required)
- `FTC_GITHUB_ISSUE_TOKEN` (required unless `--dry-run`)
- `FTC_WORKFLOW_NAME` (default `PD Test`)
- `FTC_MAX_RUNS` (default `20`)
- `FTC_MAX_JOBS` (default `50`)
- `FTC_CONFIDENCE_THRESHOLD` (default `0.75`)
- `FTC_REQUEST_TIMEOUT` (default `30s`)
- `FTC_RUN_INTERVAL` (default `0`, run once)
- `FTC_TIDB_ENABLED` (default `false`)
- `FTC_WORKSPACE_MIRROR` (default `cache/tikv-pd.git`, bare mirror path)
- `FTC_WORKSPACE_WORKTREES` (default `worktrees`, directory for git worktrees)
- `FTC_WORKSPACE_MAX` (default `2`, max concurrent worktrees)

Flags:
- `--dry-run` (default true)
- `--interval`

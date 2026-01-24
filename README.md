# flaky-test-cleaner

MVP implementation of the flaky test cleaner for `tikv/pd`.

## Quick start

Prereqs:
- Go 1.23+
- GitHub token(s):
  - `FTC_GITHUB_READ_TOKEN`: read Actions logs from the source repo (e.g. `tikv/pd`)
  - `FTC_GITHUB_ISSUE_TOKEN`: write issues/PRs to the write repo (e.g. your fork `okjiang/pd`) — required unless dry-run
- (Optional) TiDB: e.g. local `mysql --host 127.0.0.1 --port 4000 -u root`

```bash
# 1) Create your local env file (not committed; ignored by git)
cp .example.env .env

# 2) Edit .env and fill in at least:
#    - FTC_GITHUB_READ_TOKEN
#    - FTC_GITHUB_ISSUE_TOKEN (unless dry-run)
#    - FTC_GITHUB_OWNER/REPO (source) and FTC_GITHUB_WRITE_OWNER/REPO (write)

# 3) Load env vars from .env into the current shell
set -a; source ./.env; set +a

# 4) Run a safe dry-run first (no GitHub writes)
go run ./cmd/flaky-test-cleaner --dry-run

# 5) Real run (writes to GitHub write repo)
# Either set FTC_DRY_RUN=false in .env, or run without --dry-run:
# go run ./cmd/flaky-test-cleaner
```

## Dry-run

This project supports a safe "dry-run" mode.

- **Dry-run = true** (`FTC_DRY_RUN=true` or `--dry-run`):
  - Reads Actions logs from the source repo.
  - Runs extraction/classification and prints what it *would* do.
  - **Does not write to GitHub** (no issue/label/comment/PR creation or updates).
  - If TiDB is enabled, it will still write state to TiDB (occurrences/fingerprints/audit log), so you can validate parsing and dedup without touching GitHub.

- **Dry-run = false** (`FTC_DRY_RUN=false` and run without `--dry-run`):
  - Everything in dry-run, plus:
  - **Writes to the GitHub write repo**: creates/updates issues, ensures labels, posts IssueAgent comments, and (when approval signals exist) may proceed to FixAgent/PR flow.
  - Requires `FTC_GITHUB_ISSUE_TOKEN` with write permissions to the write repo.

Notes:
- The `--dry-run` flag overrides the `FTC_DRY_RUN` env var.
- Recommended workflow: start with dry-run, then switch to real run after you confirm the target repos/tokens are correct.

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

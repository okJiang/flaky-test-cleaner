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
go run ./cmd/flaky-test-cleaner --once --dry-run

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

## Overall workflow (when to do what)

This tool is designed to be run periodically (cron / GitHub Actions schedule / `--interval`). It scans failed GitHub Actions jobs on the **source repo** and manages issues/PRs on the **write repo**.

1) Configure once (you)
- Set `FTC_GITHUB_OWNER/REPO` (source; read Actions logs) and `FTC_GITHUB_WRITE_OWNER/REPO` (write; where issues/PRs are created).
- Make sure `FTC_BASE_BRANCH` matches the branch you want to scan/open PRs against (default: `master`; many repos use `main`).
- Run `--dry-run` first to verify the target repos/tokens.

2) Discovery & classification (tool)
- Lists failed workflow runs on the base branch, downloads failed job logs, extracts Go test failures, computes a fingerprint, and classifies each occurrence.
- `infra-flake` / `likely-regression` are recorded in the state store (if enabled) but **no issue is created/updated**.

3) Issue creation/update (tool; requires write token)
- For `flaky-test` / `unknown`, creates or updates a GitHub issue (labels + evidence table + log excerpts) in the write repo.
- On the first time a fingerprint appears, posts an initial IssueAgent analysis comment and transitions the fingerprint to “waiting for signal”.

4) Human gate (you)
- If you want the bot to proceed to the Fix/PR flow, approve it on the issue by either:
  - Adding label: `flaky-test-cleaner/ai-fix-approved`, or
  - Commenting: `/ai-fix`

5) FixAgent & PR flow (tool; requires GitHub write + git push capability)
- On the next non-dry-run execution after approval, FixAgent:
  - Leases a git worktree in `FTC_WORKSPACE_WORKTREES`,
  - Writes/updates `FIX_AGENT_TODO.md`, runs `go test ./...` (best-effort),
  - Creates branch `ai/fix/<fingerprint-prefix>` (first 12 chars), pushes it to the write repo, and opens a PR targeting `FTC_BASE_BRANCH`.
- Ensure your environment can `git push` to the write repo (e.g. `gh auth login` or a configured git credential helper), in addition to `FTC_GITHUB_ISSUE_TOKEN` for the GitHub API.

6) Review / CI feedback loop (you + tool)
- Review the PR normally:
  - In daemon mode, the tool will poll and follow up automatically.
  - In `--once` mode, re-run the tool to process new review/CI feedback.

7) Resolution (tool)
- If the PR is merged, the tool comments and closes the issue and marks the fingerprint as resolved.
- If the PR is closed without merging, the tool marks the fingerprint as `CLOSED_WONTFIX`.

Scheduling tips:
- Run once: `go run ./cmd/flaky-test-cleaner --once`
- Run continuously (default): `go run ./cmd/flaky-test-cleaner`
- Customize loops: `go run ./cmd/flaky-test-cleaner --discovery-interval 72h --interaction-interval 10m`
- Legacy: `--interval` sets both loops (deprecated)

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
- `FTC_RUN_ONCE` (default `false`)
- `FTC_DISCOVERY_INTERVAL` (default `72h`)
- `FTC_INTERACTION_INTERVAL` (default `10m`)
- `FTC_RUN_INTERVAL` (deprecated; sets both loops)
- `FTC_TIDB_ENABLED` (default `false`)
- `FTC_BASE_BRANCH` (default `master`, branch used for opening PRs)
- `FTC_WORKSPACE_MIRROR` (default `cache/tikv-pd.git`, bare mirror path)
- `FTC_WORKSPACE_WORKTREES` (default `worktrees`, directory for git worktrees)
- `FTC_WORKSPACE_MAX` (default `2`, max concurrent worktrees)

# Copilot CLI SDK (Go)
- `FTC_COPILOT_MODEL` (default `gpt-5`)
- `FTC_COPILOT_TIMEOUT` (default `60s`)
- `FTC_COPILOT_LOG_LEVEL` (default `error`)

Flags:
- `--dry-run` (default true)
- `--owner` / `--repo` (source repo for Actions logs)
- `--write-owner` / `--write-repo` (write repo for issues/PRs)
- `--once`
- `--discovery-interval`
- `--interaction-interval`
- `--interval` (deprecated)
- `--copilot-model`
- `--copilot-timeout`
- `--copilot-log-level`

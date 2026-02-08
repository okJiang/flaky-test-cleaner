# flaky-test-cleaner

AI-driven flaky test cleaner for `tikv/pd` (source repo) with issue/PR automation on a write repo.

## Quick start

Prereqs:
- Go 1.23+
- GitHub token(s):
  - `FTC_GITHUB_READ_TOKEN`: read Actions logs from source repo
  - `FTC_GITHUB_ISSUE_TOKEN`: write issues/PRs on write repo (required unless dry-run)
- Optional TiDB (MySQL protocol)

```bash
cp .example.env .env
set -a; source ./.env; set +a

# safe validation
go run ./cmd/flaky-test-cleaner --once --dry-run

# real run (writes to GitHub write repo)
go run ./cmd/flaky-test-cleaner --once --dry-run=false
```

## Runtime modes

- `--once`: run one discovery+interaction cycle then exit
- default (daemon): periodic loops
  - discovery loop interval: `FTC_DISCOVERY_INTERVAL` (default `72h`)
  - interaction loop interval: `FTC_INTERACTION_INTERVAL` (default `10m`)

## Workflow

1. Discovery scans failed workflow runs on base branch.
2. Extractor finds test failures and builds occurrences.
3. Fingerprint deduplicates failures.
4. Classifier marks each occurrence (`flaky-test`, `infra-flake`, `likely-regression`, `unknown`).
5. Issue manager creates/updates FTC-managed issues for `flaky-test`/`unknown`.
6. IssueAgent posts initial analysis comment.
7. Interaction loop waits for approval signal:
   - label: `flaky-test-cleaner/ai-fix-approved`
   - comment: `/ai-fix`
8. FixAgent opens/updates PR and handles review feedback.
9. On merge, issue is closed and state moves to `MERGED`.

## Key config

- Source repo: `FTC_GITHUB_OWNER`, `FTC_GITHUB_REPO`
- Write repo: `FTC_GITHUB_WRITE_OWNER`, `FTC_GITHUB_WRITE_REPO`
- Base branch: `FTC_BASE_BRANCH`
- Dry run: `FTC_DRY_RUN`
- TiDB store: `FTC_TIDB_ENABLED` + `TIDB_*`
- Workspace: `FTC_WORKSPACE_MIRROR`, `FTC_WORKSPACE_WORKTREES`, `FTC_WORKSPACE_MAX`
- Copilot IssueAgent (best-effort): `FTC_COPILOT_MODEL`, `FTC_COPILOT_TIMEOUT`, `FTC_COPILOT_LOG_LEVEL`

## State machine

`DISCOVERED -> ISSUE_OPEN -> TRIAGED -> WAITING_FOR_SIGNAL -> APPROVED_TO_FIX -> PR_OPEN -> PR_NEEDS_CHANGES -> PR_UPDATING -> PR_OPEN -> MERGED|CLOSED_WONTFIX`

## Architecture (rewrite branch)

- `internal/domain`: core entities and state enums
- `internal/ports`: abstract interfaces
- `internal/adapters/github`: GitHub API adapter
- `internal/adapters/store`: Memory + TiDB store adapter
- `internal/usecase`: discovery/interaction orchestration
- `internal/runtime`: loop scheduler
- Supporting modules: `extract`, `fingerprint`, `classify`, `issue`, `issueagent`, `fixagent`, `workspace`

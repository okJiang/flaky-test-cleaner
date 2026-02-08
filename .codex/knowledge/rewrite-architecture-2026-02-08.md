# Rewrite architecture baseline (2026-02-08)

## Branch/worktree facts
- Base commit: `6831cfcb1d04f34f8c9356bd12c1159b62debe16` (`generate spec`)
- New branch: `codex/rebuild-from-6831cfc`
- New worktree path:
  - `/Users/jiangxianjie/code/okjiang/flaky-test-cleaner.worktrees/copilot-worktree-2026-01-21T15-15-05/worktrees/rebuild-6831cfc`

## Codebase state at base commit
- Tracked files only:
  - `.codex/knowledge/spec-decisions.md`
  - `AGENTS.md`
  - `README.md`
  - `SPEC.md`
  - `WORK.md`
- No Go module / no implementation code.

## Target package layout for rewrite
- `cmd/flaky-test-cleaner`
- `internal/config`
- `internal/domain`
- `internal/ports`
- `internal/usecase`
- `internal/adapters/github`
- `internal/adapters/store`
- `internal/runtime`
- Existing domain-specific packages to be rebuilt:
  - `internal/extract`
  - `internal/fingerprint`
  - `internal/classify`
  - `internal/issue`
  - `internal/issueagent`
  - `internal/fixagent`
  - `internal/workspace`

## Compatibility decisions in rewrite
- Keep env/flag compatibility for:
  - `FTC_GITHUB_*`, `FTC_DISCOVERY_INTERVAL`, `FTC_INTERACTION_INTERVAL`, `FTC_DRY_RUN`, `FTC_BASE_BRANCH`, `FTC_WORKSPACE_*`, `FTC_TIDB_*`, `TIDB_*`, `FTC_COPILOT_*`
- Keep issue machine markers:
  - `<!-- FTC:* -->`
- Keep state names and progression semantics:
  - `DISCOVERED -> ISSUE_OPEN -> TRIAGED -> WAITING_FOR_SIGNAL -> APPROVED_TO_FIX -> PR_* -> MERGED/CLOSED_WONTFIX`
- Fingerprint:
  - Keep v1 input dimensions and add `fingerprint_version` in state record.

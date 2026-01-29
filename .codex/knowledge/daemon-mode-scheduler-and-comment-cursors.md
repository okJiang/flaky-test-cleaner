# Daemon mode, dual loops, and comment cursors (2026-01-29)

## Config (env/flags)

- File: `internal/config/config.go`
- New fields on `config.Config`:
  - `RunOnce` (env `FTC_RUN_ONCE`, flag `--once`)
  - `DiscoveryInterval` (env `FTC_DISCOVERY_INTERVAL`, flag `--discovery-interval`)
  - `InteractionInterval` (env `FTC_INTERACTION_INTERVAL`, flag `--interaction-interval`)
  - Legacy: `RunInterval` (env `FTC_RUN_INTERVAL`, flag `--interval`, deprecated; sets both loops)
- Defaults (when using `config.FromEnvAndFlags`):
  - `FTC_RUN_ONCE=false`
  - `FTC_DISCOVERY_INTERVAL=72h`
  - `FTC_INTERACTION_INTERVAL=10m`
- Precedence:
  - `--discovery-interval` / `FTC_DISCOVERY_INTERVAL` overrides legacy `--interval` / `FTC_RUN_INTERVAL`
  - `--interaction-interval` / `FTC_INTERACTION_INTERVAL` overrides legacy `--interval` / `FTC_RUN_INTERVAL`
  - Legacy interval fills any loop interval that wasn’t explicitly set, **only when it is a positive duration** (`>0`).

## Runner loops (daemon behavior)

- Entrypoint: `internal/runner/run.go` `Run(ctx, cfg)`
  - Backward compat:
    - If `DiscoveryInterval==0 && InteractionInterval==0 && RunInterval>0`, sets both loop intervals to `RunInterval`.
    - If all intervals are zero and `RunOnce=false`, falls back to `RunOnce(ctx, cfg)` (old “run once” behavior).
  - Initializes shared runtime via `newRuntime(ctx, cfg, RunOnceDeps{})`.
  - Runs:
    - `runtime.DiscoveryOnce(ctx)` on `DiscoveryInterval` ticker (and once immediately on startup).
    - `runtime.InteractionOnce(ctx)` on `InteractionInterval` ticker (and once immediately on startup).
  - Each cycle is wrapped with `context.WithTimeout(..., 30*time.Minute)`.
  - In daemon mode, per-cycle errors are logged and the process continues.

- Runtime init: `internal/runner/runtime.go` `newRuntime(ctx, cfg, deps)`
  - Accepts injected GitHub clients via `RunOnceDeps.GitHubRead/GitHubIssue` (used by tests).
  - Copilot SDK start is best-effort and is **skipped** when `cfg.CopilotModel` is empty.

- Cycle implementations: `internal/runner/cycles.go`
  - `(*runtime).DiscoveryOnce`: workflows/runs/jobs/logs → extract → classify → store → issue planning/apply → initial IssueAgent comment.
  - `(*runtime).InteractionOnce`: polls issue approval/comments (`WAITING_FOR_SIGNAL`) and drives FixAgent + PR review loop when needed.

## Graceful shutdown

- File: `cmd/flaky-test-cleaner/main.go`
  - Uses `signal.NotifyContext(..., os.Interrupt, syscall.SIGTERM)`.
  - Treats `context.Canceled` from `runner.Run` as a clean exit (exit code 0).

## Comment cursors (issue + PR)

- Store schema + struct:
  - File: `internal/store/store.go`
  - `store.FingerprintRecord` adds:
    - `LastIssueCommentID int64`
    - `LastPRCommentID int64`
  - TiDB table `fingerprints` adds columns:
    - `last_issue_comment_id BIGINT NOT NULL DEFAULT 0`
    - `last_pr_comment_id BIGINT NOT NULL DEFAULT 0`
  - `TiDBStore.UpsertFingerprint` updates the cursors using:
    - `last_* = GREATEST(last_*, VALUES(last_*))`

- Issue comment polling:
  - File: `internal/runner/run_once.go`
  - `checkApprovalSignals` now also:
    - Lists issue comments (`GET /issues/{n}/comments`)
    - Updates `FingerprintRecord.LastIssueCommentID` to the max comment ID
    - Records audit `signal.issue_comment` when new **human** comments arrive (filters `<!-- FTC:` blocks and `/ai-fix`).

- PR comment polling (PR issue comments):
  - File: `internal/runner/run_once.go`
  - `buildPRFeedback(ctx, cfg, gh, prNumber, sinceCommentID)`:
    - Lists PR issue comments via `ListIssueComments` on `number=prNumber`
    - Filters bot-managed comments via `isFTCManagedBody` (`<!-- FTC:`)
    - Returns `fixagent.PRFeedback.NewIssueComments` + `LatestIssueCommentID`.
  - `handlePRFeedbackLoop`:
    - Passes `FingerprintRecord.LastPRCommentID` as `sinceCommentID`
    - After successful follow-up, persists `LastPRCommentID = LatestIssueCommentID`.

## FixAgent surface for PR comments

- File: `internal/fixagent/agent.go`
  - `PRFeedback` adds:
    - `LatestIssueCommentID int64`
    - `NewIssueComments []github.IssueComment`
  - `PRFeedback.NeedsUpdate()` returns true when `NewIssueComments` is non-empty.
  - `renderFeedbackChecklist` appends a `### PR comments` checklist to `FIX_AGENT_TODO.md`.

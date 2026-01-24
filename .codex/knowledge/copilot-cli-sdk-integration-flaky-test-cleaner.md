# Copilot CLI SDK integration in this repo (Go)

## Intent
Use Copilot CLI SDK (Go) to improve **IssueAgent initial analysis** comment generation, while keeping automation safe.

## Integration points (code locations)
- Config: `internal/config/config.go`
  - Adds opt-in flags/env for enabling Copilot SDK and choosing model/timeout.
- Copilot SDK wrapper: `internal/copilotsdk/*`
  - Starts/stops Copilot client once per run.
  - Runs a single prompt in a short-lived session and waits for `session.idle`.
- Runner usage: `internal/runner/run_once.go`
  - In `runInitialAnalysis(...)`, if enabled, tries Copilot-generated comment first; on error falls back to deterministic heuristic comment.
- Prompt builder: `internal/issueagent/*` (prompt-only helpers)
  - Builds a constrained system+user prompt.
  - Requires output to contain the HTML markers:
    - `<!-- FTC:ISSUE_AGENT_START -->`
    - `<!-- FTC:ISSUE_AGENT_END -->`

## Safety / defaults
- Feature is **disabled by default**.
- When enabled, prompts instruct:
  - “Do not call tools / do not modify files”
  - “Return markdown only”
  - “Must include FTC markers for idempotent parsing”

## Runtime requirements
- Local machine must have `copilot` CLI installed and authenticated.
- A Copilot subscription (or CLI free tier with limited usage) is required.

## How to enable (expected)
- `FTC_COPILOT_SDK_ENABLED=true`
- `FTC_COPILOT_MODEL=gpt-5` (or other Copilot CLI-supported model)
- `FTC_COPILOT_TIMEOUT=2m`

(Exact keys live in `internal/config/config.go` as the source of truth.)

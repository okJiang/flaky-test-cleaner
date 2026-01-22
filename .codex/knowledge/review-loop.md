# Review Loop (Task 5.2) — Facts & Code Locations

## Goal (SPEC §9.3, §8.3)
Implement FixAgent “review follow-up” loop:
- Detect PR feedback signals (review changes requested, CI failures)
- Transition state machine: `PR_OPEN -> PR_NEEDS_CHANGES -> PR_UPDATING -> PR_OPEN`
- Produce a machine-managed PR comment and update a workspace TODO file

## GitHub API endpoints

### PR details
- `GET /repos/{owner}/{repo}/pulls/{pull_number}`
- Required fields:
  - `number`, `html_url`, `state`, `merged`, `merged_at`
  - `head.sha`, `head.ref` (needed for CI status + branch ops)

### PR reviews (for CHANGES_REQUESTED)
- `GET /repos/{owner}/{repo}/pulls/{pull_number}/reviews`
- Useful fields:
  - `state` (e.g. `CHANGES_REQUESTED`, `APPROVED`, `COMMENTED`)
  - `body`, `user.login`, `submitted_at`

### Commit combined status (CI failure signal)
- `GET /repos/{owner}/{repo}/commits/{ref}/status`
- Useful fields:
  - `state` (e.g. `success`, `failure`, `error`, `pending`)
  - `statuses[]`: `context`, `state`, `description`, `target_url`, `updated_at`

## State machine rules (internal/store)
- States are defined in `internal/store/store.go`.
- Allowed transitions include:
  - `PR_OPEN -> PR_NEEDS_CHANGES`
  - `PR_NEEDS_CHANGES -> PR_UPDATING`
  - `PR_UPDATING -> PR_OPEN`

## Planned implementation touch points

### GitHub client
- File: `internal/github/client.go`
- Add methods:
  - `ListPullRequestReviews(owner, repo, prNumber)`
  - `GetCombinedStatus(owner, repo, ref)`
- Extend `PullRequest` struct to include `Head.SHA` and `Head.Ref`.

### Runner
- File: `internal/runner/run_once.go`
- Add a loop that:
  - Reads `PR_OPEN` fingerprints, checks review+CI signals, transitions to `PR_NEEDS_CHANGES` when necessary
  - Reads `PR_NEEDS_CHANGES` fingerprints, invokes FixAgent follow-up and drives `PR_UPDATING -> PR_OPEN`

### FixAgent
- File: `internal/fixagent/agent.go`
- Add a follow-up method that:
  - Checks out existing `ai/fix/<fingerprint-short>` branch
  - Updates `FIX_AGENT_TODO.md` with extracted feedback items
  - Commits + pushes a small update commit
  - Posts/updates a PR comment using a guarded block marker (e.g. `FTC:REVIEW_RESPONSE_*`)
  - Records audit log events

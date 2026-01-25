# validate.log scope filters

## Base-branch only
- Requirement: ignore failures from PR branches and `release-*` cherry-picks; only consider base branch (default `main`).
- Implementation:
  - `internal/runner/run_once.go`: ListWorkflowRuns uses `Branch: cfg.GitHubBaseBranch` and `Event: "push"`.
  - `internal/github/client.go`: ListWorkflowRunsOptions supports `Branch` and `Event` query params; `WorkflowRun` includes `HeadBranch` and `Event` fields.

## Regression vs flaky
- Requirement: CI failures due to incomplete code / compile errors should not be treated as flaky.
- Implementation:
  - `internal/runner/run_once.go`: skip issue create/update when classifier returns `likely-regression` (still upserts occurrence/fingerprint).

## Parent/subtest dedupe
- Requirement: if failures include both parent test and subtest, only keep leaf test.
- Implementation:
  - `internal/extract/extract.go`: `dropParentTests` filters out parent tests when any subtest exists.
  - Tests: `internal/extract/extract_dedup_test.go`.

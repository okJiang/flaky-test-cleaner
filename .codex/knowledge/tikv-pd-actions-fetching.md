# tikv/pd GitHub Actions Fetching (Validated)

## Validation date
- 2026-01-21

## Tooling used
- `gh api` with authenticated token scopes including `workflow` and `repo`

## Endpoints and purpose

### List workflows
- `GET /repos/tikv/pd/actions/workflows`
- Used to obtain `workflow_id` and workflow names.

### List failed runs for a workflow
- `GET /repos/tikv/pd/actions/workflows/{workflow_id}/runs?status=failure&per_page=N`
- Example workflow ids seen:
  - `Check PD` -> `3944072`
  - `PD Test` -> `3933317`

### List jobs for a run
- `GET /repos/tikv/pd/actions/runs/{run_id}/jobs?per_page=N`
- Used to locate jobs whose `conclusion == failure`.

### Download job logs
- `GET /repos/tikv/pd/actions/jobs/{job_id}/logs`
- Observed behavior:
  - HTTP 200
  - `Content-Type: text/plain`
  - Can be stored as a `.txt` file and parsed line-by-line.

## Evidence that flaky detection is feasible

- A failing job from `PD Test` contained stable test-failure markers searchable via regex:
  - `[FAIL]`
  - `--- FAIL:`
  - `panic:`
  - `timeout`
  - `DATA RACE`

## Implementation notes
- Prefer parsing job logs rather than run logs (zip) to reduce payload size.
- Extract:
  - `test_name` when present
  - `error_signature` (normalized)
  - excerpts around failure markers (fixed window)
- Use these to compute fingerprint and feed the classifier.

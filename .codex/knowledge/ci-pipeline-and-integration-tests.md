# CI pipeline & integration tests (facts)

## Added files

- GitHub Actions workflow: `.github/workflows/ci.yml`
  - Triggers: `push` (main), `pull_request` (main), `workflow_dispatch`
  - Steps: gofmt check, go vet, go test

- Test design doc: `TEST.md`

- Runner integration test: `internal/runner/integration_test.go`
  - Uses `httptest.Server` as a stub GitHub API
  - Stubs endpoints used by discovery + issue creation:
    - `GET /repos/{owner}/{repo}/actions/workflows`
    - `GET /repos/{owner}/{repo}/actions/workflows/{workflow_id}/runs`
    - `GET /repos/{owner}/{repo}/actions/runs/{run_id}/jobs`
    - `GET /repos/{owner}/{repo}/actions/jobs/{job_id}/logs`
    - `POST /repos/{owner}/{repo}/labels` (returns 422 for idempotence)
    - `POST /repos/{owner}/{repo}/issues`
    - `POST /repos/{owner}/{repo}/issues/{issue}/comments`
    - `GET /repos/{owner}/{repo}/issues/{issue}`
    - `GET /repos/{owner}/{repo}/issues/{issue}/comments`
  - Injects a Memory store via `runner.RunOnceWithDeps` and asserts:
    - one fingerprint reaches `WAITING_FOR_SIGNAL`
    - issue number is linked

## Code changes enabling integration tests

### GitHub API base URL override

- `internal/config/config.go`
  - Adds `Config.GitHubAPIBaseURL`
  - Env: `FTC_GITHUB_API_BASE_URL` (default `https://api.github.com`)
  - Flag: `--github-api-base-url`

- `internal/github/client.go`
  - Adds `github.NewClientWithBaseURL(token, timeout, baseURL)`
  - `NewClient` now delegates to it

- `internal/runner/run_once.go`
  - Uses `NewClientWithBaseURL` for both read and issue clients

### Runner dependency injection + lazy fixagent

- `internal/runner/run_once.go`
  - Adds `RunOnceDeps` and `RunOnceWithDeps(ctx, cfg, deps)`
  - `RunOnce` remains the default entrypoint (calls `RunOnceWithDeps`)
  - Lazily initializes `workspace.Manager` + `fixagent.Agent` only when Store has fingerprints in fix/PR states
  - Helper: `needsFixAgent(ctx, st)`


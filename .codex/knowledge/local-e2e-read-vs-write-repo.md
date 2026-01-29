# Local E2E: source repo vs write repo

## Config

- Source repo (read-only; Actions/logs):
  - `internal/config/config.go`
    - fields: `Config.GitHubOwner`, `Config.GitHubRepo`
    - env: `FTC_GITHUB_OWNER`, `FTC_GITHUB_REPO`
    - flags: `--owner`, `--repo`

- Write repo (write; issues/PRs/labels/comments/workspace):
  - `internal/config/config.go`
    - fields: `Config.GitHubWriteOwner`, `Config.GitHubWriteRepo`
    - env: `FTC_GITHUB_WRITE_OWNER`, `FTC_GITHUB_WRITE_REPO` (default fallback to source owner/repo)
    - flags: `--write-owner`, `--write-repo`
    - remote URL helper: `Config.WriteRepoRemoteURL()`

## Runner wiring

- `internal/runner/run_once.go`
  - Actions fetch uses source repo:
    - `FindWorkflowByName(ctx, cfg.GitHubOwner, cfg.GitHubRepo, ...)`
    - `ListWorkflowRuns(ctx, cfg.GitHubOwner, cfg.GitHubRepo, ...)`
    - `ListRunJobs(ctx, cfg.GitHubOwner, cfg.GitHubRepo, ...)`
    - `DownloadJobLogs(ctx, cfg.GitHubOwner, cfg.GitHubRepo, ...)`
  - Issue manager uses write repo:
    - `issue.NewManager(issue.Options{ Owner: cfg.GitHubWriteOwner, Repo: cfg.GitHubWriteRepo, ... })`
  - FixAgent + RepoWorkspaceManager use write repo:
    - `workspace.NewManager(workspace.Options{ RemoteURL: cfg.WriteRepoRemoteURL(), ... })`
    - `fixagent.New(fixagent.Options{ Owner: cfg.GitHubWriteOwner, Repo: cfg.GitHubWriteRepo, ... })`

## Goal

During validation:
- Scan upstream failures (e.g. `tikv/pd`) via `FTC_GITHUB_OWNER/REPO`.
- Create issues/PRs only in fork (e.g. `okjiang/pd`) via `FTC_GITHUB_WRITE_OWNER/REPO`.

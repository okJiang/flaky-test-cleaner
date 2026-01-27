# IssueAgent code-aware analysis (RepoContextSnippets)

## Goal
Make IssueAgent comments approvable by maintainers by grounding analysis in *actual repo code* at the failing commit SHA.

## Code locations
- Repo context builder: `internal/runner/issueagent_repo_context.go`
  - Entry: `buildIssueAgentRepoContext(ctx, ws, occ)`
  - Reads failing SHA from occurrences: `pickHeadSHA(occ)`
  - Reads files from git mirror via `workspace.Manager` (read-only):
    - `ws.HasPath(ctx, sha, path)`
    - `ws.CatFile(ctx, sha, path)`
    - `ws.Grep(ctx, sha, pattern)`
  - Output format: a markdown section starting with:
    - `RepoContextSnippets (read-only, from failing commit):`
  - Snippet labeling: `S1`, `S2`, ...
    - Example line: `- S1: pkg/foo/bar.go@abcdef1 L10-L20`

- IssueAgent comment (deterministic fallback): `internal/issueagent/issueagent.go`
  - Input now includes `RepoContextSnippets string`
  - Rendered section (if present): `## Repo Context (from failing commit)`

- Runner integration: `internal/runner/run_once.go`
  - `runInitialAnalysis(...)` always attempts repo context (best-effort) when `cfg.GitHubAPIBaseURL == "https://api.github.com"`.
  - The deterministic IssueAgent comment includes RepoContextSnippets.
  - If Copilot SDK is enabled and returns a valid FTC-marked block, it overrides the body.

- Copilot prompt/system constraints: `internal/issueagent/copilot_prompt.go`
  - System message requires:
    - cite snippet IDs ("S1") and file+line ranges
    - provide concrete reproduction commands
    - provide concrete patch plan and optional diff sketch
    - end with a maintainer approval checklist

## Current extraction logic
- Primary: parse `path.go:line` patterns from `occ.ErrorSignature + "\n" + occ.Excerpt` (up to first 3 occurrences), then slice ±40 lines.
- Fallback: locate `func TestX` by grepping for `func <baseTestName>`.
- Snippet cap: up to 3 snippets.

## Notes / limits
- Everything is read-only (git mirror operations); no checkout/worktree used for IssueAgent.
- Repo context is best-effort; on any git/workspace failure returns empty string.

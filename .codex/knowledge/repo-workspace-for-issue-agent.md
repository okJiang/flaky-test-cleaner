# RepoWorkspace for IssueAgent (Read Code On Demand)

## Goal
Enable IssueAgent to read `tikv/pd` source code *only when needed* for analysis/interaction, while keeping the default path cheap, fast, and safe (read-only).

## Key principle
- Prefer **no checkout** and **no full clone** for IssueAgent.
- Bind code context to the CI failure’s `head_sha` to avoid analyzing the wrong revision.

## Inputs
- `head_sha`: commit SHA of the failing workflow run (`workflow_runs[].head_sha`).
- Optional:
  - `stack_paths`: `path:line` pairs extracted from logs.
  - `test_name`: extracted test identifier (Go: `--- FAIL:` blocks or `[FAIL]` markers).
  - `fingerprint_short`: for naming worktree lease if needed.

## RepoWorkspaceManager: recommended implementation

### Storage layout
- Bare mirror: `cache/tikv-pd.git` (single shared mirror)
- Worktrees (rare for IssueAgent): `worktrees/<fingerprint-short>/`

### Mirror lifecycle
- Initialize once:
  - `git clone --mirror https://github.com/tikv/pd.git cache/tikv-pd.git`
- Refresh periodically:
  - `git -C cache/tikv-pd.git fetch --prune`

### Read-only operations (preferred path)
IssueAgent should use these operations against the mirror; they do not require a checkout.

1) Check if a path exists
- `git -C cache/tikv-pd.git cat-file -e <sha>:<path>`

2) Read a full file
- `git -C cache/tikv-pd.git show <sha>:<path>`

3) Read a directory listing under a prefix
- `git -C cache/tikv-pd.git ls-tree -r --name-only <sha> -- <prefix>`

4) Search for patterns/symbols
- `git -C cache/tikv-pd.git grep -n --no-color -e '<pattern>' <sha> -- <scope>`

5) Get blame-like context (optional; use sparingly)
- Prefer `git log -L` only if necessary due to cost.

### Concurrency & safety
- Mirror is shared read-only for IssueAgent; concurrent reads are safe.
- RepoWorkspaceManager should gate any mutation (fetch) with a simple lock (file lock) if needed.

## When IssueAgent needs a worktree
Only lease a worktree when IssueAgent must run tooling that requires a checkout:
- `go test` / `go list` / `go env` validation
- Cross-file navigation that is hard/slow with `git grep` alone (rare)
- Build-tag or generated files inspection that is easier in a working tree

### Worktree lease (read-only)
- Create:
  - `git -C cache/tikv-pd.git worktree add worktrees/<fingerprint-short> <sha>`
- Concurrency limit:
  - Max K worktrees concurrently (default K=2)
- Cleanup:
  - Remove worktree after TTL (default 7 days idle) while keeping the mirror.

## Repo Context Builder (how to decide what to read)

### 1) If stack traces contain `path:line`
- For each `path:line`:
  1) `git show <sha>:<path>`
  2) Slice around the line (e.g., ±60 lines) in-memory
  3) Provide the snippet to LLM with:
     - file path
     - commit sha
     - line range

### 2) If only `test_name` exists (no stack path)
- Use `git grep` to locate the test definition/registration:
  - Search `TestRuleTestSuite` or suite name
  - Search for `func Test...` or suite runner
- Read the most relevant files (test file + helpers) using `git show`.

### 3) Keep context small and relevant
- Prefer 1–3 files, each with short snippets.
- Do not dump entire files unless the file is very small.

## Allowed command whitelist (IssueAgent)
- `git show`, `git grep`, `git ls-tree`, `git cat-file -e`, `git rev-parse` (read-only)
- Optional: `git log -L` (guarded; expensive)

## Disallowed for IssueAgent
- Any write operations:
  - `git checkout`, `git commit`, `git push`, editing files
- Creating PRs or branches

## Notes
- FixAgent uses separate write-capable worktree and code token; IssueAgent must remain read-only.
- If `head_sha` is missing (rare), fall back to the PR head SHA via GitHub API.

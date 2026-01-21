## MVP Go implementation layout (2026-01-21)

- `cmd/flaky-test-cleaner/main.go` wires `config.FromEnvAndFlags` with `runner.Run`, using `context.Background()` and exits non-zero on error.
- `internal/config/config.go` merges env + flags; enforces `FTC_GITHUB_READ_TOKEN`, optional issue token when `--dry-run=false`, and TiDB TLS inputs when `FTC_TIDB_ENABLED=true`.
- `internal/runner/run_once.go` orchestrates the MVP loop:
  - Builds GitHub clients (read + optional write), store (`Memory` or `TiDB`), extractor (`GoTestExtractor`), classifier (`Heuristic`), and `issue.Manager`.
  - For each failed workflow run/job it downloads logs, extracts occurrences, sanitizes excerpts, normalizes signatures, computes fingerprint v1 (`internal/fingerprint`), and persists via `store.Store`.
  - Infra flakes (`classify.ClassInfraFlake`) are dropped before issue planning.
  - Issue planning uses `issue.Manager.PlanIssueUpdate` with last 5 occurrences and applies changes (respecting `--dry-run`).
- `internal/github/client.go` currently implements the verified endpoints from SPEC §6.5 (workflow lookup, runs, jobs, job logs) plus Issue CRUD + label ensures, with retry on HTTP 429/5xx and RunnerOS detection via `labels`.
- `internal/extract/extract.go` detects go test failure markers (`--- FAIL:`, `[FAIL]`, `panic:`, `DATA RACE`, `timeout`), captures ±40 line windows (max 120 lines), and deduplicates by `test/error` pair before emitting `extract.Occurrence`.
- `internal/classify/classify.go` heuristic classifier tags infra keywords, regression keywords, or flaky keywords with confidences (0.8–0.9) and leaves unknown at 0.5.
- `internal/store/store.go`:
  - `Memory` store for dry-runs/tests.
  - `TiDBStore` migrates schema (tables `occurrences`, `fingerprints`, `audit_log`, `costs` per SPEC §10) and now keeps `state` + `state_changed_at` columns plus `Store.UpdateFingerprintState` guardrails (`DISCOVERED → ISSUE_OPEN → TRIAGED → WAITING_FOR_SIGNAL → ...`).
- `internal/issue/issue.go` produces deterministic titles (`[flaky] <test> — <sig>`), wraps body sections in `<!-- FTC:NAME_START -->` comments, and ensures labels prefixed with `flaky-test-cleaner/`.
- `internal/issueagent/issueagent.go` (Task 3.2) renders the initial AI analysis comment with guarded block markers, heuristic hypotheses (panic/timeout/race/network), reproduction commands (`go test ./... -run '^TestName$' -count=30 -race`), next actions, risk notes, and evidence bullets.

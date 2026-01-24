# validate.log false positives: timeout noise

## Symptom (validate.log)
- Many issue updates with `unknown-test` and fingerprints driven by lines containing `election-timeout`, `lease-timeout`, etc.
- These are etcd config/log fields, not go test failures.

## Root cause
- `internal/extract/extract.go`: timeout detection was too broad (matching any `timeout` token), causing non-failure log lines (e.g. `election-timeout`, `lease-timeout`) to be treated as failures.

## Fix
- `internal/extract/extract.go`:
  - Tighten timeout matching to real test-timeout/deadline signals only (e.g. `panic: test timed out after`, `test timed out after`, `context deadline exceeded`, `deadline exceeded`).
  - Improve test name inference for `[FAIL]`/panic/timeout by backtracking to nearest `--- FAIL:` / `=== RUN` / `[FAIL]` markers.
  - If test name cannot be inferred, skip the occurrence (prevents `unknown-test` explosion).

## Regression coverage
- `internal/extract/extract_test.go:TestGoTestExtractorIgnoresTimeoutNoise`
- Fixture: `internal/extract/testdata/noise-timeout.log`

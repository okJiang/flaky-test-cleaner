# Issue formatting + cleanup

## Created issues (validation fork)
Repo: okJiang/pd
Label used: flaky-test-cleaner/ai-managed

`make clean/issue` closes open issues with this label.

## Improvements implemented (2026-01-25)
- Occurrence timestamp: use WorkflowRun.CreatedAt (Runner input OccurredAt), so issue First/Last seen reflect real run time.
- Fingerprint normalization: `NormalizeErrorSignature` now strips RFC3339 timestamps to reduce duplicates.
- Stored occurrence signature: keep raw-ish signature; only use normalized signature for fingerprint.
- Issue Evidence table: includes OS column (RunnerOS).
- Issue title/signature readability: strip leading Actions timestamp in `summarizeSignature`.

Files:
- internal/runner/run_once.go
- internal/fingerprint/fingerprint.go
- internal/issue/issue.go
- internal/extract/extract.go
- Makefile (clean/issue)

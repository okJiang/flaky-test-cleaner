# Rewrite implementation map (2026-02-08)

## Entrypoint and runtime
- Main:
  - `cmd/flaky-test-cleaner/main.go`
  - loads config via `config.FromEnvAndFlags`
  - creates `usecase.Service` via `usecase.NewService`
  - starts scheduler via `runtime.New(...).Run(ctx)`
- Scheduler:
  - `internal/runtime/runtime.go`
  - supports `RunOnce` and daemon dual-loop scheduling
  - loop-level timeout: 30 minutes per cycle

## Core architecture packages
- Domain model:
  - `internal/domain/types.go`
  - includes `Occurrence`, `FingerprintRecord`, `Classification`, state enums, issue/PR models
- Ports:
  - `internal/ports/interfaces.go`
  - CI / Issue / Store / Workspace / Analysis interfaces
- Use cases:
  - `internal/usecase/service.go`
  - `DiscoveryOnce`: workflows -> runs -> jobs -> logs -> extraction -> classification -> store -> issue
  - `InteractionOnce`: approval signal -> fixagent -> review loop -> PR status closeout

## Adapters
- GitHub adapter:
  - `internal/adapters/github/client.go`
  - methods include Actions endpoints, issue CRUD, comments, PR/review/status APIs
  - retry policy for 429/502/503/504 with `Retry-After`
- Store adapter:
  - `internal/adapters/store/store.go`
  - `Memory` implementation for tests/dry validation
  - `TiDBStore` implementation with migrate/upsert/list APIs
  - state transition guard in `validateTransition`
  - adds `fingerprint_version` field (`v1` default)

## Signal extraction and planning
- Extractor:
  - `internal/extract/extract.go`
  - detects `--- FAIL`, `[FAIL]`, `panic`, `DATA RACE`, test timeout/deadline signals
  - supports Actions group window clipping (`##[group]` / `##[endgroup]`)
  - drops parent tests when subtests exist (`dropParentTests`)
- Fingerprint:
  - `internal/fingerprint/fingerprint.go`
  - `NormalizeErrorSignature` and `V1` hash
- Classifier:
  - `internal/classify/classify.go`
  - outputs `flaky-test`, `infra-flake`, `likely-regression`, `unknown`
- Issue body planning:
  - `internal/issue/issue.go`
  - FTC machine blocks: SUMMARY/EVIDENCE/EXCERPTS/AUTOMATION

## Agent behavior
- IssueAgent:
  - `internal/issueagent/issueagent.go`
  - deterministic initial analysis with hypotheses/repro/fix/risk sections
- Copilot wrapper:
  - `internal/copilotsdk/copilotsdk.go`
  - optional best-effort generation + fallback
- Repo context snippets for IssueAgent:
  - `internal/usecase/issueagent_repo_context.go`
  - reads source mirror at failing SHA and creates `S1/S2/...` snippets

## Fix and review loop
- Workspace manager:
  - `internal/workspace/manager.go`
  - mirror ensure/fetch + worktree lease acquire/release
- FixAgent:
  - `internal/fixagent/agent.go`
  - `Attempt`: prepare todo + test + create branch/commit/push/PR
  - `FollowUp`: consume review/CI/PR comments and append checklist
- Usecase PR loop:
  - `internal/usecase/service.go`
  - feedback transition: `PR_OPEN -> PR_NEEDS_CHANGES -> PR_UPDATING -> PR_OPEN`
  - terminal: merged => `MERGED`, closed without merge => `CLOSED_WONTFIX`

## Tests and CI
- Unit + integration tests by package:
  - `internal/*/*_test.go`, `internal/usecase/*integration_test.go`
- CI workflow:
  - `.github/workflows/ci.yml`
  - gofmt/vet/test
- Local commands:
  - `Makefile`
  - includes `check`, `test`, `run/dry`, `run/once`, `run`

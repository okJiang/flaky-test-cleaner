# TEST

## Goal

- CI must stay deterministic without external network dependencies.
- Cover core flows: discovery, issue creation/update, approval signal, state progression.

## Layers

### Unit tests

- `internal/config`: env/flag validation
- `internal/extract`: failure extraction, timeout noise filtering, excerpt windows
- `internal/fingerprint`: normalization + hash stability
- `internal/classify`: heuristic class mapping
- `internal/issue`: FTC block rendering and issue planning
- `internal/issueagent`: analysis comment sections and required markers
- `internal/adapters/store`: memory behavior + TiDB DSN behavior
- `internal/workspace`: mirror/worktree lifecycle
- `internal/fixagent`: comment/checklist rendering
- `internal/adapters/github`: retry/body consistency + query encoding

### Integration tests

- `internal/usecase/integration_test.go`
  - workflows -> runs -> jobs -> logs -> issue create -> issue comment
  - verifies fingerprint reaches `WAITING_FOR_SIGNAL`
- `internal/usecase/infra_integration_test.go`
  - infra failure is recorded but does not create issue
- `internal/usecase/approval_integration_test.go`
  - approval via label and `/ai-fix` comment moves state to `APPROVED_TO_FIX`

## Run

```bash
go test ./... -count=1
```

## CI

- File: `.github/workflows/ci.yml`
- Steps: `gofmt` check, `go vet`, `go test ./... -count=1`

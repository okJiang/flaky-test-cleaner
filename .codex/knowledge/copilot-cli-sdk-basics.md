# Copilot CLI SDK — Basics (Source Facts)

## Upstream
- Repo: https://github.com/github/copilot-sdk
- Status: **Technical Preview** (may introduce breaking changes)
- Purpose: Programmatic access to the **same agent runtime behind Copilot CLI**.

## Architecture
- All SDKs talk to **Copilot CLI server mode** via **JSON-RPC**.

```
Your Application
       ↓
  SDK Client
       ↓ JSON-RPC
  Copilot CLI (server mode)
```

- SDK can manage the Copilot CLI process lifecycle automatically; it can also connect to an external CLI server.

## Prerequisites / Requirements
- Copilot CLI must be installed and available in `PATH` (or configured via language SDK options).
- A GitHub Copilot subscription is required to use the SDK (the Copilot CLI free tier exists but has limited usage).

## Default tool surface (security note)
- Upstream FAQ states: by default the SDK runs the equivalent of **Copilot CLI `--allow-all`**, enabling first‑party tools (filesystem/git/web).
- Implication for this repo: if we use the SDK to generate issue comments, prompts must strongly instruct “no tool calls / no filesystem changes”, and the feature should be **opt-in**.

## Billing model
- SDK usage is billed like Copilot CLI prompts (counts against premium request quota).

## Go SDK pointer
- Go SDK installation: `go get github.com/github/copilot-sdk/go`
- Go SDK API reference entrypoint: https://github.com/github/copilot-sdk/blob/main/go/README.md

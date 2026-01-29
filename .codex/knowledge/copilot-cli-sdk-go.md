# Copilot CLI SDK — Go usage notes (Facts + API)

Upstream ref: https://github.com/github/copilot-sdk/blob/main/go/README.md

## Install
```bash
go get github.com/github/copilot-sdk/go
```

## Core types
### Client
- Create: `client := copilot.NewClient(options *copilot.ClientOptions)`
- Start CLI server: `client.Start()`
- Stop CLI server: `client.Stop() []error` (upstream doc: returns array of errors)
- Force stop: `client.ForceStop()`
- Create session: `client.CreateSession(config *copilot.SessionConfig) (*copilot.Session, error)`
- Resume session: `client.ResumeSession(sessionID string) (*copilot.Session, error)`
- Ping: `client.Ping(message string) (*PingResponse, error)`

### ClientOptions (selected)
- `CLIPath`: path to `copilot` executable (default: `copilot` or `COPILOT_CLI_PATH` env)
- `CLIUrl`: connect to existing CLI server; if set the SDK won’t spawn a CLI process
- `Cwd`: working directory for CLI process
- `UseStdio`: default true (stdio transport)
- `Port`: TCP port (TCP mode)
- `Env`: env vars for CLI process
- `LogLevel`: default `info`
- `AutoStart`, `AutoRestart`: `*bool` (use `copilot.Bool(false)` helper)

### Session
- Send message: `session.Send(options copilot.MessageOptions) (string, error)`
- Subscribe to events: `unsubscribe := session.On(func(event copilot.SessionEvent) { ... })`
- Abort: `session.Abort()`
- History: `session.GetMessages()`
- Destroy: `session.Destroy()`

### SessionConfig (selected)
- `Model` (string)
- `Streaming` (bool)
- `Tools` ([]copilot.Tool) — custom tools you define

## Events (string-typed)
Common event types documented:
- `assistant.message_delta` — streaming text chunk (`event.Data.DeltaContent`)
- `assistant.message` — final assistant message (`event.Data.Content`)
- `session.idle` — session becomes idle (use to signal completion)

## Custom tools
### DefineTool (recommended)
- Type-safe tool definitions with JSON schema generation via struct tags:
```go
type LookupIssueParams struct {
    ID string `json:"id" jsonschema:"Issue identifier"`
}

lookupIssue := copilot.DefineTool(
  "lookup_issue",
  "Fetch issue details",
  func(params LookupIssueParams, inv copilot.ToolInvocation) (any, error) {
      return "...", nil
  },
)
```

### Tool struct (manual schema)
- Provide `Parameters` schema manually and implement `Handler` returning `copilot.ToolResult`.

## Attachments (images)
- `MessageOptions.Attachments` supports attaching local images by file path.

## Session persistence (cookbook)
Upstream cookbook: https://github.com/github/copilot-sdk/tree/main/cookbook/go
- Custom session IDs via `SessionConfig.SessionID`
- `ResumeSession(sessionID)`
- `ListSessions()`
- `DeleteSession(sessionID)`
- `Session.GetMessages()`

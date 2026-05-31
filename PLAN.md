# ACP SDK Retrofit Plan

This repository started as a fork of the official MCP Go SDK. The goal is to retrofit it into an Agent Client Protocol SDK at module path `github.com/spachava753/acp-sdk`, removing MCP-specific server/client/tool/resource abstractions and preserving only the generic JSON-RPC and transport machinery that ACP can reuse.

## Protocol Direction

ACP is a JSON-RPC 2.0 protocol for communication between clients/editors and coding agents. It is transport-agnostic, with stdio as the currently standardized transport. ACP is multi-session oriented: session identity is carried explicitly in request/notification payloads via `sessionId`; the SDK should not preserve MCP's `ClientSession`/`ServerSession` connection/session abstraction.

Primary ACP docs and schemas:

- https://agentclientprotocol.com/llms.txt
- https://agentclientprotocol.com/protocol/overview
- https://agentclientprotocol.com/protocol/initialization
- https://agentclientprotocol.com/protocol/transports
- https://agentclientprotocol.com/protocol/schema
- https://raw.githubusercontent.com/agentclientprotocol/agent-client-protocol/main/schema/schema.json

## Target Public Shape

### Module And Packages

- Module path: `github.com/spachava753/acp-sdk`
- Primary package: `acp`
- Keep public `jsonrpc` package as protocol-neutral JSON-RPC helpers.
- Keep internal JSON-RPC implementation under `internal/jsonrpc2`.
- Keep internal JSON helpers under `internal/json` if still useful.

Likely final tree:

```text
acp/
  agent.go          Agent interface, optional agent method interfaces, agent runner
  client.go         Concrete ACP client API
  protocol.go       ACP schema/request/response/update types
  content.go        ACP content blocks and tool-call content variants
  transport.go      Transport, Connection, stdio/io/in-memory transports
  cmd.go            subprocess command transport
  rpc.go            shared JSON-RPC dispatch/call helpers
  errors.go         JSON-RPC/ACP error helpers if needed
jsonrpc/
internal/jsonrpc2/
internal/json/
examples/
  agent/
  client/
```

## Core API Design

### Agent Side

ACP agent servers should be user-defined concrete implementations, not registries of SDK-owned features. The SDK should expose an `Agent` interface for required agent-side methods, plus optional small interfaces for optional ACP methods.

Required baseline agent interface:

```go
type Agent interface {
    Initialize(context.Context, *InitializeRequest) (*InitializeResponse, error)
    NewSession(context.Context, *NewSessionRequest) (*NewSessionResponse, error)
    Prompt(context.Context, *PromptRequest) (*PromptResponse, error)
    Cancel(context.Context, *CancelNotification) error
}
```

Optional interfaces:

```go
type AuthenticatingAgent interface {
    Authenticate(context.Context, *AuthenticateRequest) (*AuthenticateResponse, error)
}

type LogoutAgent interface {
    Logout(context.Context, *LogoutRequest) (*LogoutResponse, error)
}

type SessionLoadingAgent interface {
    LoadSession(context.Context, *LoadSessionRequest) (*LoadSessionResponse, error)
}

type SessionResumingAgent interface {
    ResumeSession(context.Context, *ResumeSessionRequest) (*ResumeSessionResponse, error)
}

type SessionListingAgent interface {
    ListSessions(context.Context, *ListSessionsRequest) (*ListSessionsResponse, error)
}

type SessionClosingAgent interface {
    CloseSession(context.Context, *CloseSessionRequest) (*CloseSessionResponse, error)
}

type ModeSettingAgent interface {
    SetSessionMode(context.Context, *SetSessionModeRequest) (*SetSessionModeResponse, error)
}

type ConfigOptionSettingAgent interface {
    SetSessionConfigOption(context.Context, *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error)
}
```

Agent construction should receive an `AgentConnection` so the user implementation can call client-side ACP methods and stream session updates:

```go
type AgentFactory func(*AgentConnection) Agent

func RunAgent(ctx context.Context, transport Transport, newAgent AgentFactory) error
```

`AgentConnection` should expose agent-to-client operations:

```go
func (c *AgentConnection) SessionUpdate(context.Context, *SessionNotification) error
func (c *AgentConnection) RequestPermission(context.Context, *RequestPermissionRequest) (*RequestPermissionResponse, error)
func (c *AgentConnection) ReadTextFile(context.Context, *ReadTextFileRequest) (*ReadTextFileResponse, error)
func (c *AgentConnection) WriteTextFile(context.Context, *WriteTextFileRequest) (*WriteTextFileResponse, error)
func (c *AgentConnection) CreateTerminal(context.Context, *CreateTerminalRequest) (*CreateTerminalResponse, error)
func (c *AgentConnection) TerminalOutput(context.Context, *TerminalOutputRequest) (*TerminalOutputResponse, error)
func (c *AgentConnection) WaitForTerminalExit(context.Context, *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error)
func (c *AgentConnection) KillTerminal(context.Context, *KillTerminalRequest) (*KillTerminalResponse, error)
func (c *AgentConnection) ReleaseTerminal(context.Context, *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error)
```

### Client Side

The client should remain a concrete SDK type because its outbound behavior is standardized by ACP. The client sends requests to the agent and handles agent-to-client callbacks through handler interfaces.

Concrete client methods:

```go
type Client struct { /* private RPC connection */ }

func Connect(ctx context.Context, transport Transport, handler ClientHandler) (*Client, error)
func (c *Client) Initialize(context.Context, *InitializeRequest) (*InitializeResponse, error)
func (c *Client) NewSession(context.Context, *NewSessionRequest) (*NewSessionResponse, error)
func (c *Client) Prompt(context.Context, *PromptRequest) (*PromptResponse, error)
func (c *Client) Cancel(context.Context, *CancelNotification) error
func (c *Client) Close() error
```

Client callback interfaces:

```go
type ClientHandler interface {
    SessionUpdate(context.Context, *SessionNotification) error
}

type FileSystemHandler interface {
    ReadTextFile(context.Context, *ReadTextFileRequest) (*ReadTextFileResponse, error)
    WriteTextFile(context.Context, *WriteTextFileRequest) (*WriteTextFileResponse, error)
}

type TerminalHandler interface {
    CreateTerminal(context.Context, *CreateTerminalRequest) (*CreateTerminalResponse, error)
    TerminalOutput(context.Context, *TerminalOutputRequest) (*TerminalOutputResponse, error)
    WaitForTerminalExit(context.Context, *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error)
    KillTerminal(context.Context, *KillTerminalRequest) (*KillTerminalResponse, error)
    ReleaseTerminal(context.Context, *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error)
}

type PermissionHandler interface {
    RequestPermission(context.Context, *RequestPermissionRequest) (*RequestPermissionResponse, error)
}
```

This implementation uses the corrected terminal `ReleaseTerminal` signature: it takes `*ReleaseTerminalRequest` and returns `*ReleaseTerminalResponse`.

## ACP Methods To Support

Agent-side methods called by the client:

- `initialize`
- `authenticate`
- `logout`
- `session/new`
- `session/load`
- `session/resume`
- `session/list`
- `session/close`
- `session/prompt`
- `session/cancel` notification
- `session/set_mode`
- `session/set_config_option`

Client-side methods/notifications called by the agent:

- `session/update` notification
- `session/request_permission`
- `fs/read_text_file`
- `fs/write_text_file`
- `terminal/create`
- `terminal/output`
- `terminal/wait_for_exit`
- `terminal/kill`
- `terminal/release`

Extension methods beginning with `_` should be possible eventually, but can be left as a later phase unless needed for initial compilation.

## Deletion Plan

Delete or replace MCP-specific code:

- `mcp/` old implementation after reusable transport/content code has been ported to `acp/`.
- `auth/`, `auth/extauth/`, `oauthex/`, `internal/authutil/`, `internal/oauthtest/`.
- MCP conformance binaries and scripts under `conformance/` and MCP conformance scripts.
- Existing MCP examples under `examples/`.
- MCP generated docs and readme source under `docs/`, `internal/docs/`, `internal/readme/`.
- MCP HTTP/SSE/Streamable HTTP transport code. ACP stdio is the initial supported transport; future HTTP/WebSocket support should be ACP-specific.
- MCP feature APIs: tools, resources, prompts, logging, sampling, elicitation, roots, completions, subscriptions, resource templates, pagination for MCP list methods, and multi-round-trip logic.

Keep or port:

- `internal/jsonrpc2/`
- `internal/json/`
- `jsonrpc/`
- stdio/io/in-memory/command transport mechanics from MCP, renamed and stripped of MCP session IDs.
- compatible content block shapes: text, image, audio, embedded resource, resource link.

## Implementation Phases

All phases below have been completed in the current working tree. The notes remain as implementation history and as a guide for future refactors.

### Phase 1: Repository Identity And Skeleton

- Change `go.mod` module path to `github.com/spachava753/acp-sdk`.
- Create `acp/` package with copyright headers.
- Port protocol-neutral transport code into `acp/transport.go` and `acp/cmd.go`.
- Update `jsonrpc` and internal imports to the new module path.
- Keep tests minimal during transition; target `go test ./acp ./jsonrpc ./internal/...` initially.

### Phase 2: ACP Protocol Types

- Add `acp/protocol.go` with ACP schema types.
- Prefer explicit hand-maintained Go structs for the initial supported schema instead of prematurely introducing a generator.
- Include `_meta` fields broadly as `Meta map[string]any`.
- Include discriminator constants for content blocks, session updates, tool-call content, config options, permission outcomes, MCP server configs, and auth methods.
- Match ACP JSON names exactly: `camelCase` fields, snake_case discriminator values.

### Phase 3: RPC Dispatch

- Add shared JSON-RPC call/notify helpers.
- Implement `RunAgent` dispatch from method name to required/optional `Agent` interfaces.
- Implement `Connect` for `Client` and dispatch client-side callbacks to `ClientHandler` optional interfaces.
- Return JSON-RPC method-not-found for unsupported optional interfaces.
- Return invalid params when request params cannot unmarshal.

### Phase 4: Examples And Tests

- Add an example stdio agent implementing `Agent` with in-memory sessions.
- Add an example client using `CommandTransport`.
- Add tests for newline-delimited JSON framing, initialize, new session, prompt, cancel notification, session/update notification, fs callback, terminal callback, and permission callback.
- Add protocol marshal/unmarshal tests for discriminated content and session updates.

### Phase 5: Cleanup

- Remove leftover MCP package and tests.
- Rewrite `README.md` and docs for ACP. If docs generation remains, update source and generation instructions; otherwise remove generation machinery.
- Update `AGENTS.md` project overview and commands if package layout or docs workflow changes.
- Run `gofmt` and `go test ./...`.

## Current Status

The MCP-to-ACP retrofit is implemented in this working tree.

Completed:

- Module path changed to `github.com/spachava753/acp-sdk`.
- Legacy MCP packages, OAuth packages, conformance binaries/scripts, generated MCP docs, and MCP examples were removed.
- Added primary `acp` package with protocol types, content types, transports, `Client`, `Agent`, optional agent interfaces, `AgentConnection`, and JSON-RPC dispatch.
- Kept protocol-neutral `jsonrpc`, `internal/jsonrpc2`, and `internal/json` packages.
- Added ACP examples under `examples/agent` and `examples/client`.
- Updated repository docs and project instructions for ACP.
- Verification passes with `go test ./...`, `go vet ./...`, and `staticcheck ./...`.

Known follow-up work:

- `acp/protocol.go` is a hand-maintained Go representation of the stable ACP schema. It covers the stable methods and major content/update types, but a future pass should add generated schema-conformance tests or a code generator to prevent drift.
- `SessionUpdate.Content` is intentionally `any` because ACP reuses the JSON field name `content` for both `ContentBlock` chunks and `[]ToolCallContent` tool-call payloads. A future ergonomic layer may add typed constructors or discriminated wrapper types.
- Extension methods beginning with `_` are not exposed through a first-class API yet.
- Remote HTTP/WebSocket ACP transports are not implemented; only stdio/IO/in-memory/command transports are included.

Keep this file updated as future ACP protocol support changes.

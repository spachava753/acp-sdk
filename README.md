# ACP Go SDK

This repository contains a Go SDK for the [Agent Client Protocol](https://agentclientprotocol.com/), a JSON-RPC protocol for connecting code editors and other clients to coding agents.

The module path is:

```sh
go get github.com/spachava753/acp-sdk
```

## Packages

- `github.com/spachava753/acp-sdk/acp`: ACP protocol types, transports, client API, and agent runner.
- `github.com/spachava753/acp-sdk/jsonrpc`: protocol-neutral JSON-RPC message helpers for custom transports.
- `github.com/spachava753/acp-sdk/internal/jsonrpc2`: internal bidirectional JSON-RPC implementation used by the SDK.

## Status

This repository is a retrofit of the MCP Go SDK into an ACP SDK. The MCP tool/resource/server abstraction has been removed. ACP agents are implemented as concrete Go types satisfying the `acp.Agent` interface; ACP clients use the concrete `acp.Client` type plus callback interfaces for client-side methods such as filesystem access, terminals, permissions, and `session/update` notifications.

See [PLAN.md](./PLAN.md) for the active migration/completion plan.

## Minimal Agent

```go
package main

import (
    "context"
    "log"

    "github.com/spachava753/acp-sdk/acp"
)

type agent struct {
    conn *acp.AgentConnection
}

func (a *agent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
    return &acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion}, nil
}

func (a *agent) NewSession(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
    return &acp.NewSessionResponse{SessionID: "session-1"}, nil
}

func (a *agent) Prompt(ctx context.Context, req *acp.PromptRequest) (*acp.PromptResponse, error) {
    err := a.conn.SessionUpdate(ctx, &acp.SessionNotification{
        SessionID: req.SessionID,
        Update: acp.SessionUpdate{
            SessionUpdate: "agent_message_chunk",
            Content: acp.ContentBlock{Type: acp.ContentBlockTypeText, Text: "Hello from ACP."},
        },
    })
    if err != nil {
        return nil, err
    }
    return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (a *agent) Cancel(context.Context, *acp.CancelNotification) error { return nil }

func main() {
    err := acp.RunAgent(context.Background(), &acp.StdioTransport{}, func(conn *acp.AgentConnection) acp.Agent {
        return &agent{conn: conn}
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

## Development

Run the full test suite with:

```sh
go test ./...
```

The `conformance` package contains black-box tests for ACP behavior and wire compatibility. It covers public protocol type round trips, JSON-RPC client/agent flows, bidirectional callbacks, cancellation while prompts are pending, concurrent requests, optional method dispatch, and binary content encoding.

To run only the conformance tests:

```sh
go test ./conformance
```


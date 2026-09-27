# ACP Go SDK

[![Go Reference](https://pkg.go.dev/badge/github.com/spachava753/acp-sdk.svg)](https://pkg.go.dev/github.com/spachava753/acp-sdk)
[![Tests](https://github.com/spachava753/acp-sdk/actions/workflows/test.yml/badge.svg)](https://github.com/spachava753/acp-sdk/actions/workflows/test.yml)

A community-maintained Go SDK for the [Agent Client Protocol (ACP)](https://agentclientprotocol.com/). ACP is a bidirectional JSON-RPC protocol that connects code editors and other clients to coding agents.

This SDK is listed in the official [ACP community libraries](https://agentclientprotocol.com/libraries/community).

## Features

- Generated Go types, method constants, handlers, and RPC clients for the checked-in ACP schema.
- Both sides of the protocol: build an agent with `RunAgent` or connect a client with `Connect`.
- Bidirectional requests and notifications, including streamed session updates, permission requests, filesystem access, and terminals.
- Partial handlers: implement only the ACP methods your application supports.
- Stdio, subprocess, caller-provided I/O, in-memory, and custom transports.
- Generated constructors for discriminated unions such as content blocks and session updates.
- Conformance coverage for wire encoding, lifecycle flows, callbacks, cancellation, concurrency, optional methods, and binary content.

## Status

The module is pre-1.0. Minor releases may contain breaking API changes as ACP and the generated Go API evolve.

The protocol surface is generated from a checked-in copy of ACP's `schema.unstable.json`. It includes stable ACP v1 features and upstream additions explicitly documented as **UNSTABLE**. Unstable methods and types may change or disappear when the upstream schema changes. A scheduled workflow checks for new schema releases and opens a regeneration pull request.

The SDK requires Go 1.25 or later. CI tests Go 1.25 and Go 1.26.

## Install

```sh
go get github.com/spachava753/acp-sdk@latest
```

For production applications, pin a specific module version in `go.mod` and review release changes before upgrading.

## Quick Start: Agent

An agent receives client-to-agent methods on a plain Go value and uses `AgentConnection` for callbacks to the client. This minimal agent implements initialization, session creation, prompts, and cancellation:

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

func (*agent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersion(1),
		AgentInfo: &acp.Implementation{
			Name:    "example-agent",
			Version: "dev",
		},
	}, nil
}

func (*agent) NewSession(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	// Production agents should generate a unique ID for every session.
	return &acp.NewSessionResponse{SessionID: "example-session"}, nil
}

func (a *agent) Prompt(ctx context.Context, req *acp.PromptRequest) (*acp.PromptResponse, error) {
	err := a.conn.SessionUpdate(ctx, &acp.SessionNotification{
		SessionID: req.SessionID,
		Update: acp.AgentMessageChunkSessionUpdate(
			acp.TextContentBlock("Hello from ACP."),
		),
	})
	if err != nil {
		return nil, err
	}
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (*agent) Cancel(context.Context, *acp.CancelNotification) error {
	return nil
}

func main() {
	err := acp.RunAgent(
		context.Background(),
		&acp.StdioTransport{},
		func(conn *acp.AgentConnection) any {
			return &agent{conn: conn}
		},
	)
	if err != nil {
		log.Fatal(err)
	}
}
```

See the complete [example agent](./examples/agent), which also demonstrates session IDs and optional session methods.

## Quick Start: Client

A client uses generated methods on `Client` to call the agent. The handler passed to `Connect` receives callbacks initiated by the agent:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/spachava753/acp-sdk/acp"
)

type clientHandler struct{}

func (clientHandler) Update(_ context.Context, notification *acp.SessionNotification) error {
	fmt.Printf("session %s update: %s\n",
		notification.SessionID,
		notification.Update.SessionUpdate,
	)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: %s /path/to/acp-agent [args...]", os.Args[0])
	}

	ctx := context.Background()
	client, err := acp.Connect(ctx, &acp.CommandTransport{
		Command: exec.Command(os.Args[1], os.Args[2:]...),
	}, clientHandler{})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	_, err = client.Initialize(ctx, &acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersion(1),
		ClientInfo: &acp.Implementation{
			Name:    "example-client",
			Version: "dev",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	session, err := client.NewSession(ctx, &acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		log.Fatal(err)
	}

	result, err := client.Prompt(ctx, &acp.PromptRequest{
		SessionID: session.SessionID,
		Prompt:    []acp.ContentBlock{acp.TextContentBlock("Hello")},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("stop reason:", result.StopReason)
}
```

See the complete [example client](./examples/client).

## Handler Model

`RunAgent`'s factory and `Connect`'s callback-handler argument accept `any`. At dispatch time, the SDK checks whether the value implements the method signature for the incoming ACP request or notification. This lets applications implement a focused subset instead of one monolithic interface.

Generated grouped interfaces are available for compile-time checks:

- Agent-side groups include `InitializeHandler`, `AuthenticateHandler`, `SessionHandler`, `DocumentHandler`, `ProvidersHandler`, `NesHandler`, and `McpHandler`.
- Client-side groups include `SessionClientHandler`, `FsClientHandler`, `TerminalClientHandler`, `ElicitationClientHandler`, and `McpClientHandler`.

A value does not need to satisfy a whole group. If it does not implement an incoming method, the SDK returns JSON-RPC `method not found`. Advertise only capabilities whose corresponding methods are implemented.

Agents call client-side methods through `AgentConnection`, including `SessionUpdate`, `RequestPermission`, `ReadTextFile`, `WriteTextFile`, and the terminal lifecycle methods. Clients call agent-side methods through `Client`, including initialization, authentication, session lifecycle, prompting, cancellation, and supported unstable extensions.

## Extensions

Custom ACP methods must start with `_`. Both `Client` and `AgentConnection` can
call extension requests and send extension notifications. Use the generic helper
for a typed response:

```go
result, err := acp.CallExtension[SearchResult](
    ctx, client, "_example.com/search", &SearchParams{Query: "main"},
)
```

The response type is explicit; parameters can be any JSON-marshalable value.
`client.CallExtension` returns `json.RawMessage` when you need the raw response.
A JSON `null` response decodes to the zero value of the response type.
Notifications have no response and return only a sending error:

```go
err := client.NotifyExtension(ctx, "_example.com/file_opened", &FileOpenedParams{
    Path: "/repo/main.go",
})
```

To receive extensions, register typed handlers on an `ExtensionMux` and embed it
in your agent or client callback handler:

```go
type SearchParams struct {
    Query string `json:"query"`
}

type SearchResult struct {
    Matches []string `json:"matches"`
}

type FileOpenedParams struct {
    Path string `json:"path"`
}

type agent struct {
    *acp.ExtensionMux
    conn *acp.AgentConnection
}

extensions := acp.NewExtensionMux()
err := acp.AddExtensionRequest(extensions, "_example.com/search",
    func(ctx context.Context, params *SearchParams) (*SearchResult, error) {
        return &SearchResult{Matches: []string{"main.go"}}, nil
    },
)
if err != nil {
    return err
}

err = acp.AddExtensionNotification(extensions, "_example.com/file_opened",
    func(ctx context.Context, params *FileOpenedParams) error {
        log.Printf("Opened %s", params.Path)
        return nil
    },
)
if err != nil {
    return err
}

return acp.RunAgent(ctx, transport, func(conn *acp.AgentConnection) any {
    return &agent{ExtensionMux: extensions, conn: conn}
})
```

Embedding the mux adds extension handling alongside your ordinary ACP methods.
For agent-to-client extensions, embed it in the callback handler passed to
`Connect` and call `acp.CallExtension[Response](ctx, conn, method, params)` using
an `AgentConnection`.

Register before connecting so handlers are ready when messages arrive. Duplicate
registrations of the same message kind are rejected; one name may have both a
request and a notification handler. Handlers may run concurrently and may call
back to the peer. JSON decoding failures return `-32602` for requests; handlers
must validate required fields and other application constraints themselves.
Missing or `null` parameters produce a zero-valued parameter struct. Return a
`*jsonrpc.Error` to preserve an extension-specific error code and data.

Unknown extension requests return `-32601`; unknown notifications are ignored.
Notification handler errors cannot be returned to the sender. Incoming extensions
are dispatched through the embedded `ExtensionMux`; register handlers using
`AddExtensionRequest` and `AddExtensionNotification`.

Advertise extension support explicitly in `AgentCapabilities.Meta` or
`ClientCapabilities.Meta` during initialization, and check the peer's advertised
capabilities before invoking an extension:

```go
capabilities := acp.AgentCapabilities{
    Meta: acp.Meta{
        "example.com": map[string]any{
            "search": true,
            "fileOpened": true,
        },
    },
}
```

Registration does not automatically advertise capabilities. See the
[ACP extensibility specification](https://agentclientprotocol.com/protocol/v1/extensibility).

## Unstructured JSON Numbers

Numbers decoded into unstructured values use `json.Number`, including `_meta`,
custom capability maps, tool inputs and outputs, and extension parameters and
results. This preserves large integers and precise decimals through a JSON round
trip, including numbers nested in objects and arrays.

```go
var meta acp.Meta
if err := json.Unmarshal([]byte(`{"id":9007199254740993}`), &meta); err != nil {
    return err
}
id := meta["id"].(json.Number)
fmt.Println(id.String()) // 9007199254740993
```

Use `Int64()` or `Float64()` when you need a Go numeric value, and check the
conversion error. Converting to `float64` can lose precision. Explicitly typed
numeric fields retain their schema-defined types.

This changes decoded unstructured numbers from `float64` to `json.Number`;
update any type assertions accordingly. When constructing outgoing values,
ordinary Go numbers still work, and `json.Number` can represent an exact JSON
number without first converting it to a float.

Generated structs and map/slice types preserve numbers even when decoded with
`json.Unmarshal`. SDK calls also preserve numbers in standalone unstructured
responses. If you decode a plain `any`, a bare `map[string]any`, or a named
interface such as `ExtResponse` yourself, use `json.Decoder.UseNumber()`.
Custom `UnmarshalJSON` implementations must likewise preserve numbers in any
unstructured values they decode.

## Union Types

Many ACP schema types are discriminated unions. Prefer the generated variant constructors so the correct discriminator and payload fields are set:

```go
content := acp.TextContentBlock("Hello")
update := acp.AgentMessageChunkSessionUpdate(content)
```

The constructors are discoverable on [pkg.go.dev](https://pkg.go.dev/github.com/spachava753/acp-sdk/acp) alongside the generated type documentation.

## Transports

| Transport | Use |
| --- | --- |
| `StdioTransport` | Serve an agent over its standard input and output. |
| `CommandTransport` | Launch an agent subprocess and connect through its standard streams. |
| `IOTransport` | Use caller-provided `io.ReadCloser` and `io.WriteCloser` streams. |
| `InMemoryTransport` | Connect a client and agent in one process, primarily for tests. |
| Custom `Transport` | Supply another bidirectional connection implementation. |

The built-in stream transports use newline-delimited JSON-RPC messages, matching the ACP [stdio transport](https://agentclientprotocol.com/protocol/v1/transports). The public [`jsonrpc`](./jsonrpc) package exposes protocol-neutral message encoding helpers for custom transports.

## Packages

- [`acp`](./acp): protocol types and union constructors, generated RPC methods and handlers, transports, the client API, and the agent runner.
- [`jsonrpc`](./jsonrpc): protocol-neutral JSON-RPC 2.0 message types and encoding helpers for custom transports.

Implementation packages under `internal/` are not public API.

## Development

Run all tests:

```sh
go test ./...
```

Run the race detector:

```sh
go test -race ./...
```

Run only the black-box conformance suite:

```sh
go test ./conformance
```

To refresh and regenerate the protocol surface:

```sh
go generate ./internal/schemagen
go generate ./acp
go test ./...
```

The first command downloads the latest upstream unstable schema. Generated files in `acp/*_gen.go` should not be edited directly. Generator changes should include a focused golden fixture under `internal/schemagen/testdata/`.

See [CONTRIBUTING.md](./CONTRIBUTING.md) for contribution guidelines and [SECURITY.md](./SECURITY.md) for reporting vulnerabilities.

## Resources

- [ACP documentation](https://agentclientprotocol.com/)
- [ACP v1 protocol overview](https://agentclientprotocol.com/protocol/v1/overview)
- [Go package documentation](https://pkg.go.dev/github.com/spachava753/acp-sdk)
- [Issue tracker](https://github.com/spachava753/acp-sdk/issues)

## License

The SDK is derived from the Go MCP SDK. New changes are licensed under Apache-2.0 unless otherwise noted; inherited contributions retain their original licensing terms. See [LICENSE](./LICENSE).

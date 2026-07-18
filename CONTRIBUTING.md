# Contributing to ACP Go SDK

This repository is a Go SDK for the Agent Client Protocol.

## Development Setup

Use the standard Go toolchain:

```sh
go test ./...
```

Run `gofmt` on changed Go files before submitting changes.

## Project Direction

The SDK should stay focused on ACP:

- ACP agents are user-defined values that implement the generated handler method signatures they support.
- ACP clients use the concrete `acp.Client` type and callback handler interfaces.
- Session identity belongs in ACP payloads as `sessionId`; do not reintroduce MCP-style client/server session abstractions.
- Keep JSON-RPC and transport code protocol-neutral where possible.

Protocol types and RPC glue are generated from `internal/schemagen/schema.json`. Do not edit `acp/*_gen.go` directly; update the schema and/or generator, then run `go generate ./acp`.

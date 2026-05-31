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

- ACP agents are user-defined implementations of `acp.Agent`.
- ACP clients use the concrete `acp.Client` type and callback handler interfaces.
- Session identity belongs in ACP payloads as `sessionId`; do not reintroduce MCP-style client/server session abstractions.
- Keep JSON-RPC and transport code protocol-neutral where possible.

See [PLAN.md](./PLAN.md) for current migration notes and open implementation work.

# AGENTS.md

## Project Overview

This repository is a Go SDK for the Agent Client Protocol (ACP). It was retrofitted from the Go MCP SDK.

The module path is `github.com/spachava753/acp-sdk`.

## Project Structure

- `acp/`: Public ACP SDK package. This contains hand-written SDK code plus generated protocol types and RPC glue in `types_gen.go`, `agent_gen.go`, and `client_gen.go`.
- `jsonrpc/`: Public protocol-neutral JSON-RPC 2.0 message helpers for custom transports.
- `internal/schemagen/`: JSON Schema to Go generator. The generator input is `schema.json`; `cmd/acpgen` writes generated files; `typegen`, `agentgen`, and `clientgen` contain the generation passes; `testdata` contains golden fixtures.
- `internal/jsonrpc2/`: Internal bidirectional JSON-RPC implementation used by the SDK.
- `internal/json/`: Internal JSON helpers used by JSON-RPC.
- `conformance/`: Wire-format and lifecycle conformance tests for the public ACP types and client/agent behavior.
- `examples/`: Minimal example agent and client programs.

## Schema And Code Generation

`internal/schemagen/schema.json` is fetched from the latest ACP schema release asset:

`https://github.com/agentclientprotocol/agent-client-protocol/releases/latest/download/schema.unstable.json`

Generated ACP package files are written into `acp/` from that schema. Do not edit `acp/*_gen.go` directly; update the schema and/or generator, then regenerate.

- Refresh the checked-in schema from the latest release: `go generate ./internal/schemagen`
- Regenerate ACP code from the checked-in schema: `go generate ./acp`
- Generator entry point: `internal/schemagen/cmd/acpgen`
- Go generate directive: `acp/acp.go`
- Generated outputs: `acp/types_gen.go`, `acp/agent_gen.go`, `acp/client_gen.go`

When changing generator behavior, add or update a focused golden fixture under `internal/schemagen/testdata/`, run `go test ./internal/schemagen/...`, then run `go test ./...`.

## Development Setup

The project uses the standard Go toolchain.

- **Build**: `go build ./...`
- **Test**: `go test ./...`

## Testing

- **Unit Tests**: Run `go test ./...` to run all tests.

## Development Guidelines

### Code Style

- Follow standard Go conventions (Effective Go).
- Use `gofmt` to format code.
- Do not add comments to the code unless they are really necessary:
  - Prefer self-documenting code.
  - Focus on the "why" not the "what" in comments.

### Documentation

- `README.md` is maintained directly.

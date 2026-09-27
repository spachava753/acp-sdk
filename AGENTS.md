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

Generate schema-dependent code only. Keep reusable runtime helpers, such as JSON decoding utilities, in handwritten files; generated code should call them.

## Development Setup

The project uses the standard Go toolchain.

- **Build**: `go build ./...`
- **Test**: `go test ./...`

## Testing

- **Unit Tests**: Run `go test ./...` to run all tests.
- Identify the observable properties worth testing before writing tests. Test SDK behavior and integration boundaries, rather than rechecking the JSON library or trivial helpers.
- Keep coverage focused. A representative regression case is preferable to a broad matrix of number formats, nulls, and malformed inputs unless those differences exercise SDK-specific behavior.
- Extend an existing test's data and control flow when related properties fit naturally. Registration order and agent/client construction can establish the conditions needed for later assertions. Do not default to separate tests or `t.Run` subtests for every property; use them when scenarios need independent setup or cannot fit clearly into the existing flow.
- Follow the structure of the test being edited. Do not reintroduce tests or scaffolding the user has deliberately removed.
- Use `package acp` when tests need internal access. Do not expose implementation details just to make them accessible to `acp_test`.
- For asynchronous assertions, wait for dispatch to finish before checking that a handler was not invoked. Use channel buffering when the producer must finish before the test reads the value, and explain that ordering when it is not obvious.

## Development Guidelines

### Code Style

- Follow standard Go conventions (Effective Go).
- Use `gofmt` to format code.
- Use named error-code constants, including in tests; do not use raw numeric error codes.
- Prefer self-documenting code, but retain useful comments explaining intent, ordering, or non-obvious behavior. In tests, explain what property a phase or loop establishes and why the setup matters. Keep comments concise and plain; avoid narrating each statement or adding technical jargon.

### API And Implementation Design

- Keep the public API small and driven by concrete use cases. Do not expose dispatch methods or override interfaces solely for hypothetical customization. Extension handling currently uses `ExtensionMux` with private dispatch methods.
- Prefer typed conveniences where they save callers repetitive decoding. Do not require pointers for outgoing parameters unless the implementation needs them; distinguish that from pointer parameters used in handler signatures.
- Preserve existing nil-handler behavior when changing dispatch. An absent handler must still follow the appropriate unknown-request or ignored-notification behavior.
- Before working around an internal layer, inspect its actual consumers. Prefer fixing behavior at the appropriate layer over adding redundant encoding or decoding solely to avoid touching shared code.
- Consider all unstructured JSON entry points when changing number handling, not only `_meta`. Preserve schema-defined numeric types and keep envelope decoding concerns, such as request IDs, distinct from payload decoding.
- Check schema annotations before changing error handling in generated decoders. Errors ignored for `x-deserialize-default-on-error` are intentional; do not turn them into panics on incoming data.

### Documentation

- `README.md` is maintained directly.

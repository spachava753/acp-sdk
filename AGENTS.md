# AGENTS.md

## Project Overview

This repository is a Go SDK for the Agent Client Protocol (ACP). It was retrofitted from the Go MCP SDK; the migration plan and historical decisions are tracked in `PLAN.md`.

The module path is `github.com/spachava753/acp-sdk`.

### Key Packages

-   `acp`: The primary package for ACP protocol types, transports, clients, and agent runners.
-   `jsonrpc`: Protocol-neutral JSON-RPC 2.0 message helpers for custom transports.
-   `internal/jsonrpc2`: Internal bidirectional JSON-RPC implementation used by the SDK.
-   `internal/json`: Internal JSON helpers used by JSON-RPC.

## Development Setup

The project uses the standard Go toolchain.

-   **Build**: `go build ./...`
-   **Test**: `go test ./...`

## Testing

-   **Unit Tests**: Run `go test ./...` to run all tests.

## Development Guidelines

### Code Style

-   Follow standard Go conventions (Effective Go).
-   Use `gofmt` to format code.
-   Add copyright headers to all new Go files:
    ```go
    // Copyright 2025 The Go MCP SDK Authors. All rights reserved.
    // Use of this source code is governed by the license
    // that can be found in the LICENSE file.
    ```
    Existing retained files preserve upstream copyright headers.
-  Do not add comments to the code unless they are really necessary:
    -   Prefer self-documenting code.
    -   Focus on the "why" not the "what" in comments.

### Documentation

-   `README.md` is maintained directly.
-   Keep `PLAN.md` updated when migration status or implementation direction changes.

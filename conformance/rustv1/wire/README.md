# Rust v1 wire scenario ports

Semantic adaptations of the official [Rust SDK](https://github.com/agentclientprotocol/rust-sdk/tree/050cd94c081056ace37a7b89abfed7f6220b47eb), pinned to `050cd94c081056ace37a7b89abfed7f6220b47eb` (Apache-2.0). Tests use the Go SDK's exported JSON-RPC helpers, public `Connect`/`RunAgent`/`IOTransport`, typed extension registration, and independent byte peers. They do not use private hooks, reflection into SDK state, a fixture implementation of missing SDK features, or `t.Skip` for observed failures.

This is the **upstream-derived test stage**, not the later local-regression or production-fix stage. Parent integration owns commits and repository-level upstream attribution. No production, generated, schema, dependency or lock files were changed here.

## Coverage accounting

[`mapping.json`](mapping.json) records canonical source/function/declaration-line IDs, Go test targets, literal loop/property coverage, and explicit unavailable portions. All assigned source functions and doc examples were read before disposition. The mapping has **141 entries**:

| Source entries | Ported | Partially ported | Not applicable |
| --- | ---: | ---: | ---: |
| Test functions | 37 | 20 | 45 |
| Doc examples | 10 | 4 | 25 |

Partial entries have structured subcases. They do **not** claim full Rust-function equivalence. Examples include:

- Peer-provided null/string/int64 IDs and correlation are tested. Rust's generated UUID/string-ID allocation assertion is explicitly not imposed on Go's numeric allocator.
- Go's raw-ID/schema aliases have no Rust enum `From`/`Display` API.
- Checked Rust message constructors, public responder/router handles, generic fallthrough/retry chains, role-aware proxy peeling, managed task spawning, and multi-version routing have individual absent-API dispositions. Fixtures do not emulate them.
- Rust compile-time `Send`/`Sync`/lifetime/associated-type/procedural-macro contracts are classified individually, not replaced by goroutine or race tests.
- V1 controls inside `protocol_v2.rs` and v2-gated inline tests are included; whole files are not excluded merely by their gate.

The parent [coverage check](../coverage_test.go) validates source IDs and declared Go test targets across all domains. The pinned source census and its verification tools are documented in the [suite README](../README.md). Source cases transferred to another domain are not duplicated in this mapping.

## Run against unchanged Go HEAD

Target Go production revision: `09349ff9c8b24d6a8f845b7959dc614406b14a57`, also peeled `v0.6.0`.

```sh
GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local \
  go test -json ./conformance/rustv1/wire -count=1 -timeout=90s
```

The unchanged-SDK baseline produced **50 top-level tests; 25 PASS, 25 FAIL; no SKIP**, exit **1**. Named subtests and loop assertions are not additional independent Rust-function counts. The parent independently reran the package. See the [suite baseline](../BASELINE.md) for the consolidated results and later comparison with added regressions.

The source's stateful sequences are retained: one peer receives all five unsupported batches before a final echo; one isolated request-ID flow initializes once, runs all 11 echoes and 11 callbacks, handles the three invalid-ID cases, and ends with clean EOF. Per-ID processes provide supplementary diagnostics; they do not replace the shared-connection scenario. An early SDK failure can prevent later phases from running and is not counted as their success.

The run completed without a package-wide panic or timeout. The valid null-ID initialization panic is contained in a child test process and fails the parent test. Raw read/write deadlines, retained stream closers, buffered result channels and cleanup-only release gates bound failure paths. Normal successful agent fixtures also require clean EOF. No broad generator/race suite or native Rust suite was run here.

Development logs are retained as local audit artifacts rather than checked into this test package. To obtain a fresh machine-readable transcript, redirect the `go test -json` command above to a file. The baseline describes the tests at the first port commit, not a promise that they will remain failing after SDK fixes.

## What is red on this revision

- Legal large IDs lose identity; explicit null IDs are not retained as calls. ID rejection, null-ID error encoding and the optional same-ID bidirectional cancellation scenario also fail their upstream expectations.
- Raw error extras are lost. Omitted versus explicit-null data is lost when projected through the generated schema error payload. Wrapped structured errors lose data.
- Conflicting envelopes, scalar params and malformed error fields are accepted; parser errors do not provide the expected structured classification.
- Unsupported arrays and malformed request-side frames close the connection without the expected upstream error response/retry behavior.
- Required-field absence reaches handlers, and Rust-style unknown-method diagnostic data is absent.

See individual test outputs for exact assertions. A failed group does not mean every subproperty in that group failed: for example, the peer-error case checks raw codec fidelity **before** checking caller-visible known fields.

## Important policy boundaries

These tests port upstream behavior; they are not an ACP certification or a security audit. Batch rejection/recovery, diagnostic data, raw error extension retention and notification dispatch order include Rust SDK policies beyond a universal ACP v1 MUST. Per-request `$/cancel_request` support is an explicitly optional profile; its absence remains RED rather than being skipped or relabeled mandatory ACP support. Rejection of exponent-form ID `1e3` is Rust's integer-token representation policy. Fractional-ID support is not asserted.

Go returns `jsonrpc.Error` for the extension call's error; there is no separate typed ACP runtime error conversion. `TestPeerErrorKnownFields` therefore checks the actual Rust source assertions—raw roundtrip and known code/message/data delivery—without requiring runtime errors to drop extras. `TestExplicitSchemaErrorProjection` separately uses real public JSON codecs, `jsonrpc.Error -> acp.Error -> jsonrpc.Error`, to test explicit schema projection. It never copies fields or introduces a hidden runtime representation flag. The exact native Rust conversion API remains an explicit partial disposition.

Expected JSON comparisons use an independent decoder with exact `json.Number` values, never float64 or JavaScript `JSON.parse` as the int64 oracle. Insignificant object order/whitespace and equivalent string escaping are not treated as protocol differences.

## Work deliberately left to the next stage

No audit-only scenario was added to make this port look exhaustive. The assigned upstream wire tests do not directly reproduce every local audit case—for example, valid-prefix-plus-suffix frame consumption, known ACP method request/notification inversion, or adjacent large IDs colliding concurrently in the same direction. Those belong in the separately committed local-regression package after the parent commits/reviews the upstream port.

Partial/N/A mappings and their reasons remain visible for parent review. They are not assertions that Go implements the corresponding Rust feature, and no production fixes have started in this worktree.

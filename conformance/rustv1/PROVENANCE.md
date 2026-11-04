# Upstream test provenance

## SDK workspace

- Official repository: <https://github.com/agentclientprotocol/rust-sdk>.
- Pinned revision: `050cd94c081056ace37a7b89abfed7f6220b47eb`.
- License: [Apache-2.0 upstream LICENSE](https://github.com/agentclientprotocol/rust-sdk/blob/050cd94c081056ace37a7b89abfed7f6220b47eb/LICENSE).
- Core/derive/conductor/HTTP/polyfill/rmcp/cookbook/trace-viewer source package versions: `5.0.0`; unpublished test utility crate and yopo package: `0.11.0`. These are SDK package versions, **not ACP protocol versions**.
- Rust minimum/edition: `1.88.0` / `2024`, per workspace `Cargo.toml:20-25`.
- Upstream execution recipe: `just test` first builds workspace binaries/examples with all features and lockfile, then runs workspace all-features tests/doctests, core no-default-features tests and rmcp `rmcp_3` tests (`justfile:1-10`). None of those Rust commands was executed by this inventory worker; no Rust toolchain was installed.
- Go source under test: `github.com/spachava753/acp-sdk`, starting at `09349ff9c8b24d6a8f845b7959dc614406b14a57`; annotated tag `v0.6.0` peels to that same revision.

The source inventory covers all ten workspace members declared at that pin. `files` in `upstream_inventory.json` records every one of the 202 Rust source files with SHA-256 and per-file discovery data. It includes inline unit tests, integration tests, explicitly test-enabled draft-v2 examples, comment code fences and macro definitions. Cargo manifests and their hashes, feature declarations, target requirements and module edges are retained. Each registered-test source declaration is separately addressable by canonical source/line/function ID.

The compact committed inventory retains identities/dispositions/cfg/module/subcase anchors and hashes without copying Rust bodies. The full census with literal tables, helper spans and doc code is preserved at `.plan/rust-v1-port-inventory/full-upstream-inventory.json`. The source-discovery tools are included in `tools/` as standalone Python standard-library utilities; raw evidence and recorded execution output remain at `.plan/rust-v1-port-inventory/`. They are explicit update/verification utilities, not dependencies of normal Go tests. The independent line-based verification matches 937 registered test-function declarations, 190 doc fences, 31 macro definitions and six separately inventoried unannotated compiler-regression functions, with no missing/extra file hash or canonical identity. Three never-run auto-trait future helpers are retained as references of their existing registered test rather than double-counted. This verifies the source census, **not** that all loop/macro/helper/doctest semantic obligations are ported.

## External protocol schema crate: separate scope

The workspace does **not** contain the source of `agent-client-protocol-schema`. It pins the crates.io dependency to **`1.15.1`** (`Cargo.toml`, workspace dependencies; `Cargo.lock:146-155`). Rust SDK integration tests importing schema types are SDK-owned source and remain in the main inventory; the dependency's own unit tests are not.

For precise accounting, the published source package was downloaded publicly without installation:

- Archive: <https://static.crates.io/crates/agent-client-protocol-schema/agent-client-protocol-schema-1.15.1.crate>.
- SHA-256: `1b2316d2cd561917bc2c16e3c64771c77e24363b56d40b58fdad091d02059992`, matching the SDK `Cargo.lock` checksum exactly.
- `.cargo_vcs_info.json`: repository <https://github.com/agentclientprotocol/agent-client-protocol>, revision **`0fc18323a9fdad4e5b4a20ec2f1645dde843fd9b`**, path **`agent-client-protocol-schema`**.
- Separate source census: **32 Rust files / 498 test-function declarations / two rustdoc candidate fences / six macro definitions**.
- Source-path partitions of those test declarations: **178** under `src/v1/`, **289** under `src/v2/`, **31** shared (`src/lib.rs`, `rpc.rs`, `serde_util.rs`, `version.rs`). These are path partitions, not a decision that every shared function is v1-portable or that a feature name alone determines protocol behavior.
- Full separate source list/declarations/fences/loop anchors: `.plan/rust-v1-port-inventory/external-schema-inventory.json`; original archive and extracted source are preserved beside it.

**Approved scope:** the first test-port commit covers SDK-clone workspace v1/shared scenarios only. Cargo dependency unit tests are not executed by the SDK workspace's `just test`, so these 498 functions remain separate external provenance rather than silently expanding the first suite or being counted as covered. Their v1/schema-serialization cases may be adapted as separately attributed additional regressions in the second commit, after the SDK-port baseline/gap analysis. They require their own exact identities and mappings, not guessed duplication of SDK wrapper tests or a claim that they live in the SDK clone. No production dependency/schema change is part of source inventory work.

## Meaning of classifications

Inventory-owned exclusions state which exact scenario lacks a Go public counterpart:

- HTTP/SSE/WebSocket-specific tests are not represented as exercised by a similar stdio test.
- Conductor process-chain orchestration/trace viewer/yopo CLI are distinct components, not features to reproduce inside SDK test fixtures.
- rmcp/polyfill/native-MCP application behavior is not implemented by merely carrying MCP schema fields over JSON-RPC.
- Rust compile-fail/auto-trait/proc-macro type-checker expectations are not Go runtime successes.
- Draft-v2-only state/prompt/session behavior is not a requirement for this v1 SDK. Mixed-version or v1 behavior compiled under a v2 feature gate remains individually reviewable.
- `ignore` or `no_run` doc flags describe upstream rustdoc execution, not a blanket semantic exclusion. Runtime behavior in those examples still requires an explicit domain disposition.

These classifications preserve differences rather than quietly translating a Rust-specific policy into an unconditional ACP rule. All unresolved entries remain visible as `needs-parent-review` until the corresponding domain mapping supplies source-backed test coverage or a precise exclusion.

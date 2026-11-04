# Rust SDK v1 semantic test port

These tests derive their expectations from the official [ACP Rust SDK](https://github.com/agentclientprotocol/rust-sdk) at **`050cd94c081056ace37a7b89abfed7f6220b47eb`**. The Go starting point is **`09349ff9c8b24d6a8f845b7959dc614406b14a57`** (`v0.6.0`, peeled annotated tag). This is a semantic port, not a transliteration or ACP certification.

Ported tests must assert the upstream expectation even when the unchanged Go SDK fails it. They are separate from additional repository-specific regressions and from subsequent implementation fixes. An absent Go public API is recorded as **not applicable**, not reimplemented inside a fixture to manufacture a passing test. Optional Rust features and Rust-specific implementation policies are not presented as universal ACP requirements.

## Public boundaries and packages

- `wire/`: exported JSON-RPC helpers and independent raw-wire peers, including method/message kind, IDs and errors.
- `lifecycle/`: public transport/connection, cancellation, context and shutdown behavior.
- `session/`: public ACP calls/handlers and session update/callback behavior.
- `schema/`: public protocol values and JSON serialization/decoding semantics.

Each domain supplies a `mapping.json` connecting source-specific scenarios to real Go test names or an explicit exclusion. Tests use public `Connect`, `RunAgent`, `Transport`/`Connection`, public JSON-RPC helpers, and public protocol types; they do not depend on private maps or Rust actors. Fixtures should retain their raw closers so failed assertions can still clean up blocked SDK operations. Process-fatal cases require isolated owned subprocesses and bounded cleanup.

## Inventory is not the same as semantic coverage

[`upstream_inventory.json`](upstream_inventory.json) inventories the entire pinned **10-crate workspace**, not merely core `tests/` filenames:

| Source census | Count |
|---|---:|
| Rust source files, including inline/unit/integration/example sources | 202 |
| `#[test]` / `#[tokio::test]` function declarations | 937 |
| All rustdoc comment code fences | 190 |
| Rust/rustdoc candidate fences within those 190 | 173 |
| Macro definitions | 31 |
| Macro definitions containing test-function attributes | 0 |
| Loop anchors inside test functions | 238 |
| Same-file helper-call anchors from test functions | 1,712 |
| Supplemental unannotated compiler-regression functions | 6 |
| Never-run future helper functions passed as values to an existing test | 3 |

These are **source declarations and anchors**, not a count of all semantic subcases or compiler-expanded runtime registrations. Literal loops, helper assertions, macro invocations, feature-specific branches and meaningful doctests must also be accounted for by domain mappings. For example, `request_id_wire_format.rs:141` checks 11 echoed request IDs, 11 callback IDs and a helper's three out-of-range rejects, not merely one property because it is one function.

The committed inventory retains source hashes, declaration/end lines, inline module qualification, cfg/module edges, Cargo target requirements, and exact loop/helper/macro source anchors and resolved literal case counts. The full reproducible census under `.plan/rust-v1-port-inventory/full-upstream-inventory.json` additionally retains code, loop iterables/literal values, helper source spans and full custom macro invocation arguments without duplicating those bodies in the committed manifest. Macro assertion cases are visible even though this pin has no declarative macro that generates additional `#[test]` functions. Non-Rust fences are retained rather than silently dropped. `no_run`/`ignore` does **not** by itself mean a described runtime scenario is irrelevant.

Feature gates are not semantic classifiers. In particular, `protocol_negotiation.rs:138:v1_only_router_preserves_legacy_fallback` is a v1 behavior test behind `unstable_protocol_v2`. HTTP-specific ID tests are recorded separately as lacking a Go HTTP API; a similar stdio test does not exercise HTTP. Rust auto-trait/ownership/proc-macro compilation contracts require individual dispositions rather than invented Go runtime equivalents.

### Canonical mapping identity

For a Rust function, the stable identity at this pin is:

```text
<workspace-relative source path>:<1-based declaration line>:<function name>
```

For example:

```text
src/agent-client-protocol/tests/request_id_wire_format.rs:141:v1_request_ids_round_trip_byte_for_byte
```

Inventory `entries` records canonical `id`, `source`, `function`, `line`, `status`, `classification`, `reason` plus richer evidence. `status` is:

- `needs-parent-review`: requires a source-specific domain mapping; **not** an assertion of ported coverage.
- `not-applicable`: inventory-owned exclusion with exact source and concrete missing-API/component/compile-contract rationale.
- `ported`: all mapped upstream properties are preserved; never inferred from source discovery.
- `partially-ported`: an explicitly reviewed mixed function has portable subcases mapped to real tests and other subcases excluded for concrete absent APIs. Its mapping also supplies `subcases: [{description, status, go_tests, reason}]` for every original property. This is reported separately, not counted as full Rust-function equivalence.

Domain mapping shape:

```json
{
  "upstream_revision": "050cd94c081056ace37a7b89abfed7f6220b47eb",
  "entries": [{
    "id": "src/agent-client-protocol/tests/request_id_wire_format.rs:141:v1_request_ids_round_trip_byte_for_byte",
    "source": "src/agent-client-protocol/tests/request_id_wire_format.rs",
    "function": "v1_request_ids_round_trip_byte_for_byte",
    "line": 141,
    "status": "needs-parent-review",
    "reason": "Replace with source-backed disposition and scenario-specific coverage.",
    "go_tests": [],
    "scenarios": ["11 exact request IDs; 11 callback IDs; 3 invalid numeric IDs"]
  }]
}
```

Doctests are in the separate `doctests` array. Their synthetic `function` is `doctest@<opening-fence-line>` and their ID uses the same source/line/function format. They retain fence flags, cfg context, source anchors and dispositions; full code stays in the reproducible raw census. Macros are separately inventoried; neither array is smuggled into the 937 function count.

The separate `supplemental` array records six intentional unannotated compilation probes: five callback-future lifetime regressions in `session_ordering.rs:131,140,152,164,176`, plus the generic child-connection return-type probe `protocol_v2.rs:88`. These are not `#[test]` registrations. The never-run `auto_traits.rs` helpers `futures_v1:135`, `futures_agent:166`, `futures_v2:183` remain subcase/function-value anchors of the existing `returned_futures_are_send:125` test, not three additional independent tests. The ordinary-function/compiler-contract discovery was checked against explicit compile/dead-code markers across the workspace.

## Source-ID reconciliation and remaining limits

The four reconciled mapping snapshots contain **806 distinct IDs**: wire 141, session 457, lifecycle 147 and schema 61. Independent reconciliation found **zero missing required IDs, zero duplicate IDs, zero unknown IDs and zero unresolved concrete Go test roots**, including cross-package references. These mappings classify 134 units as ported, 109 partially ported and 563 not applicable; 327 additional inventory-owned exclusions complete the 1,133 function/doc/supplemental source units. Macro definitions are not extra units in that total.

These numbers are **not test PASS counts** or proof of complete assertion fidelity. Multiple source functions can map to one Go test, a partial function is not full equivalence, and source loops/helper/macro properties need semantic review. Independent review found and corrected incoming routing, exact response association, negative method predicates, post-abandonment/barrier payloads, late-EOF drain and error-data equality gaps. The unchanged SDK baseline intentionally remains RED.

Source enumeration and identity reconciliation are independently cross-checked, but **semantic coverage is not established solely by those counts**. Each domain's source-specific scenarios and partial exclusions remain the authority for what is actually exercised. The root metadata checker must not treat a matching function count as proof of complete scenario coverage. It should reject unresolved, duplicate, unknown or nonexistent Go test references. No Rust checkout/toolchain is needed for normal Go test runs: the checked-in metadata and Go tests are sufficient.

At the initial census, inventory-owned exclusions include transport/component APIs absent from this Go SDK (HTTP/SSE/WebSocket, conductor orchestration/trace application, MCP polyfill/rmcp runtime, and yopo CLI), the draft-v2 example applications, three exact tracing-subscriber scenarios, non-Rust fences and explicit compile-fail doc contracts. Domain owners still classify core v1/shared versus v2-only, Rust compile contracts and absent high-level session/runtime APIs individually. Mixed functions must keep portable assertions and absent-API properties separate through structured subcase dispositions; mapping a wire subset alone does not make the complete function ported. No blanket v2-gate or file-name exemption is used to resolve those entries.

The schema crate `agent-client-protocol-schema = 1.15.1` is a **separate crates.io dependency**, not a workspace member. Its source-only archive was separately inspected: 32 Rust files, 498 test declarations, two rustdoc candidates and six macros. Those 498 are **not** included in the SDK's 937 or the first test-port commit: the approved first-port scope is the SDK clone workspace's v1/shared scenarios, and Cargo does not execute dependency unit tests with the workspace suite. Source-path partitions are 178 `src/v1/`, 289 `src/v2/`, and 31 shared; path counts are not semantic applicability decisions. Their v1 cases may inform separately attributed additional regressions in the second commit. See [provenance](PROVENANCE.md) for the independent pin/checksum and scope decision.

## Explicit provenance verification

The standalone Python standard-library tools are included under [`tools/`](tools/); raw census artifacts and intermediate copies remain under `.plan/rust-v1-port-inventory/`. They do not install a parser/toolchain or change the Rust checkout. They require Python 3.11+ (`tomllib`) and local Git; they are not invoked by normal Go tests.

```sh
python3 conformance/rustv1/tools/inventory.py \
  --rust-root /path/to/pinned/rust-sdk \
  --verify conformance/rustv1/upstream_inventory.json

python3 conformance/rustv1/tools/verify_census.py \
  --rust-root /path/to/pinned/rust-sdk \
  --inventory conformance/rustv1/upstream_inventory.json
```

The first check uses a comment/string-masking, balanced-delimiter scanner. The second independently scans anchored attributes/function declarations, doc fence lines and macro definitions, and compares complete source file hashes. Both are **explicit source-provenance checks**, not normal Go test prerequisites or a replacement for semantic mapping review. Cargo is unavailable in the inventory environment, so no `cargo test --list`, compiler expansion, Rust execution or Rust test pass is claimed.

## Attribution

Upstream Rust SDK source and tests are licensed **Apache-2.0**. Ported source-derived tests should keep useful upstream attribution and identify their exact source function/path/subcases in comments and mappings. Repository versions, licenses, source provenance and external dependency scope are documented in [PROVENANCE.md](PROVENANCE.md).

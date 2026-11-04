# Baseline before SDK fixes

This records the first Rust-derived test stage against unchanged Go production code at **`09349ff9c8b24d6a8f845b7959dc614406b14a57`**, also annotated release `v0.6.0`. The upstream Rust pin is **`050cd94c081056ace37a7b89abfed7f6220b47eb`**. These historical results are not the expected result after the SDK fixes.

Executed on Linux with Go 1.27.3, November 4, 2026. No production implementation, checked-in schema, generated code, or dependency changed before this baseline. `go vet ./conformance/rustv1/...` and formatting checks passed. The original repository `go test ./...` passed before the port was added.

## Source accounting

The [source inventory](upstream_inventory.json), four domain mappings, and [coverage checker](coverage_test.go) account for:

- 937 registered Rust test functions;
- 190 documentation code fences (173 Rust candidates);
- six separately identified unannotated compiler-contract probes;
- 31 reviewed macro definitions, not counted again as test registrations.

All 1,133 function/doc/supplemental source items have a disposition: **134 ported, 109 partially ported, 890 excluded**, with zero missing IDs, duplicate IDs, unknown IDs, or undeclared Go test roots. Exclusions include v2-only scenarios, non-Rust fences, Rust compilation/ownership contracts, and APIs/components that this Go SDK does not implement. Partial mappings enumerate covered and unavailable properties; they are not full Rust-function equivalence. See [README.md](README.md) and [PROVENANCE.md](PROVENANCE.md).

The external schema crate's 498 declarations are not part of the SDK workspace's own test execution and are excluded from this first-port baseline. Its v1 tests may inform separately attributed additional regressions.

Counts establish accounting, not assertion completeness or ACP certification. Semantic review also compared original assertions, helper loops, macro cases and sequencing. It caught and corrected weakened ports: outgoing-only checks standing in for incoming dispatch, erased response-type association, missing negative method controls, missing post-cancel RPC liveness, weakened data comparisons and stateful sequences reset between cases.

## Executed test results

```sh
go test -json -count=1 -timeout=120s ./conformance/rustv1/...
```

The command **compiled all packages and exited 1**, without an uncontained process panic or package timeout. These are top-level Go test groups, not counts of Rust scenarios or distinct defects:

| Package | Pass | Fail | Skip |
| --- | ---: | ---: | ---: |
| Root source-accounting check | 1 | 0 | 0 |
| `wire` | 25 | 25 | 0 |
| `lifecycle` | 19 | 10 | 0 |
| `session` | 37 | 8 | 0 |
| `schema` | 29 | 8 | 0 |

The schema pass count includes two subprocess support entrypoints; the session count includes one. They are not claimed as independent protocol coverage. Failing request-ID/method-kind cases run in owned subprocesses so an SDK panic fails the enclosing test without aborting other packages.

The parent retained complete JSON output in local `.plan/rust-v1-port/ported-baseline.jsonl`; worker execution evidence remains in the isolated audit worktrees. Logs are not dependencies of the committed tests. The command above produces a new transcript when run at this first-port commit.

### Ordering race detected by an actual ported scenario

The single baseline run passed `session/TestToolUpdatesFollowAnnouncements`. Repetition exposed the existing race:

```sh
GOMAXPROCS=4 go test -json -count=100 -timeout=60s \
  ./conformance/rustv1/session -run '^TestToolUpdatesFollowAnnouncements$'
```

**95 passed, 5 failed.** Failures observed updates referencing an unannounced call or a final status regressing from `completed` to `pending`/`in_progress`. The observer is protected by a mutex; this is application-visible order reversal, not an unsynchronized test map. No sleeps or invented agent policy were added to create the failure. These are the Rust Testy observer's portable source expectations.

That establishes relevant upstream coverage, but it is not a deterministic gate for the scheduling bug. Explicit callback gates and response-completion assertions belong in the subsequent local-regression stage, with their Go API policy stated separately.

## What the port detects

| Area | Representative tests | Interpretation |
| --- | --- | --- |
| Large/null request ID fidelity | `wire/TestRequestIDWireSequence`, `TestRequestIDWireFormat`, `TestRawIDRoundTrip`, `TestV1NullInitialize` | Valid v1 IDs are corrupted or misclassified; the null-ID process exit is contained. Rust UUID allocation is not imposed on Go. |
| Stateful invalid-frame handling | `wire/TestUnsupportedBatches`, `TestInvalidRequestIDsWireError`, `TestMalformedFrameErrors` | Error shape/classification plus the original same-connection sequences. Batch/recovery/diagnostic policies are explicitly labeled. A failing early frame may prevent later phases executing; they are not claimed successful. |
| Envelope/error shape validation | `wire/TestRawMessageShape`, `TestMalformedRawErrorsRejected`, `TestMalformedFrameClassification` | Conflicting members, invalid parameter/error forms and structural classification. |
| Raw/structured error fidelity | `wire/TestRawErrorsPreserveFields`, `TestWrappedStructuredError`, `TestExplicitSchemaErrorProjection`; `session/TestRawInitializeErrorPreservesFields` | Unknown raw error fields and wrapped data are lost; schema projection loses explicit-null data. No invented raw-versus-typed Go runtime flag is required. |
| Inner MCP error fidelity | `schema/TestMcpInnerOutcomes`, `session/TestV1MCPOutcomesPreserveRawErrors` | Opaque result controls pass; inner error extension fields are lost in decoder/constructor paths. |
| Request/notification direction | `schema/TestMcpRequestWrongDirection`, `TestMcpNotificationWrongDirection` | Real receiving handlers expose process-fatal wrong-kind dispatch, rather than masking it with absent handlers. |
| Required protocol fields | `schema/TestActedOnSessionSetupLists`, `TestMalformedSystemPrompts`, proxy tests; `wire/TestRequiredParamsErrorAndRecovery`; `session/TestMalformedInitialization` | Required properties are accepted when absent. Null-default arrays remain separate positive controls. Custom Go extension structs do not acquire an invented automatic required-field validator. |
| Metadata recovery | Proxy/successor metadata cases in `schema` and `session` | Invalid annotated optional metadata is rejected rather than defaulted locally. |
| Closed v1 enum | `schema/TestV1PromptRejectsV2ErrorStopReason` | The explicit v1 assertion inside a v2-gated upstream test is retained. |
| Per-request cancellation | `lifecycle/TestOptionalIncomingRequestCancellation`, outgoing/successor/parser cases; `wire/TestRequestIDsSeparatePerDirection` | Optional Rust SDK cancellation profile, not a mandatory ACP feature claim. |
| Half-close, errors and drain | `lifecycle/TestFinalResponseSurvivesInputHalfClose`, `TestReadErrorIsNotHiddenBehindBackpressuredDrain`, EOF-cause tests | Actual Rust preservation/drain policy, not the earlier audit shorthand that every EOF cancels all work. |
| Process cleanup | `session/TestCommandShutdownEscalatesToSIGKILL` | Rust intentional-shutdown result policy; distinct from unexpected process failure. |
| Initialization and diagnostic policies | `session/TestInitializationGateWithHandler`, unknown-request tests | Explicit Rust SDK policy expectations, not undocumented claims of universal automatic Go application behavior. |

## Gaps for the next, separately committed stage

The first suite does not establish every property found in the audit:

- Deterministic session-update application order and prompt/load success/error completion boundaries; reentrant notification callbacks must remain live.
- Persistent pending-call retention after a canceled wait when the peer never sends a late response. Existing late-response tests do not prove the table or shutdown behavior in that case.
- Deadlines during actual built-in stream writes and while queued for write serialization. The Rust-derived fatal-read-error/drain cases are different.
- Constructor-versus-decoded Go named select-value identity.
- Stable session-info and compaction omission/null/value patch fidelity. Those stronger serde cases live in the external schema crate, not this SDK workspace's own suite.
- Tolerant pointer decoding that retains allocated/partial values, and known config variants incorrectly surviving invalid-item filtering.
- Metadata recovery across ordinary content/update unions, beyond the proxy-object case already caught.
- Ordinary known-method kind inversion, complete-frame suffix rejection and concurrent adjacent large-ID correlation as focused local regressions where they add distinct coverage.
- Containment of invalid application-produced generated results, explicitly labeled as Go reliability rather than peer-input-only ACP nonconformance.

Additional tests are to live outside `rustv1/` and in a later commit. Production fixes follow both test commits. Passing the source-accounting test alone, or the original `go test ./conformance` command without `...`, is not a behavioral conformance result.

# Additional-regression baseline before fixes

These tests were added **after** the Rust-derived port commit `0823ac1`. Production code still matches v0.6.0 / `09349ff9c8b24d6a8f845b7959dc614406b14a57`. This is the second, local test stage; no implementation change is part of it.

Executed November 4, 2026 on Linux with Go 1.27.3:

```sh
go test -json -count=1 -timeout=90s ./conformance/regressions/...
go test -v -count=1 -timeout=15s ./internal/jsonrpc2 \
  -run '^TestWrittenCallContextCancellationReleasesPendingState$'
go vet ./conformance/regressions/... ./internal/jsonrpc2
```

Both test commands compile and exit **1** as intended. Vet and formatting checks pass. No test skips, uncontained package panic, timeout, or cleanup failure occurred in the captured parent run.

| Package | Passing top-level groups | Failing top-level groups | Passing named leaves | Failing named leaves |
| --- | ---: | ---: | ---: | ---: |
| `ordering` | 0 | 3 | 0 | 6 |
| `lifecycle` | 0 | 5 | 0 | 11 |
| `schema` | 1 | 9 | 19 | 24 |
| `wire` | 0 | 5 | 1 | 12 |
| Private pending-state test | 0 | 1 | — | — |

Groups, named leaves and assertions are different counts, not distinct bug totals. The private test contains cancellation and deadline phases in one flow; both fail before any peer response/EOF, and both late-response/live-call controls then succeed. The public schema and wire tests retain passing controls inside failing groups.

Complete parent transcripts are local audit artifacts under `.plan/rust-v1-port/` (`additional-baseline.jsonl`, `retention-additional-baseline.log`). They are not required to run the tests and are not checked into these packages. Historical RED results are not expected after SDK repairs.

## Which audit properties needed additions?

| Property | Rust-derived coverage | Local addition |
| --- | --- | --- |
| Stream update order | The tool-update observer failed 5/100 runs. | Deterministic gated A/B; prompt/load success/error boundaries; received prefix excludes later state updates; nested request/reverse callback stays live. |
| `$/cancel_request` support | Explicit optional-feature profile already fails. | No duplicate optional-support suite. Application-owned `session/cancel` decisions are not recast as missing SDK automation. |
| Abandoned call retention without a late reply | Late-response ports deliberately supply the response and do not prove retirement without it. | Exact internal Call/Await pending-state check before response/EOF, plus public Close after an unanswered abandoned request and a subsequent healthy RPC. |
| Select value dynamic Go type | JSON values survive upstream codec scenarios. | Constructor-versus-decoded named-ID equality and boolean-false controls; explicitly Go API consistency. |
| Large/null IDs | Sequential source tables and opposite-direction scope already catch corruption. | Adjacent large IDs simultaneously active in one incoming direction; exact correlation and both handler entries. |
| Method kind | MCP receiving-direction ports expose the underlying panic. | Ordinary implemented ACP methods on both sides, correct-form controls, no invocation/reply for wrong-kind notifications, contained failure processes. |
| Envelope parsing | Source tests cover ordinary malformed frames and conflicting members. | Valid prefix plus garbage/second object cannot execute; empty string method stays a request member and receives unknown-method handling. |
| Error fidelity | Wrapped data, raw extensions and schema data:null already fail in the port. | No redundant error-fidelity matrix. |
| Required payload fields | Port covers missing required keys and some wrong types. | Explicit-null scalar/array/nested-union rejection, with valid zero/empty and annotated default-on-null controls. |
| Session/compaction clears | Stronger serde tests live in the external schema dependency, not the SDK workspace suite. | Omission/null/value codec cases and existing null markers; one real initialized SessionInfoUpdate sender/receiver boundary case. |
| Tolerant decode rollback / config branch filtering | Not established by the concrete SDK workspace scenarios. | Invalid pointer/partial cost defaults, valid sibling retention, malformed known variants dropped without losing valid items. |
| Metadata recovery | Proxy-object recovery is already RED. | Ordinary content/update union recovery and null-sensitive controls, not just the proxy shape. |
| Context/write lifecycle | Source fatal-error and graceful-drain policies are different from these Go behaviors. | Actual active/queued IOTransport context expiry, no late canceled send, serving-context cancellation and non-EOF fatal failure cleanup. |
| Invalid application result | Not caught by the source paths exercised through extension adapters. | Generated-handler encoding failure produces InternalError and a healthy followup; explicitly conditional caller-error reliability. |
| Unknown elicitation response payload | Additional source-informed gap, not an original confirmed transport failure. | Unknown vendor fields and same-name opaque content survive; malformed known accept content does not acquire an unknown fallback. |

The schema additions record their separate `agent-client-protocol-schema` 1.15.1 provenance; they are not retroactively counted as Rust SDK workspace ports.

## Policy limits retained

- Ordinary request callbacks remain concurrent; prior tool-announcement state and per-session scheduling isolation are not newly imposed universal guarantees. (Later superseded for request start order: requests now wait for preceding update callbacks. See [ordering](ordering/README.md).)
- A clean input EOF may be a half-close. The added tests do not demand cancellation of all work on every EOF; Rust-derived final-reply/drain tests remain authoritative controls.
- Queue readiness is observed at public formatting progress, not by inspecting a private mutex. The live-context deadline case complements the deterministic precheck/encoding race case.
- Private pending-count inspection is narrowly justified for the Go memory-retention property. It is not a conformance assertion about another SDK's registry strategy.
- Malformed-frame connection survival and exact wrong-kind error-code choice are not invented ACP requirements. The tests permit the explicitly documented alternatives.

Implementation fixes must come after this separate local-test commit. No passing control, unsupported-feature label or cleanup action is used to hide a failing desired-behavior assertion.

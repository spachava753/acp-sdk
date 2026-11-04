# Local regressions added after the Rust v1 port

The upstream-derived suite was committed first as **`0823ac1`**, under [`../rustv1`](../rustv1/README.md). These packages add properties that its concrete scenarios do not establish, or make a race deterministic. They are not counted as Rust SDK workspace ports.

```sh
go test ./conformance/regressions/...
go test ./internal/jsonrpc2 -run '^TestWrittenCallContextCancellationReleasesPendingState$'
```

The second command covers a deliberately narrow Go-private resource assertion. Public Close can mask a retained pending entry by provoking EOF, so that test checks the real Call/Await pending table before any response or closure. It adds no inspection API, reflection, unsafe code, or fixture-side retirement. This is Go lifecycle behavior, not portable ACP conformance.

| Package | Added properties |
| --- | --- |
| [`ordering`](ordering/README.md) | Deterministic session-update order; prompt/load success/error completion barriers for the received prefix; later state updates do not extend that prefix; nested requests and reverse callbacks stay live. |
| [`lifecycle`](lifecycle/README.md) | Public Close after abandonment with no late response; active/queued built-in stream write cancellation; serving-context cancellation; genuine non-EOF failure cleanup. |
| [`schema`](schema/README.md) | Omitted/null/value patch meaning, transactional recovery, variant filtering, Go named-ID consistency, metadata recovery and opaque unknown-action payloads. Separate schema-crate attribution is recorded there. |
| [`wire`](wire/README.md) | Ordinary method-kind inversion, complete-frame parsing, simultaneous adjacent large-ID correlation and generated-result encoding containment. |

These tests distinguish protocol requirements, schema/generator recovery rules, Go API policies and conditional caller-error robustness. Per-request cancellation remains an optional profile in the Rust-derived suite. Session identity, capability policy and user permission decisions remain application responsibilities; a fixture is not a substitute implementation of those features.

Clean input EOF can be a half-close and must preserve the Rust-derived final-response/drain behavior. The local lifecycle tests do not replace that with an unconditional cancel-everything-on-EOF rule. Similarly, ordering tests do not impose a generic permission-callback state barrier or promise per-session scheduling isolation.

The desired assertions are intentionally RED against unchanged v0.6.0. They do not pass by expecting a bug or skip known failures. See [the recorded pre-fix baseline](BASELINE.md). Production repairs must follow this separate test commit, not be hidden inside the port or these additions.

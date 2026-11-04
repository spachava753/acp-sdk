# Local wire regressions added after the Rust port

These are **local ACP Go SDK additions**, not Rust SDK workspace ports or copied external schema-crate fixtures. They belong in the second test commit, after the upstream-derived suite committed by the parent as `0823ac1`. They exercise unchanged production code at `09349ff9c8b24d6a8f845b7959dc614406b14a57` / peeled `v0.6.0`.

The first-stage comparison is recorded in `conformance/rustv1/BASELINE.md`. This package adds distinct properties that its Rust-derived tests do not establish:

| Local test | Additional property | First-stage boundary / qualification |
| --- | --- | --- |
| `TestKnownMethodKinds` | Ordinary implemented ACP methods cannot cross request/notification handler contracts or terminate the process. Agent: `session/new` as notification, `session/cancel` as request. Client: `fs/read_text_file` as notification, `session/update` as request. | The first port's receiving kind tests used MCP-specific dispatch. Every new case has a successful correct-form control, valid parameters, and an implemented handler; absence of a handler cannot mask the bug. |
| `TestWholeFrameRejectsSuffix` | A complete NDJSON frame containing a valid request plus garbage or a second object is rejected as a whole; its valid prefix never executes or produces success. Covers public message helpers and actual `RunAgent` dispatch. | Ordinary malformed-JSON probes do not expose a single-Decode parser accepting a valid prefix and silently losing the suffix. This test does **not** require keeping a malformed connection open or processing a subsequent RPC. |
| `TestEmptyMethodIsUnknownRequest` | An explicitly present empty string method remains a request, encodes with its method member, and receives MethodNotFound with the same ID before a healthy followup. | Missing methods are different from an empty method string. This requires unknown-method handling, not implementation of an empty-name ACP method. |
| `TestAdjacentLargeIDsRemainIndependent` | Two adjacent legal IDs above `2^53` remain distinct while simultaneously active in one incoming direction. Both handlers enter, each response uses its original numeric ID and matching session result. | The Rust table was sequential; its same-ID concurrency test covered opposite directions. Here `9007199254740995` and `9007199254740996` collapse to the same float64 value on the defective decoder. |
| `TestUnencodableHandlerResultIsContained` | A generated handler returning an unencodable `Meta` channel produces an InternalError instead of a worker panic, and a healthy followup still succeeds. Includes a typed extension encoder control. | **Conditional Go caller-error reliability**, not valid peer-input-only ACP nonconformance or a security finding. A channel cannot arrive through valid JSON. The direct generated-result path differs from the extension adapter's existing encoding guard. |

## Requirements and policy choices

- [ACP v1 RequestId](https://agentclientprotocol.com/protocol/v1/schema#requestid) permits signed int64 IDs. [JSON-RPC request/response correlation](https://www.jsonrpc.org/specification#request_object) requires replying with the same value. Both chosen IDs are within range. An independent exact-number parser uses `json.Number` and `math/big.Rat`; no float64 or JavaScript number parsing is an oracle.
- [ACP v1 overview](https://agentclientprotocol.com/protocol/v1/overview#communication-model) and named method definitions distinguish methods from notifications. [JSON-RPC notification semantics](https://www.jsonrpc.org/specification#notification) forbid success/error replies to notifications. Guarding an ordinary method's handler kind is a local SDK behavior requirement. An ID-bearing wrong-kind request may return either named `CodeInvalidRequest` or `CodeMethodNotFound`; no universal code choice is invented.
- [ACP v1 stdio framing](https://agentclientprotocol.com/protocol/v1/transports#stdio) defines individual newline-delimited JSON-RPC messages. A valid JSON prefix does not make an invalid complete frame valid. Standard parse-error response tests already exist in `rustv1`; these local cases test non-execution and no successful prefix response, not a new recovery mandate.
- InternalError containment for an application-produced unencodable Go value is explicitly an SDK reliability policy. It must not be presented as a requirement to support channels in protocol JSON.

## Fixture and cleanup boundaries

Only exported `jsonrpc` helpers, public `RunAgent` / `Connect` / `IOTransport`, public typed handlers and extension registration are used. No internal map inspection, reflection into SDK state, private hooks or SDK fixes are involved. File callbacks return a constant and never access real files. Paths are absolute wire fixtures only.

Each potentially process-fatal scenario re-executes its test in an owned subprocess. Success means the desired assertions pass; a current panic is a test **failure**, not a characterization success. The child has a 10-second timeout and parent supervision is bounded to 12 seconds. Individual wire/operation waits use two-second bounds.

Independent input/output pipes permit input half-close while a collector keeps draining output. Retained raw closers unblock failure cleanup. Collector writes are bounded and abortable; result channels are buffered so producers can finish if assertions fail.

Correct request/error replies are read before input half-close. Notification absence checks first read a normal follow-up request barrier, then close/drain and inspect handler counts and the complete output transcript. Thus an early-EOF reply-drop bug cannot masquerade as a method-kind failure. Malformed-frame cases observe a reply, EOF, or bounded silence before draining and explicitly permit either termination or recovery. Collision handlers use gates: the first stays active while the second request and a read-loop barrier arrive; the test separately waits for actual second-handler entry. Cleanup-only gate release does not simulate successful SDK dispatch.

## Actual baseline run

```sh
GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local \
  go test -json ./conformance/regressions/wire -count=1 -timeout=90s
```

The final worker run exited **1** in **6.052 seconds**: five top-level regression groups **FAIL**, zero skips, no package-wide timeout/panic. The nested extension encoder control **PASSed**; it is a positive control, not an additional gap claim. The parent independently reran the final package. Complete local transcripts are retained as audit artifacts rather than committed into this package. See the [consolidated pre-fix baseline](../BASELINE.md) for the final integrated cases; the command above produces a fresh transcript.

Observed desired-assertion failures:

- All four ordinary method-kind inversions reach SDK worker panics in their isolated processes. Their correct-form controls completed first.
- Both invalid complete frames execute their first request once and send a successful prefix response; helper decoding also incorrectly accepts the prefix.
- An empty string method is decoded as a response, omitted during encoding, and silently receives no live unknown-method reply.
- The adjacent-ID case never enters the distinct second handler, answers numeric `9007199254740996` with the first request's `/first` result, and produces no second reply before the bounded wait.
- The generated `Meta` channel result terminates its subprocess while the equivalent extension control returns InternalError and serves a healthy followup.

These are RED tests of desired behavior against the unchanged SDK. They do not assert bugs as success, add broad fuzzing, import v2 requirements, or claim full ACP certification. No production/generated/schema/dependency changes, staging, commits or pushes were made in this stage.

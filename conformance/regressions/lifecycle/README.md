# Local Go lifecycle regressions

These are local additions after the Rust-derived test commit `0823ac1`, not Rust SDK workspace ports or external schema-crate fixtures. They test selected **Go SDK context/resource policies**, not new mandatory ACP requirements. Production baseline: `09349ff9c8b24d6a8f845b7959dc614406b14a57` (`v0.6.0`).

| Tests | Desired property |
| --- | --- |
| [`TestAbandonedCallDoesNotHoldPublicClose`](abandoned_call_test.go) — cancel/deadline | After the peer observes a request, its caller can abandon the wait. The peer **never** answers that ID but answers a later RPC. Public `Close` must then reach the underlying closer and finish without test-induced EOF. Optional request-cancel notifications are ignored. |
| [`TestActivePartialWriteHonorsOperationContext`](write_context_test.go) — cancel/deadline | Real built-in `IOTransport` writes one byte to `net.Pipe`, then blocks. Ending the operation context must return its context cause **and release the actual Write**, not merely detach a blocked writer. No stream reuse is required after aborting this partial JSON frame. |
| [`TestCanceledQueuedWriteNeverSendsAndStreamRemainsUsable`](write_context_test.go) — two cases | Canceled unsent work must return while an unrelated writer remains held, never appear after release, and leave the stream usable for a later complete write. The encoding-cancel case isolates the precheck-to-serialization race. The live-context case remains active at the public formatting barrier, then reaches its real deadline while the first writer is still held; a pre-lock recheck alone is insufficient. |
| [`TestRunAgentContextOwnsServingLifetime`](serving_context_test.go) — idle/pending × cancel/deadline | Ending the serving context must finish `RunAgent`, close its connection, and cancel/join cooperative pending handler work. Clean completion or a context-cancellation error is accepted; no exact message is imposed. |
| [`TestFatalReadErrorCancelsCooperativeHandler`](serving_context_test.go) | A genuine **non-EOF** transport failure must release cooperative handler work and complete serving with the failure cause. This must not be generalized to clean input half-close. |

## Boundaries and cleanup

All assertions use public `Client`, `RunAgent`, `Transport`/`Connection`, handler contexts, or real `IOTransport` streams. There is no pending-map, private lock, goroutine-stack, or private dispatch inspection.

`testing/synctest` establishes quiescence for channel/pipe cases and advances deadline tests in fake time. Serialization contention is not synctest-durably-blocked, so the queued-write cases use real context/time bounds. The live-context case observes public error-formatting progress while its context is active; it deliberately does **not** claim an instruction-level observation of `Mutex.Lock`. The first writer remains held throughout the deadline and the canceled-completion assertion.

Raw transport/stream closure is an independent **cleanup fallback after assertions**, never the event that makes a Close/cancellation check pass. Test-owned operations are joined after forced endpoint closure. The accepted background-context first writer is released by peer reads, not by a claim that graceful Close must interrupt arbitrary writers.

Clean EOF is not asserted to cancel all inbound work. The separately committed Rust-derived tests retain final-response/input-half-close behavior. No application `session/cancel`, authorization, filesystem, or terminal policy is implemented by these fixtures.

## Baseline results

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  go vet ./conformance/regressions/lifecycle

GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  go test ./conformance/regressions/lifecycle -v -count=1 -timeout=25s

GOMAXPROCS=4 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  go test ./conformance/regressions/lifecycle -json -count=2 -timeout=25s
```

Executed November 4, 2026, Linux, Go 1.27.3, against unchanged production:

- Formatting and `go vet`: **PASS**.
- Single focused run: compiles, **11 failing leaf cases**, no skips, panic, or package timeout; approximately 3.4 seconds including build/command overhead.
- Two repetitions with `GOMAXPROCS=4`: every leaf case fails twice; approximately 6.3 seconds. No cleanup timeout, scheduling-precondition failure, or unjoined task was reported.
- The failures are desired RED assertions, not characterization probes expecting the bugs. The abandoned calls return their context errors and the later RPC succeeds, but Close remains blocked. Active physical writes remain blocked. Both queued-write cases send the canceled frame after release while still allowing the later write. Serving cancellation and fatal-read cleanup leave handler/serving work pending until the independent cleanup fallback.

The commands produce the current transcript; historical RED results are not the expected outcome after SDK fixes.

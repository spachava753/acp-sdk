# Deterministic local update-order regressions

These tests are local additions after Rust-port commit `0823ac1`. They are not attributed to an upstream Rust test or presented as universal ACP callback-settlement requirements.

The ported `session/TestToolUpdatesFollowAnnouncements` already exposed the scheduling bug in 5 of 100 baseline runs. This package makes the defect deterministic and adds the prompt/load completion and reentrancy properties that the available Go API needs.

```sh
go test -v -count=1 -timeout=30s ./conformance/regressions/ordering
```

## Chosen Go SDK contract

- `SessionUpdate` callbacks apply in receive order. A blocked callback cannot be overtaken by a later update callback.
- Normal `SessionPrompt` and `SessionLoad` calls, on success or error, do not return before their preceding update callbacks have completed.
- The completion boundary is the prefix received before that response. Later permitted session-state notifications must not keep an earlier call pending.
- A notification handler can use its supplied context for a nested request, including one that requires a reverse client callback. Handler-initiated calls may bypass the completion barrier so they cannot wait on their own handler. Ordering must not lock the reader or serialize every request behind a blocked notification.

ACP v1 defines agent-side update/response wire sequencing and per-ID compaction application order. It does not prescribe every Go callback completion policy. These tests deliberately choose stronger usable Go API behavior without calling it a new ACP MUST. Their independent peer presents only v1 messages and declares load support; filesystem support is advertised in the nested callback case.

Ordinary request callbacks remain concurrent. The tests do not require permission callbacks to see all earlier tool-announcement effects: ACP does not require a prior announcement in every case, and clients must handle the permission request's payload independently. Nor do these tests promise per-session scheduling isolation. Those are separate API policy choices, not hidden requirements of this repair.

## Tests and controls

- `TestSessionUpdatesAndResultRespectReceivedPrefix`: a channel holds update A, then the peer queues update B and the matching result/error. After runnable work settles, B and the public operation must still be pending. Releasing A must produce AB, with the original result/error intact. Both prompt and load are exercised.
- `TestPromptDoesNotWaitForUpdatesAfterItsResponse`: a later legal `session_info_update` is held independently. Once the preceding update finishes, the earlier prompt must finish even while that later callback remains blocked. This rejects a lazy or ever-growing queue-tail barrier.
- `TestUpdateCallbackCanAwaitNestedRequestAndReverseCallback`: update callback → agent extension request → client filesystem callback → extension response. The outer prompt remains behind its update, while the nested chain makes progress. The handler propagates its context; failure cleanup can cancel that nested wait without private SDK hooks.

The public custom transport is only an independent message source/sink. It implements no dispatcher, state projection, notification queue, or response barrier. `synctest.Wait` establishes quiescence before absence assertions. Buffered result/observation channels let producers finish independently. Test-owned gates are released before raw transport cleanup, so cleanup is not what makes an ordering assertion pass.

At the unchanged v0.6.0 baseline, all six leaf cases fail: callbacks apply BA, prompt/load results overtake earlier callbacks, and the outer prompt returns during nested work. The nested request/reverse-callback chain itself completes, which is a control that a future ordering fix must preserve. There are no sleeps used to manufacture ordering, skipped failures, or assertions that expect the bug as success.

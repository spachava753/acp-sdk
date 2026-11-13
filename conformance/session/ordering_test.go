// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// These tests hold an update callback open, let every runnable goroutine
// settle with synctest.Wait, check what has and has not happened, then
// release the callback and check again. A negative check proves nothing if
// the frame it depends on has not yet reached the SDK's read loop, so tests
// follow that frame with a barrier.

// gate is a channel that a test closes once to release a held callback.
type gate struct {
	ch   chan struct{}
	once sync.Once
}

func newGate() *gate  { return &gate{ch: make(chan struct{})} }
func (g *gate) open() { g.once.Do(func() { close(g.ch) }) }

// wait blocks until the gate opens or ctx ends.
func (g *gate) wait(ctx context.Context) error {
	select {
	case <-g.ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isClosed reports whether ch is closed, without blocking.
func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// tryRecv receives from ch if a value is ready, without blocking.
func tryRecv[T any](ch <-chan T) (T, bool) {
	select {
	case v := <-ch:
		return v, true
	default:
		var zero T
		return zero, false
	}
}

// barrier sends a notification the client ignores. The SDK's transport
// reads one line ahead of its read loop and hands each line over before
// reading the next, so this send completes only after the read loop has
// taken every earlier frame. A read loop stalled behind a held callback
// fails the send.
func barrier(t *testing.T, peer *acptest.Peer) {
	t.Helper()
	peer.Notify("_barrier", map[string]any{})
}

// startOrderingClient connects a client inside the current synctest bubble
// and initializes it against an agent that supports session/load.
func startOrderingClient(t *testing.T, handler any) (*acp.Client, *acptest.Peer) {
	t.Helper()
	client, peer := acptest.StartClient(t, handler)
	acptest.InitializeClient(t, client, peer, nil, map[string]any{"loadSession": true})
	return client, peer
}

// heldUpdates holds the callback for update "A" until released, and records
// the text of every update once its callback finishes.
type heldUpdates struct {
	startedA chan struct{}
	startedB chan struct{}
	releaseA *gate
	applied  chan string // buffered for both updates
}

func (h *heldUpdates) SessionUpdate(ctx context.Context, n *acp.SessionNotification) error {
	content, ok := n.Update.Content.(acp.ContentBlock)
	if !ok {
		return fmt.Errorf("content type %T", n.Update.Content)
	}
	switch content.Text {
	case "A":
		close(h.startedA)
		if err := h.releaseA.wait(ctx); err != nil {
			return err
		}
	case "B":
		close(h.startedB)
	}
	h.applied <- content.Text
	return nil
}

// TestUpdatesApplyBeforeResponse checks that session updates apply in receive
// order and that session/prompt and session/load, whether they succeed or
// fail, return only after the updates received before their response have
// been applied. Update B and the response both arrive while A's callback is
// held; neither B's callback nor the call may finish until A is released.
// The barrier after them also shows the held callback does not stop the SDK
// reading later frames.
func TestUpdatesApplyBeforeResponse(t *testing.T) {
	for _, tc := range []struct {
		method string
		fail   bool
	}{
		{"session/prompt", false},
		{"session/prompt", true},
		{"session/load", false},
		{"session/load", true},
	} {
		name := tc.method + "/success"
		if tc.fail {
			name = tc.method + "/error"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := &heldUpdates{startedA: make(chan struct{}), startedB: make(chan struct{}), releaseA: newGate(), applied: make(chan string, 2)}
				defer h.releaseA.open()
				client, peer := startOrderingClient(t, h)

				// Load needs no prior session; it restores a saved one.
				session := acp.SessionId("saved")
				var done <-chan acptest.Result[any]
				if tc.method == "session/prompt" {
					acptest.NewSession(t, client, peer, "s")
					session = "s"
					done = acptest.Go(func() (any, error) {
						r, err := client.SessionPrompt(context.Background(), &acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{acp.TextContentBlock("question")}})
						return r, err
					})
				} else {
					done = acptest.Go(func() (any, error) {
						r, err := client.SessionLoad(context.Background(), &acp.LoadSessionRequest{SessionID: session, Cwd: "/workspace", McpServers: []acp.McpServer{}})
						return r, err
					})
				}
				request := peer.ExpectCall(tc.method)
				peer.Update(string(session), "A")
				acptest.Await(t, h.startedA)
				peer.Update(string(session), "B")
				switch {
				case tc.fail:
					peer.ReplyError(request.ID, jsonrpc.CodeInternalError, "remote failure", nil)
				case tc.method == "session/prompt":
					endTurn(peer, request)
				default:
					peer.Reply(request.ID, map[string]any{})
				}
				barrier(t, peer)

				synctest.Wait()
				if isClosed(h.startedB) {
					t.Error("B's callback started before A's callback returned")
				}
				result, returned := tryRecv(done)
				if returned {
					t.Errorf("%s returned before the update received earlier was applied", tc.method)
				}

				h.releaseA.open()
				synctest.Wait()
				if !returned {
					result = acptest.Await(t, done)
				}
				if got := acptest.Await(t, h.applied) + acptest.Await(t, h.applied); got != "AB" {
					t.Errorf("updates applied as %q, want AB", got)
				}
				var remote *jsonrpc.Error
				switch {
				case tc.fail:
					if !errors.As(result.Err, &remote) || remote.Code != jsonrpc.CodeInternalError || remote.Message != "remote failure" {
						t.Errorf("error = %v, want the peer's InternalError", result.Err)
					}
				case result.Err != nil:
					t.Error(result.Err)
				case tc.method == "session/prompt":
					if r := result.Value.(*acp.PromptResponse); r.StopReason != acp.StopReasonEndTurn {
						t.Errorf("prompt result = %#v, want end_turn", r)
					}
				}
			})
		})
	}
}

// splitUpdates holds a message chunk and a session_info_update on separate
// gates.
type splitUpdates struct {
	chunkStarted, infoStarted chan struct{}
	chunkRelease, infoRelease *gate
}

func (h *splitUpdates) SessionUpdate(ctx context.Context, n *acp.SessionNotification) error {
	started, release := h.chunkStarted, h.chunkRelease
	if n.Update.SessionUpdate == acp.SessionUpdateTypeSessionInfoUpdate {
		started, release = h.infoStarted, h.infoRelease
	}
	close(started)
	return release.wait(ctx)
}

// TestPromptDoesNotWaitForLaterUpdates checks that a prompt waits only for
// the updates received before its response. Session state updates may arrive
// at any time, so one received after the response and still held must not
// keep the prompt pending once the earlier chunk has been applied.
func TestPromptDoesNotWaitForLaterUpdates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &splitUpdates{chunkStarted: make(chan struct{}), infoStarted: make(chan struct{}), chunkRelease: newGate(), infoRelease: newGate()}
		defer h.chunkRelease.open()
		defer h.infoRelease.open()
		client, peer := startOrderingClient(t, h)
		acptest.NewSession(t, client, peer, "s")
		done := prompt(client, "s", "question")
		request := peer.ExpectCall("session/prompt")
		peer.Update("s", "A")
		acptest.Await(t, h.chunkStarted)
		endTurn(peer, request)
		peer.Notify("session/update", map[string]any{"sessionId": "s", "update": map[string]any{"sessionUpdate": "session_info_update", "title": "background change"}})
		barrier(t, peer)

		synctest.Wait()
		result, returned := tryRecv(done)
		if returned {
			t.Error("prompt returned before the chunk received earlier was applied")
		}

		h.chunkRelease.open()
		synctest.Wait()
		acptest.Await(t, h.infoStarted)
		if !returned {
			if result, returned = tryRecv(done); !returned {
				t.Error("prompt waited for an update received after its response")
			}
		}

		h.infoRelease.open()
		if !returned {
			result = acptest.Await(t, done)
		}
		if result.Err != nil {
			t.Error(result.Err)
		}
	})
}

type nestedValue struct {
	Value string `json:"value"`
}

// nestingClient makes an extension call and then a prompt from inside its
// update callback, waiting for each answer.
type nestingClient struct {
	client   *acp.Client
	result   chan acptest.Result[*nestedValue]        // buffered for the one update
	prompted chan acptest.Result[*acp.PromptResponse] // buffered for the one update
}

func (h *nestingClient) SessionUpdate(ctx context.Context, _ *acp.SessionNotification) error {
	value, err := acp.CallExtension[nestedValue](ctx, h.client, "_nested", struct{}{})
	h.result <- acptest.Result[*nestedValue]{Value: value, Err: err}
	if err != nil {
		return err
	}
	response, err := h.client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: "s", Prompt: []acp.ContentBlock{acp.TextContentBlock("nested")}})
	h.prompted <- acptest.Result[*acp.PromptResponse]{Value: response, Err: err}
	return err
}

// TestUpdateCallbackCanAwaitRequest checks that an update callback can make a
// request and receive its response, even though the prompt whose response has
// already arrived stays pending behind that update. Responses do not queue
// behind held callbacks, so the nested call cannot deadlock. A prompt made with
// the callback's own context also returns: it does not wait for the callback
// that is making it.
func TestUpdateCallbackCanAwaitRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &nestingClient{result: make(chan acptest.Result[*nestedValue], 1), prompted: make(chan acptest.Result[*acp.PromptResponse], 1)}
		client, peer := startOrderingClient(t, h)
		h.client = client
		acptest.NewSession(t, client, peer, "s")
		done := prompt(client, "s", "question")
		request := peer.ExpectCall("session/prompt")
		peer.Update("s", "nested")
		nested := peer.ExpectCall("_nested")
		endTurn(peer, request)
		barrier(t, peer)

		synctest.Wait()
		result, returned := tryRecv(done)
		if returned {
			t.Error("prompt returned while its update callback awaited a nested request")
		}

		peer.Reply(nested.ID, nestedValue{Value: "nested content"})
		got := acptest.Value(t, h.result)
		if got.Value != "nested content" {
			t.Errorf("nested result = %#v, want nested content", got)
		}
		endTurn(peer, peer.ExpectCall("session/prompt"))
		if stop := acptest.Value(t, h.prompted).StopReason; stop != acp.StopReasonEndTurn {
			t.Errorf("nested prompt stop reason = %q, want end_turn", stop)
		}
		if !returned {
			result = acptest.Await(t, done)
		}
		if result.Err != nil {
			t.Error(result.Err)
		}
	})
}

// announcedTools holds each update callback until released, then records the
// announced tool call. Its permission callback reports whether the tool call
// it is asked about has been recorded.
type announcedTools struct {
	started chan struct{}
	release *gate
	mu      sync.Mutex
	known   map[acp.ToolCallId]bool
	asked   chan bool // buffered for the one permission request
}

func (h *announcedTools) SessionUpdate(ctx context.Context, n *acp.SessionNotification) error {
	close(h.started)
	if err := h.release.wait(ctx); err != nil {
		return err
	}
	h.mu.Lock()
	h.known[n.Update.ToolCallID] = true
	h.mu.Unlock()
	return nil
}

func (h *announcedTools) SessionRequestPermission(_ context.Context, r *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	h.mu.Lock()
	known := h.known[r.ToolCall.ToolCallID]
	h.mu.Unlock()
	h.asked <- known
	return &acp.RequestPermissionResponse{Outcome: acp.SelectedRequestPermissionOutcome("allow_once")}, nil
}

// TestRequestWaitsForEarlierUpdates checks that an incoming request starts
// only after the update callbacks received before it have returned. Agents
// commonly announce a tool call and then ask permission to run it; the
// permission callback must find the announced call.
func TestRequestWaitsForEarlierUpdates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &announcedTools{started: make(chan struct{}), release: newGate(), known: map[acp.ToolCallId]bool{}, asked: make(chan bool, 1)}
		defer h.release.open()
		client, peer := startOrderingClient(t, h)
		acptest.NewSession(t, client, peer, "s")
		peer.Notify("session/update", map[string]any{"sessionId": "s", "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call_1", "title": "Write file"}})
		acptest.Await(t, h.started)
		peer.Request("permission", "session/request_permission", map[string]any{"sessionId": "s", "toolCall": map[string]any{"toolCallId": "call_1"}, "options": []any{map[string]any{"optionId": "allow_once", "name": "Allow once", "kind": "allow_once"}}})
		barrier(t, peer)

		synctest.Wait()
		if known, started := tryRecv(h.asked); started {
			t.Fatalf("permission callback started while the earlier update was held (tool call known: %v)", known)
		}

		h.release.open()
		if !acptest.Await(t, h.asked) {
			t.Error("permission callback did not see the announced tool call")
		}
		response := peer.Next()
		if !response.IsResponse() || string(response.ID) != string(acptest.RawID("permission")) || response.Error != nil {
			t.Fatalf("permission response = %s", response)
		}
	})
}

// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// A $/cancel_request from the peer cancels the context of the matching
// in-flight handler, and whatever the handler then returns is sent as the
// reply. One handler waits on its context; the other only looks at it after
// the cancellation has arrived.
func TestIncomingCancelReachesHandler(t *testing.T) {
	for _, tc := range []struct {
		name     string
		waitDone bool
	}{
		{"handler waits on Done", true},
		{"handler checks Err later", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started := make(chan context.Context, 1)
				proceed := make(chan struct{})
				mux := acp.NewExtensionMux()
				if err := acp.AddExtensionRequest(mux, heldMethod, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
					started <- ctx
					if tc.waitDone {
						select {
						case <-ctx.Done():
						case <-proceed:
						}
					} else {
						<-proceed
					}
					if ctx.Err() == nil {
						return &struct{}{}, nil
					}
					return nil, &jsonrpc.Error{Code: int64(acp.ErrorCodeRequestCancelled), Message: "Request cancelled"}
				}); err != nil {
					t.Fatal(err)
				}
				_, peer := acptest.StartAgent(t, mux)
				release := sync.OnceFunc(func() { close(proceed) })
				defer release()

				peer.Request("req-1", heldMethod, struct{}{})
				ctx := acptest.Await(t, started)
				if ctx.Err() != nil {
					t.Fatalf("new request already canceled: %v", ctx.Err())
				}
				peer.Notify(cancelMethod, map[string]any{"requestId": "req-1"})
				synctest.Wait()
				if !errors.Is(ctx.Err(), context.Canceled) {
					t.Errorf("handler context after cancellation: %v", ctx.Err())
				}
				if !tc.waitDone {
					release()
				}
				reply := nextNow(t, peer)
				acptest.EqualJSON(t, reply.ID, `"req-1"`)
				if reply.Error == nil || reply.Error.Code != int64(acp.ErrorCodeRequestCancelled) || reply.Error.Message != "Request cancelled" {
					t.Errorf("reply to canceled request = %s, want Request cancelled", reply)
				}
				expectNoFrames(peer)
			})
		})
	}
}

// A cancellation notification, plain or wrapped in proxy/successor, cancels
// only the in-flight request whose ID it names. Notifications that are not
// cancellations, or that are malformed, cancel nothing, get no reply, and
// leave the connection serving. Both handlers ignore cancellation, so the test
// can inspect their contexts without either one replying.
func TestCancelTargetsOnlyItsRequest(t *testing.T) {
	successor := func(method string, params any) map[string]any {
		return map[string]any{"method": method, "params": params}
	}
	for _, tc := range []struct {
		name       string
		id         any // ID of the request the notification may target
		method     string
		params     any
		wantCancel bool
	}{
		{"plain integer id", 1, cancelMethod, map[string]any{"requestId": 1}, true},
		{"wrapped string id", "req-1", "proxy/successor", successor(cancelMethod, map[string]any{"requestId": "req-1"}), true},
		{"wrapped integer id", 1, "proxy/successor", successor(cancelMethod, map[string]any{"requestId": 1}), true},
		{"wrapped unknown id", 1, "proxy/successor", successor(cancelMethod, map[string]any{"requestId": "req-1"}), false},
		{"wrapped other method", 1, "proxy/successor", successor("session/update", struct{}{}), false},
		{"malformed wrapper", 1, "proxy/successor", map[string]any{"bogus": true}, false},
		{"object request id", 1, cancelMethod, map[string]any{"requestId": map[string]any{"not": "an id"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				mux := echoMux(t)
				h := addHeld(t, mux, false)
				peer, transport := acptest.NewPeer(t)
				_, serving := serve(t, context.Background(), transport, mux)
				defer h.unblock()

				peer.Request(tc.id, heldMethod, map[string]any{"tag": "target"})
				peer.Request("other", heldMethod, map[string]any{"tag": "other"})
				contexts := map[string]context.Context{}
				for range 2 {
					call := acptest.Await(t, h.started)
					contexts[call.tag] = call.ctx
				}
				for tag, ctx := range contexts {
					if ctx.Err() != nil {
						t.Fatalf("new %s request already canceled", tag)
					}
				}

				peer.Notify(tc.method, tc.params)
				synctest.Wait()
				switch err := contexts["target"].Err(); {
				case tc.wantCancel && !errors.Is(err, context.Canceled):
					t.Errorf("target context = %v, want canceled", err)
				case !tc.wantCancel && err != nil:
					t.Errorf("notification ended the target's context: %v", err)
				}
				if err := contexts["other"].Err(); err != nil {
					t.Errorf("notification canceled a request with another ID: %v", err)
				}

				// The notification gets no reply of its own; the only frame is
				// the answer to a later call, so the connection still serves.
				peer.Request("barrier", echoMethod, echoParams{Message: "after"})
				frames := settledFrames(peer)
				if len(frames) != 1 {
					t.Fatalf("frames after notification = %v, want only the barrier reply", frames)
				}
				acptest.EqualJSON(t, frames[0].Line, `{"jsonrpc":"2.0","id":"barrier","result":{"result":"echo: after"}}`)
				if err, ok := poll(serving); ok {
					t.Fatalf("notification ended serving: %v", err)
				}

				// Releasing the handlers and ending input finishes serving cleanly.
				h.unblock()
				peer.CloseInput()
				if err := acptest.Await(t, serving); err != nil {
					t.Errorf("RunAgent after input EOF = %v, want nil", err)
				}
			})
		})
	}
}

// When a caller abandons an outgoing call the peer has already received, the
// call returns the context's error at once, the SDK sends one $/cancel_request
// naming the call's ID, and the connection stays usable: later calls get fresh
// IDs, a late reply to the abandoned call is ignored, and Close neither waits
// for the reply that never comes nor needs the peer to end its input.
func TestAbandonedCall(t *testing.T) {
	for _, tc := range []struct {
		name      string
		deadline  bool // abandon by letting a deadline pass instead of canceling
		twice     bool // cancel a second time; still only one notification
		lateReply bool // the peer ignores the cancellation and answers anyway
	}{
		{name: "cancel"},
		{name: "cancel twice", twice: true},
		{name: "deadline", deadline: true},
		{name: "late reply", lateReply: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, peer := acptest.StartClient(t, nil)
				ctx, cancel := context.WithCancel(context.Background())
				if tc.deadline {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				}
				defer cancel()

				abandoned := callEcho(client, ctx, "abandoned")
				// Seeing the whole request, and letting the SDK's write return,
				// makes this the abandonment of a sent call. Canceling while
				// that write is still returning would instead interrupt it,
				// which closes a stream that has no write deadline.
				sent := expectEcho(t, peer, "abandoned")
				synctest.Wait()
				if tc.deadline {
					<-ctx.Done()
				} else {
					cancel()
				}
				if tc.twice {
					cancel()
				}
				r := acptest.Await(t, abandoned)
				if ctx.Err() == nil || !errors.Is(r.Err, ctx.Err()) {
					t.Fatalf("abandoned call error = %v, want %v", r.Err, ctx.Err())
				}
				expectCancel(t, peer, sent.ID)
				expectNoFrames(peer)
				if tc.lateReply {
					replyEcho(peer, sent, "abandoned")
				}

				later := roundTrip(t, client, peer, "later")
				if string(later.ID) == string(sent.ID) {
					t.Errorf("later call reused the abandoned call's ID %s", sent.ID)
				}
				expectNoFrames(peer)

				closing := acptest.Go(func() (struct{}, error) { return struct{}{}, client.Close() })
				synctest.Wait()
				closed, ok := poll(closing)
				if !ok {
					t.Fatal("Close waits for the abandoned call's reply")
				}
				if closed.Err != nil {
					t.Errorf("Close = %v", closed.Err)
				}
				// Output having ended shows Close reached the underlying connection.
				if frames := drainNow(t, peer); len(frames) != 0 {
					t.Errorf("frames written by Close: %v", frames)
				}
			})
		})
	}
}

// A call already answered when its caller cancels the context sends no
// $/cancel_request, whether or not the caller has read the result yet. The
// barrier call before cancellation shows the earlier reply was routed; the one
// after shows the late cancellation did not disturb the connection.
func TestCancelAfterReplySendsNothing(t *testing.T) {
	for _, tc := range []struct {
		name      string
		readFirst bool
	}{
		{"result read before cancel", true},
		{"result read after cancel", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, peer := acptest.StartClient(t, nil)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				answered := callEcho(client, ctx, "answered")
				replyEcho(peer, expectEcho(t, peer, "answered"), "answered")
				if tc.readFirst {
					checkEcho(t, acptest.Await(t, answered), "answered")
				}
				roundTrip(t, client, peer, "before cancel")
				cancel()
				expectNoFrames(peer)
				roundTrip(t, client, peer, "after cancel")
				if !tc.readFirst {
					checkEcho(t, acptest.Await(t, answered), "answered")
				}
				expectNoFrames(peer)
			})
		})
	}
}

// Canceling a proxy/successor call sends a plain $/cancel_request for the
// outer request's ID, not a wrapped one, and the wrapped request reaches the
// peer unchanged.
func TestSuccessorCancelUsesOuterID(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, peer := acptest.StartClient(t, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := acptest.Go(func() (*acp.ProxySuccessorResponse, error) {
			return client.ProxySuccessor(ctx, &acp.ProxySuccessorRequest{Method: echoMethod, Params: echoParams{Message: "wrapped"}})
		})
		sent := peer.ExpectCall(acp.MethodProxySuccessor)
		var envelope struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(sent.Params, &envelope); err != nil || envelope.Method != echoMethod {
			t.Fatalf("successor envelope = %s, %v", sent.Params, err)
		}
		acptest.EqualJSON(t, envelope.Params, `{"message":"wrapped"}`)

		synctest.Wait() // let the write return, as in TestAbandonedCall
		cancel()
		if r := acptest.Await(t, result); !errors.Is(r.Err, context.Canceled) {
			t.Errorf("canceled successor call error = %v", r.Err)
		}
		expectCancel(t, peer, sent.ID)
		expectNoFrames(peer)
	})
}

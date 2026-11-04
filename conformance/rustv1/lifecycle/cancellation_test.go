// Copyright 2026 ACP Go SDK contributors.
// Rust SDK semantic ports, Apache-2.0; provenance and policy labels: mapping.json.
package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust: tests/jsonrpc_request_cancellation.rs:267,
// unhandled_wrapped_protocol_level_notifications_are_ignored.
// The custom echo method uses ACP's required extension prefix in Go.
func TestUnhandledWrappedProtocolNotificationIsIgnored(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		_, done := startAgent(t, p, echoMux(t))
		defer func() { _ = p.Close(); synctest.Wait() }()
		p.push(t, notification("proxy/successor", map[string]any{
			"method": "$/cancel_request", "params": map[string]any{"requestId": "req-1"},
		}))
		p.push(t, request(2, probeMethod, map[string]any{"message": "after wrapped cancel"}))
		synctest.Wait()
		success(t, p.response(t, int64(2)), `{"result":"echo: after wrapped cancel"}`)
		p.noOutput(t)
		_ = p.Close()
		synctest.Wait()
		if err := receive(t, done); err != nil {
			t.Errorf("unhandled protocol notification broke the connection: %v", err)
		}
	})
}

// Rust: tests/jsonrpc_request_cancellation.rs:2340,
// request_handler_can_observe_cancellation_from_responder; :2119,
// cancellation_marker_requested_after_cancel_is_already_cancelled.
// This is the optional Rust SDK request-cancel policy, NOT mandatory session
// cancellation. Go's supplied handler context is its public cancellation signal;
// no fixture intercepts $/cancel_request or manufactures canceled contexts.
func TestOptionalIncomingRequestCancellation(t *testing.T) {
	for _, late := range []bool{false, true} {
		name := "observe-existing-signal"
		if late {
			name = "observe-after-cancel"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newFrames()
				started := make(chan context.Context, 1)
				release := make(chan struct{})
				inspect := make(chan struct{})
				mux := acp.NewExtensionMux()
				err := acp.AddExtensionRequest(mux, probeMethod, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
					started <- ctx
					if late {
						select {
						case <-inspect:
							// Rust requests two markers after registry cancellation.
							// Go provides its context eagerly; both observations must
							// still report the already-recorded cancellation.
							first, second := ctx.Err(), ctx.Err()
							if first == nil || second == nil {
								return &struct{}{}, nil
							}
						case <-release:
							return &struct{}{}, nil
						}
					} else {
						select {
						case <-ctx.Done():
						case <-release:
							return &struct{}{}, nil
						}
					}
					return nil, &jsonrpc.Error{Code: codeRequestCancelled, Message: "Request cancelled"}
				})
				if err != nil {
					t.Fatal(err)
				}
				_, done := startAgent(t, p, mux)
				defer func() { close(release); _ = p.Close(); synctest.Wait() }()
				p.push(t, request("req-1", probeMethod, struct{}{}))
				ctx := receive(t, started)
				if ctx.Err() != nil {
					t.Fatalf("new request is already canceled: %v", ctx.Err())
				}
				p.push(t, notification("$/cancel_request", map[string]any{"requestId": "req-1"}))
				synctest.Wait()
				if !errors.Is(ctx.Err(), context.Canceled) {
					t.Errorf("optional request cancellation did not reach the handler: %v", ctx.Err())
				}
				if late {
					close(inspect)
					synctest.Wait()
				}
				// An unsupported implementation leaves the handler pending. Report
				// RED without waiting forever; cleanup uses a separate app signal.
				select {
				case message := <-p.output:
					response, ok := message.(*jsonrpc.Response)
					if !ok || response.ID.Raw() != "req-1" {
						t.Errorf("unexpected cancellation reply: %#v", message)
					} else {
						var wireError *jsonrpc.Error
						if !errors.As(response.Error, &wireError) || wireError.Code != codeRequestCancelled || wireError.Message != "Request cancelled" {
							t.Errorf("cancellation reply = %#v, want Request cancelled", response)
						}
					}
				default:
					t.Error("optional request cancellation produced no response to the original request")
				}
				p.noOutput(t)
				// No assertion that input EOF cancels all inbound work: that is
				// not this Rust test's oracle, and conflicts with half-close drain.
				_ = done
			})
		})
	}
}

// Rust: tests/jsonrpc_request_cancellation.rs:708 and :793,
// sent_request_can_send_cancellation_for_its_id and
// dropped_sent_request_sends_cancellation_for_its_id.
// Go has synchronous calls, not owned SentRequest handles: canceling a context
// AFTER observing the request is the public analogue of abandoning that wait.
// Explicit cancel followed by drop becomes two idempotent cancel invocations.
// Response-wait lifetime differs; the asserted wire notification/id/once-only
// behavior is the optional Rust SDK policy being ported.
func TestOptionalOutgoingRequestCancellation(t *testing.T) {
	for _, mode := range []string{"explicit-cancel-then-release", "abandon-in-flight-wait"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newFrames()
				c := client(t, p, nil)
				defer cleanupClient(p, c)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				message := "slow"
				if mode == "abandon-in-flight-wait" {
					message = "abandoned"
				}
				pending := call(c, ctx, map[string]any{"message": message})
				sent := outgoingRequest(t, p)
				requireOutgoingMessage(t, sent, message)
				cancel()
				if mode == "explicit-cancel-then-release" {
					cancel()
				}
				synctest.Wait()
				if result := receive(t, pending); !errors.Is(result.err, context.Canceled) {
					t.Fatalf("abandoned wait error = %v", result.err)
				}
				checkCancellation(t, p, sent.ID)
				p.noOutput(t)
				// Rust's explicit-cancel case uses a follow-up round trip before
				// checking for duplicates. A real request must still work.
				barrier := call(c, context.Background(), map[string]any{"message": "barrier"})
				req := outgoingRequest(t, p)
				requireOutgoingMessage(t, req, "barrier")
				p.push(t, reply(req.ID, map[string]any{"result": "echo: barrier"}))
				synctest.Wait()
				requireEcho(t, receive(t, barrier), "echo: barrier")
				p.noOutput(t)
			})
		})
	}
}

// Rust: tests/jsonrpc_request_cancellation.rs:563,
// cancelling_request_sent_to_successor_peer_sends_plain_cancel. Use the actual
// Go ProxySuccessor call rather than adding envelope routing to the fixture.
// Cancellation is optional; the outer RPC ID, not the wrapped method, is targeted.
func TestOptionalSuccessorCancellationUsesOuterID(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		c := client(t, p, nil)
		defer cleanupClient(p, c)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := c.ProxySuccessor(ctx, &acp.ProxySuccessorRequest{
				Method: probeMethod, Params: map[string]any{"message": "wrapped cancel"},
			})
			done <- err
		}()
		message := p.next(t)
		req, ok := message.(*jsonrpc.Request)
		if !ok || !req.IsCall() || req.Method != acp.MethodProxySuccessor {
			t.Fatalf("expected successor-wrapped request, got %#v", message)
		}
		var params acp.ProxySuccessorRequest
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Method != probeMethod {
			t.Fatalf("successor envelope changed: %s, %v", req.Params, err)
		}
		inner, err := json.Marshal(params.Params)
		if err != nil || string(inner) != `{"message":"wrapped cancel"}` {
			t.Fatalf("wrapped request parameters changed: %s, %v", inner, err)
		}
		cancel()
		synctest.Wait()
		if err := receive(t, done); !errors.Is(err, context.Canceled) {
			t.Errorf("local canceled successor wait = %v", err)
		}
		checkCancellation(t, p, req.ID)
		p.noOutput(t) // In particular, no wrapped/duplicate cancellation.
	})
}

func checkCancellation(t *testing.T, p *frameTransport, id jsonrpc.ID) {
	t.Helper()
	select {
	case message := <-p.output:
		n, ok := message.(*jsonrpc.Request)
		if !ok || n.IsCall() || n.Method != "$/cancel_request" {
			t.Errorf("expected optional cancellation notification, got %#v", message)
			return
		}
		var params struct {
			RequestID any `json:"requestId"`
		}
		if err := json.Unmarshal(n.Params, &params); err != nil {
			t.Fatal(err)
		}
		actual, _ := json.Marshal(params.RequestID)
		expected, _ := json.Marshal(id.Raw())
		if string(actual) != string(expected) {
			t.Errorf("canceled id = %s, want %s", actual, expected)
		}
	default:
		t.Error("abandoning an observed request emitted no optional $/cancel_request")
	}
}

// Rust: tests/jsonrpc_request_cancellation.rs:943,
// late_response_after_dropped_sent_request_does_not_close_connection.
// The context/owned-handle distinction is the same as the preceding test. The
// peer deliberately ignores cancellation and sends a late valid response.
func TestLateResponseAfterAbandonmentKeepsConnectionUsable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		c := client(t, p, nil)
		defer cleanupClient(p, c)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		pending := call(c, ctx, map[string]any{"message": "late"})
		sent := outgoingRequest(t, p)
		requireOutgoingMessage(t, sent, "late")
		cancel()
		synctest.Wait()
		if result := receive(t, pending); !errors.Is(result.err, context.Canceled) {
			t.Fatalf("abandoned wait error = %v", result.err)
		}
		checkCancellation(t, p, sent.ID)
		// Continue even if optional cancellation is absent: the late-response
		// safety assertion must still run, rather than being hidden by that RED.
		p.push(t, reply(sent.ID, map[string]any{"result": "late response"}))
		later := call(c, context.Background(), map[string]any{"message": "after late"})
		req := outgoingRequest(t, p)
		requireOutgoingMessage(t, req, "after late")
		p.push(t, reply(req.ID, map[string]any{"result": "echo: after late"}))
		synctest.Wait()
		requireEcho(t, receive(t, later), "echo: after late")
		p.noOutput(t)
	})
}

// Rust: tests/jsonrpc_request_cancellation.rs:1041,
// response_buffered_before_drop_disarms_auto_cancellation. Go's synchronous
// result is held unread in the caller-owned buffered result channel. Cancel
// only after a later round trip proves the earlier reply was already routed.
func TestBufferedResponseBeforeAbandonmentDoesNotCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		c := client(t, p, nil)
		defer cleanupClient(p, c)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		unconsumed := call(c, ctx, map[string]any{"message": "buffered"})
		req := outgoingRequest(t, p)
		requireOutgoingMessage(t, req, "buffered")
		p.push(t, reply(req.ID, map[string]any{"result": "echo: buffered"}))
		barrier := call(c, context.Background(), map[string]any{"message": "barrier"})
		req = outgoingRequest(t, p)
		requireOutgoingMessage(t, req, "barrier")
		p.push(t, reply(req.ID, map[string]any{"result": "echo: barrier"}))
		synctest.Wait()
		requireEcho(t, receive(t, barrier), "echo: barrier")
		cancel()
		synctest.Wait()
		p.noOutput(t)
		// The source's second barrier is AFTER dropping the settled handle.
		// This catches an erroneous late cancel that closes the connection.
		after := call(c, context.Background(), map[string]any{"message": "after buffered"})
		req = outgoingRequest(t, p)
		requireOutgoingMessage(t, req, "after buffered")
		p.push(t, reply(req.ID, map[string]any{"result": "echo: after buffered"}))
		synctest.Wait()
		requireEcho(t, receive(t, after), "echo: after buffered")
		requireEcho(t, receive(t, unconsumed), "echo: buffered")
		p.noOutput(t)
	})
}

// Rust: tests/jsonrpc_request_cancellation.rs:1349,
// completed_sent_request_does_not_send_cancellation_on_drop. Go context cancel
// after a completed synchronous call is the equivalent late cancellation event.
// Also tests/jsonrpc_transport_close.rs:511,
// active_channel_output_is_copied_before_component_completion: its portable
// request-dependent output-progress subset, not its absent owned-driver sealing.
func TestCompletedRequestDoesNotSendSpuriousCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		c := client(t, p, nil)
		defer cleanupClient(p, c)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		pending := call(c, ctx, map[string]any{"message": "complete"})
		req := outgoingRequest(t, p)
		requireOutgoingMessage(t, req, "complete")
		p.push(t, reply(req.ID, map[string]any{"result": "echo: complete"}))
		synctest.Wait()
		requireEcho(t, receive(t, pending), "echo: complete")
		cancel()
		barrier := call(c, context.Background(), map[string]any{"message": "barrier"})
		req = outgoingRequest(t, p)
		requireOutgoingMessage(t, req, "barrier")
		p.push(t, reply(req.ID, map[string]any{"result": "echo: barrier"}))
		synctest.Wait()
		requireEcho(t, receive(t, barrier), "echo: barrier")
		p.noOutput(t)
	})
}

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

// Rust: src/jsonrpc.rs:8893 cancellation_request_id_is_extracted_from_wrapped_notifications;
// :8907 malformed_successor_envelope_is_not_treated_as_cancellation;
// :8917 cancel_request_notifications_are_detected_even_when_wrapped;
// :8945 malformed_cancel_request_params_error. The literal predicate branches,
// string-ID extraction, and object-ID validation are an explicitly OPTIONAL
// Rust protocol/proxy cancellation-policy projection onto live PLAIN requests.
// No wrapped REQUEST is constructed or auto-dispatched; these tests make no
// claim that Go has Rust's automatic proxy request/role framework. Wrapped
// notifications test only the source helper's matching-ID selection policy.
//
// Public runtime projection of the optional Rust SDK policy: cancellation must
// affect ONLY the matching actual handler context. The fixture never receives,
// peels, parses, or intercepts cancellation notifications. Both held handlers
// ignore cancellation until the independent cleanup release; this lets the
// test inspect SDK-owned contexts without manufacturing canceled ones.
// Private bool/Option/Err parser returns and automatic role-chain forwarding are separate
// absent-API subcases in mapping.json, not silently counted as runtime coverage.
func TestOptionalCancellationEnvelopeRecognitionAndValidation(t *testing.T) {
	wrapped := func(method string, params any) map[string]any {
		// Construct peer wire data only, not a fixture envelope parser.
		return notification("proxy/successor", map[string]any{"method": method, "params": params})
	}
	for _, tc := range []struct {
		name       string
		id         any
		wire       map[string]any
		wantCancel bool
	}{
		{"wrapped-string-id", "req-1", wrapped("$/cancel_request", map[string]any{"requestId": "req-1"}), true},
		{"plain-integer-id", 1, notification("$/cancel_request", map[string]any{"requestId": 1}), true},
		{"wrapped-integer-id", 1, wrapped("$/cancel_request", map[string]any{"requestId": 1}), true},
		{"wrapped-other-method", 1, wrapped("session/update", struct{}{}), false},
		{"malformed-successor-envelope", 1, notification("proxy/successor", map[string]any{"bogus": true}), false},
		{"invalid-request-id-object", 1, notification("$/cancel_request", map[string]any{"requestId": map[string]any{"not": "an id"}}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newFrames()
				mux := echoMux(t)
				type observation struct {
					tag string
					ctx context.Context
				}
				started := make(chan observation, 2)
				release := make(chan struct{})
				if err := acp.AddExtensionRequest(mux, "_rust/held", func(ctx context.Context, params *struct {
					Tag string `json:"tag"`
				}) (*struct{}, error) {
					started <- observation{params.Tag, ctx}
					<-release
					return &struct{}{}, nil
				}); err != nil {
					t.Fatal(err)
				}
				_, done := startAgent(t, p, mux)
				defer func() { close(release); _ = p.Close(); synctest.Wait() }()
				p.push(t, request(tc.id, "_rust/held", map[string]any{"tag": "target"}))
				p.push(t, request("untargeted", "_rust/held", map[string]any{"tag": "other"}))
				contexts := map[string]context.Context{}
				for range 2 {
					seen := receive(t, started)
					contexts[seen.tag] = seen.ctx
				}
				for _, tag := range []string{"target", "other"} {
					if contexts[tag] == nil || contexts[tag].Err() != nil {
						t.Fatalf("new %s request is not live", tag)
					}
				}
				p.push(t, tc.wire)
				synctest.Wait()
				if tc.wantCancel {
					if !errors.Is(contexts["target"].Err(), context.Canceled) {
						t.Errorf("optional cancellation envelope did not target its request ID: %v", contexts["target"].Err())
					}
				} else if err := contexts["target"].Err(); err != nil {
					t.Errorf("non-cancel/malformed notification canceled active work: %v", err)
				}
				if err := contexts["other"].Err(); err != nil {
					t.Errorf("notification canceled a different request ID: %v", err)
				}

				// A real round trip proves malformed notifications neither stop
				// the connection nor get confused with request/response traffic.
				p.push(t, request("parser-barrier", probeMethod, map[string]any{"message": "after parser check"}))
				synctest.Wait()
				barrierSeen := false
				targetID, _ := json.Marshal(tc.id)
			frames:
				for {
					select {
					case message := <-p.output:
						response, ok := message.(*jsonrpc.Response)
						if !ok {
							t.Errorf("unexpected parser output: %#v", message)
							continue
						}
						if response.ID.Raw() == "parser-barrier" {
							if barrierSeen {
								t.Error("duplicate parser barrier response")
							}
							barrierSeen = true
							success(t, response, `{"result":"echo: after parser check"}`)
							continue
						}
						// Some supporting runtimes may answer the canceled
						// ORIGINAL request early. That is not a notification reply.
						actualID, _ := json.Marshal(response.ID.Raw())
						if !tc.wantCancel || string(actualID) != string(targetID) {
							t.Errorf("unexpected response after notification: %#v", response)
						}
					default:
						break frames
					}
				}
				if !barrierSeen {
					t.Error("cancellation parser notification prevented the subsequent RPC")
				}
				select {
				case err := <-done:
					t.Errorf("notification terminated the serving connection: %v", err)
				default:
				}
			})
		})
	}
}

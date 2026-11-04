// Copyright 2026 ACP Go SDK contributors.
// Rust SDK semantic ports, Apache-2.0; provenance and policy labels: mapping.json.
package lifecycle_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust: tests/jsonrpc_dropped_responder.rs:214,
// answered_responder_sends_nothing_extra; :200,
// handler_error_answers_instead_of_dropped_responder_fallback.
// Go has return-based handlers, not an owned Responder that can be dropped.
// This ports each actual wire outcome and two ping barriers, but does not
// manufacture a dropped-responder API or its automatic fallback in the fixture.
func TestHandlerProducesExactlyOneResponse(t *testing.T) {
	for _, outcome := range []string{"answered", "handler-error"} {
		t.Run(outcome, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newFrames()
				mux := acp.NewExtensionMux()
				if err := acp.AddExtensionRequest(mux, probeMethod, func(context.Context, *struct{}) (*map[string]any, error) {
					if outcome == "handler-error" {
						return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Invalid params", Data: []byte(`"handler error"`)}
					}
					result := map[string]any{"answered": true}
					return &result, nil
				}); err != nil {
					t.Fatal(err)
				}
				if err := acp.AddExtensionRequest(mux, "_rust/ping", func(context.Context, *struct{}) (*string, error) {
					result := "pong"
					return &result, nil
				}); err != nil {
					t.Fatal(err)
				}
				_, done := startAgent(t, p, mux)
				defer func() { _ = p.Close(); synctest.Wait() }()
				p.push(t, request(1, probeMethod, struct{}{}))
				synctest.Wait()
				response := p.response(t, int64(1))
				if outcome == "answered" {
					success(t, response, `{"answered":true}`)
				} else {
					var wire *jsonrpc.Error
					if !errors.As(response.Error, &wire) || wire.Code != jsonrpc.CodeInvalidParams {
						t.Errorf("handler error was replaced: %#v", response)
					}
				}
				for _, id := range []int{2, 3} {
					p.push(t, request(id, "_rust/ping", struct{}{}))
					synctest.Wait()
					success(t, p.response(t, int64(id)), `"pong"`)
				}
				p.noOutput(t)
				_ = p.Close()
				synctest.Wait()
				if err := receive(t, done); err != nil {
					t.Errorf("handler response broke connection: %v", err)
				}
			})
		})
	}
}

// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk documentation, Apache-2.0.
package session_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust src/agent-client-protocol/src/concepts/callbacks.rs doctests10/38/56/73/97: typed requests,
// notifications, successful/error replies, and multiple registered handlers.
// Go custom methods must be underscore-prefixed. No Rust Responder or managed
// task API is emulated; ordinary Go handler return values are the public seam.
func TestTypedExtensionCallbacks(t *testing.T) {
	type validateRequest struct {
		Data string `json:"data"`
	}
	type validateResponse struct {
		IsValid bool    `json:"is_valid"`
		Error   *string `json:"error"`
	}
	type executeResponse struct {
		Result string `json:"result"`
	}
	type statusResponse struct {
		Status string `json:"status"`
	}
	type statusNotification struct {
		Message string `json:"message"`
	}
	mux := acp.NewExtensionMux()
	if err := acp.AddExtensionRequest(mux, "_rust/validate", func(_ context.Context, request *validateRequest) (*validateResponse, error) {
		return &validateResponse{IsValid: len(request.Data) > 0}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := acp.AddExtensionRequest(mux, "_rust/validate_always", func(context.Context, *validateRequest) (*validateResponse, error) {
		return &validateResponse{IsValid: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := acp.AddExtensionRequest(mux, "_rust/execute", func(context.Context, *struct{}) (*executeResponse, error) {
		return &executeResponse{Result: "done"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := acp.AddExtensionRequest(mux, "_rust/success", func(context.Context, *struct{}) (*statusResponse, error) {
		return &statusResponse{Status: "ok"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := acp.AddExtensionRequest(mux, "_rust/error", func(context.Context, *struct{}) (*statusResponse, error) {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Invalid params"}
	}); err != nil {
		t.Fatal(err)
	}
	notifications := make(chan string, 1)
	if err := acp.AddExtensionNotification(mux, "_rust/status", func(_ context.Context, notification *statusNotification) error {
		notifications <- notification.Message
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, _, peer := clientPeer(t, mux)
	for index, scenario := range []struct{ method, params, response string }{
		{"_rust/validate", `{"data":"hello"}`, `{"is_valid":true,"error":null}`},
		{"_rust/validate", `{"data":""}`, `{"is_valid":false,"error":null}`},
		{"_rust/validate_always", `{"data":""}`, `{"is_valid":true,"error":null}`},
		{"_rust/execute", `{}`, `{"result":"done"}`},
		{"_rust/success", `{}`, `{"status":"ok"}`},
		{"_rust/error", `{}`, ""},
	} {
		peer.send(t, map[string]any{"jsonrpc": "2.0", "id": index + 1, "method": scenario.method, "params": json.RawMessage(scenario.params)})
		response := peer.read(t)
		if scenario.method == "_rust/error" {
			if response.Error == nil || response.Error.Code != jsonrpc.CodeInvalidParams {
				t.Fatalf("typed callback error = %+v", response)
			}
			continue
		}
		if response.Error != nil {
			t.Fatalf("typed callback failed: %+v", response.Error)
		}
		assertJSON(t, response.Result, scenario.response)
	}
	peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "_rust/status", "params": map[string]any{"message": "ready"}})
	if got := await(t, notifications); got != "ready" {
		t.Fatalf("status notification = %q", got)
	}
}

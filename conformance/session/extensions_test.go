// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// TestExtensionHandlers checks typed extension handlers registered on one
// mux: each request decodes into its handler's params, each result encodes
// back onto the wire, a returned *jsonrpc.Error keeps its code, and a
// notification reaches its handler.
func TestExtensionHandlers(t *testing.T) {
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
	type statusNotification struct {
		Message string `json:"message"`
	}
	mux := acp.NewExtensionMux()
	add := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	add(acp.AddExtensionRequest(mux, "_example/validate", func(_ context.Context, r *validateRequest) (*validateResponse, error) {
		return &validateResponse{IsValid: r.Data != ""}, nil
	}))
	add(acp.AddExtensionRequest(mux, "_example/validate_always", func(context.Context, *validateRequest) (*validateResponse, error) {
		return &validateResponse{IsValid: true}, nil
	}))
	add(acp.AddExtensionRequest(mux, "_example/execute", func(context.Context, *struct{}) (*executeResponse, error) {
		return &executeResponse{Result: "done"}, nil
	}))
	add(acp.AddExtensionRequest(mux, "_example/error", func(context.Context, *struct{}) (*struct{}, error) {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Invalid params"}
	}))
	notifications := make(chan string, 1)
	add(acp.AddExtensionNotification(mux, "_example/status", func(_ context.Context, n *statusNotification) error {
		notifications <- n.Message
		return nil
	}))
	_, peer := acptest.StartClient(t, mux)

	for i, call := range []struct{ method, params, result string }{
		{"_example/validate", `{"data":"hello"}`, `{"is_valid":true,"error":null}`},
		{"_example/validate", `{"data":""}`, `{"is_valid":false,"error":null}`},
		{"_example/validate_always", `{"data":""}`, `{"is_valid":true,"error":null}`},
		{"_example/execute", `{}`, `{"result":"done"}`},
		{"_example/error", `{}`, ""},
	} {
		response := peer.Call(i+1, call.method, json.RawMessage(call.params))
		if call.result == "" {
			if response.Error == nil || response.Error.Code != jsonrpc.CodeInvalidParams {
				t.Fatalf("%s response = %s, want InvalidParams", call.method, response)
			}
			continue
		}
		if response.Error != nil {
			t.Fatalf("%s %s failed: %s", call.method, call.params, response)
		}
		acptest.EqualJSON(t, response.Result, call.result)
	}

	peer.Notify("_example/status", map[string]any{"message": "ready"})
	if got := acptest.Await(t, notifications); got != "ready" {
		t.Fatalf("notification message = %q, want ready", got)
	}
}

type ping struct {
	Value uint32 `json:"value"`
}

// TestExtensionCallCorrelation starts three concurrent extension calls and
// answers them in reverse order. Each request must carry a distinct ID and
// its exact params, and each call must receive the result for its own ID.
func TestExtensionCallCorrelation(t *testing.T) {
	client, peer := acptest.StartClient(t, nil)
	var results [3]<-chan acptest.Result[*ping]
	for i := range results {
		value := uint32(i + 1)
		results[i] = acptest.Go(func() (*ping, error) {
			return acp.CallExtension[ping](context.Background(), client, "_example/ping", ping{Value: value})
		})
	}
	// The calls race onto the wire, so index the requests by their params.
	requests := map[uint32]acptest.Message{}
	ids := map[string]bool{}
	for range results {
		request := peer.ExpectCall("_example/ping")
		if ids[string(request.ID)] {
			t.Fatalf("request ID reused: %s", request)
		}
		ids[string(request.ID)] = true
		var params ping
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Fatal(err)
		}
		acptest.EqualJSON(t, request.Params, fmt.Sprintf(`{"value":%d}`, params.Value))
		requests[params.Value] = request
	}
	for value := uint32(len(results)); value >= 1; value-- {
		request, ok := requests[value]
		if !ok {
			t.Fatalf("no request with value %d", value)
		}
		peer.Reply(request.ID, ping{Value: value + 10})
	}
	for i, result := range results {
		if got, want := acptest.Value(t, result).Value, uint32(i+11); got != want {
			t.Errorf("call %d result = %d, want %d", i+1, got, want)
		}
	}
}

// TestUnreadResultDoesNotBlockNotification checks that a call's result
// waiting unread in its buffered channel does not hold up dispatch of a
// notification received after the response.
func TestUnreadResultDoesNotBlockNotification(t *testing.T) {
	mux := acp.NewExtensionMux()
	notifications := make(chan uint32, 1)
	if err := acp.AddExtensionNotification(mux, "_example/after_response", func(_ context.Context, p *ping) error {
		notifications <- p.Value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	client, peer := acptest.StartClient(t, mux)
	result := acptest.Go(func() (*ping, error) {
		return acp.CallExtension[ping](context.Background(), client, "_example/ping", ping{Value: 7})
	})
	peer.Reply(peer.Expect("_example/ping").ID, ping{Value: 8})
	peer.Notify("_example/after_response", ping{Value: 7})
	if got := acptest.Await(t, notifications); got != 7 {
		t.Fatalf("notification value = %d, want 7", got)
	}
	if got := acptest.Value(t, result).Value; got != 8 {
		t.Fatalf("result = %d, want 8", got)
	}
}

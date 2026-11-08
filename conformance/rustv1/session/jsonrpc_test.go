// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk tests, Apache-2.0.
package session_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

type ping struct {
	Value uint32 `json:"value"`
}
type pong struct {
	Value uint32 `json:"value"`
}

// Rust: src/agent-client-protocol/tests/jsonrpc_advanced.rs::test_bidirectional_communication.
// Upstream's actual exchange is ping(10)->pong(11). The Go extension boundary
// requires underscore-prefixed custom methods, hence _rust/ping rather than ping.
func TestBidirectionalCommunication(t *testing.T) {
	ctx, client, peer := clientPeer(t, nil)
	result := startCall(func() (*pong, error) { return acp.CallExtension[pong](ctx, client, "_rust/ping", ping{Value: 10}) })
	request := peer.read(t)
	if request.Method != "_rust/ping" {
		t.Fatalf("method = %q", request.Method)
	}
	assertJSON(t, request.Params, `{"value":10}`)
	peer.result(t, request, map[string]any{"value": 11})
	if got := valueOf(t, result).Value; got != 11 {
		t.Fatalf("pong = %d, want 11", got)
	}
}

// Rust: src/agent-client-protocol/tests/jsonrpc_advanced.rs::test_request_ids, all three requests 1/2/3.
func TestRequestCorrelation(t *testing.T) {
	ctx, client, peer := clientPeer(t, nil)
	var results [3]<-chan callResult[*pong]
	for index := range results {
		value := uint32(index + 1)
		results[index] = startCall(func() (*pong, error) { return acp.CallExtension[pong](ctx, client, "_rust/ping", ping{Value: value}) })
	}
	seen := make(map[string]bool)
	for range results {
		request := peer.read(t)
		if len(request.ID) == 0 || seen[string(request.ID)] {
			t.Fatalf("outgoing request ID absent or reused: %s", request.ID)
		}
		seen[string(request.ID)] = true
		var params ping
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Fatal(err)
		}
		peer.result(t, request, map[string]any{"value": params.Value + 1})
	}
	for index, result := range results {
		if got := valueOf(t, result).Value; got != uint32(index+2) {
			t.Fatalf("request %d received %d, want %d", index+1, got, index+2)
		}
	}
}

// Rust: src/agent-client-protocol/tests/jsonrpc_advanced.rs::test_out_of_order_responses.
// Preserve requests (id,delay_ms)=(1,100),(2,50),(3,10), but the independent peer
// deliberately replies 3,2,1 instead of relying on three timer races.
func TestOutOfOrderResponses(t *testing.T) {
	ctx, client, peer := clientPeer(t, nil)
	type slow struct {
		DelayMS uint64 `json:"delay_ms"`
		ID      uint32 `json:"id"`
	}
	type response struct {
		ID uint32 `json:"id"`
	}
	var results [3]<-chan callResult[*response]
	for index, delay := range []uint64{100, 50, 10} {
		params := slow{DelayMS: delay, ID: uint32(index + 1)}
		results[index] = startCall(func() (*response, error) { return acp.CallExtension[response](ctx, client, "_rust/slow", params) })
	}
	requests := make(map[uint32]wireMessage)
	for range results {
		request := peer.read(t)
		var params slow
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Fatal(err)
		}
		requests[params.ID] = request
	}
	for _, id := range []uint32{3, 2, 1} {
		request, ok := requests[id]
		if !ok {
			t.Fatalf("missing request %d", id)
		}
		peer.result(t, request, map[string]any{"id": id})
	}
	for index, result := range results {
		if got := valueOf(t, result).ID; got != uint32(index+1) {
			t.Fatalf("request %d received result %d", index+1, got)
		}
	}
}

// Rust: src/agent-client-protocol/tests/jsonrpc_advanced.rs::unconsumed_response_does_not_block_following_message.
// Go has a synchronous call API: a goroutine starts the call, while the test
// intentionally leaves its buffered public result unconsumed until notification.
func TestUnconsumedResultDoesNotBlockFollowingNotification(t *testing.T) {
	mux := acp.NewExtensionMux()
	notifications := make(chan uint32, 1)
	if err := acp.AddExtensionNotification(mux, "_rust/after_response", func(_ context.Context, params *pong) error {
		notifications <- params.Value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, client, peer := clientPeer(t, mux)
	result := startCall(func() (*pong, error) { return acp.CallExtension[pong](ctx, client, "_rust/ping", ping{Value: 7}) })
	request := peer.read(t)
	peer.result(t, request, map[string]any{"value": 8})
	peer.send(t, map[string]any{"jsonrpc": "2.0", "method": "_rust/after_response", "params": map[string]any{"value": 7}})
	if got := await(t, notifications); got != 7 {
		t.Fatalf("notification = %d", got)
	}
	if got := valueOf(t, result).Value; got != 8 {
		t.Fatalf("result = %d", got)
	}
}

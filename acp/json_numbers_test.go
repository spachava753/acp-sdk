package acp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestUnstructuredNumbers(t *testing.T) {
	// This integer loses precision if decoded as float64.
	want := json.Number("9007199254740993")

	// Generated types must preserve unstructured numbers with the standard decoder.
	var tool ToolCall
	if err := json.Unmarshal([]byte(`{"rawInput":9007199254740993}`), &tool); err != nil {
		t.Fatal(err)
	}
	if tool.RawInput != want {
		t.Errorf("raw input = %v (%T), want %v (json.Number)", tool.RawInput, tool.RawInput, want)
	}

	mux := NewExtensionMux()
	if err := AddExtensionRequest(mux, "_numbers", func(context.Context, *struct{}) (*map[string]any, error) {
		result := map[string]any{"integer": want}
		return &result, nil
	}); err != nil {
		t.Fatal(err)
	}
	client, done := connectTestClientWithHandler(t, nil, func(*AgentConnection) any { return mux })
	defer closeClient(t, client, done)

	// A plain map has no custom decoder, so this relies on Await preserving numbers.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := call[map[string]any](ctx, client.rpc.conn, "_numbers", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if got := (*result)["integer"]; got != want {
		t.Errorf("response integer = %v (%T), want %v (json.Number)", got, got, want)
	}
}

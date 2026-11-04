package wire_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

// Rust src/agent-client-protocol/src/jsonrpc.rs doctest@844: client-mode configuration handles incoming
// requests while foreground client work sends requests and notifications.
// Rust's main_fn-owned automatic shutdown has no Go counterpart; this tests
// only the public bidirectional exchange, not a fixture implementation of it.
func TestBidirectionalClientHandler(t *testing.T) {
	mux := acp.NewExtensionMux()
	check(t, acp.AddExtensionRequest(mux, "_test/service", func(_ context.Context, p *message) (*answer, error) {
		return &answer{Result: p.Message}, nil
	}))
	c, p := clientPeerWithHandler(t, mux)
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	done := make(chan callResult[json.RawMessage], 1)
	go func() {
		r, err := c.CallExtension(ctx, "_test/foreground", struct{}{})
		done <- callResult[json.RawMessage]{r, err}
	}()
	foreground := p.receive(t)
	incoming := p.call(t, `"peer"`, "_test/service", `{"message":"incoming"}`)
	equalJSON(t, incoming["id"], `"peer"`)
	equalJSON(t, incoming["result"], `{"result":"incoming"}`)
	p.send(t, `{"jsonrpc":"2.0","id":`+string(foreground["id"])+`,"result":{"result":"outgoing"}}`)
	select {
	case result := <-done:
		check(t, result.err)
		equalJSON(t, result.value, `{"result":"outgoing"}`)
	case <-ctx.Done():
		t.Fatal("foreground request did not receive its response")
	}
	notified := make(chan error, 1)
	go func() { notified <- c.NotifyExtension(ctx, "_test/status", message{"ready"}) }()
	n := p.receive(t)
	notice, err := json.Marshal(n)
	check(t, err)
	equalJSON(t, notice, `{"jsonrpc":"2.0","method":"_test/status","params":{"message":"ready"}}`)
	select {
	case err := <-notified:
		check(t, err)
	case <-ctx.Done():
		t.Fatal("notification did not finish")
	}
}

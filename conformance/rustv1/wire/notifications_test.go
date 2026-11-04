package wire_test

import (
	"context"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

// Rust src/agent-client-protocol/tests/jsonrpc_connection_builder.rs::test_handler_claims_notification:
// one EventNotification reaches the registered handler with its original value.
func TestSingleNotification(t *testing.T) {
	a := newAgent()
	events := make(chan string, 1)
	t.Cleanup(func() {
		if len(events) != 0 {
			t.Errorf("extra event deliveries: %d", len(events))
		}
	})
	check(t, acp.AddExtensionNotification(a.ExtensionMux, "_test/event", func(_ context.Context, p *struct {
		Event string `json:"event"`
	}) error {
		events <- p.Event
		return nil
	}))
	c := pair(t, a)
	_, err := call(t, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, c.NotifyExtension(ctx, "_test/event", struct {
			Event string `json:"event"`
		}{Event: "test_event"})
	})
	check(t, err)
	select {
	case got := <-events:
		if got != "test_event" {
			t.Errorf("event = %q, want test_event", got)
		}
	case <-time.After(wait):
		t.Fatal("event was not delivered")
	}
}

package rpcgen

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestGeneratePanicsOnSchemaItCannotMap(t *testing.T) {
	// A new upstream side needs a decision about which endpoints handle it.
	assertPanics(t, `x-side "relay"`, func() {
		Generate(schemaWith(method("RelayRequest", "relay/send", "relay"), method("RelayResponse", "relay/send", "relay")))
	})

	// chat/send has both forms, so its notification is ChatSendNotification,
	// the same Go method as the chat/send_notification request.
	assertPanics(t, "both generate Go method ChatSendNotification", func() {
		Generate(schemaWith(
			method("SendRequest", "chat/send", "agent"),
			method("SendResponse", "chat/send", "agent"),
			method("SendNotification", "chat/send", "agent"),
			method("SendNotificationRequest", "chat/send_notification", "agent"),
			method("SendNotificationResponse", "chat/send_notification", "agent"),
		))
	})
}

func schemaWith(defs ...map[string]*jsonschema.Schema) *jsonschema.Schema {
	schema := &jsonschema.Schema{Defs: map[string]*jsonschema.Schema{}}
	for _, def := range defs {
		for name, s := range def {
			schema.Defs[name] = s
		}
	}
	return schema
}

func method(name, wire, side string) map[string]*jsonschema.Schema {
	return map[string]*jsonschema.Schema{name: {
		Type:  "object",
		Extra: map[string]any{"x-method": wire, "x-side": side},
	}}
}

func assertPanics(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		got := recover()
		if got == nil {
			t.Fatalf("expected panic containing %q", want)
		}
		if !strings.Contains(fmt.Sprint(got), want) {
			t.Fatalf("panic = %q, want it to contain %q", got, want)
		}
	}()
	f()
}

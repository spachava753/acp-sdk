package wire_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// TestSequentialRequests checks that a client can make repeated calls on one
// connection and each gets the result for its own params.
func TestSequentialRequests(t *testing.T) {
	a := newAgent()
	type pong struct {
		Echo string `json:"echo"`
	}
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/ping", func(_ context.Context, m *message) (*pong, error) {
		return &pong{Echo: "pong: " + m.Message}, nil
	}))
	client, _ := connect(t, a)
	for _, text := range []string{"hello world", "message 1", "message 2", "message 3", "message 4", "message 5"} {
		r, err := callExtension(t, client, "_test/ping", message{text})
		check(t, err)
		acptest.EqualJSON(t, r, fmt.Sprintf(`{"echo":"pong: %s"}`, text))
	}
}

// TestConcurrentRequests checks that concurrent calls from one client are
// each matched with their own response.
func TestConcurrentRequests(t *testing.T) {
	a := newAgent()
	a.echo(t, "_test/ping")
	client, _ := connect(t, a)
	results := make([]<-chan acptest.Result[json.RawMessage], 5)
	for i := range results {
		results[i] = acptest.Go(func() (json.RawMessage, error) {
			return client.CallExtension(context.Background(), "_test/ping", message{fmt.Sprintf("concurrent message %d", i)})
		})
	}
	for i, result := range results {
		acptest.EqualJSON(t, acptest.Value(t, result), fmt.Sprintf(`{"result":"echo: concurrent message %d"}`, i))
	}
}

// TestNotificationsDelivered checks that client notifications reach the
// agent's handler, each exactly once and with its original params. Extension
// notification handlers may run concurrently, so each delivery is awaited
// before the next notification is sent rather than asserting an order.
func TestNotificationsDelivered(t *testing.T) {
	a := newAgent()
	// Buffered for every expected delivery, so an extra one is visible
	// after dispatch has settled rather than blocking the handler.
	logs := make(chan string, 3)
	check(t, acp.AddExtensionNotification(a.ExtensionMux, "_test/log", func(_ context.Context, m *message) error {
		logs <- m.Message
		return nil
	}))
	client, stop := connect(t, a)
	for _, text := range []string{"test log 1", "test log 2"} {
		acptest.Value(t, acptest.Go(func() (struct{}, error) {
			return struct{}{}, client.NotifyExtension(context.Background(), "_test/log", message{text})
		}))
		if got := acptest.Await(t, logs); got != text {
			t.Errorf("notification = %q, want %q", got, text)
		}
	}
	check(t, stop())
	if len(logs) != 0 {
		t.Errorf("%d extra notification deliveries", len(logs))
	}
}

// TestRegisteredMethods checks that each registered extension request is
// served by its own handler with its own params, that registered methods
// that are not called are never invoked, and that an unregistered method gets
// MethodNotFound.
func TestRegisteredMethods(t *testing.T) {
	a := newAgent()
	// Buffered so every handler can record its call; the record is read after
	// dispatch has settled.
	invoked := make(chan string, 8)
	for _, method := range []string{"foo", "bar", "method1", "method2", "component"} {
		check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/"+method, func(_ context.Context, m *struct {
			Value string `json:"value"`
		}) (*answer, error) {
			invoked <- method
			return &answer{Result: method + ": " + m.Value}, nil
		}))
	}
	client, stop := connect(t, a)
	calls := []struct{ method, value string }{{"foo", "test1"}, {"bar", "test2"}, {"method2", "fallthrough"}, {"component", "test"}}
	for _, c := range calls {
		r, err := callExtension(t, client, "_test/"+c.method, map[string]string{"value": c.value})
		check(t, err)
		acptest.EqualJSON(t, r, fmt.Sprintf(`{"result":%q}`, c.method+": "+c.value))
	}
	_, err := callExtension(t, client, "_test/unclaimed", struct{}{})
	wantCode(t, err, jsonrpc.CodeMethodNotFound)

	check(t, stop())
	close(invoked)
	var got []string
	for method := range invoked {
		got = append(got, method)
	}
	if len(got) != len(calls) {
		t.Fatalf("handlers invoked = %q, want one per call", got)
	}
	for i, c := range calls {
		if got[i] != c.method {
			t.Errorf("handler %d = %q, want %q", i, got[i], c.method)
		}
	}
}

// TestEmptyAndOptionalParams checks that a request with no params reaches a
// handler expecting an empty struct, and an empty object reaches a handler
// whose fields are all optional.
func TestEmptyAndOptionalParams(t *testing.T) {
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/empty", func(context.Context, *struct{}) (*answer, error) {
		return &answer{"Got empty request"}, nil
	}))
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/optional", func(context.Context, *struct {
		Value *string `json:"value,omitempty"`
	}) (*answer, error) {
		return &answer{"Has params: true"}, nil
	}))
	client, _ := connect(t, a)
	r, err := callExtension(t, client, "_test/empty", nil)
	check(t, err)
	acptest.EqualJSON(t, r, `{"result":"Got empty request"}`)
	r, err = callExtension(t, client, "_test/optional", struct{}{})
	check(t, err)
	acptest.EqualJSON(t, r, `{"result":"Has params: true"}`)
}

type helloRequest struct {
	Name string `json:"name"`
}

type helloResponse struct {
	Greeting string `json:"greeting"`
}

// TestTypedRequestHandler checks that a typed extension handler receives its
// decoded params and its typed result is encoded as the response, while a
// request for a different method does not reach it.
func TestTypedRequestHandler(t *testing.T) {
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/hello", func(_ context.Context, p *helloRequest) (*helloResponse, error) {
		return &helloResponse{Greeting: "Hello, " + p.Name + "!"}, nil
	}))
	_, peer := startAgent(t, a)
	acptest.EqualJSON(t, peer.Call(1, "_test/hello", helloRequest{"world"}).Result, `{"greeting":"Hello, world!"}`)
	wantError(t, peer.Call(2, "_test/wrong", helloRequest{"world"}), jsonrpc.CodeMethodNotFound)
}

// TestTypedNotificationHandler checks that a typed notification handler
// receives its decoded params exactly once, and a notification for a
// different method does not reach it.
func TestTypedNotificationHandler(t *testing.T) {
	a := newAgent()
	timestamps := make(chan uint64, 2)
	check(t, acp.AddExtensionNotification(a.ExtensionMux, "_test/ping", func(_ context.Context, p *struct {
		Timestamp uint64 `json:"timestamp"`
	}) error {
		timestamps <- p.Timestamp
		return nil
	}))
	a.echo(t, "_test/barrier")
	ag, peer := startAgent(t, a)
	peer.Notify("_test/ping", map[string]any{"timestamp": 99999})
	peer.Notify("_test/wrong", map[string]any{"timestamp": 99999})
	acptest.EqualJSON(t, peer.Call(1, "_test/barrier", message{"done"}).ID, `1`)

	finishClean(t, ag, peer)
	if len(timestamps) != 1 {
		t.Fatalf("handler ran %d times, want 1", len(timestamps))
	}
	if got := <-timestamps; got != 99999 {
		t.Errorf("timestamp = %d, want 99999", got)
	}
}

// TestTypedResultDecoded checks that the package-level CallExtension decodes
// the peer's result into the requested type.
func TestTypedResultDecoded(t *testing.T) {
	client, peer := startClient(t, nil)
	result := acptest.Go(func() (*helloResponse, error) {
		return acp.CallExtension[helloResponse](context.Background(), client, "_test/hello", helloRequest{Name: "test"})
	})
	request := peer.Expect("_test/hello")
	acptest.EqualJSON(t, request.Params, `{"name":"test"}`)
	peer.Reply(request.ID, helloResponse{"Hi there!"})
	if got := acptest.Value(t, result); got == nil || got.Greeting != "Hi there!" {
		t.Errorf("result = %#v", got)
	}
}

// TestClientServesWhileCalling checks that a client answers an incoming
// extension request while its own call is waiting for a response, that the
// call then completes, and that a client notification is written as a
// notification frame with no id.
func TestClientServesWhileCalling(t *testing.T) {
	mux := acp.NewExtensionMux()
	check(t, acp.AddExtensionRequest(mux, "_test/service", func(_ context.Context, m *message) (*answer, error) {
		return &answer{Result: m.Message}, nil
	}))
	client, peer := startClient(t, mux)
	outgoing := acptest.Go(func() (json.RawMessage, error) {
		return client.CallExtension(context.Background(), "_test/foreground", struct{}{})
	})
	foreground := peer.Expect("_test/foreground")

	incoming := peer.Call("peer", "_test/service", message{"incoming"})
	acptest.EqualJSON(t, incoming.ID, `"peer"`)
	acptest.EqualJSON(t, incoming.Result, `{"result":"incoming"}`)

	peer.Reply(foreground.ID, answer{"outgoing"})
	acptest.EqualJSON(t, acptest.Value(t, outgoing), `{"result":"outgoing"}`)

	notified := acptest.Go(func() (struct{}, error) {
		return struct{}{}, client.NotifyExtension(context.Background(), "_test/status", message{"ready"})
	})
	acptest.EqualJSON(t, peer.Next().Line, `{"jsonrpc":"2.0","method":"_test/status","params":{"message":"ready"}}`)
	acptest.Value(t, notified)
}

// TestHandlersSendNotifications checks that handlers can send notifications
// through their connection: a notification handler sends one instead of a
// reply, and a request handler sends one before its response, which follows.
func TestHandlersSendNotifications(t *testing.T) {
	a := newAgent()
	check(t, acp.AddExtensionNotification(a.ExtensionMux, "_test/update", func(ctx context.Context, _ *message) error {
		return a.conn.NotifyExtension(ctx, "_test/status", message{"Acknowledged"})
	}))
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/process", func(ctx context.Context, _ *struct{}) (*answer, error) {
		if err := a.conn.NotifyExtension(ctx, "_test/status", message{"processing"}); err != nil {
			return nil, err
		}
		return &answer{"done"}, nil
	}))
	_, peer := startAgent(t, a)

	peer.Notify("_test/update", message{"update"})
	acptest.EqualJSON(t, peer.Next().Line, `{"jsonrpc":"2.0","method":"_test/status","params":{"message":"Acknowledged"}}`)

	peer.Request(1, "_test/process", map[string]any{})
	acptest.EqualJSON(t, peer.Next().Line, `{"jsonrpc":"2.0","method":"_test/status","params":{"message":"processing"}}`)
	r := peer.Next()
	acptest.EqualJSON(t, r.ID, `1`)
	acptest.EqualJSON(t, r.Result, `{"result":"done"}`)
}

package wire_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

type callResult[T any] struct {
	value T
	err   error
}

func call[T any](t *testing.T, f func(context.Context) (T, error)) (T, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	done := make(chan callResult[T], 1)
	go func() { v, err := f(ctx); done <- callResult[T]{v, err} }()
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		t.Fatal("public call did not complete before the test deadline")
		var zero T
		return zero, ctx.Err()
	}
}

func pair(t *testing.T, a *agent) *acp.Client {
	t.Helper()
	p := agentPeer(t, a, false)
	c, err := acp.Connect(context.Background(), &acp.IOTransport{Reader: p.conn, Writer: p.conn}, nil)
	check(t, err)
	t.Cleanup(func() {
		_ = p.conn.Close()
		done := make(chan error, 1)
		go func() { done <- c.Close() }()
		select {
		case <-done:
		case <-time.After(wait):
			t.Error("client did not close after raw stream closure")
		}
	})
	_, err = call(t, func(ctx context.Context) (*acp.InitializeResponse, error) {
		return c.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1})
	})
	check(t, err)
	return c
}

func extension(t *testing.T, c *acp.Client, method string, params any) json.RawMessage {
	t.Helper()
	r, err := call(t, func(ctx context.Context) (json.RawMessage, error) {
		return c.CallExtension(ctx, method, params)
	})
	check(t, err)
	return r
}

// Rust src/agent-client-protocol/tests/jsonrpc_hello.rs::test_hello_world and ::test_multiple_sequential_requests.
func TestHelloAndSequentialRequests(t *testing.T) {
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/ping", func(_ context.Context, p *message) (*struct {
		Echo string `json:"echo"`
	}, error) {
		return &struct {
			Echo string `json:"echo"`
		}{Echo: "pong: " + p.Message}, nil
	}))
	c := pair(t, a)
	equalJSON(t, extension(t, c, "_test/ping", message{"hello world"}), `{"echo":"pong: hello world"}`)
	for i := 1; i <= 5; i++ {
		equalJSON(t, extension(t, c, "_test/ping", message{fmt.Sprintf("message %d", i)}), fmt.Sprintf(`{"echo":"pong: message %d"}`, i))
	}
}

// Rust src/agent-client-protocol/tests/jsonrpc_hello.rs::test_concurrent_requests, all five responses matched.
func TestConcurrentRequests(t *testing.T) {
	a := newAgent()
	echoHandler(t, a, "_test/ping")
	c := pair(t, a)
	type result struct {
		n   int
		raw json.RawMessage
		err error
	}
	done := make(chan result, 5)
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	for i := 1; i <= 5; i++ {
		go func(n int) {
			r, err := c.CallExtension(ctx, "_test/ping", message{fmt.Sprintf("concurrent message %d", n)})
			done <- result{n, r, err}
		}(i)
	}
	for range 5 {
		select {
		case r := <-done:
			check(t, r.err)
			equalJSON(t, r.raw, fmt.Sprintf(`{"result":"echo: concurrent message %d"}`, r.n))
		case <-ctx.Done():
			t.Fatal("concurrent requests did not all complete")
		}
	}
}

// Rust src/agent-client-protocol/tests/jsonrpc_hello.rs::test_notification. Buffered events let handlers finish before
// the test reads, without the upstream's fixed 100ms sleep.
func TestNotifications(t *testing.T) {
	a := newAgent()
	logs := make(chan string, 2)
	t.Cleanup(func() {
		if len(logs) != 0 {
			t.Errorf("unexpected extra notification deliveries: %d", len(logs))
		}
	})
	check(t, acp.AddExtensionNotification(a.ExtensionMux, "_test/log", func(_ context.Context, m *message) error {
		logs <- m.Message
		return nil
	}))
	c := pair(t, a)
	for _, s := range []string{"test log 1", "test log 2"} {
		_, err := call(t, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.NotifyExtension(ctx, "_test/log", message{s})
		})
		check(t, err)
	}
	for _, want := range []string{"test log 1", "test log 2"} {
		select {
		case got := <-logs:
			if got != want {
				t.Errorf("notification = %q, want %q", got, want)
			}
		case <-time.After(wait):
			t.Fatal("notification was not delivered")
		}
	}
}

// Rust src/agent-client-protocol/tests/jsonrpc_connection_builder.rs::test_multiple_handlers_different_methods,
// ::test_fallthrough_behavior, ::test_no_handler_claims, and
// ::test_connection_builder_as_component: independent registered routes serve
// their own payloads and leave unsupported requests as MethodNotFound.
func TestRegisteredMethods(t *testing.T) {
	a := newAgent()
	invoked := make(chan string, 32)
	// This checker runs after pair's raw-close/drain cleanup. The buffer lets
	// every registered route return before its invocation list is inspected.
	t.Cleanup(func() {
		for _, want := range []string{"foo", "bar", "method2", "component"} {
			select {
			case got := <-invoked:
				if got != want {
					t.Errorf("handler = %q, want %q", got, want)
				}
			default:
				t.Errorf("handler %q was not invoked", want)
			}
		}
		if len(invoked) != 0 {
			t.Errorf("unmatched routes were invoked %d extra times", len(invoked))
		}
	})
	for _, method := range []string{"foo", "bar", "method1", "method2", "component"} {
		check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/"+method, func(_ context.Context, m *struct {
			Value string `json:"value"`
		}) (*answer, error) {
			invoked <- method
			return &answer{Result: method + ": " + m.Value}, nil
		}))
	}
	c := pair(t, a)
	for _, tc := range []struct{ method, value string }{
		{"foo", "test1"}, {"bar", "test2"}, {"method2", "fallthrough"}, {"component", "test"},
	} {
		equalJSON(t, extension(t, c, "_test/"+tc.method, map[string]string{"value": tc.value}), fmt.Sprintf(`{"result":%q}`, tc.method+": "+tc.value))
	}
	_, err := call(t, func(ctx context.Context) (json.RawMessage, error) {
		return c.CallExtension(ctx, "_test/unclaimed", struct{}{})
	})
	var rpcError *jsonrpc.Error
	if !errors.As(err, &rpcError) || rpcError.Code != jsonrpc.CodeMethodNotFound {
		t.Fatalf("unclaimed request = %v, want MethodNotFound", err)
	}
}

// Rust src/agent-client-protocol/tests/jsonrpc_edge_cases.rs::test_empty_request and ::test_null_params.
// The latter's actual payload is an empty object (OptionalParamsRequest None),
// not a literal null; keep that distinction instead of inventing a null case.
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
	c := pair(t, a)
	equalJSON(t, extension(t, c, "_test/empty", nil), `{"result":"Got empty request"}`)
	equalJSON(t, extension(t, c, "_test/optional", struct{}{}), `{"result":"Has params: true"}`)
}

// Rust src/agent-client-protocol/tests/jsonrpc_error_handling.rs::test_unknown_method,
// ::test_handler_returns_error, ::test_missing_required_params. The last Rust
// test explicitly returns InvalidParams from its handler; it does not exercise
// automatic required-field detection.
func TestHandlerErrors(t *testing.T) {
	a := newAgent()
	for _, tc := range []struct {
		method string
		code   int64
	}{
		{"internal", jsonrpc.CodeInternalError}, {"strict", jsonrpc.CodeInvalidParams},
	} {
		check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/"+tc.method, func(context.Context, *struct{}) (*answer, error) {
			return nil, &jsonrpc.Error{Code: tc.code, Message: "handler error"}
		}))
	}
	c := pair(t, a)
	for _, tc := range []struct {
		method string
		code   int64
	}{
		{"unknown", jsonrpc.CodeMethodNotFound}, {"internal", jsonrpc.CodeInternalError}, {"strict", jsonrpc.CodeInvalidParams},
	} {
		_, err := call(t, func(ctx context.Context) (json.RawMessage, error) {
			return c.CallExtension(ctx, "_test/"+tc.method, struct{}{})
		})
		var e *jsonrpc.Error
		if !errors.As(err, &e) || e.Code != tc.code {
			t.Errorf("%s: got %v, want code %d", tc.method, err, tc.code)
		}
	}
}

package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/jsonrpc"
)

type extTestAgent struct {
	*ExtensionMux
	ac                   *AgentConnection
	notificationsHandled chan<- string
}

func (a *extTestAgent) handleExtensionNotification(ctx context.Context, method string, params json.RawMessage) error {
	err := a.ExtensionMux.handleExtensionNotification(ctx, method, params)
	// Signal after dispatch, even when the mux ignores the notification.
	a.notificationsHandled <- method
	return err
}

type extClientHandler struct {
	*ExtensionMux
}

type CustomNotification struct {
	Field string
}

type CustomRequest struct {
	Field string
}

type CustomResponse struct {
	Field string
}

// TestExtensions checks client-to-agent extensions as handlers are registered.
// It covers method names, responses, handler errors, and ignored notifications.
// Registration order lets us check request-only, notification-only, and shared names.
func TestExtensions(t *testing.T) {
	// Keep the connection alive until cleanup closes the client and waits for the agent.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	at, ct := NewInMemoryTransports()
	agentExt := NewExtensionMux()
	client, err := Connect(ctx, ct, &extClientHandler{})
	if err != nil {
		t.Fatal(err)
	}
	notificationsHandled := make(chan string, 1)
	agentDone := make(chan error, 1)
	go func() {
		agentDone <- RunAgent(ctx, at, func(ac *AgentConnection) any {
			return &extTestAgent{
				ExtensionMux:         agentExt,
				ac:                   ac,
				notificationsHandled: notificationsHandled,
			}
		})
	}()
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
		select {
		case err := <-agentDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("timed out waiting for agent shutdown")
		}
	})

	// Request method names must start with an underscore.
	const malformedCustomCallMethod = "myBadMethod"
	if err := AddExtensionRequest(
		agentExt,
		malformedCustomCallMethod,
		func(ctx context.Context, p *CustomRequest) (*CustomResponse, error) {
			panic("unreachable")
		},
	); err == nil {
		t.Error("accepted extension request method without underscore")
	}

	// Before registration, a request should return method not found and no result.
	const customCallMethod = "_test/customCallMethod"
	req := &CustomRequest{"myField"}
	result, err := CallExtension[CustomResponse](t.Context(), client, customCallMethod, req)
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("expected error method not found, got %v", err)
	}
	if result != nil {
		t.Error("expected nil result")
	}

	// Register an echo handler that rejects an empty field.
	var requestCalls atomic.Int32
	handlerErr := &jsonrpc.Error{
		Code:    jsonrpc.CodeInvalidParams,
		Message: "extension failed",
		Data:    json.RawMessage(`{"retryable":true,"retryAfter":3}`),
	}
	if err := AddExtensionRequest(
		agentExt,
		customCallMethod,
		func(ctx context.Context, p *CustomRequest) (*CustomResponse, error) {
			requestCalls.Add(1)
			if p.Field == "" {
				return nil, handlerErr
			}
			return &CustomResponse{Field: p.Field}, nil
		},
	); err != nil {
		t.Fatal(err)
	}

	// This name has only a request handler, so a notification must not invoke it.
	if err := client.NotifyExtension(t.Context(), customCallMethod, req); err != nil {
		t.Fatal(err)
	}
	select {
	case method := <-notificationsHandled:
		if method != customCallMethod {
			t.Fatalf("handled notification %q, want %q", method, customCallMethod)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for notification dispatch")
	}
	if got := requestCalls.Load(); got != 0 {
		t.Errorf("notification invoked request handler %d times", got)
	}

	// Once registered, the request should return the handler's response.
	result, err = CallExtension[CustomResponse](t.Context(), client, customCallMethod, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Field != req.Field {
		t.Error("fields were expected to be equal")
	}

	// The caller should receive the handler's error code, message, and data unchanged.
	result, err = CallExtension[CustomResponse](t.Context(), client, customCallMethod, &CustomRequest{})
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected JSON-RPC error, got %v", err)
	}
	if rpcErr.Code != handlerErr.Code || rpcErr.Message != handlerErr.Message || !bytes.Equal(rpcErr.Data, handlerErr.Data) {
		t.Errorf("error = %#v, want %#v", rpcErr, handlerErr)
	}
	if result != nil {
		t.Errorf("expected nil result, got %#v", result)
	}

	// Notification method names must also start with an underscore.
	const malformedCustomNotifyMethod = "myBadNotify"
	if err := AddExtensionNotification(
		agentExt,
		malformedCustomNotifyMethod,
		func(ctx context.Context, p *CustomNotification) error {
			panic("unreachable")
		},
	); err == nil {
		t.Error("accepted extension notification method without underscore")
	}

	const customNotificationMethod = "_test/customNotificationMethod"
	notification := &CustomNotification{"myNotif"}

	// An unknown notification is ignored; wait until the mux has processed it.
	const unknownNotificationMethod = "_test/unknownNotification"
	if err := client.NotifyExtension(t.Context(), unknownNotificationMethod, notification); err != nil {
		t.Fatal(err)
	}
	select {
	case method := <-notificationsHandled:
		if method != unknownNotificationMethod {
			t.Fatalf("handled notification %q, want %q", method, unknownNotificationMethod)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for unknown notification dispatch")
	}

	// Buffer the value so the handler can finish before the test reads it.
	received := make(chan string, 1)
	onNotification := func(ctx context.Context, p *CustomNotification) error {
		received <- p.Field
		return nil
	}
	if err := AddExtensionNotification(agentExt, customNotificationMethod, onNotification); err != nil {
		t.Fatal(err)
	}

	// This name has only a notification handler, so a request must not invoke it.
	result, err = CallExtension[CustomResponse](t.Context(), client, customNotificationMethod, notification)
	if !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("expected method not found, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %#v", result)
	}
	select {
	case value := <-received:
		t.Errorf("request invoked notification handler with %q", value)
	default:
	}

	// Give the request name a notification handler too.
	// Requests must still reach only the request handler.
	if err := AddExtensionNotification(agentExt, customCallMethod, onNotification); err != nil {
		t.Fatal(err)
	}
	result, err = CallExtension[CustomResponse](t.Context(), client, customCallMethod, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Field != req.Field {
		t.Error("fields were expected to be equal")
	}
	select {
	case value := <-received:
		t.Errorf("request invoked notification handler with %q", value)
	default:
	}

	// One name has only a notification handler; the other has both handler kinds.
	// Both must deliver the notification. The request count below checks that
	// neither notification invoked the request handler.
	for _, method := range []string{customNotificationMethod, customCallMethod} {
		if err := client.NotifyExtension(t.Context(), method, notification); err != nil {
			t.Fatal(err)
		}
		select {
		case handled := <-notificationsHandled:
			if handled != method {
				t.Fatalf("handled notification %q, want %q", handled, method)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %q notification dispatch", method)
		}
		// Dispatch has finished, so the handler's value must already be buffered.
		select {
		case value := <-received:
			if value != notification.Field {
				t.Errorf("notification field = %q, want %q", value, notification.Field)
			}
		default:
			t.Errorf("notification %q did not invoke its handler", method)
		}
	}

	// Only the two successful requests and the empty-field request should be counted.
	if got := requestCalls.Load(); got != 3 {
		t.Errorf("request handler called %d times, want 3", got)
	}
}

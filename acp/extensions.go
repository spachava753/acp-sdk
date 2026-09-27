package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/spachava753/acp-sdk/internal/jsonrpc2"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

type extensionRequestHandler interface {
	handleExtensionRequest(context.Context, string, json.RawMessage) (json.RawMessage, error)
}

type extensionNotificationHandler interface {
	handleExtensionNotification(context.Context, string, json.RawMessage) error
}

// ExtensionCaller is implemented by Client and AgentConnection.
type ExtensionCaller interface {
	CallExtension(context.Context, string, any) (json.RawMessage, error)
}

// CallExtension calls an underscore-prefixed method and decodes its result into R.
// A JSON null result decodes to the zero value of R.
func CallExtension[R any](ctx context.Context, peer ExtensionCaller, method string, params any) (*R, error) {
	raw, err := peer.CallExtension(ctx, method, params)
	if err != nil {
		return nil, err
	}
	var result R
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding extension %q result: %w", method, err)
	}
	return &result, nil
}

// CallExtension calls an underscore-prefixed agent method and returns its raw result.
// Use the package-level CallExtension function for a typed result.
func (c *Client) CallExtension(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.rpc.callExtension(ctx, method, params)
}

// NotifyExtension sends an underscore-prefixed notification to the agent.
func (c *Client) NotifyExtension(ctx context.Context, method string, params any) error {
	return c.rpc.notifyExtension(ctx, method, params)
}

// CallExtension calls an underscore-prefixed client method and returns its raw result.
// Use the package-level CallExtension function for a typed result.
func (c *AgentConnection) CallExtension(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.rpc.callExtension(ctx, method, params)
}

// NotifyExtension sends an underscore-prefixed notification to the client.
func (c *AgentConnection) NotifyExtension(ctx context.Context, method string, params any) error {
	return c.rpc.notifyExtension(ctx, method, params)
}

func validateExtensionMethod(method string) error {
	if !strings.HasPrefix(method, "_") {
		return fmt.Errorf("extension method %q must start with an underscore", method)
	}
	return nil
}

func (e *rpcEndpoint) callExtension(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := validateExtensionMethod(method); err != nil {
		return nil, err
	}
	result, err := call[json.RawMessage](ctx, e.conn, method, params)
	if err != nil {
		return nil, err
	}
	return *result, nil
}

func (e *rpcEndpoint) notifyExtension(ctx context.Context, method string, params any) error {
	if err := validateExtensionMethod(method); err != nil {
		return err
	}
	return notify(ctx, e.conn, method, params)
}

func handleExtension(ctx context.Context, handler any, req *jsonrpc.Request) (any, error) {
	if !strings.HasPrefix(req.Method, "_") {
		return nil, methodNotFound(req.Method)
	}
	if !req.IsCall() {
		if h, ok := handler.(extensionNotificationHandler); ok {
			return nil, h.handleExtensionNotification(ctx, req.Method, req.Params)
		}
		return nil, nil
	}
	h, ok := handler.(extensionRequestHandler)
	if !ok {
		return nil, methodNotFound(req.Method)
	}
	result, err := h.handleExtensionRequest(ctx, req.Method, req.Params)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return json.RawMessage("null"), nil
	}
	if !json.Valid(result) {
		return nil, fmt.Errorf("%w: extension %q returned invalid JSON", jsonrpc2.ErrInternal, req.Method)
	}
	return result, nil
}

type extensionRequestFunc func(context.Context, json.RawMessage) (json.RawMessage, error)
type extensionNotificationFunc func(context.Context, json.RawMessage) error

// ExtensionMux routes extension requests and notifications to typed handlers.
// Embed a *ExtensionMux in an agent or client handler to receive extensions.
// The zero value is ready to use. A mux must not be copied after first use.
// Registration and dispatch are safe to call concurrently; handlers may run
// concurrently and must synchronize their own state. Prefer registering before
// connecting so every advertised method is available when messages arrive.
type ExtensionMux struct {
	mu            sync.RWMutex
	requests      map[string]extensionRequestFunc
	notifications map[string]extensionNotificationFunc
}

func NewExtensionMux() *ExtensionMux {
	return &ExtensionMux{}
}

// AddExtensionRequest registers a typed request handler. Names must start with
// an underscore and must not already have a request handler. Missing or null
// params produce a zero-valued P; field validation is the handler's responsibility.
func AddExtensionRequest[P, R any](mux *ExtensionMux, method string, handler func(context.Context, *P) (*R, error)) error {
	if err := validateExtensionMethod(method); err != nil {
		return err
	}
	if mux == nil || handler == nil {
		return fmt.Errorf("extension %q requires a non-nil mux and handler", method)
	}
	mux.mu.Lock()
	defer mux.mu.Unlock()
	if _, ok := mux.requests[method]; ok {
		return fmt.Errorf("extension request %q already registered", method)
	}
	if mux.requests == nil {
		mux.requests = make(map[string]extensionRequestFunc)
	}
	mux.requests[method] = func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		params, err := decodeParams[P](&jsonrpc.Request{Params: raw})
		if err != nil {
			return nil, err
		}
		result, err := handler(ctx, params)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("%w: encoding extension %q result: %v", jsonrpc2.ErrInternal, method, err)
		}
		return data, nil
	}
	return nil
}

// AddExtensionNotification registers a typed notification handler. Names must
// start with an underscore and must not already have a notification handler.
// A name may have both a request and a notification handler. Missing or null
// params produce a zero-valued P; field validation is the handler's responsibility.
func AddExtensionNotification[P any](mux *ExtensionMux, method string, handler func(context.Context, *P) error) error {
	if err := validateExtensionMethod(method); err != nil {
		return err
	}
	if mux == nil || handler == nil {
		return fmt.Errorf("extension %q requires a non-nil mux and handler", method)
	}
	mux.mu.Lock()
	defer mux.mu.Unlock()
	if _, ok := mux.notifications[method]; ok {
		return fmt.Errorf("extension notification %q already registered", method)
	}
	if mux.notifications == nil {
		mux.notifications = make(map[string]extensionNotificationFunc)
	}
	mux.notifications[method] = func(ctx context.Context, raw json.RawMessage) error {
		params, err := decodeParams[P](&jsonrpc.Request{Params: raw})
		if err != nil {
			return err
		}
		return handler(ctx, params)
	}
	return nil
}

func (m *ExtensionMux) handleExtensionRequest(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if m == nil {
		return nil, methodNotFound(method)
	}
	m.mu.RLock()
	handler := m.requests[method]
	m.mu.RUnlock()
	if handler == nil {
		return nil, methodNotFound(method)
	}
	return handler(ctx, params)
}

func (m *ExtensionMux) handleExtensionNotification(ctx context.Context, method string, params json.RawMessage) error {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	handler := m.notifications[method]
	m.mu.RUnlock()
	if handler == nil {
		return nil
	}
	return handler(ctx, params)
}

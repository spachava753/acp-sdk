// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// initializeAgent answers initialize with the requested protocol version and
// serves session/new.
type initializeAgent struct{ sessionOnlyAgent }

func (initializeAgent) Initialize(_ context.Context, r *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: r.ProtocolVersion}, nil
}

// sessionOnlyAgent serves session/new and has no initialize handler.
type sessionOnlyAgent struct{}

func (sessionOnlyAgent) SessionNew(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: "session-1"}, nil
}

const newSessionParams = `{"cwd":"/tmp","mcpServers":[]}`

// TestInitializationGate checks that an agent with an initialize handler
// rejects session requests before initialize with InvalidRequest, serves them
// afterwards, and accepts a repeated initialize.
func TestInitializationGate(t *testing.T) {
	_, peer := acptest.StartAgent(t, initializeAgent{})
	if r := peer.Call(1, "session/new", json.RawMessage(newSessionParams)); r.Error == nil || r.Error.Code != jsonrpc.CodeInvalidRequest {
		t.Errorf("session/new before initialize = %s, want InvalidRequest", r)
	}
	initialize := map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}
	acptest.EqualJSON(t, peer.Call(2, "initialize", initialize).Result, `{"protocolVersion":1}`)
	acptest.EqualJSON(t, peer.Call(3, "session/new", json.RawMessage(newSessionParams)).Result, `{"sessionId":"session-1"}`)
	acptest.EqualJSON(t, peer.Call(4, "initialize", initialize).Result, `{"protocolVersion":1}`)
}

// dualModeAgent can start either standalone or as a proxy.
type dualModeAgent struct{ initializeAgent }

func (dualModeAgent) ProxyInitialize(_ context.Context, r *acp.InitializeProxyRequest) (*acp.InitializeProxyResponse, error) {
	return &acp.InitializeProxyResponse{ProtocolVersion: r.ProtocolVersion}, nil
}

// TestProxyInitializationOpensGate checks that a conductor can start an agent
// that also supports standalone use with proxy/initialize instead of
// initialize, and that later requests are then served.
func TestProxyInitializationOpensGate(t *testing.T) {
	_, peer := acptest.StartAgent(t, dualModeAgent{})
	if r := peer.Call(1, "session/new", json.RawMessage(newSessionParams)); r.Error == nil || r.Error.Code != jsonrpc.CodeInvalidRequest {
		t.Errorf("session/new before proxy/initialize = %s, want InvalidRequest", r)
	}
	initialize := map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}
	acptest.EqualJSON(t, peer.Call(2, "proxy/initialize", initialize).Result, `{"protocolVersion":1}`)
	acptest.EqualJSON(t, peer.Call(3, "session/new", json.RawMessage(newSessionParams)).Result, `{"sessionId":"session-1"}`)
}

// TestNoInitializeHandler checks that an agent without an initialize handler
// serves requests without being initialized.
func TestNoInitializeHandler(t *testing.T) {
	_, peer := acptest.StartAgent(t, sessionOnlyAgent{})
	acptest.EqualJSON(t, peer.Call(1, "session/new", json.RawMessage(newSessionParams)).Result, `{"sessionId":"session-1"}`)
}

// TestMalformedInitialize checks that initialize params with a missing or
// invalid protocol version are rejected with InvalidParams before reaching
// the handler.
func TestMalformedInitialize(t *testing.T) {
	for _, params := range []string{`{}`, `{"protocolVersion":"1"}`, `{"protocolVersion":-1}`, `{"protocolVersion":1.5}`, `{"protocolVersion":65536}`} {
		t.Run(params, func(t *testing.T) {
			_, peer := acptest.StartAgent(t, initializeAgent{})
			if r := peer.Call(1, "initialize", json.RawMessage(params)); r.Error == nil || r.Error.Code != jsonrpc.CodeInvalidParams {
				t.Fatalf("response = %s, want InvalidParams", r)
			}
		})
	}
}

// TestAgentKeepsRequestedVersion checks that the SDK does not alter the
// protocol version an agent's handler returns, even one it does not support.
func TestAgentKeepsRequestedVersion(t *testing.T) {
	_, peer := acptest.StartAgent(t, initializeAgent{})
	response := peer.Call(1, "initialize", map[string]any{"protocolVersion": 2})
	if response.Error != nil {
		t.Fatalf("initialize = %s", response)
	}
	acptest.EqualJSON(t, response.Result, `{"protocolVersion":2}`)
}

// TestInitializeVersionNegotiation checks that initialize and
// proxy/initialize send the requested version, return the version the peer
// selects, and return a peer rejection with its code, message and data intact.
func TestInitializeVersionNegotiation(t *testing.T) {
	ctx := context.Background()
	initialize := func(version acp.ProtocolVersion) func(*acp.Client) (acp.ProtocolVersion, error) {
		return func(c *acp.Client) (acp.ProtocolVersion, error) {
			r, err := c.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: version})
			if err != nil {
				return 0, err
			}
			return r.ProtocolVersion, nil
		}
	}
	proxyInitialize := func(c *acp.Client) (acp.ProtocolVersion, error) {
		r, err := c.ProxyInitialize(ctx, &acp.InitializeProxyRequest{ProtocolVersion: 1, ClientCapabilities: &acp.ClientCapabilities{}, ClientInfo: &acp.Implementation{Name: "router-test-client", Version: "1.0.0"}})
		if err != nil {
			return 0, err
		}
		return r.ProtocolVersion, nil
	}
	const proxyParams = `{"protocolVersion":1,"clientCapabilities":{},"clientInfo":{"name":"router-test-client","version":"1.0.0"}}`
	for _, tc := range []struct {
		name, method, params string
		call                 func(*acp.Client) (acp.ProtocolVersion, error)
		reject               bool
	}{
		{"initialize_selects_1", "initialize", `{"protocolVersion":2}`, initialize(2), false},
		{"initialize_rejected", "initialize", `{"protocolVersion":1}`, initialize(1), true},
		{"proxy_selects_1", "proxy/initialize", proxyParams, proxyInitialize, false},
		{"proxy_rejected", "proxy/initialize", proxyParams, proxyInitialize, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, peer := acptest.StartClient(t, nil)
			result := acptest.Go(func() (acp.ProtocolVersion, error) { return tc.call(client) })
			request := expectRequest(t, peer, tc.method, []byte(tc.params))
			if !tc.reject {
				peer.Reply(request.ID, json.RawMessage(`{"protocolVersion":1}`))
				if got := acptest.Value(t, result); got != 1 {
					t.Fatalf("selected version = %d, want 1", got)
				}
				return
			}
			peer.ReplyError(request.ID, codeUnsupportedProtocolVersion, "Unsupported protocol version", map[string]any{"requested": 1, "supported": []int{2}})
			var remote *jsonrpc.Error
			if err := acptest.Await(t, result).Err; !errors.As(err, &remote) || remote.Code != codeUnsupportedProtocolVersion || remote.Message != "Unsupported protocol version" {
				t.Fatalf("error = %v, want the peer's rejection", err)
			}
			acptest.EqualJSON(t, remote.Data, `{"requested":1,"supported":[2]}`)
		})
	}
}

// TestRawConnectionDoesNotRewriteInitialize checks that a transport
// connection used directly passes a non-ACP initialize request and its
// response through unchanged.
func TestRawConnectionDoesNotRewriteInitialize(t *testing.T) {
	peer, transport := acptest.NewPeer(t)
	ctx := context.Background()
	conn, err := transport.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := jsonrpc.MakeID("foreign-initialize")
	if err != nil {
		t.Fatal(err)
	}
	const params = `{"protocolVersion":"2025-06-18"}`
	result := acptest.Go(func() (jsonrpc.Message, error) {
		if err := conn.Write(ctx, &jsonrpc.Request{ID: id, Method: "initialize", Params: json.RawMessage(params)}); err != nil {
			return nil, err
		}
		return conn.Read(ctx)
	})
	request := expectRequest(t, peer, "initialize", []byte(params))
	peer.Reply(request.ID, json.RawMessage(params))
	message := acptest.Value(t, result)
	response, ok := message.(*jsonrpc.Response)
	if !ok || response.Error != nil {
		t.Fatalf("response = %#v", message)
	}
	acptest.EqualJSON(t, response.Result, params)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestProxySuccessorResultUnchanged checks that proxy/successor returns the
// wrapped result as sent, including unknown fields and a result that is not
// a valid session/new response.
func TestProxySuccessorResultUnchanged(t *testing.T) {
	client, peer := acptest.StartClient(t, nil)
	for _, tc := range []struct{ name, response string }{
		{"malformed", `{"_futureResponseField":{"preserved":false}}`},
		{"valid", `{"sessionId":"raw-session","_futureResponseField":{"preserved":true}}`},
	} {
		result := acptest.Go(func() (*acp.ProxySuccessorResponse, error) {
			return client.ProxySuccessor(context.Background(), &acp.ProxySuccessorRequest{Method: "session/new", Params: map[string]any{"cwd": "/" + tc.name, "_case": tc.name}})
		})
		request := expectRequest(t, peer, "proxy/successor", []byte(`{"method":"session/new","params":{"cwd":"/`+tc.name+`","_case":"`+tc.name+`"}}`))
		peer.Reply(request.ID, json.RawMessage(tc.response))
		acptest.EqualMarshaled(t, acptest.Value(t, result), tc.response)
	}
}

type proxyInitializeAgent struct{}

func (proxyInitializeAgent) ProxyInitialize(_ context.Context, r *acp.InitializeProxyRequest) (*acp.InitializeProxyResponse, error) {
	return &acp.InitializeProxyResponse{ProtocolVersion: r.ProtocolVersion}, nil
}

// TestMalformedFramesBeforeProxyInitialize checks that an agent answers an
// unparsable line with a single ParseError and a batch with a single
// InvalidRequest, both with a null ID, then keeps reading and serves the next
// individual request.
func TestMalformedFramesBeforeProxyInitialize(t *testing.T) {
	_, peer := acptest.StartAgent(t, proxyInitializeAgent{})
	peer.SendLine("{not json")
	if r := peer.Next(); string(r.ID) != "null" || r.Error == nil || r.Error.Code != jsonrpc.CodeParseError {
		t.Fatalf("unparsable line response = %s, want ParseError with null ID", r)
	}
	params := map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}, "clientInfo": map[string]any{"name": "router-test-client", "version": "1.0.0"}, "_futureInitializeField": map[string]any{"preserved": true}}
	peer.Send([]any{map[string]any{"jsonrpc": "2.0", "id": 7, "method": "proxy/initialize", "params": params}})
	if r := peer.Next(); string(r.ID) != "null" || r.Error == nil || r.Error.Code != jsonrpc.CodeInvalidRequest {
		t.Fatalf("batch response = %s, want InvalidRequest with null ID", r)
	}
	// The next frame answering id 1 shows the batch produced no other reply.
	r := peer.Call(1, "proxy/initialize", params)
	if string(r.ID) != "1" || r.Error != nil {
		t.Fatalf("proxy/initialize after malformed frames = %s", r)
	}
	acptest.EqualJSON(t, r.Result, `{"protocolVersion":1}`)
}

// TestStrayResponseBeforeInitialize checks that an agent ignores a response
// to a request it never sent and then serves initialize.
func TestStrayResponseBeforeInitialize(t *testing.T) {
	_, peer := acptest.StartAgent(t, initializeAgent{})
	peer.Send(map[string]any{"jsonrpc": "2.0", "id": 99, "result": nil, "error": map[string]any{"code": jsonrpc.CodeInternalError, "message": "Internal error"}})
	r := peer.Call(1, "initialize", map[string]any{"protocolVersion": 1})
	if string(r.ID) != "1" || r.Error != nil {
		t.Fatalf("first frame after stray response = %s, want initialize result", r)
	}
	acptest.EqualJSON(t, r.Result, `{"protocolVersion":1}`)
}

// streamingAgent streams two chunks before ending each prompt turn.
type streamingAgent struct{ conn *acp.AgentConnection }

func (a *streamingAgent) Initialize(_ context.Context, r *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: r.ProtocolVersion, AgentCapabilities: &acp.AgentCapabilities{}}, nil
}

func (a *streamingAgent) SessionNew(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: "session-1"}, nil
}

func (a *streamingAgent) SessionPrompt(ctx context.Context, r *acp.PromptRequest) (*acp.PromptResponse, error) {
	for _, text := range []string{"Hello, ", "world!"} {
		if err := a.conn.SessionUpdate(ctx, &acp.SessionNotification{SessionID: r.SessionID, Update: acp.AgentMessageChunkSessionUpdate(acp.TextContentBlock(text))}); err != nil {
			return nil, err
		}
	}
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// TestAgentLifecycle drives an agent through initialize, session/new and a
// prompt whose two streamed updates must precede its response. An unknown
// request then gets MethodNotFound, and an unhandled notification gets no
// reply.
func TestAgentLifecycle(t *testing.T) {
	_, peer := acptest.StartAgentFunc(t, func(conn *acp.AgentConnection) any { return &streamingAgent{conn: conn} })
	acptest.EqualJSON(t, peer.Call(1, "initialize", map[string]any{"protocolVersion": 1}).Result, `{"protocolVersion":1,"agentCapabilities":{}}`)
	acptest.EqualJSON(t, peer.Call(2, "session/new", json.RawMessage(newSessionParams)).Result, `{"sessionId":"session-1"}`)
	peer.Request(3, "session/prompt", map[string]any{"sessionId": "session-1", "prompt": []any{map[string]any{"type": "text", "text": "Hello, agent!"}}})
	for _, text := range []string{"Hello, ", "world!"} {
		update := peer.Expect("session/update")
		if !update.IsNotification() {
			t.Fatalf("update sent as request: %s", update)
		}
		acptest.EqualJSON(t, update.Params, `{"sessionId":"session-1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"`+text+`"}}}`)
	}
	acptest.EqualJSON(t, peer.Next().Result, `{"stopReason":"end_turn"}`)

	if r := peer.Call(4, "_example/unknown", map[string]any{}); r.Error == nil || r.Error.Code != jsonrpc.CodeMethodNotFound {
		t.Fatalf("unknown request response = %s, want MethodNotFound", r)
	}
	peer.Notify("_example/unhandled", map[string]any{})
	if r := peer.Call(5, "session/new", json.RawMessage(newSessionParams)); string(r.ID) != "5" {
		t.Fatalf("frame after unhandled notification = %s, want the reply to id 5", r)
	}
}

// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package acptest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

// StartClient connects an SDK client with handler to a new peer. Cleanup ends
// the peer's input, closes the client and reports an error from Close.
func StartClient(t testing.TB, handler any) (*acp.Client, *Peer) {
	t.Helper()
	peer, transport := NewPeer(t)
	client, err := acp.Connect(context.Background(), transport, handler)
	if err != nil {
		t.Fatalf("acptest: connecting client: %v", err)
	}
	// Registered after NewPeer, so this runs before the pipes are closed.
	t.Cleanup(func() {
		peer.CloseInput()
		done := make(chan error, 1)
		go func() { done <- client.Close() }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("acptest: closing client: %v", err)
			}
		case <-time.After(Timeout):
			t.Error("acptest: client did not close")
		}
	})
	return client, peer
}

// Agent is an SDK agent served by RunAgent for the duration of a test.
type Agent struct {
	// Conn is the connection RunAgent passed to the agent factory.
	Conn *acp.AgentConnection
	done chan error
	err  error
	ok   bool
}

// Wait returns RunAgent's result, failing the test if it has not returned
// within Timeout.
func (a *Agent) Wait(t testing.TB) error {
	t.Helper()
	if a.ok {
		return a.err
	}
	select {
	case a.err = <-a.done:
		a.ok = true
		return a.err
	case <-time.After(Timeout):
		t.Fatal("acptest: RunAgent did not return")
		return nil
	}
}

// StartAgent serves handler as an SDK agent connected to a new peer.
func StartAgent(t testing.TB, handler any) (*Agent, *Peer) {
	t.Helper()
	return StartAgentFunc(t, func(*acp.AgentConnection) any { return handler })
}

// StartAgentFunc serves the agent built by newAgent, connected to a new peer.
// Cleanup ends the peer's input, waits for RunAgent to return, and reports an
// error if it failed. Tests that expect a failure read it first with Wait.
func StartAgentFunc(t testing.TB, newAgent acp.AgentFactory) (*Agent, *Peer) {
	t.Helper()
	peer, transport := NewPeer(t)
	agent := &Agent{done: make(chan error, 1)}
	conns := make(chan *acp.AgentConnection, 1)
	go func() {
		agent.done <- acp.RunAgent(context.Background(), transport, func(conn *acp.AgentConnection) any {
			conns <- conn
			return newAgent(conn)
		})
	}()
	t.Cleanup(func() {
		peer.CloseInput()
		if agent.ok {
			return
		}
		select {
		case agent.err = <-agent.done:
			agent.ok = true
			if agent.err != nil {
				t.Errorf("acptest: RunAgent returned %v after input EOF", agent.err)
			}
		case <-time.After(Timeout):
			t.Error("acptest: RunAgent did not return after input EOF")
		}
	})
	select {
	case agent.Conn = <-conns:
	case <-time.After(Timeout):
		t.Fatal("acptest: RunAgent did not build the agent")
	}
	return agent, peer
}

// InitializeAgent sends a protocol version 1 initialize request with the given
// client capabilities and fails unless the agent answers successfully.
func (p *Peer) InitializeAgent(capabilities any) Message {
	p.t.Helper()
	if capabilities == nil {
		capabilities = map[string]any{}
	}
	response := p.Call("initialize", acp.MethodInitialize, map[string]any{"protocolVersion": 1, "clientCapabilities": capabilities})
	if string(response.ID) != `"initialize"` || response.Error != nil || response.Result == nil {
		p.t.Fatalf("acptest: initialize failed: %s", response)
	}
	return response
}

// InitializeClient drives client.Initialize at protocol version 1, answering
// as an agent with agentCapabilities, and returns the client's view of the
// response.
func InitializeClient(t testing.TB, client *acp.Client, peer *Peer, capabilities *acp.ClientCapabilities, agentCapabilities any) *acp.InitializeResponse {
	t.Helper()
	if agentCapabilities == nil {
		agentCapabilities = map[string]any{}
	}
	params := &acp.InitializeRequest{ProtocolVersion: 1, ClientCapabilities: capabilities}
	result := Go(func() (*acp.InitializeResponse, error) { return client.Initialize(context.Background(), params) })
	request := peer.ExpectCall(acp.MethodInitialize)
	EqualJSON(t, request.Params, string(Marshal(t, params)))
	peer.Reply(request.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": agentCapabilities})
	response := Value(t, result)
	if response.ProtocolVersion != 1 {
		t.Fatalf("acptest: negotiated protocol version %d, want 1", response.ProtocolVersion)
	}
	return response
}

// NewSession drives client.SessionNew, answering with sessionID.
func NewSession(t testing.TB, client *acp.Client, peer *Peer, sessionID string) {
	t.Helper()
	result := Go(func() (*acp.NewSessionResponse, error) {
		return client.SessionNew(context.Background(), &acp.NewSessionRequest{Cwd: "/workspace", McpServers: []acp.McpServer{}})
	})
	request := peer.ExpectCall(acp.MethodSessionNew)
	EqualJSON(t, request.Params, `{"cwd":"/workspace","mcpServers":[]}`)
	peer.Reply(request.ID, map[string]any{"sessionId": sessionID})
	if got := Value(t, result).SessionID; got != acp.SessionId(sessionID) {
		t.Fatalf("acptest: new session ID = %q, want %q", got, sessionID)
	}
}

// Update sends a session/update notification carrying an agent text chunk.
func (p *Peer) Update(sessionID, text string) {
	p.t.Helper()
	p.Notify(acp.MethodSessionUpdate, map[string]any{
		"sessionId": sessionID,
		"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}},
	})
}

// RawID returns the wire form of id, for comparing with Message.ID.
func RawID(id any) json.RawMessage {
	data, err := json.Marshal(id)
	if err != nil {
		panic(err)
	}
	return data
}

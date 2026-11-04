// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Semantic ports of agentclientprotocol/rust-sdk tests, Apache-2.0.
package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust: src/agent-client-protocol/tests/session_ordering.rs::active_session_preserves_config_options_from_new_session_response.
// Ports exact response/configuration preservation; Go has no ActiveSession object.
func TestNewSessionPreservesConfiguration(t *testing.T) {
	ctx, client, peer := clientPeer(t, nil)
	result := startCall(func() (*acp.NewSessionResponse, error) {
		return client.SessionNew(ctx, &acp.NewSessionRequest{Cwd: "/workspace", McpServers: []acp.McpServer{}})
	})
	request := peer.read(t)
	if request.Method != "session/new" {
		t.Fatalf("method = %q", request.Method)
	}
	const response = `{"sessionId":"config-options-session","configOptions":[{"type":"select","id":"model","name":"Model","category":"model","currentValue":"sonnet","options":[{"value":"sonnet","name":"Sonnet"},{"value":"opus","name":"Opus"}]}]}`
	peer.result(t, request, json.RawMessage(response))
	assertJSON(t, valueOf(t, result), response)
}

// Rust: src/agent-client-protocol/tests/session_ordering.rs::start_session_returns_session_new_error_without_closing_connection.
func TestNewSessionErrorKeepsConnection(t *testing.T) {
	ctx, client, peer := clientPeer(t, nil)
	for _, cwd := range []string{"/rejected", "/accepted"} {
		result := startCall(func() (*acp.NewSessionResponse, error) {
			return client.SessionNew(ctx, &acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
		})
		request := peer.read(t)
		assertJSON(t, request.Params, `{"cwd":"`+cwd+`","mcpServers":[]}`)
		if cwd == "/rejected" {
			peer.failure(t, request, jsonrpc.CodeInvalidParams, "Invalid params", "session rejected")
			got := await(t, result)
			var remote *jsonrpc.Error
			if !errors.As(got.err, &remote) || remote.Code != jsonrpc.CodeInvalidParams || remote.Message != "Invalid params" {
				t.Fatalf("error = %#v / %v, want original InvalidParams and data", remote, got.err)
			}
			assertJSON(t, remote.Data, `"session rejected"`)
		} else {
			peer.result(t, request, map[string]any{"sessionId": "accepted-session"})
			if got := valueOf(t, result).SessionID; got != "accepted-session" {
				t.Fatalf("session ID = %q", got)
			}
		}
	}
}

// Rust: src/agent-client-protocol/tests/session_ordering.rs::prompt_error_reaches_the_session_stream_and_keeps_the_connection.
// Ports error delivery, other-session/same-session reuse and complete response
// preservation. The Rust ActiveSession stream itself has no Go counterpart.
func TestPromptErrorKeepsConnectionAndSessions(t *testing.T) {
	handler := newUpdateHandler()
	ctx, client, peer := clientPeer(t, handler)
	for _, id := range []string{"prompt-error-session", "other-session"} {
		result := startCall(func() (*acp.NewSessionResponse, error) {
			return client.SessionNew(ctx, &acp.NewSessionRequest{Cwd: "/workspace", McpServers: []acp.McpServer{}})
		})
		peer.result(t, peer.read(t), map[string]any{"sessionId": id})
		if valueOf(t, result).SessionID != acp.SessionId(id) {
			t.Fatal("wrong session returned")
		}
	}
	for _, scenario := range []struct{ session, text string }{
		{"prompt-error-session", "fail"}, {"other-session", "hello"}, {"prompt-error-session", "hello"},
	} {
		result := startCall(func() (*acp.PromptResponse, error) {
			return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: acp.SessionId(scenario.session), Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: scenario.text}}})
		})
		request := peer.read(t)
		if request.Method != "session/prompt" {
			t.Fatalf("method = %q", request.Method)
		}
		expectedParams, _ := json.Marshal(map[string]any{"sessionId": scenario.session, "prompt": []any{map[string]any{"type": "text", "text": scenario.text}}})
		assertJSON(t, request.Params, string(expectedParams))
		if scenario.text == "fail" {
			peer.failure(t, request, jsonrpc.CodeInvalidParams, "Invalid params", "rejected prompt")
			got := await(t, result)
			var remote *jsonrpc.Error
			if !errors.As(got.err, &remote) || remote.Code != jsonrpc.CodeInvalidParams {
				t.Fatalf("prompt error = %v", got.err)
			}
			continue
		}
		peer.update(t, scenario.session, "ok")
		peer.result(t, request, json.RawMessage(`{"stopReason":"end_turn","_meta":{"turn":{"kept":true}}}`))
		update := await(t, handler.updates)
		if update.SessionID != acp.SessionId(scenario.session) || updateText(t, update) != "ok" {
			t.Fatalf("update = %#v", update)
		}
		assertJSON(t, valueOf(t, result), `{"stopReason":"end_turn","_meta":{"turn":{"kept":true}}}`)
	}
}

const restoreResponse = `{"modes":{"currentModeId":"default","availableModes":[{"id":"default","name":"Default"}]},"configOptions":[{"type":"select","id":"thinking","name":"Thinking","category":"thought_level","currentValue":"standard","options":[{"value":"standard","name":"Standard"},{"value":"extended","name":"Extended"}]}],"_meta":{"trace":"TRACE"}}`

// Rust: src/agent-client-protocol/tests/session_restore.rs::load_session_preserves_pre_response_replay_and_exact_response.
// The callback receives the two replay notifications in wire order; no test-only
// ActiveSession implementation is introduced to stand in for Rust's stream.
func TestLoadPreservesReplayAndResponse(t *testing.T) {
	handler := newUpdateHandler()
	ctx, client, peer := clientPeer(t, handler)
	result := startCall(func() (*acp.LoadSessionResponse, error) {
		return client.SessionLoad(ctx, &acp.LoadSessionRequest{SessionID: "restore-load", Cwd: "/restore/load", McpServers: []acp.McpServer{}})
	})
	request := peer.read(t)
	if request.Method != "session/load" {
		t.Fatalf("method = %q", request.Method)
	}
	assertJSON(t, request.Params, `{"sessionId":"restore-load","cwd":"/restore/load","mcpServers":[]}`)
	peer.update(t, "restore-load", "first replay")
	peer.update(t, "restore-load", "second replay")
	expected := strings.ReplaceAll(restoreResponse, "TRACE", "load")
	peer.result(t, request, json.RawMessage(expected))
	assertJSON(t, valueOf(t, result), expected)
	for _, text := range []string{"first replay", "second replay"} {
		update := await(t, handler.updates)
		if update.SessionID != "restore-load" || updateText(t, update) != text {
			t.Fatalf("replay = %#v, want %q", update, text)
		}
	}
	// Rust concepts/sessions.rs doctest64 continues a loaded session with a prompt.
	continued := startCall(func() (*acp.PromptResponse, error) {
		return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: "restore-load", Prompt: []acp.ContentBlock{{Type: acp.ContentBlockTypeText, Text: "Continue where we left off"}}})
	})
	continuation := peer.read(t)
	if continuation.Method != "session/prompt" {
		t.Fatalf("continuation method = %q", continuation.Method)
	}
	assertJSON(t, continuation.Params, `{"sessionId":"restore-load","prompt":[{"type":"text","text":"Continue where we left off"}]}`)
	peer.result(t, continuation, map[string]any{"stopReason": "end_turn"})
	valueOf(t, continued)
}

// Rust: src/agent-client-protocol/tests/session_restore.rs::resume_session_returns_exact_response_and_an_active_session.
// Ports response and post-response notification preservation; ActiveSession is absent.
func TestResumePreservesResponseAndLaterUpdate(t *testing.T) {
	handler := newUpdateHandler()
	ctx, client, peer := clientPeer(t, handler)
	result := startCall(func() (*acp.ResumeSessionResponse, error) {
		return client.SessionResume(ctx, &acp.ResumeSessionRequest{SessionID: "restore-resume", Cwd: "/restore/resume"})
	})
	request := peer.read(t)
	if request.Method != "session/resume" {
		t.Fatalf("method = %q", request.Method)
	}
	assertJSON(t, request.Params, `{"sessionId":"restore-resume","cwd":"/restore/resume"}`)
	expected := strings.ReplaceAll(restoreResponse, "TRACE", "resume")
	peer.result(t, request, json.RawMessage(expected))
	peer.update(t, "restore-resume", "resume update")
	assertJSON(t, valueOf(t, result), expected)
	update := await(t, handler.updates)
	if update.SessionID != "restore-resume" || updateText(t, update) != "resume update" {
		t.Fatalf("resume update = %#v", update)
	}
}

// Rust: src/agent-client-protocol/tests/session_restore.rs::resume_session_from_preserves_the_existing_request.
func TestResumePreservesExistingRequest(t *testing.T) {
	ctx, client, peer := clientPeer(t, nil)
	directories := []string{"/restore/extra"}
	result := startCall(func() (*acp.ResumeSessionResponse, error) {
		return client.SessionResume(ctx, &acp.ResumeSessionRequest{SessionID: "restore-from", Cwd: "/restore/from", AdditionalDirectories: directories, Meta: acp.Meta{"trace": "request"}})
	})
	request := peer.read(t)
	assertJSON(t, request.Params, `{"sessionId":"restore-from","cwd":"/restore/from","additionalDirectories":["/restore/extra"],"_meta":{"trace":"request"}}`)
	peer.result(t, request, map[string]any{})
	valueOf(t, result)
}

type initializeAgent struct{}

func (initializeAgent) Initialize(_ context.Context, request *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: request.ProtocolVersion}, nil
}

func (initializeAgent) SessionNew(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: "v1-session"}, nil
}

type sessionOnlyAgent struct{}

func (sessionOnlyAgent) SessionNew(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: "v1-session"}, nil
}

// Rust SDK policy, not a claim that ACP mandates this particular Go state machine.
// Rust: src/agent-client-protocol/tests/protocol_negotiation.rs::v1_agent_with_initialize_handler_rejects_requests_before_initialization.
func TestInitializationGateWithHandler(t *testing.T) {
	peer := agentPeer(t, initializeAgent{})
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "session/new", "params": map[string]any{"cwd": "/tmp", "mcpServers": []any{}}})
	response := peer.read(t)
	if response.Error == nil || response.Error.Code != jsonrpc.CodeInvalidRequest {
		t.Errorf("before initialize: response = %+v, want InvalidRequest (Rust initialization-gate policy)", response)
	}
	for _, id := range []int{2, 4} {
		peer.send(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": "initialize", "params": map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}})
		assertJSON(t, peer.read(t).Result, `{"protocolVersion":1}`)
		if id == 2 {
			peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "session/new", "params": map[string]any{"cwd": "/tmp", "mcpServers": []any{}}})
			assertJSON(t, peer.read(t).Result, `{"sessionId":"v1-session"}`)
		}
	}
}

// Rust: src/agent-client-protocol/tests/protocol_negotiation.rs::v1_agent_without_initialize_handler_serves_requests_without_initialization.
func TestWithoutInitializeHandlerCanServeRequests(t *testing.T) {
	peer := agentPeer(t, sessionOnlyAgent{})
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "session/new", "params": map[string]any{"cwd": "/tmp", "mcpServers": []any{}}})
	assertJSON(t, peer.read(t).Result, `{"sessionId":"v1-session"}`)
}

// Rust: src/agent-client-protocol/tests/protocol_negotiation.rs::standalone_v1_rejects_malformed_initialization_before_dispatch.
// All five upstream parameter cases are retained, including absent protocolVersion.
func TestMalformedInitialization(t *testing.T) {
	for _, params := range []string{`{}`, `{"protocolVersion":"1"}`, `{"protocolVersion":-1}`, `{"protocolVersion":1.5}`, `{"protocolVersion":65536}`} {
		t.Run(params, func(t *testing.T) {
			peer := agentPeer(t, initializeAgent{})
			peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": json.RawMessage(params)})
			response := peer.read(t)
			if response.Error == nil || response.Error.Code != jsonrpc.CodeInvalidParams {
				t.Fatalf("response = %+v, want InvalidParams before dispatch", response)
			}
		})
	}
}

// Rust SDK error-detail policy: tests/v1_feature_equivalence.rs::v1_client_answers_unhandled_session_request.
func TestUnhandledSessionRequestIsAnswered(t *testing.T) {
	_, _, peer := clientPeer(t, nil)
	peer.send(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "session/request_permission", "params": map[string]any{"sessionId": "untracked", "toolCall": map[string]any{"toolCallId": "call-1"}, "options": []any{}}})
	response := peer.read(t)
	if string(response.ID) != "1" || response.Error == nil || response.Error.Code != jsonrpc.CodeMethodNotFound {
		t.Fatalf("unhandled permission response = %+v", response)
	}
	assertJSON(t, response.Error.Data, `"session/request_permission"`)
}

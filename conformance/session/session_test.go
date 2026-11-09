// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

// Package session_test checks SDK client and agent behavior across a session's
// lifetime, driven by an independent newline-delimited JSON peer: the wire
// form of each client method and the values it returns, client callbacks and
// their effects, session update delivery and ordering relative to responses
// and later requests, extension methods, _meta and raw error fidelity,
// initialization handling, and shutdown of agents started as child processes.
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

// TestClientMethods drives each stable client-to-agent method on one
// connection. Each request must reach the wire with exactly the given params,
// a peer error must reach the caller, and results must decode into the typed
// response. Notifications must not wait for a reply.
func TestClientMethods(t *testing.T) {
	ctx := context.Background()
	client, peer := acptest.StartClient(t, nil)
	cwd := "/tmp"
	const session = acp.SessionId("session-1")
	const options = `[{"type":"select","id":"verbosity","name":"Verbosity","currentValue":"verbose","options":[{"value":"brief","name":"Brief"},{"value":"normal","name":"Normal"},{"value":"verbose","name":"Verbose"}]}]`
	promptCall := func(text string) func() (any, error) {
		return func() (any, error) {
			return client.SessionPrompt(ctx, &acp.PromptRequest{SessionID: session, Prompt: []acp.ContentBlock{acp.TextContentBlock(text)}})
		}
	}
	stopReason := func(want acp.StopReason) func(*testing.T, any) {
		return func(t *testing.T, v any) {
			if got := v.(*acp.PromptResponse).StopReason; got != want {
				t.Errorf("stop reason = %q, want %q", got, want)
			}
		}
	}
	steps := []struct {
		method, params string
		// response is the peer's result; empty means the peer replies with an
		// error, unless notification is set.
		response     string
		notification bool
		call         func() (any, error)
		check        func(*testing.T, any)
	}{
		{
			method:   "initialize",
			params:   `{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":true,"writeTextFile":true},"terminal":true}}`,
			response: `{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"promptCapabilities":{"image":true,"audio":true,"embeddedContext":true}},"authMethods":[{"id":"agent-auth","name":"Agent auth"}]}`,
			call: func() (any, error) {
				return client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1, ClientCapabilities: &acp.ClientCapabilities{Fs: &acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true}, Terminal: true}})
			},
			check: func(t *testing.T, v any) {
				r := v.(*acp.InitializeResponse)
				c := r.AgentCapabilities
				if c == nil || !c.LoadSession || c.PromptCapabilities == nil || !c.PromptCapabilities.Image || !c.PromptCapabilities.Audio || !c.PromptCapabilities.EmbeddedContext || len(r.AuthMethods) != 1 {
					t.Errorf("initialize capabilities or auth methods lost: %#v", r)
				}
			},
		},
		{method: "authenticate", params: `{"methodId":"unknown-auth-method"}`, call: func() (any, error) {
			return client.Authenticate(ctx, &acp.AuthenticateRequest{MethodID: "unknown-auth-method"})
		}},
		{method: "authenticate", params: `{"methodId":"agent-auth"}`, response: `{}`, call: func() (any, error) {
			return client.Authenticate(ctx, &acp.AuthenticateRequest{MethodID: "agent-auth"})
		}},
		{method: "logout", params: `{}`, response: `{}`, call: func() (any, error) {
			return client.Logout(ctx, &acp.LogoutRequest{})
		}},
		{
			method:   "session/new",
			params:   `{"cwd":"/tmp","additionalDirectories":["/var/tmp"],"mcpServers":[]}`,
			response: `{"sessionId":"session-1","modes":{"currentModeId":"chat","availableModes":[{"id":"chat","name":"Chat"},{"id":"plan","name":"Plan"}]},"configOptions":` + options + `}`,
			call: func() (any, error) {
				return client.SessionNew(ctx, &acp.NewSessionRequest{Cwd: cwd, AdditionalDirectories: []string{"/var/tmp"}, McpServers: []acp.McpServer{}})
			},
			check: func(t *testing.T, v any) {
				if r := v.(*acp.NewSessionResponse); r.SessionID != session || r.Modes == nil || r.ConfigOptions == nil {
					t.Errorf("new session result lost: %#v", r)
				}
			},
		},
		{method: "session/set_mode", params: `{"sessionId":"missing-session","modeId":"chat"}`, call: func() (any, error) {
			return client.SessionSetMode(ctx, &acp.SetSessionModeRequest{SessionID: "missing-session", ModeID: "chat"})
		}},
		{method: "session/set_mode", params: `{"sessionId":"session-1","modeId":"invalid-mode"}`, call: func() (any, error) {
			return client.SessionSetMode(ctx, &acp.SetSessionModeRequest{SessionID: session, ModeID: "invalid-mode"})
		}},
		{
			method:   "session/list",
			params:   `{"cwd":"/tmp"}`,
			response: `{"sessions":[{"sessionId":"session-1","cwd":"/tmp","additionalDirectories":["/var/tmp"]}]}`,
			call:     func() (any, error) { return client.SessionList(ctx, &acp.ListSessionsRequest{Cwd: &cwd}) },
			check: func(t *testing.T, v any) {
				r := v.(*acp.ListSessionsResponse)
				if len(r.Sessions) != 1 || r.Sessions[0].SessionID != session || len(r.Sessions[0].AdditionalDirectories) != 1 || r.Sessions[0].AdditionalDirectories[0] != "/var/tmp" {
					t.Errorf("listed session or its additional directories lost: %#v", r)
				}
			},
		},
		{method: "session/set_mode", params: `{"sessionId":"session-1","modeId":"plan"}`, response: `{}`, call: func() (any, error) {
			return client.SessionSetMode(ctx, &acp.SetSessionModeRequest{SessionID: session, ModeID: "plan"})
		}},
		{
			method:   "session/set_config_option",
			params:   `{"sessionId":"session-1","configId":"verbosity","value":"verbose"}`,
			response: `{"configOptions":` + options + `}`,
			call: func() (any, error) {
				return client.SessionSetConfigOption(ctx, &acp.SetSessionConfigOptionRequest{SessionID: session, ConfigID: "verbosity", Value: "verbose"})
			},
			check: func(t *testing.T, v any) {
				if got := len(v.(*acp.SetSessionConfigOptionResponse).ConfigOptions); got != 1 {
					t.Errorf("config options length = %d, want 1", got)
				}
			},
		},
		{method: "session/set_config_option", params: `{"sessionId":"missing-session","configId":"verbosity","value":"verbose"}`, call: func() (any, error) {
			return client.SessionSetConfigOption(ctx, &acp.SetSessionConfigOptionRequest{SessionID: "missing-session", ConfigID: "verbosity", Value: "verbose"})
		}},
		{method: "session/set_config_option", params: `{"sessionId":"session-1","configId":"unknown","value":"verbose"}`, call: func() (any, error) {
			return client.SessionSetConfigOption(ctx, &acp.SetSessionConfigOptionRequest{SessionID: session, ConfigID: "unknown", Value: "verbose"})
		}},
		{method: "session/set_config_option", params: `{"sessionId":"session-1","configId":"verbosity","value":"loud"}`, call: func() (any, error) {
			return client.SessionSetConfigOption(ctx, &acp.SetSessionConfigOptionRequest{SessionID: session, ConfigID: "verbosity", Value: "loud"})
		}},
		{method: "session/cancel", params: `{"sessionId":"session-1"}`, notification: true, call: func() (any, error) {
			return nil, client.SessionCancel(ctx, &acp.CancelNotification{SessionID: session})
		}},
		{method: "session/prompt", params: `{"sessionId":"session-1","prompt":[{"type":"text","text":"{\"command\":\"greet\"}"}]}`, response: `{"stopReason":"end_turn"}`, call: promptCall(`{"command":"greet"}`), check: stopReason(acp.StopReasonEndTurn)},
		{method: "session/prompt", params: `{"sessionId":"session-1","prompt":[{"type":"text","text":"cancel"}]}`, response: `{"stopReason":"cancelled"}`, call: promptCall("cancel"), check: stopReason(acp.StopReasonCancelled)},
	}
	for _, step := range steps {
		result := acptest.Go(step.call)
		request := peer.Expect(step.method)
		if request.IsNotification() != step.notification {
			t.Fatalf("%s: frame %s, want notification=%v", step.method, request, step.notification)
		}
		acptest.EqualJSON(t, request.Params, step.params)
		switch {
		case step.notification:
			acptest.Value(t, result)
		case step.response == "":
			peer.ReplyError(request.ID, jsonrpc.CodeInvalidParams, "Invalid params", "rejected by peer")
			var remote *jsonrpc.Error
			if err := acptest.Await(t, result).Err; !errors.As(err, &remote) || remote.Code != jsonrpc.CodeInvalidParams {
				t.Fatalf("%s %s: error = %v, want the peer's InvalidParams", step.method, step.params, err)
			}
		default:
			peer.Reply(request.ID, json.RawMessage(step.response))
			value := acptest.Value(t, result)
			if step.check != nil {
				step.check(t, value)
			}
		}
	}
}

// TestNewSessionPreservesResponse checks that every field of a session/new
// result, including config options, survives decoding.
func TestNewSessionPreservesResponse(t *testing.T) {
	client, peer := acptest.StartClient(t, nil)
	result := acptest.Go(func() (*acp.NewSessionResponse, error) {
		return client.SessionNew(context.Background(), &acp.NewSessionRequest{Cwd: "/workspace", McpServers: []acp.McpServer{}})
	})
	request := peer.Expect("session/new")
	const response = `{"sessionId":"config-options-session","configOptions":[{"type":"select","id":"model","name":"Model","category":"model","currentValue":"sonnet","options":[{"value":"sonnet","name":"Sonnet"},{"value":"opus","name":"Opus"}]}]}`
	peer.Reply(request.ID, json.RawMessage(response))
	acptest.EqualMarshaled(t, acptest.Value(t, result), response)
}

// TestNewSessionErrorKeepsConnection checks that a rejected session/new
// returns the peer's error unchanged and the next session/new on the same
// connection still succeeds.
func TestNewSessionErrorKeepsConnection(t *testing.T) {
	client, peer := acptest.StartClient(t, nil)
	newSession := func(cwd string) (<-chan acptest.Result[*acp.NewSessionResponse], acptest.Message) {
		t.Helper()
		result := acptest.Go(func() (*acp.NewSessionResponse, error) {
			return client.SessionNew(context.Background(), &acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
		})
		request := peer.Expect("session/new")
		acptest.EqualJSON(t, request.Params, `{"cwd":"`+cwd+`","mcpServers":[]}`)
		return result, request
	}

	result, request := newSession("/rejected")
	peer.ReplyError(request.ID, jsonrpc.CodeInvalidParams, "Invalid params", "session rejected")
	var remote *jsonrpc.Error
	if err := acptest.Await(t, result).Err; !errors.As(err, &remote) || remote.Code != jsonrpc.CodeInvalidParams || remote.Message != "Invalid params" {
		t.Fatalf("error = %v, want the peer's InvalidParams", err)
	}
	acptest.EqualJSON(t, remote.Data, `"session rejected"`)

	result, request = newSession("/accepted")
	peer.Reply(request.ID, map[string]any{"sessionId": "accepted-session"})
	if got := acptest.Value(t, result).SessionID; got != "accepted-session" {
		t.Fatalf("session ID = %q, want accepted-session", got)
	}
}

// TestPromptErrorKeepsConnection checks that a failed prompt leaves both the
// connection and its sessions usable: a prompt to another session and a
// second prompt to the failed session both succeed, each receiving its
// update and its complete response, including _meta.
func TestPromptErrorKeepsConnection(t *testing.T) {
	h := newUpdateRecorder()
	client, peer := acptest.StartClient(t, h)
	acptest.NewSession(t, client, peer, "failing")
	acptest.NewSession(t, client, peer, "other")
	const response = `{"stopReason":"end_turn","_meta":{"turn":{"kept":true}}}`
	for _, step := range []struct{ session, text string }{
		{"failing", "fail"}, {"other", "hello"}, {"failing", "hello"},
	} {
		result := prompt(client, acp.SessionId(step.session), step.text)
		request := peer.Expect("session/prompt")
		acptest.EqualJSON(t, request.Params, `{"sessionId":"`+step.session+`","prompt":[{"type":"text","text":"`+step.text+`"}]}`)
		if step.text == "fail" {
			peer.ReplyError(request.ID, jsonrpc.CodeInvalidParams, "Invalid params", "rejected prompt")
			var remote *jsonrpc.Error
			if err := acptest.Await(t, result).Err; !errors.As(err, &remote) || remote.Code != jsonrpc.CodeInvalidParams {
				t.Fatalf("prompt error = %v, want the peer's InvalidParams", err)
			}
			continue
		}
		peer.Update(step.session, "ok")
		peer.Reply(request.ID, json.RawMessage(response))
		if update := h.next(t); update.SessionID != acp.SessionId(step.session) || text(t, update) != "ok" {
			t.Fatalf("update = %#v, want %q for session %s", update, "ok", step.session)
		}
		acptest.EqualMarshaled(t, acptest.Value(t, result), response)
	}
}

// restoreResponse is a load or resume result with modes, config options and
// _meta, all of which must survive decoding.
const restoreResponse = `{"modes":{"currentModeId":"default","availableModes":[{"id":"default","name":"Default"}]},"configOptions":[{"type":"select","id":"thinking","name":"Thinking","category":"thought_level","currentValue":"standard","options":[{"value":"standard","name":"Standard"},{"value":"extended","name":"Extended"}]}],"_meta":{"trace":"restored"}}`

// TestLoadSession checks that session/load delivers the history replayed
// before its response in order, returns the response unchanged, and leaves
// the session usable for a follow-up prompt.
func TestLoadSession(t *testing.T) {
	h := newUpdateRecorder()
	client, peer := acptest.StartClient(t, h)
	result := acptest.Go(func() (*acp.LoadSessionResponse, error) {
		return client.SessionLoad(context.Background(), &acp.LoadSessionRequest{SessionID: "restored", Cwd: "/restore/load", McpServers: []acp.McpServer{}})
	})
	request := peer.Expect("session/load")
	acptest.EqualJSON(t, request.Params, `{"sessionId":"restored","cwd":"/restore/load","mcpServers":[]}`)
	peer.Update("restored", "first replay")
	peer.Update("restored", "second replay")
	peer.Reply(request.ID, json.RawMessage(restoreResponse))
	acptest.EqualMarshaled(t, acptest.Value(t, result), restoreResponse)
	for _, want := range []string{"first replay", "second replay"} {
		if update := h.next(t); update.SessionID != "restored" || text(t, update) != want {
			t.Fatalf("replay = %#v, want %q", update, want)
		}
	}

	continued := prompt(client, "restored", "Continue where we left off")
	request = peer.Expect("session/prompt")
	acptest.EqualJSON(t, request.Params, `{"sessionId":"restored","prompt":[{"type":"text","text":"Continue where we left off"}]}`)
	endTurn(peer, request)
	acptest.Value(t, continued)
}

// TestResumeSession checks session/resume twice on one connection. A request
// with only the required fields omits the optional ones on the wire and
// accepts an empty result. A request with every field sends them all, its
// full response is returned unchanged, and an update sent after the response
// is delivered.
func TestResumeSession(t *testing.T) {
	h := newUpdateRecorder()
	client, peer := acptest.StartClient(t, h)
	minimal := acptest.Go(func() (*acp.ResumeSessionResponse, error) {
		return client.SessionResume(context.Background(), &acp.ResumeSessionRequest{SessionID: "minimal", Cwd: "/restore/minimal"})
	})
	request := peer.ExpectCall("session/resume")
	acptest.EqualJSON(t, request.Params, `{"sessionId":"minimal","cwd":"/restore/minimal"}`)
	peer.Reply(request.ID, map[string]any{})
	acptest.Value(t, minimal)

	full := &acp.ResumeSessionRequest{SessionID: "resumed", Cwd: "/restore/resume", AdditionalDirectories: []string{"/restore/extra"}, Meta: acp.Meta{"trace": "request"}}
	result := acptest.Go(func() (*acp.ResumeSessionResponse, error) { return client.SessionResume(context.Background(), full) })
	request = peer.ExpectCall("session/resume")
	acptest.EqualJSON(t, request.Params, `{"sessionId":"resumed","cwd":"/restore/resume","additionalDirectories":["/restore/extra"],"_meta":{"trace":"request"}}`)
	peer.Reply(request.ID, json.RawMessage(restoreResponse))
	peer.Update("resumed", "resume update")
	acptest.EqualMarshaled(t, acptest.Value(t, result), restoreResponse)
	if update := h.next(t); update.SessionID != "resumed" || text(t, update) != "resume update" {
		t.Fatalf("update = %#v, want %q", update, "resume update")
	}
}

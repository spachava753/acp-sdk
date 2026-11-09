package wire_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// TestUnhandledNotificationsIgnored checks that notifications with no handler,
// including cancellation notices for unknown requests and vendor or unknown
// methods, get no reply, and the next request is answered normally.
func TestUnhandledNotificationsIgnored(t *testing.T) {
	a := newAgent()
	a.echo(t, "_test/simple")
	_, peer := startAgent(t, a)
	peer.Notify("$/cancel_request", map[string]any{"requestId": "req-1"})
	peer.Notify("$/cancel_request", map[string]any{"requestId": true})
	peer.Notify("_x.ai/settings/update", map[string]any{"theme": "auto"})
	peer.Notify("unknown/notification", map[string]any{"value": 1})
	r := peer.Call(2, "_test/simple", message{"after notifications"})
	acptest.EqualJSON(t, r.ID, `2`)
	acptest.EqualJSON(t, r.Result, `{"result":"echo: after notifications"}`)
}

// TestUnknownRequestErrorData checks that a request for an unknown method gets
// MethodNotFound with a non-empty message and the method name as its data.
func TestUnknownRequestErrorData(t *testing.T) {
	for name, method := range map[string]string{"protocol_style": "unknown/request", "extension": "_test/unknown"} {
		t.Run(name, func(t *testing.T) {
			_, peer := startAgent(t, newAgent())
			r := peer.Call(1, method, map[string]any{"value": 1})
			wantError(t, r, jsonrpc.CodeMethodNotFound)
			if r.Error.Message == "" {
				t.Error("error message is empty")
			}
			acptest.EqualMarshaled(t, method, string(r.Error.Data))
		})
	}
}

// TestEntityCancelMethodsUnknown checks that tool_call/cancel and
// terminal/cancel, which protocol version 1 does not define, get
// MethodNotFound, both from an initialized agent and from an agent with no
// handlers that was never initialized.
func TestEntityCancelMethodsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		start   func(*testing.T) *acptest.Peer
		firstID int
	}{
		{"initialized", func(t *testing.T) *acptest.Peer { _, p := startAgent(t, newAgent()); return p }, 2},
		{"no_handlers", func(t *testing.T) *acptest.Peer { _, p := acptest.StartAgent(t, struct{}{}); return p }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			peer := tc.start(t)
			for i, method := range []string{"tool_call/cancel", "terminal/cancel"} {
				r := peer.Call(tc.firstID+i, method, map[string]any{"sessionId": "s", "toolCallId": "c", "terminalId": "t"})
				wantError(t, r, jsonrpc.CodeMethodNotFound)
			}
		})
	}
}

// TestEmptyMethodUnknown checks that a request whose method is the empty
// string is answered with MethodNotFound under its own id, runs no handler,
// and does not disturb the next request.
func TestEmptyMethodUnknown(t *testing.T) {
	a := newAgent()
	ag, peer := startAgent(t, a)
	r := peer.Call("empty", "", map[string]any{})
	acptest.EqualJSON(t, r.ID, `"empty"`)
	wantError(t, r, jsonrpc.CodeMethodNotFound)

	followup := peer.Call("followup", acp.MethodSessionNew, raw(newParams))
	acptest.EqualJSON(t, followup.ID, `"followup"`)
	acptest.EqualJSON(t, followup.Result, `{"sessionId":"session"}`)
	finishClean(t, ag, peer)
	if got := a.newCalls.Load(); got != 1 {
		t.Errorf("session/new calls = %d, want 1", got)
	}
}

// wantWrongKindRejected checks the reply to a notification method sent as a
// request with id "wrong". Either InvalidRequest or MethodNotFound is a
// reasonable answer.
func wantWrongKindRejected(t *testing.T, r acptest.Message) {
	t.Helper()
	acptest.EqualJSON(t, r.ID, `"wrong"`)
	if r.Error == nil || (r.Error.Code != jsonrpc.CodeInvalidRequest && r.Error.Code != jsonrpc.CodeMethodNotFound) {
		t.Errorf("reply %s, want InvalidRequest or MethodNotFound", r)
	}
	if r.Has("result") {
		t.Errorf("rejection also has a result: %s", r)
	}
}

// fileClient answers file reads with constant content and counts session
// updates.
type fileClient struct {
	reads   atomic.Int32
	updates chan struct{}
}

func (h *fileClient) FsReadTextFile(context.Context, *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
	h.reads.Add(1)
	return &acp.ReadTextFileResponse{Content: "constant file"}, nil
}

func (h *fileClient) SessionUpdate(context.Context, *acp.SessionNotification) error {
	h.updates <- struct{}{}
	return nil
}

// startPrompt creates session "session" and starts a prompt turn, so the
// agent's callbacks arrive while a turn is active. The returned function ends
// the turn.
func startPrompt(t *testing.T, client *acp.Client, peer *acptest.Peer) func() {
	t.Helper()
	acptest.NewSession(t, client, peer, "session")
	result := acptest.Go(func() (*acp.PromptResponse, error) {
		return client.SessionPrompt(context.Background(), &acp.PromptRequest{SessionID: "session", Prompt: []acp.ContentBlock{acp.TextContentBlock("test callbacks")}})
	})
	prompt := peer.Expect(acp.MethodSessionPrompt)
	return func() {
		peer.Reply(prompt.ID, map[string]any{"stopReason": "end_turn"})
		acptest.Value(t, result)
	}
}

// TestMethodKindMismatch checks that a known ACP method sent with the wrong
// kind never reaches its handler: a request method sent as a notification is
// ignored without a reply, and a notification method sent as a request is
// rejected. Each case first sends the method in its correct form, so the
// handler is known to work, and checks the full output so no reply goes
// unnoticed.
func TestMethodKindMismatch(t *testing.T) {
	t.Run("agent_request_as_notification", func(t *testing.T) {
		a := newAgent()
		ag, peer := startAgent(t, a)
		acptest.EqualJSON(t, peer.Call("control", acp.MethodSessionNew, raw(newParams)).Result, `{"sessionId":"session"}`)
		peer.Notify(acp.MethodSessionNew, raw(newParams))
		barrier := peer.Call("barrier", acp.MethodSessionNew, raw(newParams))
		acptest.EqualJSON(t, barrier.ID, `"barrier"`)
		acptest.EqualJSON(t, barrier.Result, `{"sessionId":"session"}`)
		if rest, _ := finish(t, ag, peer); len(rest) != 0 {
			t.Errorf("notification was answered: %v", rest)
		}
		if got := a.newCalls.Load(); got != 2 {
			t.Errorf("session/new calls = %d, want 2", got)
		}
	})
	t.Run("agent_notification_as_request", func(t *testing.T) {
		a := newAgent()
		ag, peer := startAgent(t, a)
		peer.Call("new", acp.MethodSessionNew, raw(newParams))
		peer.Notify(acp.MethodSessionCancel, raw(cancelParams))
		acptest.Await(t, a.cancels)
		wantWrongKindRejected(t, peer.Call("wrong", acp.MethodSessionCancel, raw(cancelParams)))
		if rest, _ := finish(t, ag, peer); len(rest) != 0 {
			t.Errorf("unexpected frames: %v", rest)
		}
		if len(a.cancels) != 0 {
			t.Error("session/cancel sent as a request reached its handler")
		}
	})
	t.Run("client_request_as_notification", func(t *testing.T) {
		h := &fileClient{updates: make(chan struct{}, 2)}
		client, peer := startClient(t, h)
		endTurn := startPrompt(t, client, peer)
		acptest.EqualJSON(t, peer.Call("control", acp.MethodFsReadTextFile, raw(readParams)).Result, `{"content":"constant file"}`)
		peer.Notify(acp.MethodFsReadTextFile, raw(readParams))
		barrier := peer.Call("barrier", acp.MethodFsReadTextFile, raw(readParams))
		acptest.EqualJSON(t, barrier.ID, `"barrier"`)
		acptest.EqualJSON(t, barrier.Result, `{"content":"constant file"}`)
		endTurn()
		if rest := closeClient(t, client, peer); len(rest) != 0 {
			t.Errorf("notification was answered: %v", rest)
		}
		if got := h.reads.Load(); got != 2 {
			t.Errorf("file reads = %d, want 2", got)
		}
	})
	t.Run("client_notification_as_request", func(t *testing.T) {
		h := &fileClient{updates: make(chan struct{}, 2)}
		client, peer := startClient(t, h)
		endTurn := startPrompt(t, client, peer)
		peer.Notify(acp.MethodSessionUpdate, raw(updateParams))
		acptest.Await(t, h.updates)
		wantWrongKindRejected(t, peer.Call("wrong", acp.MethodSessionUpdate, raw(updateParams)))
		endTurn()
		if rest := closeClient(t, client, peer); len(rest) != 0 {
			t.Errorf("unexpected frames: %v", rest)
		}
		if len(h.updates) != 0 {
			t.Error("session/update sent as a request reached its handler")
		}
	})
}

// TestInvalidParamsThenRecovery checks that session/new without its required
// cwd gets InvalidParams under the request's id, with diagnostic data naming
// the decoding phase and echoing the params, and that a valid request on the
// same connection then succeeds.
func TestInvalidParamsThenRecovery(t *testing.T) {
	for name, params := range map[string]string{"wrong_field": `{"wrong_field":"hello"}`, "content": `{"content":"hello"}`} {
		t.Run(name, func(t *testing.T) {
			_, peer := startAgent(t, newAgent())
			r := peer.Call(3, acp.MethodSessionNew, raw(params))
			wantError(t, r, jsonrpc.CodeInvalidParams)
			acptest.EqualJSON(t, r.ID, `3`)
			var data struct {
				Error string          `json:"error"`
				JSON  json.RawMessage `json:"json"`
				Phase string          `json:"phase"`
			}
			check(t, json.Unmarshal(r.Error.Data, &data))
			if data.Phase != "deserialization" || data.Error == "" {
				t.Errorf("error data %s lacks the decoding diagnostic", r.Error.Data)
			}
			acptest.EqualJSON(t, data.JSON, params)

			good := peer.Call(4, acp.MethodSessionNew, raw(`{"cwd":"/tmp","mcpServers":[]}`))
			acptest.EqualJSON(t, good.ID, `4`)
			acptest.EqualJSON(t, good.Result, `{"sessionId":"session"}`)
		})
	}
}

// TestNotificationErrorsIgnored checks that a notification that fails, either
// because its params lack a required field or because its handler returns an
// error, gets no reply and leaves the connection serving. A notification with
// invalid params must not reach its handler; a valid one whose handler fails
// must still have been delivered.
func TestNotificationErrorsIgnored(t *testing.T) {
	a := newAgent()
	delivered := make(chan string, 1)
	check(t, acp.AddExtensionNotification(a.ExtensionMux, "_test/notification", func(_ context.Context, m *message) error {
		delivered <- m.Message
		return &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "handler error"}
	}))
	a.echo(t, "_test/simple")
	ag, peer := startAgent(t, a)
	peer.Notify(acp.MethodSessionCancel, map[string]any{"wrong_field": "hello"})
	peer.Notify("_test/notification", message{"handler error"})
	r := peer.Call(10, "_test/simple", message{"after bad notification"})
	acptest.EqualJSON(t, r.ID, `10`)
	acptest.EqualJSON(t, r.Result, `{"result":"echo: after bad notification"}`)

	finishClean(t, ag, peer)
	if len(a.cancels) != 0 {
		t.Errorf("session/cancel without sessionId reached its handler with %q", <-a.cancels)
	}
	select {
	case m := <-delivered:
		if m != "handler error" {
			t.Errorf("handler received %q", m)
		}
	default:
		t.Error("the failing handler's notification was not delivered")
	}
}

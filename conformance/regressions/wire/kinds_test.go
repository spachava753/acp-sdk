package wire_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

type agent struct {
	*acp.ExtensionMux
	newCalls    atomic.Int32
	cancelCalls atomic.Int32
	cancelSeen  chan struct{}
	newSession  func(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error)
}

func newAgent() *agent {
	return &agent{ExtensionMux: acp.NewExtensionMux(), cancelSeen: make(chan struct{}, 2)}
}
func (*agent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: 1, AgentCapabilities: &acp.AgentCapabilities{}}, nil
}
func (a *agent) SessionNew(ctx context.Context, p *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	a.newCalls.Add(1)
	if a.newSession != nil {
		return a.newSession(ctx, p)
	}
	return &acp.NewSessionResponse{SessionID: "session"}, nil
}
func (a *agent) SessionCancel(context.Context, *acp.CancelNotification) error {
	a.cancelCalls.Add(1)
	a.cancelSeen <- struct{}{}
	return nil
}

type clientHandler struct {
	reads      atomic.Int32
	updates    atomic.Int32
	updateSeen chan struct{}
}

func (h *clientHandler) FsReadTextFile(context.Context, *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
	h.reads.Add(1)
	return &acp.ReadTextFileResponse{Content: "constant file"}, nil
}
func (h *clientHandler) SessionUpdate(context.Context, *acp.SessionNotification) error {
	h.updates.Add(1)
	h.updateSeen <- struct{}{}
	return nil
}

func startPrompt(t *testing.T, c *acp.Client, p *peer) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		_, err := c.SessionPrompt(ctx, &acp.PromptRequest{SessionID: "session", Prompt: []acp.ContentBlock{acp.TextContentBlock("test callbacks")}})
		done <- err
	}()
	r := p.receive(t)
	wantJSON(t, r["method"], `"session/prompt"`)
	return func() { p.respond(t, r, `{"stopReason":"end_turn"}`); must(t, await(t, done)) }
}

const newParams = `{"cwd":"/workspace","mcpServers":[]}`
const cancelParams = `{"sessionId":"session"}`
const readParams = `{"sessionId":"session","path":"/workspace/example.txt"}`
const updateParams = `{"sessionId":"session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"text"}}}`

// LOCAL additions: ordinary implemented ACP methods, unlike the first port's
// MCP-specific kind cases. Each correct-form control succeeds before inversion.
// Invalid ACP method kinds must not invoke the other kind's handler or panic.
func TestKnownMethodKinds(t *testing.T) {
	t.Run("agent/request_as_notification", func(t *testing.T) {
		if !isolated(t) {
			return
		}
		a := newAgent()
		p := agentPeer(t, a)
		r := p.call(t, `"control"`, acp.MethodSessionNew, newParams)
		wantJSON(t, r["result"], `{"sessionId":"session"}`)
		before := len(p.frames)
		p.send(t, noteFrame(acp.MethodSessionNew, newParams))
		barrier := p.call(t, `"barrier"`, acp.MethodSessionNew, newParams)
		wantJSON(t, barrier["id"], `"barrier"`)
		wantJSON(t, barrier["result"], `{"sessionId":"session"}`)
		if err := p.finish(t); err != nil {
			t.Logf("endpoint close policy: %v", err)
		}
		if got := a.newCalls.Load(); got != 2 {
			t.Errorf("request handler calls=%d, want only control and barrier", got)
		}
		if len(p.frames) != before+1 {
			t.Errorf("notification received an extra response: %v", p.frames[before:])
		}
	})
	t.Run("agent/notification_as_request", func(t *testing.T) {
		if !isolated(t) {
			return
		}
		a := newAgent()
		p := agentPeer(t, a)
		p.call(t, `"new"`, acp.MethodSessionNew, newParams)
		// Idle session/cancel is a valid notification control; no real work is
		// needed to establish the implemented notification handler's return path.
		p.send(t, noteFrame(acp.MethodSessionCancel, cancelParams))
		await(t, a.cancelSeen)
		rejectKind(t, p.call(t, `"wrong"`, acp.MethodSessionCancel, cancelParams))
		if err := p.finish(t); err != nil {
			t.Logf("endpoint close policy: %v", err)
		}
		if got := a.cancelCalls.Load(); got != 1 {
			t.Errorf("notification handler calls=%d, want only the correct-form control", got)
		}
		if len(p.frames) != 3 {
			t.Errorf("unexpected responses, including possible notification reply: %v", p.frames)
		}
	})
	t.Run("client/request_as_notification", func(t *testing.T) {
		if !isolated(t) {
			return
		}
		h := &clientHandler{updateSeen: make(chan struct{}, 2)}
		c, p := clientPeer(t, h)
		finishPrompt := startPrompt(t, c, p)
		r := p.call(t, `"control"`, acp.MethodFsReadTextFile, readParams)
		wantJSON(t, r["result"], `{"content":"constant file"}`)
		before := len(p.frames)
		p.send(t, noteFrame(acp.MethodFsReadTextFile, readParams))
		barrier := p.call(t, `"barrier"`, acp.MethodFsReadTextFile, readParams)
		wantJSON(t, barrier["id"], `"barrier"`)
		wantJSON(t, barrier["result"], `{"content":"constant file"}`)
		finishPrompt()
		if err := p.finish(t); err != nil {
			t.Logf("endpoint close policy: %v", err)
		}
		if got := h.reads.Load(); got != 2 {
			t.Errorf("file handler calls=%d, want only control and barrier", got)
		}
		if len(p.frames) != before+1 {
			t.Errorf("notification received an extra response: %v", p.frames[before:])
		}
	})
	t.Run("client/notification_as_request", func(t *testing.T) {
		if !isolated(t) {
			return
		}
		h := &clientHandler{updateSeen: make(chan struct{}, 2)}
		c, p := clientPeer(t, h)
		finishPrompt := startPrompt(t, c, p)
		p.send(t, noteFrame(acp.MethodSessionUpdate, updateParams))
		await(t, h.updateSeen)
		rejectKind(t, p.call(t, `"wrong"`, acp.MethodSessionUpdate, updateParams))
		finishPrompt()
		if err := p.finish(t); err != nil {
			t.Logf("endpoint close policy: %v", err)
		}
		if got := h.updates.Load(); got != 1 {
			t.Errorf("update handler calls=%d, want only the correct-form control", got)
		}
		if len(p.frames) != 4 {
			t.Errorf("unexpected responses, including possible notification reply: %v", p.frames)
		}
	})
}

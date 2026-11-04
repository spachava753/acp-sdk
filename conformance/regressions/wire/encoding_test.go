package wire_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// LOCAL Go reliability addition, conditional on a caller programming error:
// a channel in Meta cannot arrive from valid decoded JSON. The generated
// handler's encoding failure should become InternalError, not a worker panic.
// The extension branch is a positive control of existing encoder containment.
func TestUnencodableHandlerResultIsContained(t *testing.T) {
	for _, mode := range []string{"generated_handler", "extension_control"} {
		t.Run(mode, func(t *testing.T) {
			if !isolated(t) {
				return
			}
			a := newAgent()
			a.newSession = func(_ context.Context, p *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
				if p.Cwd == "/bad" {
					return &acp.NewSessionResponse{SessionID: "bad", Meta: acp.Meta{"invalid": make(chan int)}}, nil
				}
				return &acp.NewSessionResponse{SessionID: "healthy"}, nil
			}
			method := acp.MethodSessionNew
			if mode == "extension_control" {
				method = "_local/new"
				must(t, acp.AddExtensionRequest(a.ExtensionMux, method, a.SessionNew))
			}
			p := agentPeer(t, a)
			bad := p.call(t, `"bad"`, method, `{"cwd":"/bad","mcpServers":[]}`)
			wantJSON(t, bad["id"], `"bad"`)
			var err jsonrpc.Error
			must(t, json.Unmarshal(bad["error"], &err))
			if err.Code != jsonrpc.CodeInternalError {
				t.Errorf("encoding failure code=%d, want InternalError", err.Code)
			}
			if _, ok := bad["result"]; ok {
				t.Error("failed encoding also produced a result")
			}
			good := p.call(t, `"healthy"`, method, `{"cwd":"/healthy","mcpServers":[]}`)
			wantJSON(t, good["id"], `"healthy"`)
			wantJSON(t, good["result"], `{"sessionId":"healthy"}`)
			must(t, p.finish(t))
			if got := a.newCalls.Load(); got != 2 {
				t.Errorf("handler calls=%d, want failed result and healthy followup", got)
			}
		})
	}
}

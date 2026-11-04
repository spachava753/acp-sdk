package wire_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// LOCAL addition: malformed full NDJSON frames must not execute their valid
// JSON prefix. Rust-port syntax probes did not include these prefix/suffix pairs.
// This does NOT require a malformed stream to stay open or accept a later RPC.
func TestWholeFrameRejectsSuffix(t *testing.T) {
	prefix := callFrame(`"prefix"`, acp.MethodSessionNew, `{"cwd":"/prefix","mcpServers":[]}`)
	for _, tc := range []struct{ name, suffix string }{
		{"garbage", ` garbage`},
		{"second_object", " " + noteFrame(acp.MethodSessionCancel, cancelParams)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("message_helper", func(t *testing.T) {
				if _, err := jsonrpc.DecodeMessage([]byte(prefix + tc.suffix)); err == nil {
					t.Error("decoder accepted only the valid prefix of an invalid complete frame")
				}
			})
			t.Run("runtime", func(t *testing.T) {
				if !isolated(t) {
					return
				}
				a := newAgent()
				var prefixes atomic.Int32
				a.newSession = func(_ context.Context, p *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
					if p.Cwd == "/prefix" {
						prefixes.Add(1)
					}
					return &acp.NewSessionResponse{SessionID: "session"}, nil
				}
				p := agentPeer(t, a)
				control := p.call(t, `"control"`, acp.MethodSessionNew, newParams)
				wantJSON(t, control["result"], `{"sessionId":"session"}`)
				p.send(t, prefix+tc.suffix)
				// Observe a reply/EOF (or bounded silence) before half-closing.
				// On the defective SDK this reads the prefix's successful result,
				// so an early-EOF reply-drop race cannot hide its execution.
				if _, err := p.poll(t); err != nil {
					t.Logf("malformed-frame receiver policy: %v", err)
				}
				if err := p.finish(t); err != nil {
					t.Logf("endpoint close policy: %v", err)
				}
				if got := prefixes.Load(); got != 0 {
					t.Errorf("invalid complete frame executed its prefix %d times", got)
				}
				if got := a.cancelCalls.Load(); got != 0 {
					t.Errorf("second object in a single frame was dispatched %d times", got)
				}
				for _, r := range p.frames {
					if string(r["id"]) == `"prefix"` && len(r["result"]) > 0 {
						t.Errorf("invalid frame produced a successful prefix response: %v", r)
					}
				}
			})
		})
	}
}

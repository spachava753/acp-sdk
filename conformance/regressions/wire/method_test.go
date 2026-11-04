package wire_test

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// LOCAL addition: an explicitly present empty STRING method is not an absent
// method. This tests unknown-method handling, not support for an empty-name ACP
// operation. Absent/null-method shape tests remain in the upstream-derived suite.
func TestEmptyMethodIsUnknownRequest(t *testing.T) {
	t.Run("message_helpers", func(t *testing.T) {
		input := callFrame(`"empty"`, "", `{}`)
		m, err := jsonrpc.DecodeMessage([]byte(input))
		must(t, err)
		r, ok := m.(*jsonrpc.Request)
		if !ok {
			t.Errorf("empty string method decoded as %T, want Request", m)
		} else if !r.IsCall() || r.Method != "" {
			t.Errorf("empty-method request lost call identity or method: %#v", r)
		}
		encoded, err := jsonrpc.EncodeMessage(m)
		must(t, err)
		// Full-object comparison requires the empty method MEMBER to survive;
		// a getter's zero value alone would not distinguish absent from empty.
		wantJSON(t, encoded, input)

		// Check encoding independently too: decoder misclassification must not
		// hide an encoder's separate omitempty loss of the empty method field.
		id, err := jsonrpc.MakeID("empty")
		must(t, err)
		requestJSON, err := jsonrpc.EncodeMessage(&jsonrpc.Request{ID: id, Method: "", Params: json.RawMessage(`{}`)})
		must(t, err)
		wantJSON(t, requestJSON, input)
	})
	t.Run("runtime", func(t *testing.T) {
		if !isolated(t) {
			return
		}
		a := newAgent()
		p := agentPeer(t, a)
		r := p.call(t, `"empty"`, "", `{}`)
		wantJSON(t, r["id"], `"empty"`)
		var err jsonrpc.Error
		must(t, json.Unmarshal(r["error"], &err))
		if err.Code != jsonrpc.CodeMethodNotFound {
			t.Errorf("empty unknown method code=%d, want MethodNotFound", err.Code)
		}
		if _, ok := r["result"]; ok {
			t.Error("unknown method returned a result")
		}
		followup := p.call(t, `"followup"`, acp.MethodSessionNew, newParams)
		wantJSON(t, followup["id"], `"followup"`)
		wantJSON(t, followup["result"], `{"sessionId":"session"}`)
		must(t, p.finish(t))
		if got := a.newCalls.Load(); got != 1 {
			t.Errorf("ordinary handler calls=%d, want only the followup", got)
		}
	})
}

package wire_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
)

// exactIntegerID distinguishes JSON strings from numbers and compares numeric
// values without float64 rounding or requiring one insignificant number spelling.
func exactIntegerID(t *testing.T, b []byte) string {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var value any
	must(t, d.Decode(&value))
	n, ok := value.(json.Number)
	if !ok {
		t.Errorf("response ID is %T, want a JSON number", value)
		return "non-number"
	}
	r, ok := new(big.Rat).SetString(n.String())
	if !ok || !r.IsInt() || !r.Num().IsInt64() {
		t.Errorf("not an exact int64 ID: %s", b)
		return "invalid-number"
	}
	return r.Num().String()
}

// LOCAL addition: unlike the upstream sequential ID table and opposite-direction
// reuse case, these adjacent IDs are simultaneously active in ONE direction.
// Both exceed 2^53 and round to the same float64 value on the defective decoder.
func TestAdjacentLargeIDsRemainIndependent(t *testing.T) {
	if !isolated(t) {
		return
	}
	const firstID = "9007199254740995"
	const secondID = "9007199254740996"
	a := newAgent()
	entered := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	a.newSession = func(_ context.Context, p *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
		if p.Cwd == "/first" || p.Cwd == "/second" {
			entered <- p.Cwd
			<-release
		}
		return &acp.NewSessionResponse{SessionID: acp.SessionId(p.Cwd)}, nil
	}
	p := agentPeer(t, a)
	t.Cleanup(unblock) // Release blocked handlers before forced raw-close cleanup.
	p.send(t, callFrame(firstID, acp.MethodSessionNew, `{"cwd":"/first","mcpServers":[]}`))
	if got := await(t, entered); got != "/first" {
		t.Fatalf("first handler=%q", got)
	}
	p.send(t, callFrame(secondID, acp.MethodSessionNew, `{"cwd":"/second","mcpServers":[]}`))
	p.send(t, callFrame(`"barrier"`, acp.MethodSessionNew, `{"cwd":"/barrier","mcpServers":[]}`))
	for {
		r := p.receive(t)
		if string(r["id"]) == `"barrier"` {
			wantJSON(t, r["result"], `{"sessionId":"/barrier"}`)
			break
		}
		t.Errorf("held request replied before release: %v", r)
	}
	// A barrier proves the read loop reached both requests; it alone cannot
	// prove an asynchronous handler started. Wait for the second actual entry.
	select {
	case got := <-entered:
		if got != "/second" {
			t.Errorf("second handler=%q", got)
		}
	case <-time.After(deadline):
		t.Error("distinct second ID never entered its handler while the first was active")
	}
	unblock()
	seen := map[string]int{}
	for range 2 {
		r := p.receive(t)
		id := exactIntegerID(t, r["id"])
		seen[id]++
		switch id {
		case firstID:
			wantJSON(t, r["result"], `{"sessionId":"/first"}`)
		case secondID:
			wantJSON(t, r["result"], `{"sessionId":"/second"}`)
		default:
			t.Errorf("reply used an unissued ID: %v", r)
		}
		if _, exists := r["error"]; exists {
			t.Errorf("valid distinct request was rejected: %v", r)
		}
	}
	if seen[firstID] != 1 || seen[secondID] != 1 {
		t.Errorf("reply correlation counts=%v", seen)
	}
	must(t, p.finish(t)) // Read both expected replies before testing orderly EOF.
	if got := a.newCalls.Load(); got != 3 {
		t.Errorf("handler calls=%d, want both requests and the barrier", got)
	}
}

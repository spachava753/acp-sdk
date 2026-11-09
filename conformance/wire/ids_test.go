package wire_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// newCallbackAgent returns an agent where _test/echo answers {} and
// _test/callback calls _test/echo on the client and answers with its result.
func newCallbackAgent(t *testing.T) *agent {
	t.Helper()
	a := newAgent()
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/echo", func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	}))
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/callback", func(ctx context.Context, _ *struct{}) (*json.RawMessage, error) {
		r, err := a.conn.CallExtension(ctx, "_test/echo", struct{}{})
		return &r, err
	}))
	return a
}

// TestRequestIDsAnswered checks that the agent answers each valid request ID
// with exactly that ID, both when the handler returns directly and when it
// first calls back to the client. The callback must be a well-formed request,
// and the peer's reply to it, which echoes the SDK's chosen ID, must reach the
// handler. Out-of-range or fractional IDs are rejected with a null id. Every
// exchange shares one connection, which must still end cleanly.
func TestRequestIDsAnswered(t *testing.T) {
	ag, peer := startAgent(t, newCallbackAgent(t))
	for _, method := range []string{"_test/echo", "_test/callback"} {
		for _, id := range validIDs {
			peer.SendLine(requestLine(id.wire, method, `{}`))
			if method == "_test/callback" {
				callback := peer.Next()
				acptest.EqualJSON(t, callback.Line, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":"_test/echo","params":{}}`, callback.ID))
				peer.SendLine(`{"jsonrpc":"2.0","id":` + string(callback.ID) + `,"result":{}}`)
			}
			acptest.EqualJSON(t, peer.Next().Line, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{}}`, id.wire))
			// Later exchanges depend on this one having completed.
			if t.Failed() {
				t.Fatalf("stopped after %s with id %s", method, id.name)
			}
		}
	}
	for _, id := range []string{"9223372036854775808", "-9223372036854775809", "1.5"} {
		peer.SendLine(requestLine(id, "_test/echo", `{}`))
		r := peer.Next()
		wantError(t, r, jsonrpc.CodeInvalidRequest)
		acptest.EqualJSON(t, r.ID, `null`)
	}
	finishClean(t, ag, peer)
}

// TestCallbackReusesIncomingID checks that while the peer's request 1 (or
// "1") is open, the agent's callback can also use id 1: the peer's reply with
// id 1 reaches the callback, not the incoming request, and the incoming
// request is then answered under its own id with the callback's result.
func TestCallbackReusesIncomingID(t *testing.T) {
	for name, id := range map[string]string{"number": `1`, "string": `"1"`} {
		t.Run(name, func(t *testing.T) {
			// A fresh connection, so the SDK numbers its first outgoing
			// request 1.
			_, peer := startAgent(t, newCallbackAgent(t))
			peer.SendLine(requestLine(id, "_test/callback", `{}`))
			callback := peer.ExpectCall("_test/echo")
			if string(callback.ID) != `1` {
				t.Fatalf("callback id = %s, want 1 to collide with the open request", callback.ID)
			}
			peer.SendLine(`{"jsonrpc":"2.0","id":1,"result":{"answer":"callback"}}`)
			acptest.EqualJSON(t, peer.Next().Line, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"answer":"callback"}}`, id))
		})
	}
}

// TestNullIDInitialize checks that protocol version 1 accepts initialize with
// a null id and answers with a null id.
func TestNullIDInitialize(t *testing.T) {
	_, peer := serve(t, newAgent())
	peer.SendLine(requestLine("null", acp.MethodInitialize, `{"protocolVersion":1,"clientCapabilities":{}}`))
	r := peer.Next()
	acptest.EqualJSON(t, r.ID, `null`)
	var result struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	check(t, json.Unmarshal(r.Result, &result))
	if result.ProtocolVersion != 1 {
		t.Errorf("protocol version = %d, want 1", result.ProtocolVersion)
	}
}

// TestAdjacentLargeIDsInFlight checks that two in-flight requests whose IDs
// differ by one above 2^53, and so would collide as float64, stay distinct:
// both handlers run concurrently and each response carries its own numeric ID
// and result.
func TestAdjacentLargeIDsInFlight(t *testing.T) {
	const firstID, secondID = "9007199254740995", "9007199254740996"
	a := newAgent()
	entered := make(chan string, 2)
	release := make(chan struct{})
	a.newSession = func(_ context.Context, p *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
		if p.Cwd == "/first" || p.Cwd == "/second" {
			entered <- p.Cwd
			<-release
		}
		return &acp.NewSessionResponse{SessionID: acp.SessionId(p.Cwd)}, nil
	}
	ag, peer := startAgent(t, a)
	unblock := sync.OnceFunc(func() { close(release) })
	// Release held handlers before the harness waits for RunAgent.
	t.Cleanup(unblock)

	peer.SendLine(requestLine(firstID, acp.MethodSessionNew, `{"cwd":"/first","mcpServers":[]}`))
	if got := acptest.Await(t, entered); got != "/first" {
		t.Fatalf("first handler saw cwd %q", got)
	}
	peer.SendLine(requestLine(secondID, acp.MethodSessionNew, `{"cwd":"/second","mcpServers":[]}`))
	// The barrier's response shows the agent has read both held requests and
	// that neither was answered early.
	peer.SendLine(requestLine(`"barrier"`, acp.MethodSessionNew, `{"cwd":"/barrier","mcpServers":[]}`))
	for {
		r := peer.Next()
		if string(r.ID) == `"barrier"` {
			acptest.EqualJSON(t, r.Result, `{"sessionId":"/barrier"}`)
			break
		}
		t.Errorf("held request answered before release: %s", r)
	}
	// Reading the barrier does not prove the second handler started, so wait
	// for it while the first is still held.
	if got := acptest.Await(t, entered); got != "/second" {
		t.Errorf("second handler saw cwd %q", got)
	}

	unblock()
	seen := map[string]int{}
	for range 2 {
		r := peer.Next()
		if r.Has("error") {
			t.Errorf("request rejected: %s", r)
		}
		id := exactInt(t, r.ID)
		seen[id]++
		switch id {
		case firstID:
			acptest.EqualJSON(t, r.Result, `{"sessionId":"/first"}`)
		case secondID:
			acptest.EqualJSON(t, r.Result, `{"sessionId":"/second"}`)
		default:
			t.Errorf("response with an id that was not sent: %s", r)
		}
	}
	if seen[firstID] != 1 || seen[secondID] != 1 {
		t.Errorf("responses per id = %v, want one each", seen)
	}
	finishClean(t, ag, peer)
	if got := a.newCalls.Load(); got != 3 {
		t.Errorf("handler calls = %d, want 3", got)
	}
}

// TestSameIDBothDirections checks that a request ID used by the agent for an
// outgoing call and by the peer for an incoming call identifies two separate
// requests: cancelling the incoming one leaves the outgoing call pending, and
// the outgoing call still receives its own result. The incoming handler
// observes cancellation only through its context.
func TestSameIDBothDirections(t *testing.T) {
	a := newAgent()
	release := make(chan struct{})
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/ask", func(ctx context.Context, _ *struct{}) (*json.RawMessage, error) {
		r, err := a.conn.CallExtension(ctx, "_test/question", struct{}{})
		return &r, err
	}))
	check(t, acp.AddExtensionRequest(a.ExtensionMux, "_test/slow", func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		select {
		case <-ctx.Done():
			return nil, &jsonrpc.Error{Code: int64(acp.ErrorCodeRequestCancelled), Message: "Request cancelled"}
		case <-release:
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "released by cleanup"}
		}
	}))
	_, peer := startAgent(t, a)
	// Only unblocks the handler if the test fails before cancelling it.
	t.Cleanup(func() { close(release) })

	peer.Request("ask", "_test/ask", map[string]any{})
	question := peer.Expect("_test/question")
	shared := question.ID
	peer.Request(shared, "_test/slow", map[string]any{})
	peer.Notify("$/cancel_request", map[string]any{"requestId": shared})

	incoming := peer.Next()
	acptest.EqualJSON(t, incoming.ID, string(shared))
	wantError(t, incoming, int64(acp.ErrorCodeRequestCancelled))

	peer.Reply(shared, map[string]any{"answer": "42"})
	r := peer.Next()
	acptest.EqualJSON(t, r.ID, `"ask"`)
	acptest.EqualJSON(t, r.Result, `{"answer":"42"}`)
}

// TestLateResponseAfterCancel checks that when a client call is cancelled
// after its request was sent, a response the peer sends later is discarded
// without closing the connection, and later calls still get their own
// results. The client may announce the cancellation with at most one
// $/cancel_request notification for that request.
func TestLateResponseAfterCancel(t *testing.T) {
	client, peer := startClient(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	abandoned := acptest.Go(func() (json.RawMessage, error) {
		return client.CallExtension(ctx, "_test/simple", message{"late"})
	})
	late := peer.Expect("_test/simple")
	// Cancel only once the request is fully written: the pipe has no write
	// deadline, so cancelling mid-write would close the connection by design.
	// The client writes one frame at a time, so its reply to this probe shows
	// the request's write has returned.
	wantError(t, peer.Call("probe", "_test/probe", map[string]any{}), jsonrpc.CodeMethodNotFound)
	cancel()
	if acptest.Await(t, abandoned).Err == nil {
		t.Fatal("cancelled call succeeded")
	}

	cancellations := 0
	for _, text := range []string{"release", "after"} {
		result := acptest.Go(func() (json.RawMessage, error) {
			return client.CallExtension(context.Background(), "_test/simple", message{text})
		})
		// Skip the optional cancellation notice, checking it targets the
		// cancelled request.
		r := peer.Next()
		for r.Method == "$/cancel_request" {
			if !r.IsNotification() {
				t.Fatalf("cancellation notice has an id: %s", r)
			}
			var params struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			check(t, json.Unmarshal(r.Params, &params))
			acptest.EqualJSON(t, params.RequestID, string(late.ID))
			if cancellations++; cancellations > 1 {
				t.Fatal("more than one cancellation notice for one request")
			}
			r = peer.Next()
		}
		if r.Method != "_test/simple" || !r.Has("id") {
			t.Fatalf("frame %s, want a _test/simple request", r)
		}
		acptest.EqualJSON(t, r.Params, fmt.Sprintf(`{"message":%q}`, text))

		if text == "release" {
			// The late response arrives before this call's own response.
			peer.Reply(late.ID, answer{"late response"})
		}
		peer.Reply(r.ID, answer{"echo: " + text})
		acptest.EqualJSON(t, acptest.Value(t, result), fmt.Sprintf(`{"result":%q}`, "echo: "+text))
	}
}

package wire_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// TestBatchesRejected checks that the SDK, which does not support JSON-RPC
// batches, answers every array frame with a single InvalidRequest error with a
// null id, runs none of its entries, and keeps serving. All batches share one
// connection, so each later rejection and the final request also show the
// connection survived the earlier ones.
func TestBatchesRejected(t *testing.T) {
	a := newAgent()
	a.echo(t, "_test/echo")
	notified := make(chan struct{}, 1)
	check(t, acp.AddExtensionNotification(a.ExtensionMux, "_test/notify", func(context.Context, *message) error {
		notified <- struct{}{}
		return nil
	}))
	ag, peer := startAgent(t, a)
	for _, tc := range []struct{ name, frame string }{
		{"empty", `[]`},
		{"primitives", `[1,2,3]`},
		{"requests", `[` + requestLine("1", "_test/echo", `{"message":"one"}`) + `,` + requestLine("2", "_test/echo", `{"message":"two"}`) + `]`},
		{"single_request", `[` + requestLine("1", "one", `{}`) + `]`},
		{"notification", `[` + notificationLine("_test/notify", `{"message":"inside a batch"}`) + `]`},
		{"response", `[{"jsonrpc":"2.0","id":7,"result":null}]`},
	} {
		peer.SendLine(tc.frame)
		r := peer.Next()
		wantError(t, r, jsonrpc.CodeInvalidRequest)
		acptest.EqualJSON(t, r.ID, `null`)
		if t.Failed() {
			t.Fatalf("stopped after batch %s", tc.name)
		}
	}
	r := peer.Call(3, "_test/echo", message{"after batches"})
	acptest.EqualJSON(t, r.ID, `3`)
	acptest.EqualJSON(t, r.Result, `{"result":"echo: after batches"}`)

	// RunAgent has returned, so a notification from the batch cannot still
	// be on its way.
	finishClean(t, ag, peer)
	if len(notified) != 0 {
		t.Error("a notification inside a batch reached its handler")
	}
}

// TestBatchedInitializeThenRetry checks that an initialize request sent inside
// a batch is rejected without initializing the connection, and that the same
// request sent on its own then succeeds.
func TestBatchedInitializeThenRetry(t *testing.T) {
	_, peer := serve(t, newAgent())
	initialize := requestLine("1", acp.MethodInitialize, `{"protocolVersion":1}`)
	peer.SendLine("[" + initialize + "]")
	r := peer.Next()
	wantError(t, r, jsonrpc.CodeInvalidRequest)
	acptest.EqualJSON(t, r.ID, `null`)

	peer.SendLine(initialize)
	r = peer.Next()
	acptest.EqualJSON(t, r.ID, `1`)
	var result struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	check(t, json.Unmarshal(r.Result, &result))
	if result.ProtocolVersion != 1 {
		t.Errorf("protocol version = %d, want 1", result.ProtocolVersion)
	}
}

// TestMalformedFramesAnswered checks the error response to frames that are
// not valid JSON or not a message object: a null id, the matching code, and
// for parse errors the offending line in the error data.
func TestMalformedFramesAnswered(t *testing.T) {
	for _, tc := range []struct {
		name, frame string
		code        int64
		echoesLine  bool
	}{
		{"invalid_json", `{"method": "test", "id": 1, INVALID}`, jsonrpc.CodeParseError, true},
		{"open_array", `[`, jsonrpc.CodeParseError, true},
		{"primitive", `17`, jsonrpc.CodeInvalidRequest, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, peer := startAgent(t, newAgent())
			peer.SendLine(tc.frame)
			r := peer.Next()
			wantError(t, r, tc.code)
			acptest.EqualJSON(t, r.ID, `null`)
			if tc.echoesLine {
				var data struct {
					Line string `json:"line"`
				}
				check(t, json.Unmarshal(r.Error.Data, &data))
				if data.Line != tc.frame {
					t.Errorf("error data %s does not carry the original line", r.Error.Data)
				}
			}
		})
	}
}

// TestMalformedResponseIgnored checks that an unsolicited response carrying
// both result and error gets no reply, and the next request is answered
// normally.
func TestMalformedResponseIgnored(t *testing.T) {
	a := newAgent()
	a.echo(t, "_test/simple")
	_, peer := startAgent(t, a)
	peer.SendLine(fmt.Sprintf(`{"jsonrpc":"2.0","id":99,"result":null,"error":{"code":%d,"message":"Internal error"}}`, jsonrpc.CodeInternalError))
	r := peer.Call(41, "_test/simple", message{"after malformed response"})
	acptest.EqualJSON(t, r.ID, `41`)
	acptest.EqualJSON(t, r.Result, `{"result":"echo: after malformed response"}`)
}

// TestFrameWithTrailingDataNotExecuted checks that when one line holds a valid
// request followed by garbage or by a second message, the SDK runs neither and
// never answers the request with success. Whether it replies to the line or
// keeps serving afterwards is left open.
func TestFrameWithTrailingDataNotExecuted(t *testing.T) {
	prefix := requestLine(`"prefix"`, acp.MethodSessionNew, `{"cwd":"/prefix","mcpServers":[]}`)
	for _, tc := range []struct{ name, suffix string }{
		{"garbage", ` garbage`},
		{"second_message", " " + notificationLine(acp.MethodSessionCancel, cancelParams)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// In the bubble, waiting for a reply that never comes ends as
			// soon as the SDK is idle, with no real delay.
			synctest.Test(t, func(t *testing.T) {
				a := newAgent()
				var prefixes atomic.Int32
				a.newSession = func(_ context.Context, p *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
					if p.Cwd == "/prefix" {
						prefixes.Add(1)
					}
					return &acp.NewSessionResponse{SessionID: "session"}, nil
				}
				ag, peer := startAgent(t, a)
				// A correct request succeeds first, so the handler is known to
				// be reachable.
				acptest.EqualJSON(t, peer.Call("control", acp.MethodSessionNew, raw(newParams)).Result, `{"sessionId":"session"}`)

				peer.SendLine(prefix + tc.suffix)
				// Read any reply before closing input, so a success response
				// to the prefix cannot be lost during shutdown.
				var frames []acptest.Message
				if m, err := peer.TryNext(acptest.Timeout); err == nil {
					frames = append(frames, m)
				}
				rest, _ := finish(t, ag, peer)
				frames = append(frames, rest...)

				if got := prefixes.Load(); got != 0 {
					t.Errorf("the request before the trailing data ran %d times", got)
				}
				if len(a.cancels) != 0 {
					t.Error("the second message on the line was dispatched")
				}
				for _, m := range frames {
					if string(m.ID) == `"prefix"` && m.Has("result") {
						t.Errorf("the request before the trailing data was answered with success: %s", m)
					}
				}
			})
		})
	}
}

package schema_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

const (
	patchSessionID = "session-patch"
	omittedPatch   = `{"sessionId":"session-patch","update":{"sessionUpdate":"session_info_update"}}`
	clearedPatch   = `{"sessionId":"session-patch","update":{"sessionUpdate":"session_info_update","title":null,"updatedAt":null}}`
)

type patchObserver struct{ updates chan *acp.SessionNotification }

func (h *patchObserver) SessionUpdate(_ context.Context, n *acp.SessionNotification) error {
	select {
	case h.updates <- n:
		return nil
	default:
		return fmt.Errorf("unexpected extra session update")
	}
}

type patchAgent struct{}

func (patchAgent) Initialize(context.Context, *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	return &acp.InitializeResponse{ProtocolVersion: 1}, nil
}
func (patchAgent) SessionNew(context.Context, *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	return &acp.NewSessionResponse{SessionID: patchSessionID}, nil
}

var (
	_ acp.SessionUpdateHandler = (*patchObserver)(nil)
	_ acp.InitializeHandler    = patchAgent{}
	_ acp.SessionNewHandler    = patchAgent{}
)

// Keep the actual streams, not just graceful SDK Close. Raw deadlines also
// bound a synchronous Write that does not observe its context. Cleanup closes
// the peer stream before joining SDK shutdown; the local stream and serving
// context are retained as independent final-cleanup fallbacks.
func patchEndpoints(t *testing.T) (context.Context, *acp.IOTransport, acp.Connection, func()) {
	t.Helper()
	// Testing cancels t.Context before Cleanup. Keep serving alive until the
	// fixture has closed the streams and joined RunAgent, then cancel it.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	sdkStream, peerStream := net.Pipe()
	force := func() { peerStream.Close() }
	t.Cleanup(func() { sdkStream.Close(); peerStream.Close(); cancel() })
	deadline, _ := ctx.Deadline()
	if err := sdkStream.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := peerStream.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	peer, err := (&acp.IOTransport{Reader: peerStream, Writer: peerStream}).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Close() })
	return ctx, &acp.IOTransport{Reader: sdkStream, Writer: sdkStream}, peer, force
}

func patchShutdown(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil && !t.Failed() {
			t.Errorf("SDK shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("SDK shutdown did not finish after raw streams closed")
	}
}

func patchRead(t *testing.T, ctx context.Context, peer acp.Connection) jsonrpc.Message {
	t.Helper()
	msg, err := peer.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

// Complete a typed client call with a literal response from an independent
// agent peer. Nothing uses a second ACP payload decoder as the wire oracle.
func patchClientCall(t *testing.T, ctx context.Context, peer acp.Connection, method, params, result string, call func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- call() }()
	req, ok := patchRead(t, ctx, peer).(*jsonrpc.Request)
	if !ok || req.Method != method || !req.IsCall() {
		t.Fatalf("setup expected %s request, got %#v", method, req)
	}
	equalJSON(t, req.Params, params)
	if err := peer.Write(ctx, &jsonrpc.Response{ID: req.ID, Result: json.RawMessage(result)}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func patchPeerCall(t *testing.T, ctx context.Context, peer acp.Connection, idText, method, params, result string) {
	t.Helper()
	id, err := jsonrpc.MakeID(idText)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.Write(ctx, &jsonrpc.Request{ID: id, Method: method, Params: json.RawMessage(params)}); err != nil {
		t.Fatal(err)
	}
	res, ok := patchRead(t, ctx, peer).(*jsonrpc.Response)
	if !ok || res.ID.Raw() != idText || res.Error != nil {
		t.Fatalf("setup %s response = %#v", method, res)
	}
	equalJSON(t, res.Result, result)
}

// Local public-delivery regression for ACP V1 session-info patch meaning.
// Each phase has valid initialize/new-session setup. The send phase reads raw
// payload bytes; the receive phase injects fixed raw payloads independently.
// Awaiting each callback avoids relying on the SDK's notification ordering or
// prompt/load response-completion policy, which is tested elsewhere.
func TestSessionInfoPatchDelivery(t *testing.T) {
	t.Run("receive", func(t *testing.T) {
		ctx, transport, peer, force := patchEndpoints(t)
		observer := &patchObserver{updates: make(chan *acp.SessionNotification, 2)}
		client, err := acp.Connect(ctx, transport, observer)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			force()
			done := make(chan error, 1)
			go func() { done <- client.Close() }()
			patchShutdown(t, done)
		})
		patchClientCall(t, ctx, peer, "initialize", `{"protocolVersion":1}`, `{"protocolVersion":1}`, func() error {
			res, err := client.Initialize(ctx, &acp.InitializeRequest{ProtocolVersion: 1})
			if err == nil && (res == nil || res.ProtocolVersion != 1) {
				return fmt.Errorf("invalid V1 initialization response: %#v", res)
			}
			return err
		})
		cwd := t.TempDir()
		patchClientCall(t, ctx, peer, "session/new", fmt.Sprintf(`{"cwd":%q,"mcpServers":[]}`, cwd), `{"sessionId":"session-patch"}`, func() error {
			res, err := client.SessionNew(ctx, &acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
			if err == nil && (res == nil || res.SessionID != patchSessionID) {
				return fmt.Errorf("invalid new-session response: %#v", res)
			}
			return err
		})
		for i, literal := range []string{omittedPatch, clearedPatch} {
			if err := peer.Write(ctx, &jsonrpc.Request{Method: "session/update", Params: json.RawMessage(literal)}); err != nil {
				t.Fatal(err)
			}
			select {
			case observed := <-observer.updates:
				if observed == nil {
					t.Fatal("nil typed session update")
				}
				// NullFields already exists on the union API at 09349ff. A
				// caller must see clear markers, not two indistinguishable nil
				// pointers; omitted fields must not acquire those markers.
				for _, field := range []string{"title", "updatedAt"} {
					if got := slices.Contains(observed.Update.NullFields, field); got != (i == 1) {
						t.Errorf("typed callback null marker %s = %v, want %v", field, got, i == 1)
					}
				}
				// Its public encoding must also retain the clear intent.
				equalJSON(t, marshal(t, observed), literal)
			case <-ctx.Done():
				t.Fatal("typed session update was not delivered:", ctx.Err())
			}
		}
	})
	t.Run("send", func(t *testing.T) {
		ctx, transport, peer, force := patchEndpoints(t)
		ready := make(chan *acp.AgentConnection, 1)
		done := make(chan error, 1)
		go func() {
			done <- acp.RunAgent(ctx, transport, func(conn *acp.AgentConnection) any {
				ready <- conn
				return patchAgent{}
			})
		}()
		t.Cleanup(func() { force(); patchShutdown(t, done) })
		var conn *acp.AgentConnection
		select {
		case conn = <-ready:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		patchPeerCall(t, ctx, peer, "init", "initialize", `{"protocolVersion":1}`, `{"protocolVersion":1}`)
		patchPeerCall(t, ctx, peer, "new", "session/new", fmt.Sprintf(`{"cwd":%q,"mcpServers":[]}`, t.TempDir()), `{"sessionId":"session-patch"}`)
		for i, literal := range []string{omittedPatch, clearedPatch} {
			update := acp.SessionInfoUpdateSessionUpdate()
			if i == 1 {
				update.NullFields = []string{"title", "updatedAt"}
			}
			sent := make(chan error, 1)
			go func() {
				sent <- conn.SessionUpdate(ctx, &acp.SessionNotification{SessionID: patchSessionID, Update: update})
			}()
			req, ok := patchRead(t, ctx, peer).(*jsonrpc.Request)
			if !ok || req.Method != "session/update" || req.IsCall() {
				t.Fatalf("expected one-way session/update, got %#v", req)
			}
			equalJSON(t, req.Params, literal)
			select {
			case err := <-sent:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	})
}

// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package ordering_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// scriptPeer is an independent message peer at the public custom-transport
// boundary. It supplies literal payloads in receive order; it has no dispatcher,
// notification queue, response barrier or request-cancellation implementation.
// Envelope byte fidelity is tested separately in rustv1/wire.
type scriptPeer struct {
	incoming chan jsonrpc.Message
	outgoing chan jsonrpc.Message
	closed   chan struct{}
	once     sync.Once
}

func newPeer() *scriptPeer {
	return &scriptPeer{incoming: make(chan jsonrpc.Message, 16), outgoing: make(chan jsonrpc.Message, 16), closed: make(chan struct{})}
}

func (p *scriptPeer) Connect(context.Context) (acp.Connection, error) { return p, nil }
func (p *scriptPeer) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case message := <-p.incoming:
		return message, nil
	case <-p.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (p *scriptPeer) Write(ctx context.Context, message jsonrpc.Message) error {
	select {
	case p.outgoing <- message:
		return nil
	case <-p.closed:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *scriptPeer) Close() error { p.once.Do(func() { close(p.closed) }); return nil }

func take[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(time.Second): // Fake time: a failure bound, not an ordering assumption.
		t.Fatal("public operation did not make progress")
		var zero T
		return zero
	}
}

func (p *scriptPeer) call(t *testing.T, method string) *jsonrpc.Request {
	t.Helper()
	message := take(t, p.outgoing)
	request, ok := message.(*jsonrpc.Request)
	if !ok || !request.IsCall() || request.Method != method {
		t.Fatalf("outgoing message = %#v, want %s request", message, method)
	}
	return request
}

func (p *scriptPeer) reply(request *jsonrpc.Request, result string) {
	p.incoming <- &jsonrpc.Response{ID: request.ID, Result: json.RawMessage(result)}
}

func (p *scriptPeer) update(t *testing.T, session, text string) {
	t.Helper()
	params, err := json.Marshal(map[string]any{"sessionId": session, "update": map[string]any{
		"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text},
	}})
	if err != nil {
		t.Fatal(err)
	}
	p.incoming <- &jsonrpc.Request{Method: acp.MethodSessionUpdate, Params: params}
}

func connect(t *testing.T, handler any, capabilities *acp.ClientCapabilities) (*acp.Client, *scriptPeer) {
	t.Helper()
	peer := newPeer()
	client, err := acp.Connect(context.Background(), peer, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Force the actual transport closed only for cleanup, never to make a
		// completion/order assertion pass. Tests release their own handler gates.
		_ = peer.Close()
		closed := make(chan error, 1)
		go func() { closed <- client.Close() }()
		synctest.Wait()
		if err := take(t, closed); err != nil {
			t.Errorf("client cleanup: %v", err)
		}
	})
	ready := make(chan error, 1)
	go func() {
		_, err := client.Initialize(context.Background(), &acp.InitializeRequest{ProtocolVersion: 1, ClientCapabilities: capabilities})
		ready <- err
	}()
	peer.reply(peer.call(t, acp.MethodInitialize), `{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}`)
	if err := take(t, ready); err != nil {
		t.Fatal(err)
	}
	return client, peer
}

func createSession(t *testing.T, client *acp.Client, peer *scriptPeer) {
	t.Helper()
	ready := make(chan error, 1)
	go func() {
		_, err := client.SessionNew(context.Background(), &acp.NewSessionRequest{Cwd: "/workspace", McpServers: []acp.McpServer{}})
		ready <- err
	}()
	peer.reply(peer.call(t, acp.MethodSessionNew), `{"sessionId":"s"}`)
	if err := take(t, ready); err != nil {
		t.Fatal(err)
	}
}

type operationResult struct {
	value any
	err   error
}

func operation(client *acp.Client, method, session string) <-chan operationResult {
	done := make(chan operationResult, 1)
	go func() {
		var result operationResult
		if method == acp.MethodSessionPrompt {
			result.value, result.err = client.SessionPrompt(context.Background(), &acp.PromptRequest{SessionID: acp.SessionId(session), Prompt: []acp.ContentBlock{acp.TextContentBlock("question")}})
		} else {
			result.value, result.err = client.SessionLoad(context.Background(), &acp.LoadSessionRequest{SessionID: acp.SessionId(session), Cwd: "/workspace", McpServers: []acp.McpServer{}})
		}
		done <- result
	}()
	return done
}

type gatedUpdates struct {
	started chan struct{}
	second  chan struct{}
	release chan struct{}
	applied chan string
	once    sync.Once
}

func (h *gatedUpdates) unblock() { h.once.Do(func() { close(h.release) }) }
func (h *gatedUpdates) SessionUpdate(ctx context.Context, notification *acp.SessionNotification) error {
	content, ok := notification.Update.Content.(acp.ContentBlock)
	if !ok {
		return fmt.Errorf("unexpected typed content %T", notification.Update.Content)
	}
	switch content.Text {
	case "A":
		close(h.started)
		select {
		case <-h.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	case "B":
		close(h.second)
	}
	h.applied <- content.Text // Buffered so observation never blocks the SDK handler.
	return nil
}

// This is an additional Go SDK contract, not a mechanical Rust port or a claim
// that ACP specifies Go callback settlement. The Rust port detects tool-state
// reordering probabilistically; here a gate makes the original A/B race reliable.
// Success AND error responses must not overtake preceding updates for prompt or
// history loading. Load uses a saved ID rather than inventing a local session store.
func TestSessionUpdatesAndResultRespectReceivedPrefix(t *testing.T) {
	for _, method := range []string{acp.MethodSessionPrompt, acp.MethodSessionLoad} {
		for _, failed := range []bool{false, true} {
			name := "success"
			if failed {
				name = "error"
			}
			t.Run(method+"/"+name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					h := &gatedUpdates{started: make(chan struct{}), second: make(chan struct{}), release: make(chan struct{}), applied: make(chan string, 2)}
					defer h.unblock()
					client, peer := connect(t, h, nil)
					session := "saved"
					if method == acp.MethodSessionPrompt {
						createSession(t, client, peer)
						session = "s"
					}
					done := operation(client, method, session)
					request := peer.call(t, method)
					peer.update(t, session, "A")
					take(t, h.started)
					peer.update(t, session, "B")
					if failed {
						peer.incoming <- &jsonrpc.Response{ID: request.ID, Error: &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "remote failure"}}
					} else if method == acp.MethodSessionPrompt {
						peer.reply(request, `{"stopReason":"end_turn"}`)
					} else {
						peer.reply(request, `{}`)
					}
					// Both frames are available to the reader. Drain runnable work
					// before negative checks; no scheduler sleep or guessed delay.
					synctest.Wait()
					if len(peer.incoming) != 0 {
						t.Error("a blocked update stopped the reader from consuming later frames")
					}
					select {
					case <-h.second:
						t.Error("B's callback started before A's callback completed")
					default:
					}
					var result *operationResult
					select {
					case value := <-done:
						result = &value
						t.Errorf("%s returned before the earlier update completed", method)
					default:
					}
					h.unblock()
					synctest.Wait()
					if result == nil {
						value := take(t, done)
						result = &value
					}
					text := take(t, h.applied) + take(t, h.applied)
					if text != "AB" {
						t.Errorf("applied content = %q, want wire order AB", text)
					}
					if failed {
						var wire *jsonrpc.Error
						if !errors.As(result.err, &wire) || wire.Code != jsonrpc.CodeInternalError || wire.Message != "remote failure" {
							t.Errorf("original failure changed: %v", result.err)
						}
					} else if result.err != nil {
						t.Error(result.err)
					} else if method == acp.MethodSessionPrompt && result.value.(*acp.PromptResponse).StopReason != acp.StopReasonEndTurn {
						t.Errorf("prompt result changed: %#v", result.value)
					}
				})
			})
		}
	}
}

type prefixUpdates struct {
	firstStarted chan struct{}
	laterStarted chan struct{}
	firstRelease chan struct{}
	laterRelease chan struct{}
}

func (h *prefixUpdates) SessionUpdate(ctx context.Context, update *acp.SessionNotification) error {
	started, release := h.firstStarted, h.firstRelease
	if update.Update.SessionUpdate == acp.SessionUpdateTypeSessionInfoUpdate {
		started, release = h.laterStarted, h.laterRelease
	}
	close(started)
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Session state updates may arrive outside prompt turns. Completion must wait
// for the prefix preceding the response, not an ever-growing set of later work.
// In particular, reading the queue tail lazily after Await wakes is insufficient.
func TestPromptDoesNotWaitForUpdatesAfterItsResponse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &prefixUpdates{firstStarted: make(chan struct{}), laterStarted: make(chan struct{}), firstRelease: make(chan struct{}), laterRelease: make(chan struct{})}
		var firstOnce, laterOnce sync.Once
		releaseFirst := func() { firstOnce.Do(func() { close(h.firstRelease) }) }
		releaseLater := func() { laterOnce.Do(func() { close(h.laterRelease) }) }
		defer releaseFirst()
		defer releaseLater()
		client, peer := connect(t, h, nil)
		createSession(t, client, peer)
		done := operation(client, acp.MethodSessionPrompt, "s")
		prompt := peer.call(t, acp.MethodSessionPrompt)
		peer.update(t, "s", "A")
		take(t, h.firstStarted)
		peer.reply(prompt, `{"stopReason":"end_turn"}`)
		peer.incoming <- &jsonrpc.Request{Method: acp.MethodSessionUpdate, Params: json.RawMessage(`{"sessionId":"s","update":{"sessionUpdate":"session_info_update","title":"background change"}}`)}
		synctest.Wait()
		var result *operationResult
		select {
		case value := <-done:
			result = &value
			t.Error("prompt returned before its preceding callback completed")
		default:
		}
		releaseFirst()
		synctest.Wait()
		take(t, h.laterStarted)
		if result == nil {
			select {
			case value := <-done:
				result = &value
			default:
				t.Error("prompt completion waited for a state update received after its response")
			}
		}
		releaseLater()
		if result == nil {
			value := take(t, done)
			result = &value
		}
		if result.err != nil {
			t.Error(result.err)
		}
	})
}

type nestedValue struct {
	Value string `json:"value"`
}

type reentrantClient struct {
	client       *acp.Client
	applied      chan operationResult
	cancelNested chan context.CancelFunc
}

func (h *reentrantClient) SessionUpdate(ctx context.Context, _ *acp.SessionNotification) error {
	// Retain handler context values for reentrancy. The extra cancellation
	// handle lets failure cleanup release this wait even if a broken fix
	// accidentally blocks the reader inside the notification handler.
	nestedCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	h.cancelNested <- cancel
	value, err := acp.CallExtension[nestedValue](nestedCtx, h.client, "_nested", struct{}{})
	h.applied <- operationResult{value: value, err: err}
	return err
}
func (*reentrantClient) FsReadTextFile(context.Context, *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
	return &acp.ReadTextFileResponse{Content: "nested content"}, nil
}

// A global reader lock or one serial queue for every incoming message would
// deadlock this existing valid API use. Handler-initiated calls propagate their
// supplied context, so a completion barrier can recognize reentrant work.
func TestUpdateCallbackCanAwaitNestedRequestAndReverseCallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &reentrantClient{applied: make(chan operationResult, 1), cancelNested: make(chan context.CancelFunc, 1)}
		client, peer := connect(t, h, &acp.ClientCapabilities{Fs: &acp.FileSystemCapabilities{ReadTextFile: true}})
		h.client = client
		createSession(t, client, peer)
		done := operation(client, acp.MethodSessionPrompt, "s")
		prompt := peer.call(t, acp.MethodSessionPrompt)
		peer.update(t, "s", "nested")
		cancelNested := take(t, h.cancelNested)
		defer cancelNested()
		nested := peer.call(t, "_nested")
		peer.reply(prompt, `{"stopReason":"end_turn"}`)
		synctest.Wait()
		var early *operationResult
		select {
		case value := <-done:
			early = &value
			t.Error("outer prompt returned while its update callback awaited nested work")
		default:
		}
		id, err := jsonrpc.MakeID("reverse-read")
		if err != nil {
			t.Fatal(err)
		}
		peer.incoming <- &jsonrpc.Request{ID: id, Method: acp.MethodFsReadTextFile, Params: json.RawMessage(`{"sessionId":"s","path":"/workspace/file"}`)}
		response, ok := take(t, peer.outgoing).(*jsonrpc.Response)
		if !ok || response.ID != id || response.Error != nil || strings.TrimSpace(string(response.Result)) != `{"content":"nested content"}` {
			t.Fatalf("reverse callback failed while update was pending: %#v", response)
		}
		peer.reply(nested, `{"value":"nested content"}`)
		applied := take(t, h.applied)
		if applied.err != nil {
			t.Fatalf("nested request failed: %v", applied.err)
		}
		if applied.value.(*nestedValue).Value != "nested content" {
			t.Errorf("nested result changed: %#v", applied.value)
		}
		if early == nil {
			value := take(t, done)
			early = &value
		}
		if early.err != nil {
			t.Error(early.err)
		}
	})
}

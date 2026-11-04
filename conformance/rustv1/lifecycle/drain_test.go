// Copyright 2026 ACP Go SDK contributors.
// Rust SDK semantic ports, Apache-2.0; provenance and policy labels: mapping.json.
package lifecycle_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

type gatedOutput struct {
	*frameTransport
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (p *gatedOutput) Connect(context.Context) (acp.Connection, error) { return p, nil }

func (p *gatedOutput) Write(ctx context.Context, message jsonrpc.Message) error {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.release:
		return p.frameTransport.Write(ctx, message)
	case <-p.closed:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Rust: tests/jsonrpc_transport_close.rs:848,
// outgoing_drain_propagates_incoming_read_error. The incoming error is injected
// ONLY after the accepted output reaches a backpressured physical sink.
// Go explicit Close is its public shutdown operation; there is no synthetic
// finite-foreground driver/queue in this fixture. This tests Rust SDK shutdown
// policy, not an ACP protocol requirement or the earlier audit's generic EOF
// cancellation expectation. Cleanup retains the real transport closer.
func TestReadErrorIsNotHiddenBehindBackpressuredDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &gatedOutput{frameTransport: newFrames(), entered: make(chan struct{})}
		ac, done := startAgent(t, p, nil)
		defer func() { _ = p.Close(); synctest.Wait() }()
		sending := make(chan error, 1)
		go func() {
			sending <- ac.NotifyExtension(context.Background(), "_rust/queued", map[string]any{"payload": strings.Repeat("x", 1024)})
		}()
		receive(t, p.entered)
		closing := make(chan error, 1)
		go func() { closing <- ac.Close() }()
		synctest.Wait()
		readFailure := errors.New("read failed during outgoing drain")
		p.endInput(readFailure)
		synctest.Wait()
		select {
		case err := <-done:
			if !errors.Is(err, readFailure) {
				t.Errorf("read error was replaced during drain: %v", err)
			}
		default:
			t.Error("incoming read error was hidden behind the blocked output drain")
		}
		// Failure-safe release, never the event that makes the above check pass.
		_ = p.Close()
		synctest.Wait()
		receive(t, sending)
		receive(t, closing)
	})
}

// Rust: tests/connection_driver_normalization.rs:336,
// finite_builder_does_not_start_close_delivery_during_physical_drain; and
// tests/cooperative_connection_driver.rs cooperative_drain_probe, used by:
// finite_builder_waits_for_custom_cooperative_drain_not_input_eof,
// custom_cooperative_flush_error_overrides_foreground_success,
// decorated_cooperative_driver_waits_for_physical_drain,
// decorated_cooperative_driver_preserves_physical_flush_error,
// direct_finite_peer_waits_for_physical_drain_not_input_eof, and
// direct_finite_peer_preserves_physical_flush_error. Shared
// observable subset: after local finish begins, physical input keeps moving
// without restarting application notification delivery; accepted output drains.
// Explicit Go Close replaces foreground success. Cooperative finish hooks,
// three-frame async queues, and separate Flush gates have no Go API counterpart
// and are NOT implemented by the fixture (see partial mappings).
func TestLocalDrainReadsWithoutRestartingHandlerDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		p := &gatedOutput{frameTransport: newFrames(), entered: make(chan struct{}), release: release}
		mux := acp.NewExtensionMux()
		delivered := 0
		if err := acp.AddExtensionNotification(mux, "_rust/late", func(context.Context, *struct{}) error {
			delivered++
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		c, err := acp.Connect(context.Background(), p, mux)
		if err != nil {
			t.Fatal(err)
		}
		defer cleanupClient(p.frameTransport, c)
		sending := make(chan error, 1)
		go func() { sending <- c.NotifyExtension(context.Background(), "_rust/accepted", struct{}{}) }()
		receive(t, p.entered)
		closing := make(chan error, 1)
		go func() { closing <- c.Close() }()
		synctest.Wait()
		for range 2 {
			p.push(t, notification("_rust/late", struct{}{}))
		}
		synctest.Wait()
		if delivered != 0 {
			t.Errorf("drain restarted application message delivery: %d", delivered)
		}
		if len(p.input) != 0 {
			t.Error("backpressured output stopped reading input during drain")
		}
		// normalization.rs:336 drops input only AFTER the sink gate/finish.
		// Clean EOF must be consumed without aborting this accepted output.
		p.endInput(io.EOF)
		synctest.Wait()
		if len(p.input) != 0 {
			t.Error("physical input EOF was not observed during the gated drain")
		}
		if delivered != 0 {
			t.Error("late EOF restarted application delivery during drain")
		}
		p.noOutput(t)
		select {
		case err := <-closing:
			t.Fatalf("late clean EOF ended drain before accepted output was released: %v", err)
		default:
		}
		close(release)
		synctest.Wait()
		if err := receive(t, sending); err != nil {
			t.Errorf("accepted output did not drain: %v", err)
		}
		if err := receive(t, closing); err != nil {
			t.Errorf("clean drain did not finish: %v", err)
		}
		n, ok := p.next(t).(*jsonrpc.Request)
		if !ok || n.Method != "_rust/accepted" || n.IsCall() {
			t.Errorf("accepted output changed: %#v", n)
		}
		p.noOutput(t)
	})
}

type enteredWriter struct {
	io.WriteCloser
	entered chan struct{}
	once    sync.Once
}

func (w *enteredWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	return w.WriteCloser.Write(data)
}

// Rust: tests/jsonrpc_transport_close.rs:783,
// outgoing_drain_keeps_the_full_duplex_read_half_moving. Retain one underlying
// duplex stream, apply actual byte backpressure, and write BOTH16/1024-byte
// input payloads before reading the accepted outgoing frame. Explicit Go Close
// substitutes only for the local finish trigger, not an owned driver/producer.
func TestFullDuplexDrainKeepsReading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		local, peer := net.Pipe()
		writer := &enteredWriter{WriteCloser: local, entered: make(chan struct{})}
		raw, err := (&acp.IOTransport{Reader: local, Writer: writer}).Connect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		c, err := acp.Connect(context.Background(), fixedTransport{raw}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = local.Close(); _ = peer.Close(); _ = raw.Close(); synctest.Wait() }()
		sending := make(chan error, 1)
		go func() {
			sending <- c.NotifyExtension(context.Background(), "_rust/queued", map[string]any{"payload": strings.Repeat("x", 1024)})
		}()
		receive(t, writer.entered)
		closing := make(chan error, 1)
		go func() { closing <- c.Close() }()
		synctest.Wait()
		type peerResult struct {
			line []byte
			err  error
		}
		finished := make(chan peerResult, 1)
		go func() {
			for _, length := range []int{16, 1024} {
				if _, err := fmt.Fprintf(peer, "{\"jsonrpc\":\"2.0\",\"method\":\"_rust/late\",\"params\":{\"payload\":%q}}\n", strings.Repeat("x", length)); err != nil {
					finished <- peerResult{err: err}
					return
				}
			}
			line, err := bufio.NewReader(peer).ReadBytes('\n')
			finished <- peerResult{line, err}
		}()
		result := receive(t, finished)
		if result.err != nil {
			t.Fatalf("full-duplex drain failed: %v", result.err)
		}
		message, err := jsonrpc.DecodeMessage(result.line)
		if err != nil {
			t.Fatal(err)
		}
		n, ok := message.(*jsonrpc.Request)
		if !ok || n.IsCall() || n.Method != "_rust/queued" {
			t.Errorf("queued output was not drained: %#v", message)
		}
		synctest.Wait()
		if err := receive(t, sending); err != nil {
			t.Errorf("queued notification failed: %v", err)
		}
		if err := receive(t, closing); err != nil {
			t.Errorf("drain did not finish: %v", err)
		}
	})
}

type failingCloseWriter struct {
	io.WriteCloser
	err error
}

func (w failingCloseWriter) Close() error { _ = w.WriteCloser.Close(); return w.err }

// Rust: tests/connection_driver_normalization.rs:113, :158 and :692:
// agent_connect_with_drains_final_notification_to_physical_bytes,
// client_connect_with_drains_final_notification_to_physical_lines, and
// direct_builder_reports_physical_close_error_after_writing_accepted_output.
// Use a conforming Go io.Writer: unlike Rust AsyncWrite, a short nil-error
// Write violates Go's contract. One-byte peer reads supply real backpressure
// while all accepted output must reach the pipe before the close error escapes.
// Go's explicit Close replaces Rust's finite foreground completion trigger.
func TestPhysicalOutputDrainOnLocalClose(t *testing.T) {
	for _, tc := range []struct {
		name         string
		client       bool
		closeFailure bool
	}{
		{"agent-clean-close", false, false},
		{"client-clean-close", true, false},
		{"agent-close-error", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) { physicalOutputDrain(t, tc.client, tc.closeFailure) })
	}
}

func physicalOutputDrain(t *testing.T, clientSide, closeFailure bool) {
	synctest.Test(t, func(t *testing.T) {
		input, inputGuard := io.Pipe()
		peer, output := io.Pipe()
		var failure error
		if closeFailure {
			failure = errors.New("physical close failed")
		}
		writer := &enteredWriter{WriteCloser: failingCloseWriter{output, failure}, entered: make(chan struct{})}
		transport := &acp.IOTransport{Reader: input, Writer: writer}
		var notify func(context.Context, string, any) error
		var finish func() error
		var done <-chan error
		if clientSide {
			c, err := acp.Connect(context.Background(), transport, nil)
			if err != nil {
				t.Fatal(err)
			}
			notify, finish = c.NotifyExtension, c.Close
		} else {
			ac, agentDone := startAgent(t, transport, nil)
			notify, finish, done = ac.NotifyExtension, ac.Close, agentDone
		}
		startReading := make(chan struct{})
		var startOnce sync.Once
		releaseReader := func() { startOnce.Do(func() { close(startReading) }) }
		defer func() {
			releaseReader()
			_ = inputGuard.Close()
			_ = peer.Close()
			_ = output.Close()
			synctest.Wait()
		}()
		physical := make(chan []byte, 1)
		go func() {
			<-startReading
			var data []byte
			one := make([]byte, 1)
			for {
				n, err := peer.Read(one)
				data = append(data, one[:n]...)
				if err != nil {
					physical <- data
					return
				}
			}
		}()
		params := map[string]any{"sequence": 0, "payload": strings.Repeat("x", 1024)}
		sending := make(chan error, 1)
		go func() { sending <- notify(context.Background(), "_rust/normalization-probe", params) }()
		receive(t, writer.entered)
		synctest.Wait() // The real physical write is blocked; no peer reads yet.
		closing := make(chan error, 1)
		go func() { closing <- finish() }()
		synctest.Wait()
		select {
		case err := <-closing:
			t.Fatalf("Close returned before its in-flight physical output drained: %v", err)
		default:
		}
		releaseReader()
		synctest.Wait()
		if err := receive(t, sending); err != nil {
			t.Errorf("accepted output failed: %v", err)
		}
		if err := receive(t, closing); !errors.Is(err, failure) {
			t.Errorf("physical Close error lost: %v", err)
		}
		if done != nil {
			if err := receive(t, done); !errors.Is(err, failure) {
				t.Errorf("serving completion lost physical Close error: %v", err)
			}
		}
		data := receive(t, physical)
		if len(data) == 0 || data[len(data)-1] != '\n' {
			t.Fatalf("physical output was truncated: %q", data)
		}
		message, err := jsonrpc.DecodeMessage(data)
		if err != nil {
			t.Fatal(err)
		}
		n, ok := message.(*jsonrpc.Request)
		if !ok || n.IsCall() || n.Method != "_rust/normalization-probe" {
			t.Fatalf("physical frame = %#v", message)
		}
		var got struct {
			Sequence int    `json:"sequence"`
			Payload  string `json:"payload"`
		}
		if err := json.Unmarshal(n.Params, &got); err != nil || got.Sequence != 0 || got.Payload != params["payload"] {
			t.Errorf("accepted output changed: %s, %v", n.Params, err)
		}
	})
}

// Rust: tests/jsonrpc_transport_close.rs:476 and :760,
// active_channel_completion_drains_output_without_remote_eof and
// wrapped_byte_streams_client_success_does_not_wait_for_peer_eof: accepted output reaches
// the peer before local completion without requiring independently open input
// to close. Go explicit Close is the public lifetime operation, not an emulated
// owned ConnectionDriver. The missing automatic foreground ownership assertions
// are accounted as absent subcases in mapping.json.
func TestAcceptedOutputBeforeLocalCloseWithoutPeerEOF(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		name := "plain"
		if wrapped {
			name = "delegated"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newFrames()
				var transport acp.Transport = p
				if wrapped {
					transport = delegatingTransport{p}
				}
				c, err := acp.Connect(context.Background(), transport, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer cleanupClient(p, c)
				for sequence := range 3 {
					if err := c.NotifyExtension(context.Background(), "_rust/completed", map[string]any{"sequence": sequence}); err != nil {
						t.Fatal(err)
					}
				}
				closed := make(chan error, 1)
				go func() { closed <- c.Close() }()
				synctest.Wait()
				if err := receive(t, closed); err != nil {
					t.Errorf("local Close needed peer EOF: %v", err)
				}
				for sequence := range 3 {
					message := p.next(t)
					n, ok := message.(*jsonrpc.Request)
					if !ok || n.IsCall() || n.Method != "_rust/completed" {
						t.Fatalf("accepted output = %#v", message)
					}
					var got struct {
						Sequence int `json:"sequence"`
					}
					if err := json.Unmarshal(n.Params, &got); err != nil || got.Sequence != sequence {
						t.Errorf("accepted output order changed: %s, %v", n.Params, err)
					}
				}
				p.noOutput(t)
				select {
				case <-p.closed:
				default:
					t.Error("local completion retained the raw transport")
				}
			})
		})
	}
}

// Rust: src/concepts/connections.rs:45 and :163, request/notification and
// blocking-response examples. These use actual public Go typed Initialize and
// notification APIs; no fixture implements a connection builder or driver.
func TestDocumentedRequestThenNotification(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newFrames()
		c := client(t, p, echoMux(t))
		defer cleanupClient(p, c)
		initialized := make(chan error, 1)
		go func() {
			response, err := c.Initialize(context.Background(), &acp.InitializeRequest{ProtocolVersion: 1})
			if err == nil && response.ProtocolVersion != 1 {
				err = errors.New("initialize response protocol version changed")
			}
			initialized <- err
		}()
		message := p.next(t)
		req, ok := message.(*jsonrpc.Request)
		if !ok || !req.IsCall() || req.Method != acp.MethodInitialize {
			t.Fatalf("initialize request = %#v", message)
		}
		p.push(t, reply(req.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}, "authMethods": []any{}}))
		if err := receive(t, initialized); err != nil {
			t.Fatal(err)
		}
		// The connect_with example (jsonrpc.rs:1678) follows initialization
		// with another blocking request on the same live connection.
		ordinary := call(c, context.Background(), struct{}{})
		req = outgoingRequest(t, p)
		p.push(t, reply(req.ID, map[string]any{"status": "ok"}))
		if result := receive(t, ordinary); result.err != nil || string(result.value) != `{"status":"ok"}` {
			t.Fatalf("request after initialization failed: %s, %v", result.value, result.err)
		}
		if err := c.NotifyExtension(context.Background(), "_rust/status", map[string]any{"message": "hello"}); err != nil {
			t.Fatal(err)
		}
		n, ok := p.next(t).(*jsonrpc.Request)
		if !ok || n.IsCall() || n.Method != "_rust/status" || string(n.Params) != `{"message":"hello"}` {
			t.Errorf("status notification = %#v", n)
		}
		synctest.Wait()
		p.noOutput(t)
	})
}

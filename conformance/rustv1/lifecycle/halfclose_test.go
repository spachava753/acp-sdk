// Copyright 2026 ACP Go SDK contributors.
// Rust SDK semantic ports, Apache-2.0; provenance and policy labels: mapping.json.
package lifecycle_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Rust: tests/connection_driver_normalization.rs:593,
// normalized_split_duplex_half_close_allows_final_reverse_response; and
// src/jsonrpc.rs:7840, dropping_unused_finish_signal_preserves_physical_half_closes.
// Both halves share ONE real duplex socket. Go does not expose Channel.tx or a
// driver finish hook: its owner explicitly calls the retained TCP CloseWrite.
// Only the shared data-plane property is ported; missing SDK ownership/hook
// subcases are separately marked in mapping.json, never simulated here.
func TestSharedDuplexReadHalfSurvivesPhysicalOutputEOF(t *testing.T) {
	for _, mode := range []string{"final-reverse-response", "input-after-output-eof"} {
		t.Run(mode, func(t *testing.T) {
			// No synctest bubble: kernel socket I/O is not durably blocked.
			// These deadlines are failure bounds, not passing timing assertions.
			listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			deadline := time.Now().Add(3 * time.Second)
			if err := listener.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			dialCtx, stopDial := context.WithDeadline(context.Background(), deadline)
			defer stopDial()
			connected, err := (&net.Dialer{}).DialContext(dialCtx, "tcp4", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			peer := connected.(*net.TCPConn)
			defer peer.Close()
			local, err := listener.AcceptTCP()
			if err != nil {
				t.Fatal(err)
			}
			defer local.Close()
			for _, conn := range []*net.TCPConn{local, peer} {
				if err := conn.SetDeadline(deadline); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := (&acp.IOTransport{Reader: local, Writer: local}).Connect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			finished := make(chan struct{})
			var peerError error
			defer func() {
				_ = local.Close()
				_ = peer.Close()
				_ = raw.Close()
				select {
				case <-finished:
				case <-time.After(3 * time.Second):
					t.Error("owned half-close peer did not join after socket closure")
				}
			}()
			go func() {
				defer close(finished)
				peerError = func() error {
					reader := bufio.NewReader(peer)
					if mode == "final-reverse-response" {
						line, err := reader.ReadBytes('\n')
						if err != nil {
							return err
						}
						message, err := jsonrpc.DecodeMessage(line)
						req, ok := message.(*jsonrpc.Request)
						if err != nil || !ok || !req.IsCall() || req.ID.Raw() != int64(41) {
							return fmt.Errorf("shared duplex lost request41: %#v, %v", message, err)
						}
					}
					// Do not send the reverse frame until physical output EOF.
					if extra, err := reader.ReadBytes('\n'); len(extra) != 0 || !errors.Is(err, io.EOF) {
						return fmt.Errorf("missing physical write EOF: %q, %v", extra, err)
					}
					frame := `{"jsonrpc":"2.0","id":41,"result":{"status":"final"}}`
					if mode == "input-after-output-eof" {
						frame = `{"jsonrpc":"2.0","method":"test/after-output-eof"}`
					}
					if _, err := fmt.Fprintln(peer, frame); err != nil {
						return err
					}
					return peer.CloseWrite()
				}()
			}()
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			if mode == "final-reverse-response" {
				message, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":41,"method":"_rust/lifecycle","params":{}}`))
				if err != nil {
					t.Fatal(err)
				}
				if err := raw.Write(ctx, message); err != nil {
					t.Fatal(err)
				}
			}
			if err := local.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			message, err := raw.Read(ctx)
			if err != nil {
				t.Fatalf("shared read half stopped at output EOF: %v", err)
			}
			if mode == "final-reverse-response" {
				response, ok := message.(*jsonrpc.Response)
				if !ok || response.ID.Raw() != int64(41) {
					t.Fatalf("final reverse response = %#v", message)
				}
				success(t, response, `{"status":"final"}`)
			} else {
				n, ok := message.(*jsonrpc.Request)
				if !ok || n.IsCall() || n.Method != "test/after-output-eof" {
					t.Fatalf("late notification = %#v", message)
				}
			}
			if _, err := raw.Read(ctx); !errors.Is(err, io.EOF) {
				t.Errorf("shared input did not finish after final frame: %v", err)
			}
			select {
			case <-finished:
				if peerError != nil {
					t.Error(peerError)
				}
			case <-ctx.Done():
				t.Fatal("owned shared-duplex peer did not finish")
			}
		})
	}
}

// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.
package acp_test

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

type borrowedDeadlineWriter struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (w *borrowedDeadlineWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	return w.Conn.Write(data)
}

func (*borrowedDeadlineWriter) Close() error { return nil }

// slowCloseReader keeps stream teardown in progress, after its read has already
// failed, so a test can observe what the connection does during that window.
type slowCloseReader struct {
	*io.PipeReader
	closing chan struct{}
	release chan struct{}
}

func (r *slowCloseReader) Close() error {
	err := r.PipeReader.CloseWithError(io.ErrClosedPipe)
	close(r.closing)
	<-r.release
	return err
}

type countingWriter struct {
	mu     sync.Mutex
	writes int
}

func (w *countingWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	return len(data), nil
}

func (*countingWriter) Close() error { return nil }

// Local Close must take effect before the stream is torn down. Otherwise the
// read failure teardown causes reaches the engine as a transport fault, making
// graceful shutdown fail intermittently, and a queued writer can start a
// physical write that nothing will interrupt.
func TestIOTransportClosePrecedesStreamTeardown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		input, inputGuard := io.Pipe()
		defer inputGuard.Close()
		reader := &slowCloseReader{PipeReader: input, closing: make(chan struct{}), release: make(chan struct{})}
		writer := &countingWriter{}
		raw, err := (&acp.IOTransport{Reader: reader, Writer: writer}).Connect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		read := make(chan error, 1)
		go func() {
			_, err := raw.Read(context.Background())
			read <- err
		}()
		closed := make(chan error, 1)
		go func() { closed <- raw.Close() }()
		<-reader.closing

		// Both checks run while the stream's Close is still blocked.
		if err := <-read; !errors.Is(err, io.EOF) {
			t.Errorf("Read during local Close = %v, want EOF", err)
		}
		if err := raw.Write(context.Background(), &jsonrpc.Request{Method: "_test/late", Params: []byte(`{}`)}); !errors.Is(err, io.EOF) {
			t.Errorf("Write during local Close = %v, want EOF", err)
		}
		writer.mu.Lock()
		if writer.writes != 0 {
			t.Errorf("Close admitted %d physical writes while tearing down", writer.writes)
		}
		writer.mu.Unlock()

		close(reader.release)
		if err := <-closed; err != nil {
			t.Errorf("Close = %v", err)
		}
	})
}

// Public transport Close is the engine's force-abort seam. A borrowed writer
// may intentionally not close its descriptor (like stdout), but its native
// deadline still needs to interrupt an active background-context response.
// The reader is a SEPARATE pipe so closing it cannot accidentally make this
// test pass by also closing the blocked output socket.
func TestIOTransportCloseInterruptsBorrowedBackgroundWriter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		input, inputGuard := io.Pipe()
		local, peer := net.Pipe()
		writer := &borrowedDeadlineWriter{Conn: local, entered: make(chan struct{})}
		raw, err := (&acp.IOTransport{Reader: input, Writer: writer}).Connect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		written := make(chan struct{})
		var writeErr error
		defer func() {
			_ = local.Close() // Independent cleanup, only after assertions.
			_ = peer.Close()
			_ = inputGuard.Close()
			_ = raw.Close()
			select {
			case <-written:
			case <-time.After(time.Second):
				t.Error("owned writer did not join after native endpoint cleanup")
			}
		}()
		go func() {
			writeErr = raw.Write(context.Background(), &jsonrpc.Request{Method: "_test/borrowed-response", Params: []byte(`{}`)})
			close(written)
		}()
		select {
		case <-writer.entered:
		case <-time.After(time.Second):
			t.Fatal("background Write did not reach the physical writer")
		}
		synctest.Wait()
		if err := raw.Close(); err != nil {
			t.Errorf("raw Close failed: %v", err)
		}
		synctest.Wait()
		select {
		case <-written:
			if writeErr == nil {
				t.Error("aborted background Write unexpectedly succeeded without peer reads")
			}
		default:
			t.Error("raw Close left a background Write blocked on a borrowed no-op closer")
		}
	})
}

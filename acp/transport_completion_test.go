// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.
package acp_test

import (
	"bufio"
	"context"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// This public writer exposes a real I/O completion/return gap: the first
// physical Write has published ALL bytes, but its wrapper has not returned.
// Deadline changes are forwarded to the real stream, never simulated.
type publishedWriteGate struct {
	net.Conn
	published       chan struct{}
	release         chan struct{}
	deadlineApplied chan struct{}
	first           sync.Once
	deadlineOnce    sync.Once
}

func (w *publishedWriteGate) Write(data []byte) (int, error) {
	first := false
	w.first.Do(func() { first = true })
	n, err := w.Conn.Write(data)
	if first && err == nil && n == len(data) {
		close(w.published)
		<-w.release
	}
	return n, err
}

func (w *publishedWriteGate) SetWriteDeadline(deadline time.Time) error {
	err := w.Conn.SetWriteDeadline(deadline)
	if err == nil && !deadline.IsZero() {
		w.deadlineOnce.Do(func() { close(w.deadlineApplied) })
	}
	return err
}

func TestIOTransportPublishedWriteSurvivesDeadlineOnlyCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		local, peer := net.Pipe()
		writer := &publishedWriteGate{Conn: local, published: make(chan struct{}), release: make(chan struct{}), deadlineApplied: make(chan struct{})}
		raw, err := (&acp.IOTransport{Reader: local, Writer: writer}).Connect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var releaseOnce sync.Once
		release := func() { releaseOnce.Do(func() { close(writer.release) }) }
		var operations []<-chan struct{}
		defer func() {
			release()
			_ = local.Close()
			_ = peer.Close()
			_ = raw.Close()
			for _, done := range operations {
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("owned writer did not join after raw stream cleanup")
				}
			}
		}()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		firstDone := make(chan struct{})
		var firstError error
		operations = append(operations, firstDone)
		go func() {
			firstError = raw.Write(ctx, &jsonrpc.Request{Method: "_test/published", Params: []byte(`{}`)})
			close(firstDone)
		}()
		if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(peer)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("first frame was not fully published: %v", err)
		}
		message, err := jsonrpc.DecodeMessage(line)
		req, ok := message.(*jsonrpc.Request)
		if err != nil || !ok || req.Method != "_test/published" {
			t.Fatalf("first physical frame = %q, %v", line, err)
		}
		select {
		case <-writer.published:
		case <-time.After(time.Second):
			t.Fatal("writer did not reach its post-publication return gate")
		}
		cancel()
		select {
		case <-writer.deadlineApplied:
		case <-time.After(time.Second):
			t.Fatal("active cancellation callback did not apply the native deadline")
		}
		// The interruption callback actually ran; this is not merely the
		// stop-before-cancellation race. Now let the successful Write return.
		release()
		select {
		case <-firstDone:
		case <-time.After(time.Second):
			t.Fatal("fully published Write did not finish after its return gate")
		}
		if firstError != nil {
			t.Errorf("fully published deadline-only completion closed the stream: %v", firstError)
		}
		secondDone := make(chan struct{})
		var secondError error
		operations = append(operations, secondDone)
		go func() {
			secondError = raw.Write(context.Background(), &jsonrpc.Request{Method: "_test/after", Params: []byte(`{}`)})
			close(secondDone)
		}()
		line, err = reader.ReadBytes('\n')
		if err != nil {
			t.Errorf("healthy stream lost after complete first frame: %v", err)
		} else {
			message, decodeErr := jsonrpc.DecodeMessage(line)
			req, ok := message.(*jsonrpc.Request)
			if decodeErr != nil || !ok || req.Method != "_test/after" {
				t.Errorf("later physical frame = %q, %v", line, decodeErr)
			}
		}
		select {
		case <-secondDone:
			if secondError != nil {
				t.Errorf("next write failed after complete first frame: %v", secondError)
			}
		case <-time.After(time.Second):
			t.Error("later write did not finish on preserved stream")
		}
	})
}

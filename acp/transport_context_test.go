// Copyright 2026 The ACP SDK Authors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.
package acp_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// The committed net.Conn regression exercises deadline-based interruption.
// This checks the other implementation branch: plain io.Pipe has no write
// deadline method, so active cancellation must close it and join the writer.
func TestIOTransportCancellationWithoutWriteDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		input, inputGuard := io.Pipe()
		peer, output := io.Pipe()
		raw, err := (&acp.IOTransport{Reader: input, Writer: output}).Connect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = inputGuard.Close(); _ = peer.Close(); _ = raw.Close(); synctest.Wait() }()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- raw.Write(ctx, &jsonrpc.Request{Method: "_test/write", Params: []byte(`{}`)})
		}()
		one := make([]byte, 1)
		if n, err := peer.Read(one); n != 1 || err != nil {
			t.Fatalf("partial write = %d, %v", n, err)
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) || !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("active cancellation error = %v, want context and permanent-close causes", err)
			}
		case <-time.After(time.Second):
			t.Fatal("plain pipe Write did not join after cancellation")
		}
		// A canceled queued/pre-write operation must NOT carry the permanent
		// sentinel: the engine uses it to distinguish harmless abandonment.
		if err := raw.Write(ctx, &jsonrpc.Request{Method: "_test/unsent"}); !errors.Is(err, context.Canceled) || errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("pre-canceled write error = %v, want only context cancellation", err)
		}
	})
}

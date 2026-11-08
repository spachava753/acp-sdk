// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package acp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/spachava753/acp-sdk/jsonrpc"
)

// ErrConnectionClosed reports that an ACP connection is no longer usable.
var ErrConnectionClosed = errors.New("connection closed")

// Transport opens a bidirectional ACP connection.
type Transport interface {
	Connect(context.Context) (Connection, error)
}

// Connection reads and writes JSON-RPC messages for an ACP transport.
type Connection interface {
	Read(context.Context) (jsonrpc.Message, error)
	Write(context.Context, jsonrpc.Message) error
	Close() error
}

// StdioTransport speaks ACP over os.Stdin and os.Stdout.
type StdioTransport struct{}

// Connect returns an ACP connection over standard input and output.
func (*StdioTransport) Connect(context.Context) (Connection, error) {
	return newIOConn(rwc{os.Stdin, nopCloserWriter{os.Stdout}}), nil
}

type nopCloserWriter struct{ io.Writer }

func (nopCloserWriter) Close() error { return nil }

func (w nopCloserWriter) SetWriteDeadline(deadline time.Time) error {
	if writer, ok := w.Writer.(writeDeadliner); ok {
		return writer.SetWriteDeadline(deadline)
	}
	return os.ErrNoDeadline
}

// IOTransport speaks ACP over caller-provided reader and writer streams.
//
// Interrupting an active write can leave a partial JSON frame, so doing so closes
// the connection. A complete frame that wins the race with cancellation is a
// successful send and leaves the connection open, unless the stream had to be
// closed to attempt the interruption. To interrupt blocked I/O, the streams must
// support write deadlines or have Close methods that unblock concurrent I/O. An
// arbitrary io.Writer cannot otherwise be interrupted safely. Cancellation while
// waiting to write does not close the connection or interrupt another writer.
type IOTransport struct {
	Reader io.ReadCloser
	Writer io.WriteCloser
}

// Connect returns an ACP connection over the configured reader and writer.
func (t *IOTransport) Connect(context.Context) (Connection, error) {
	if t.Reader == nil {
		return nil, fmt.Errorf("nil reader")
	}
	if t.Writer == nil {
		return nil, fmt.Errorf("nil writer")
	}
	return newIOConn(rwc{t.Reader, t.Writer}), nil
}

// InMemoryTransport is one endpoint of an in-process ACP connection pair.
type InMemoryTransport struct {
	rwc io.ReadWriteCloser
}

// Connect returns the in-memory ACP connection endpoint.
func (t *InMemoryTransport) Connect(context.Context) (Connection, error) {
	return newIOConn(t.rwc), nil
}

// NewInMemoryTransports returns connected client and agent in-memory transports.
func NewInMemoryTransports() (*InMemoryTransport, *InMemoryTransport) {
	c1, c2 := net.Pipe()
	return &InMemoryTransport{c1}, &InMemoryTransport{c2}
}

type rwc struct {
	rc io.ReadCloser
	wc io.WriteCloser
}

func (r rwc) Read(p []byte) (int, error)  { return r.rc.Read(p) }
func (r rwc) Write(p []byte) (int, error) { return r.wc.Write(p) }

type writeDeadliner interface {
	SetWriteDeadline(time.Time) error
}

func (r rwc) SetWriteDeadline(deadline time.Time) error {
	if writer, ok := r.wc.(writeDeadliner); ok {
		return writer.SetWriteDeadline(deadline)
	}
	return os.ErrNoDeadline
}

func (r rwc) Close() error {
	return errors.Join(r.rc.Close(), r.wc.Close())
}

type ioConn struct {
	writeGate chan struct{}
	rwc       io.ReadWriteCloser
	readCh    <-chan msgOrErr

	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error
}

type msgOrErr struct {
	msg jsonrpc.Message
	err error
}

func newIOConn(rwc io.ReadWriteCloser) *ioConn {
	readCh := make(chan msgOrErr)
	closed := make(chan struct{})
	go func() {
		reader := bufio.NewReader(rwc)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				line = bytes.TrimRight(line, "\r\n")
				msg, decodeErr := jsonrpc.DecodeMessage(line)
				select {
				case readCh <- msgOrErr{msg: msg, err: decodeErr}:
				case <-closed:
					return
				}
			}
			if err != nil {
				// A read failing after local Close began is an expected result of
				// closing the stream, not a transport fault. Read reports EOF.
				select {
				case <-closed:
					return
				default:
				}
				select {
				case readCh <- msgOrErr{err: err}:
				case <-closed:
				}
				return
			}
		}
	}()
	return &ioConn{rwc: rwc, readCh: readCh, closed: closed, writeGate: make(chan struct{}, 1)}
}

func (c *ioConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case v := <-c.readCh:
		return v.msg, v.err
	case <-c.closed:
		return nil, io.EOF
	}
}

func (c *ioConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	data = append(data, '\n')
	// Encoding may invoke caller code. Do not enqueue work it canceled.
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return io.EOF
	case c.writeGate <- struct{}{}:
	}
	defer func() { <-c.writeGate }()
	// A ready gate and cancellation can win the select together. Only the
	// active writer may install the interruption callback below.
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-c.closed:
		return io.EOF
	default:
	}

	var stop func() bool
	var interrupted chan struct{}
	var deadlineInterrupted writeDeadliner // Read only after callback completion.
	if ctx.Done() != nil {
		interrupted = make(chan struct{})
		defer func() {
			// Also unregister interruption if a caller-supplied Writer panics.
			if stop != nil && !stop() {
				<-interrupted
			}
		}()
		stop = context.AfterFunc(ctx, func() {
			defer close(interrupted)
			// Deadlines also interrupt borrowed stdio writers whose Close is
			// intentionally a no-op. No other writer can own the gate yet.
			if writer, ok := c.rwc.(writeDeadliner); ok {
				if writer.SetWriteDeadline(time.Now()) == nil {
					deadlineInterrupted = writer
					return
				}
			}
			_ = c.Close()
		})
	}
	// Keep Write on the caller's goroutine: returning on ctx.Done while a
	// detached writer still holds the stream would leak work and permit a
	// canceled frame to appear later.
	n, err := c.rwc.Write(data)
	interruptionStopped := true
	if stop != nil {
		interruptionStopped = stop()
		stop = nil
		if !interruptionStopped {
			<-interrupted
		}
	}
	if err == nil && n == len(data) {
		if interruptionStopped {
			// The complete frame won the race. Once interruption is disarmed,
			// later cancellation must not close an unrelated healthy connection.
			return nil
		}
		if deadlineInterrupted != nil {
			// Publication can finish before Write returns. A deadline-only
			// interruption did not damage this complete frame; reset it while
			// still holding the gate, before another writer can use the stream.
			if resetErr := deadlineInterrupted.SetWriteDeadline(time.Time{}); resetErr != nil {
				closeErr := c.Close()
				return errors.Join(ctx.Err(), io.ErrClosedPipe, resetErr, closeErr)
			}
			return nil
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		// A partial frame is not reusable. Join interruption before closing
		// and releasing serialization, even when Write itself returned nil.
		closeErr := c.Close()
		return errors.Join(ctxErr, io.ErrClosedPipe, closeErr)
	}
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func (c *ioConn) Close() error {
	c.closeOnce.Do(func() {
		// Publish first. A completing writer may clear the deadline set below,
		// so queued writers must already see closed when it releases the gate.
		// The reader also relies on this to recognize failures caused by Close.
		close(c.closed)
		// Raw Close is an abort seam and must not wait for the write gate.
		// Borrowed writers can have no-op Close methods but still support
		// deadline interruption, including noncancelable response writes.
		// Graceful RPC shutdown drains accepted writes before calling us.
		if writer, ok := c.rwc.(writeDeadliner); ok {
			_ = writer.SetWriteDeadline(time.Now())
		}
		c.closeErr = c.rwc.Close()
	})
	return c.closeErr
}

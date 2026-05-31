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

// IOTransport speaks ACP over caller-provided reader and writer streams.
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

func (r rwc) Close() error {
	return errors.Join(r.rc.Close(), r.wc.Close())
}

type ioConn struct {
	writeMu sync.Mutex
	rwc     io.ReadWriteCloser
	readCh  <-chan msgOrErr

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
				select {
				case readCh <- msgOrErr{err: err}:
				case <-closed:
				}
				return
			}
		}
	}()
	return &ioConn{rwc: rwc, readCh: readCh, closed: closed}
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
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	data = append(data, '\n')

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.rwc.Write(data)
	return err
}

func (c *ioConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.rwc.Close()
		close(c.closed)
	})
	return c.closeErr
}

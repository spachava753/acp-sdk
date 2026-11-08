// Copyright 2018 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package jsonrpc2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spachava753/acp-sdk/internal/json"
)

// Connection manages the jsonrpc2 protocol, connecting responses back to their
// calls. Connection is bidirectional; it does not have a designated server or
// client end.
//
// The jsonrpc2.Connection layers RPC logic on top of a stream, dispatching RPC
// handlers and correlating requests with responses from the peer.
type Connection struct {
	seq int64 // must only be accessed using atomic operations

	stateMu sync.Mutex
	state   inFlightState // accessed only in updateInFlight
	done    chan struct{} // closed after transport, reader and accepted handlers finish

	writer         Writer
	handler        Handler
	cancelHandlers context.CancelFunc
	controlContext context.Context
	cancelControl  context.CancelFunc
	onCallCanceled func(context.Context, ID)

	onInternalError func(error)
	onDone          func()
}

// inFlightState records the state of the incoming and outgoing calls on a
// Connection.
type inFlightState struct {
	connClosing bool  // true when the Connection's Close method has been called
	reading     bool  // true while the readIncoming goroutine is running
	readErr     error // non-nil when the readIncoming goroutine exits (typically io.EOF)
	writeErr    error // non-nil after a terminal Writer failure
	abortErr    error // fatal I/O or explicit serving-context cancellation

	// closer shuts down and cleans up the Reader and Writer state, ideally
	// interrupting any Read or Write call that is currently blocked. It is closed
	// when the state is idle and one of: connClosing is true, readErr is non-nil,
	// or writeErr is non-nil.
	//
	// The closer is removed before it runs outside stateMu. A subprocess closer
	// may wait for output/exit, so holding the state lock across it can deadlock
	// the reader. Completion waits for the closer's result as well.
	closer           io.Closer
	closingTransport bool
	closeErr         error

	outgoingCalls         map[ID]*AsyncCall // calls only
	outgoingNotifications int               // # of notifications awaiting "write"
	writes                int               // physical writes already accepted

	// incoming stores the total number of incoming calls and notifications
	// that have not yet written or processed a result.
	incoming int

	incomingByID map[ID]*incomingRequest // calls only

	// Requests and notifications start in receive order. Each holds the queue
	// until its handler returns or calls Async. Responses bypass the queue, so a
	// handler can still await the answer to its own call.
	handlerQueue     []*incomingRequest
	handlerRunning   bool
	lastNotification <-chan struct{}
}

// updateInFlight locks the state of the connection's in-flight requests, allows
// f to mutate that state, and closes the connection if it is idle and either
// is closing or has a read or write error.
func (c *Connection) updateInFlight(f func(*inFlightState)) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	s := &c.state

	f(s)

	select {
	case <-c.done:
		// The connection was already completely done at the start of this call to
		// updateInFlight, so it must remain so. (The call to f should have noticed
		// that and avoided making any updates that would cause the state to be
		// non-idle.)
		if !s.idle() {
			panic("jsonrpc2: updateInFlight transitioned to non-idle when already done")
		}
		return
	default:
	}

	if s.abortErr != nil || (s.idle() && s.shuttingDown(ErrUnknown) != nil) {
		if s.closer != nil {
			closer := s.closer
			s.closer = nil
			s.closingTransport = true
			go func() {
				err := closer.Close()
				c.updateInFlight(func(s *inFlightState) {
					s.closeErr = err
					s.closingTransport = false
				})
			}()
		}
		if s.idle() && !s.reading && !s.closingTransport {
			c.cancelHandlers()
			c.cancelControl()
			if c.onDone != nil {
				c.onDone()
			}
			close(c.done)
		}
	}
}

// idle reports whether the connection is in a state with no pending calls or
// notifications.
//
// If idle returns true, the readIncoming goroutine may still be running,
// but no other goroutines are doing work on behalf of the connection.
func (s *inFlightState) idle() bool {
	return len(s.outgoingCalls) == 0 && s.outgoingNotifications == 0 && s.writes == 0 && s.incoming == 0 && !s.handlerRunning
}

// shuttingDown reports whether the connection is in a state that should
// disallow new (incoming and outgoing) calls. It returns either nil or
// an error that is or wraps the provided errClosing.
func (s *inFlightState) shuttingDown(errClosing error) error {
	if s.abortErr != nil {
		return fmt.Errorf("%w: %w", errClosing, s.abortErr)
	}
	if s.readErr != nil {
		return fmt.Errorf("%w: %w", errClosing, s.readErr)
	}
	if s.writeErr != nil {
		return fmt.Errorf("%w: %w", errClosing, s.writeErr)
	}
	if s.connClosing {
		return errClosing
	}
	return nil
}

// incomingRequest is used to track an incoming request as it is being handled
type incomingRequest struct {
	*Request // the request being processed
	ctx      context.Context
	cancel   context.CancelFunc
	release  *releaser
}

// Reader abstracts the transport mechanics from the JSON RPC protocol.
// A Connection reads messages from the reader it was provided on construction,
// and assumes that each call to Read fully transfers a single message,
// or returns an error.
//
// A reader is not safe for concurrent use, it is expected it will be used by
// a single Connection in a safe manner.
type Reader interface {
	// Read gets the next message from the stream.
	Read(context.Context) (Message, error)
}

// Writer abstracts the transport mechanics from the JSON RPC protocol.
// A Connection writes messages using the writer it was provided on construction,
// and assumes that each call to Write fully transfers a single message,
// or returns an error.
//
// A writer must be safe for concurrent use, as writes may occur concurrently
// in practice: libraries may make calls or respond to requests asynchronously.
type Writer interface {
	// Write sends a message to the stream.
	Write(context.Context, Message) error
}

// A ConnectionConfig configures a bidirectional jsonrpc2 connection.
type ConnectionConfig struct {
	Reader          Reader                    // required
	Writer          Writer                    // required
	Closer          io.Closer                 // required
	Preempter       Preempter                 // optional
	Bind            func(*Connection) Handler // required
	OnDone          func()                    // optional
	OnInternalError func(error)               // optional
	// OnCallCanceled sends an optional protocol-specific notification after a
	// written call's context expires. The supplied context ends at local Close.
	OnCallCanceled func(context.Context, ID)
}

// NewConnection creates a new [Connection] object and starts processing
// incoming messages.
func NewConnection(ctx context.Context, cfg ConnectionConfig) *Connection {
	ctx, cancelHandlers := context.WithCancel(notDone{ctx})
	controlContext, cancelControl := context.WithCancel(notDone{ctx})
	c := &Connection{
		state:           inFlightState{closer: cfg.Closer},
		done:            make(chan struct{}),
		writer:          cfg.Writer,
		onDone:          cfg.OnDone,
		onInternalError: cfg.OnInternalError,
		cancelHandlers:  cancelHandlers,
		controlContext:  controlContext,
		cancelControl:   cancelControl,
		onCallCanceled:  cfg.OnCallCanceled,
	}
	c.handler = cfg.Bind(c)
	c.start(ctx, cfg.Reader, cfg.Preempter)
	return c
}

func (c *Connection) start(ctx context.Context, reader Reader, preempter Preempter) {
	c.updateInFlight(func(s *inFlightState) {
		select {
		case <-c.done:
			// The connection was already closed; don't start a goroutine to read it.
			return
		default:
		}

		// The goroutine started here will continue until the underlying stream is closed.
		s.reading = true
		go c.readIncoming(ctx, reader, preempter)
	})
}

// Notify invokes the target method but does not wait for a response.
// The params will be marshaled to JSON before sending over the wire, and will
// be handed to the method invoked.
func (c *Connection) Notify(ctx context.Context, method string, params any) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	attempted := false

	defer func() {
		if attempted {
			c.updateInFlight(func(s *inFlightState) {
				s.outgoingNotifications--
			})
		}
	}()

	c.updateInFlight(func(s *inFlightState) {
		// If the connection is shutting down, allow outgoing notifications only if
		// there is at least one call still in flight. The number of calls in flight
		// cannot increase once shutdown begins, and allowing outgoing notifications
		// may permit notifications that will cancel in-flight calls.
		if len(s.outgoingCalls) == 0 && len(s.incomingByID) == 0 {
			err = s.shuttingDown(ErrClientClosing)
			if err != nil {
				return
			}
		}
		s.outgoingNotifications++
		attempted = true
	})
	if err != nil {
		return err
	}

	notify, err := NewNotification(method, params)
	if err != nil {
		return fmt.Errorf("marshaling notify parameters: %v", err)
	}

	return c.write(ctx, notify)
}

// Call invokes the target method and returns an object that can be used to await the response.
// The params will be marshaled to JSON before sending over the wire, and will
// be handed to the method invoked.
// You do not have to wait for the response, it can just be ignored if not needed.
// If sending the call failed, the response will be ready and have the error in it.
func (c *Connection) Call(ctx context.Context, method string, params any) *AsyncCall {
	// Generate a new request identifier.
	id := Int64ID(atomic.AddInt64(&c.seq, 1))

	ac := &AsyncCall{
		id:    id,
		ready: make(chan struct{}),
	}
	if scope, ok := ctx.Value(asyncKey).(*releaser); ok && scope.connection == c {
		ac.reentrant = true
	}
	if err := ctx.Err(); err != nil {
		ac.retire(&Response{ID: id, Error: err})
		return ac
	}
	// When this method returns, either ac is retired, or the request has been
	// written successfully and the call is awaiting a response (to be provided by
	// the readIncoming goroutine).

	call, err := NewCall(ac.id, method, params)
	if err != nil {
		ac.retire(&Response{ID: id, Error: fmt.Errorf("marshaling call parameters: %w", err)})
		return ac
	}

	c.updateInFlight(func(s *inFlightState) {
		err = s.shuttingDown(ErrClientClosing)
		if err != nil {
			return
		}
		if s.outgoingCalls == nil {
			s.outgoingCalls = make(map[ID]*AsyncCall)
		}
		s.outgoingCalls[ac.id] = ac
	})
	if err != nil {
		ac.retire(&Response{ID: id, Error: err})
		return ac
	}

	if err := c.write(ctx, call); err != nil {
		c.Retire(ac, err)
	} else {
		// Register only after a complete successful write. Cancellation of an
		// unsent/partial frame must not emit a second protocol message.
		c.updateInFlight(func(s *inFlightState) {
			if s.outgoingCalls[ac.id] == ac {
				ac.stopCancellation = context.AfterFunc(ctx, func() {
					if c.Retire(ac, ctx.Err()) && c.onCallCanceled != nil {
						c.onCallCanceled(c.controlContext, ac.id)
					}
				})
			}
		})
	}
	return ac
}

// Retire stops tracking the call, and reports err as its terminal error.
//
// Retire is safe to call multiple times: if the call is already no longer
// tracked, Retire is a no op.
func (c *Connection) Retire(ac *AsyncCall, err error) bool {
	retired := false
	c.updateInFlight(func(s *inFlightState) {
		if s.outgoingCalls[ac.id] == ac {
			delete(s.outgoingCalls, ac.id)
			ac.retire(&Response{ID: ac.id, Error: err})
			retired = true
		}
	})
	return retired
}

// Async releases an incoming message's serialization boundary, letting later
// messages start while its handler continues. A handler that does not call it
// delays every later request and notification until it returns.
//
// Async must be called at most once on each handler context and its descendants.
func Async(ctx context.Context) {
	if r, ok := ctx.Value(asyncKey).(*releaser); ok {
		r.release(false)
	}
}

type asyncKeyType struct{}

var asyncKey = asyncKeyType{}

// A releaser implements concurrency safe 'releasing' of async requests. (A
// request is released when it is allowed to run concurrent with other
// requests, via a call to [Async].)
type releaser struct {
	connection *Connection
	mu         sync.Mutex
	ch         chan struct{}
	released   bool
}

// release closes the associated channel. If soft is set, multiple calls to
// release are allowed.
func (r *releaser) release(soft bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.released {
		if !soft {
			panic("jsonrpc2.Async called multiple times")
		}
	} else {
		close(r.ch)
		r.released = true
	}
}

type AsyncCall struct {
	id               ID
	ready            chan struct{} // closes after response and prefix are set
	response         *Response
	notificationDone <-chan struct{}
	reentrant        bool
	stopCancellation func() bool // accessed under the connection state lock
}

// ID used for this call.
// This can be used to cancel the call if needed.
func (ac *AsyncCall) ID() ID { return ac.id }

// retire processes the response to the call.
//
// It is an error to call retire more than once: retire is guarded by the
// connection's outgoingCalls map.
func (ac *AsyncCall) retire(response *Response) {
	select {
	case <-ac.ready:
		panic(fmt.Sprintf("jsonrpc2: retire called twice for ID %v", ac.id))
	default:
	}

	if ac.stopCancellation != nil {
		ac.stopCancellation()
	}
	ac.response = response
	close(ac.ready)
}

// Await waits for (and decodes) the results of a Call.
// The response will be unmarshaled from JSON into the result.
// Numbers in interface values are decoded as json.Number.
//
// If the call is cancelled due to context cancellation, the result is
// ctx.Err().
func (ac *AsyncCall) Await(ctx context.Context, result any) error {
	return ac.await(ctx, result, false)
}

// AwaitOrdered also waits for the notification prefix received before this
// response. Calls initiated with this connection's handler context bypass the
// barrier: a callback must not wait on its own completion or reverse requests.
func (ac *AsyncCall) AwaitOrdered(ctx context.Context, result any) error {
	return ac.await(ctx, result, true)
}

func (ac *AsyncCall) await(ctx context.Context, result any, ordered bool) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ac.ready:
	}
	if ordered && !ac.reentrant && ac.notificationDone != nil {
		select {
		case <-ac.notificationDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if ac.response.Error != nil {
		// Interrupting a canceled call's write can close the stream, so the reader
		// may retire the call with EOF first. Report the cancellation that caused it.
		if err := ctx.Err(); err != nil {
			return err
		}
		return ac.response.Error
	}
	if result == nil {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(ac.response.Result))
	decoder.UseNumber()
	if err := decoder.Decode(result); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("invalid trailing response data")
	}
	return nil
}

// Cancel cancels the Context passed to the Handle call for the inbound message
// with the given ID.
//
// Cancel will not complain if the ID is not a currently active message, and it
// will not cause any messages that have not arrived yet with that ID to be
// cancelled.
func (c *Connection) Cancel(id ID) {
	var req *incomingRequest
	c.updateInFlight(func(s *inFlightState) {
		req = s.incomingByID[id]
	})
	if req != nil {
		req.cancel()
	}
}

// Wait blocks until the connection is fully closed, but does not close it.
func (c *Connection) Wait() error {
	return c.wait(true)
}

// wait for the connection to close, and aggregates the most cause of its
// termination, if abnormal.
//
// The fromWait argument allows this logic to be shared with Close, where we
// only want to expose the closeErr.
//
// (Previously, Wait also only returned the closeErr, which was misleading if
// the connection was broken for another reason).
func (c *Connection) wait(fromWait bool) error {
	var err error
	<-c.done
	c.updateInFlight(func(s *inFlightState) {
		if fromWait {
			err = s.abortErr
			if err == nil && !errors.Is(s.readErr, io.EOF) {
				err = s.readErr
			}
			if err == nil && !errors.Is(s.writeErr, io.EOF) {
				err = s.writeErr
			}
		}
		if err == nil {
			err = s.closeErr
		}
	})
	return err
}

// Close stops admitting work, abandons outgoing calls and cancels inbound call
// contexts. It drains accepted writes and notification handlers before closing
// the underlying stream. Callers needing immediate I/O interruption use Abort.
//
// After the start of a Close, notification requests (that lack IDs and do not
// receive responses) will continue to be passed to the Preempter, but calls
// with IDs will receive immediate responses with ErrServerClosing, and no new
// requests (not even notifications!) will be enqueued to the Handler.
func (c *Connection) Close() error {
	// Stop handling new requests, and interrupt the reader (by closing the
	// connection) as soon as the active requests finish.
	c.cancelControl()
	c.updateInFlight(func(s *inFlightState) {
		s.connClosing = true
		for _, req := range s.incomingByID {
			req.cancel()
		}
		for id, call := range s.outgoingCalls {
			call.retire(&Response{ID: id, Error: ErrClientClosing})
		}
		s.outgoingCalls = nil
	})
	return c.wait(false)
}

// Abort interrupts physical I/O and cooperative handlers. Unlike graceful
// Close, it does not wait for an unread output stream to drain. Clean input EOF
// does not use this path, because the peer may still be reading final replies.
func (c *Connection) Abort(err error) {
	if err == nil {
		err = ErrClientClosing
	}
	c.updateInFlight(func(s *inFlightState) {
		select {
		case <-c.done:
			return
		default:
		}
		if s.abortErr == nil {
			s.abortErr = err
		}
		c.cancelHandlers()
		c.cancelControl()
		for id, call := range s.outgoingCalls {
			call.retire(&Response{ID: id, Error: s.abortErr})
		}
		s.outgoingCalls = nil
	})
}

// readIncoming collects inbound messages from the reader and delivers them, either responding
// to outgoing calls or feeding requests to the queue.
func (c *Connection) readIncoming(ctx context.Context, reader Reader, preempter Preempter) {
	var err error
	for {
		var msg Message
		msg, err = reader.Read(ctx)
		if err != nil {
			// Built-in framing reports recoverable codec failures separately from
			// I/O failure. Never answer malformed response/notification shapes.
			var malformed *DecodeError
			if errors.As(err, &malformed) {
				if response := malformed.Response(); response != nil {
					if writeErr := c.write(ctx, response); writeErr != nil {
						err = writeErr
						break
					}
				}
				continue
			}
			break
		}

		switch msg := msg.(type) {
		case *Request:
			c.acceptRequest(ctx, msg, preempter)

		case *Response:
			c.updateInFlight(func(s *inFlightState) {
				if ac, ok := s.outgoingCalls[msg.ID]; ok {
					delete(s.outgoingCalls, msg.ID)
					// Capture now, not when Await wakes: later notifications must
					// not extend this response's completion boundary.
					ac.notificationDone = s.lastNotification
					ac.retire(msg)
				} else {
					// TODO: How should we report unexpected responses?
				}
			})

		default:
			c.internalErrorf("Read returned an unexpected message of type %T", msg)
		}
	}

	c.updateInFlight(func(s *inFlightState) {
		s.reading = false
		s.readErr = err

		// Retire any outgoing requests that were still in flight: with the Reader no
		// longer being processed, they necessarily cannot receive a response.
		for id, ac := range s.outgoingCalls {
			ac.retire(&Response{ID: id, Error: err})
		}
		s.outgoingCalls = nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		c.Abort(err)
	}
}

// acceptRequest either handles msg synchronously or enqueues it to be handled
// asynchronously.
func (c *Connection) acceptRequest(ctx context.Context, msg *Request, preempter Preempter) {
	// In theory notifications cannot be cancelled, but we build them a cancel
	// context anyway.
	reqCtx, cancel := context.WithCancel(ctx)
	req := &incomingRequest{
		Request: msg,
		ctx:     reqCtx,
		cancel:  cancel,
		release: &releaser{connection: c, ch: make(chan struct{})},
	}

	// If the request is a call, add it to the incoming map so it can be
	// cancelled (or responded) by ID.
	var err error
	c.updateInFlight(func(s *inFlightState) {
		s.incoming++

		if req.IsCall() {
			if s.incomingByID[req.ID] != nil {
				err = fmt.Errorf("%w: request ID %v already in use", ErrInvalidRequest, req.ID)
				req.ID = ID{} // Don't misattribute this error to the existing request.
				return
			}

			if s.incomingByID == nil {
				s.incomingByID = make(map[ID]*incomingRequest)
			}
			s.incomingByID[req.ID] = req

			// When shutting down, reject all new Call requests, even if they could
			// theoretically be handled by the preempter. The preempter could return
			// ErrAsyncResponse, which would increase the amount of work in flight
			// when we're trying to ensure that it strictly decreases.
			err = s.shuttingDown(ErrServerClosing)
		}
	})
	if err != nil {
		c.processResult("acceptRequest", req, nil, err)
		return
	}

	if preempter != nil {
		result, err := preempter.Preempt(req.ctx, req.Request)

		if !errors.Is(err, ErrNotHandled) {
			c.processResult("Preempt", req, result, err)
			return
		}
	}

	c.updateInFlight(func(s *inFlightState) {
		// If the connection is shutting down, don't enqueue anything to the
		// handler — not even notifications. That ensures that if the handler
		// continues to make progress, it will eventually become idle and
		// close the connection.
		err = s.shuttingDown(ErrServerClosing)
		if err != nil {
			return
		}

		// Keep the reader independent of callback completion: responses a running
		// callback awaits never enter this queue. Requests queue so they do not
		// start before the notifications received ahead of them are handled.
		if !req.IsCall() {
			s.lastNotification = req.release.ch
		}
		s.handlerQueue = append(s.handlerQueue, req)
		if !s.handlerRunning {
			s.handlerRunning = true
			go c.handleAsync()
		}
	})
	if err != nil {
		c.processResult("acceptRequest", req, nil, err)
	}
}

// handleAsync starts request and notification handlers in receive order. A
// handler must return or call Async before the following message can start.
func (c *Connection) handleAsync() {
	for {
		var req *incomingRequest
		c.updateInFlight(func(s *inFlightState) {
			if len(s.handlerQueue) > 0 {
				req = s.handlerQueue[0]
				s.handlerQueue[0] = nil // Do not retain processed payloads in the backing array.
				s.handlerQueue = s.handlerQueue[1:]
			} else {
				s.handlerQueue = nil
				s.handlerRunning = false
			}
		})
		if req == nil {
			return
		}

		go c.handleRequest(req)
		<-req.release.ch
	}
}

func (c *Connection) handleRequest(req *incomingRequest) {
	if req.ctx.Err() != nil {
		c.processResult(c.handler, req, nil, ErrRequestCancelled)
		return
	}
	ctx := context.WithValue(req.ctx, asyncKey, req.release)
	result, err := c.handler.Handle(ctx, req.Request)
	c.processResult(c.handler, req, result, err)
}

// processResult processes the result of a request and, if appropriate, sends a response.
func (c *Connection) processResult(from any, req *incomingRequest, result any, err error) error {
	defer req.release.release(true)
	switch err {
	case ErrNotHandled, ErrMethodNotFound:
		data, _ := jsonMarshal(req.Method)
		err = &WireError{Code: CodeMethodNotFound, Message: "Method not found", Data: data}
	}
	if err != nil {
		result = nil
		var coded *WireError
		if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && !errors.As(err, &coded) {
			err = ErrRequestCancelled
		}
	}
	if req.IsCall() {
		if result == nil && err == nil {
			err = ErrInternal
		}
		response, encodeErr := NewResponse(req.ID, result, err)
		if encodeErr != nil {
			// An application result that is not JSON is an ordinary request
			// failure, not a reason to terminate the hosting process.
			if c.onInternalError != nil {
				c.onInternalError(fmt.Errorf("encoding %q result: %w", req.Method, encodeErr))
			}
			response, _ = NewResponse(req.ID, nil, ErrInternal)
		}
		// Retire before publishing: the peer can reuse the ID once it reads it.
		// A local Close cancels inbound calls; do not start an unread response
		// write for those canceled handlers while trying to disconnect.
		deliver := true
		c.updateInFlight(func(s *inFlightState) {
			deliver = s.abortErr == nil && !(s.connClosing && req.ctx.Err() != nil)
			if s.incomingByID[req.ID] == req {
				delete(s.incomingByID, req.ID)
			}
		})
		if deliver {
			if observer, ok := c.handler.(interface{ BeforeResponse(*Request, *Response) }); ok {
				observer.BeforeResponse(req.Request, response)
			}
			err = c.write(notDone{req.ctx}, response)
		}
	}
	// Notifications have no response, even if a generic handler returned a
	// result or failed. ACP method-kind guards run before application dispatch.
	req.cancel()
	c.updateInFlight(func(s *inFlightState) {
		if s.incoming == 0 {
			panic("jsonrpc2: processResult called when incoming count is already zero")
		}
		s.incoming--
	})
	return err
}

// write finishes already accepted work during graceful Close or clean input
// half-close. Admission of NEW work is checked by Call/Notify/acceptRequest.
func (c *Connection) write(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var unavailable error
	c.updateInFlight(func(s *inFlightState) {
		switch {
		case s.abortErr != nil:
			unavailable = fmt.Errorf("%w: %w", ErrServerClosing, s.abortErr)
		case s.writeErr != nil:
			unavailable = fmt.Errorf("%w: %w", ErrServerClosing, s.writeErr)
		case s.closer == nil:
			unavailable = ErrServerClosing
		default:
			s.writes++
		}
	})
	if unavailable != nil {
		return unavailable
	}
	defer c.updateInFlight(func(s *inFlightState) { s.writes-- })
	err := c.writer.Write(ctx, msg)
	// Queued cancellation/rejection need not break the stream. An interrupted
	// partial frame may additionally report ErrClosedPipe: that stream is gone.
	if err != nil && !errors.Is(err, ErrRejected) && (ctx.Err() == nil || errors.Is(err, io.ErrClosedPipe)) {
		c.updateInFlight(func(s *inFlightState) {
			if s.writeErr == nil {
				s.writeErr = err
			}
		})
		c.Abort(err)
	}
	return err
}

// internalErrorf reports an internal error. By default it panics, but if
// c.onInternalError is non-nil it instead calls that and returns an error
// wrapping ErrInternal.
func (c *Connection) internalErrorf(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	if c.onInternalError == nil {
		panic("jsonrpc2: " + err.Error())
	}
	c.onInternalError(err)

	return fmt.Errorf("%w: %v", ErrInternal, err)
}

// notDone is a context.Context wrapper that returns a nil Done channel.
type notDone struct{ ctx context.Context }

func (ic notDone) Value(key any) any {
	return ic.ctx.Value(key)
}

func (notDone) Done() <-chan struct{}       { return nil }
func (notDone) Err() error                  { return nil }
func (notDone) Deadline() (time.Time, bool) { return time.Time{}, false }

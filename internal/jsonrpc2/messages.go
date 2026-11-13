// Copyright 2018 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package jsonrpc2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	internaljson "github.com/spachava753/acp-sdk/internal/json"
)

// ID is a Request identifier, which is defined by the spec to be a string, integer, or null.
// https://www.jsonrpc.org/specification#request_object
type ID struct {
	value any
	null  bool
}

// MakeID constructs a present request ID. A nil value denotes explicit JSON
// null, not an absent ID. Use the zero ID value for a notification.
//
// Integers are accepted as int, int64, or json.Number. Legacy float64 values
// must be finite integral int64 values; precision already lost by a caller's
// float64 conversion cannot be recovered. Prefer json.Number for decoded IDs.
func MakeID(v any) (ID, error) {
	switch v := v.(type) {
	case nil:
		return ID{null: true}, nil
	case int:
		return Int64ID(int64(v)), nil
	case int64:
		return Int64ID(v), nil
	case json.Number:
		n, err := v.Int64()
		if err == nil {
			return Int64ID(n), nil
		}
	case float64:
		if !math.IsNaN(v) && !math.IsInf(v, 0) && math.Trunc(v) == v && v >= -0x1p63 && v < 0x1p63 {
			return Int64ID(int64(v)), nil
		}
	case string:
		return StringID(v), nil
	}
	return ID{}, fmt.Errorf("%w: invalid ID %v (%T)", ErrInvalidRequest, v, v)
}

// Message is the interface to all jsonrpc2 message types.
// They share no common functionality, but are a closed set of concrete types
// that are allowed to implement this interface. The message types are *Request
// and *Response.
type Message interface {
	// marshal builds the wire form from the API form.
	// It is private, which makes the set of Message implementations closed.
	marshal(to *wireCombined)
}

// Request is a Message sent to a peer to request behavior.
// If it has an ID it is a call, otherwise it is a notification.
type Request struct {
	// ID of this request, used to tie the Response back to the request.
	// This is the zero ID for notifications; explicit null is a valid call ID.
	ID ID
	// Method is a string containing the method name to invoke.
	Method string
	// Params is either a struct or an array with the parameters of the method.
	Params json.RawMessage
	// Extra is additional information that does not appear on the wire. It can be
	// used to pass information from the application to the underlying transport.
	Extra any
}

// Response is a Message used as a reply to a call Request.
// It will have the same ID as the call it is a response to.
type Response struct {
	// result is the content of the response.
	Result json.RawMessage
	// err is set only if the call failed.
	Error error
	// id of the request this is a response to.
	ID ID
	// Extra is additional information that does not appear on the wire. It can be
	// used to pass information from the underlying transport to the application.
	Extra any
}

// StringID creates a new string request identifier.
func StringID(s string) ID { return ID{value: s} }

// Int64ID creates a new integer request identifier.
func Int64ID(i int64) ID { return ID{value: i} }

// IsValid returns true if the ID is a valid identifier.
// The default value for ID will return false.
func (id ID) IsValid() bool { return id.value != nil || id.null }

// Raw returns the underlying string, int64, or nil value. Use IsValid to
// distinguish an explicit null ID from an absent ID.
func (id ID) Raw() any { return id.value }

// MarshalJSON encodes both an explicit null and a zero/absent ID as null.
// The message envelope decides whether an absent ID member is omitted.
func (id ID) MarshalJSON() ([]byte, error) { return jsonMarshal(id.Raw()) }

// NewNotification constructs a new Notification message for the supplied
// method and parameters.
func NewNotification(method string, params any) (*Request, error) {
	p, merr := marshalToRaw(params)
	return &Request{Method: method, Params: p}, merr
}

// NewCall constructs a new Call message for the supplied ID, method and
// parameters.
func NewCall(id ID, method string, params any) (*Request, error) {
	p, merr := marshalToRaw(params)
	return &Request{ID: id, Method: method, Params: p}, merr
}

func (msg *Request) IsCall() bool { return msg.ID.IsValid() }

func (msg *Request) marshal(to *wireCombined) {
	if msg.IsCall() {
		to.ID = &msg.ID
	}
	to.Method = &msg.Method
	to.Params = msg.Params
}

// NewResponse constructs a new Response message that is a reply to the
// supplied. If err is set result may be ignored.
func NewResponse(id ID, result any, rerr error) (*Response, error) {
	if rerr != nil {
		return &Response{ID: id, Error: rerr}, nil
	}
	r, merr := marshalToRaw(result)
	if merr == nil && len(r) == 0 {
		r = json.RawMessage("null")
	}
	return &Response{ID: id, Result: r}, merr
}

func (msg *Response) marshal(to *wireCombined) {
	to.ID = &msg.ID
	to.Error = toWireError(msg.Error)
	if to.Error == nil {
		to.Result = msg.Result
		if len(to.Result) == 0 {
			to.Result = json.RawMessage("null")
		}
	}
}

func toWireError(err error) *WireError {
	if err == nil {
		// no error, the response is complete
		return nil
	}
	if err, ok := err.(*WireError); ok {
		// already a wire error, just use it
		return err
	}
	var wrapped *WireError
	if errors.As(err, &wrapped) && wrapped != nil {
		// Preserve the structured payload and raw extras, while retaining the
		// caller's outer context message exactly as Error() presents it.
		result := *wrapped
		result.Message = err.Error()
		return &result
	}
	return &WireError{Message: err.Error()}
}

func EncodeMessage(msg Message) ([]byte, error) {
	wire := wireCombined{VersionTag: wireVersion}
	msg.marshal(&wire)
	data, err := jsonMarshal(&wire)
	if err != nil {
		return nil, fmt.Errorf("marshaling jsonrpc message: %w", err)
	}
	return data, nil
}

// EncodeIndent is like EncodeMessage, but honors indents.
// TODO(rfindley): refactor so that this concern is handled independently.
// Perhaps we should pass in a json.Encoder?
func EncodeIndent(msg Message, prefix, indent string) ([]byte, error) {
	wire := wireCombined{VersionTag: wireVersion}
	msg.marshal(&wire)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(prefix, indent)
	if err := enc.Encode(&wire); err != nil {
		return nil, fmt.Errorf("marshaling jsonrpc message: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// DecodeError is a rejected JSON-RPC frame, distinct from a transport failure.
// Unwrap exposes its structured protocol error. Response returns the error
// frame to send, or nil for malformed responses/notifications that need no reply.
// The connection can continue reading after either form of DecodeError.
type DecodeError struct {
	cause    *WireError
	response *Response
}

func (err *DecodeError) Error() string       { return err.cause.Error() }
func (err *DecodeError) Unwrap() error       { return err.cause }
func (err *DecodeError) Response() *Response { return err.response }

func decodeError(cause error, reply bool, data json.RawMessage) *DecodeError {
	protocol := *toWireError(cause)
	protocol.Data = data
	err := &DecodeError{cause: &protocol}
	if reply {
		err.response = &Response{Error: &protocol}
	}
	return err
}

func isJSONString(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '"'
}

func DecodeMessage(data []byte) (Message, error) {
	// Validate the complete frame before decoding any prefix or choosing its
	// kind. A second value or trailing garbage is a parse error, not a message.
	if !json.Valid(data) {
		diagnostic, _ := jsonMarshal(map[string]string{"line": string(data)})
		return nil, decodeError(ErrParse, true, diagnostic)
	}
	var fields map[string]json.RawMessage
	if err := internaljson.Unmarshal(data, &fields); err != nil || fields == nil {
		return nil, decodeError(ErrInvalidRequest, true, nil)
	}
	methodRaw, hasMethod := fields["method"]
	_, hasID := fields["id"]
	result, hasResult := fields["result"]
	errorRaw, hasError := fields["error"]
	methodIsString := hasMethod && isJSONString(methodRaw)
	// Shape, not successful validation, determines whether replying would
	// create a response loop or answer a one-way notification.
	responseShape := !hasMethod && (hasResult || hasError)
	notificationShape := methodIsString && !hasID
	invalid := func() (Message, error) {
		return nil, decodeError(ErrInvalidRequest, !responseShape && !notificationShape, nil)
	}
	var version string
	if !isJSONString(fields["jsonrpc"]) || internaljson.Unmarshal(fields["jsonrpc"], &version) != nil || version != wireVersion {
		return invalid()
	}
	var id ID
	if hasID {
		// Payloads remain raw; UseNumber applies only to the envelope ID.
		dec := internaljson.NewDecoder(bytes.NewReader(fields["id"]))
		dec.UseNumber()
		var value any
		if err := dec.Decode(&value); err != nil {
			return invalid()
		}
		var err error
		id, err = MakeID(value)
		if err != nil {
			return invalid()
		}
	}
	if hasMethod {
		if !methodIsString || hasResult || hasError {
			return invalid()
		}
		var method string
		if err := internaljson.Unmarshal(methodRaw, &method); err != nil {
			return invalid()
		}
		params := fields["params"]
		trimmed := bytes.TrimSpace(params)
		if bytes.Equal(trimmed, []byte("null")) {
			params = nil
		} else if len(trimmed) > 0 && trimmed[0] != '{' && trimmed[0] != '[' {
			return invalid()
		}
		return &Request{ID: id, Method: method, Params: params}, nil
	}
	if !hasID || hasResult == hasError {
		return invalid()
	}
	if hasError {
		var protocol WireError
		if err := internaljson.Unmarshal(errorRaw, &protocol); err != nil {
			return invalid()
		}
		return &Response{ID: id, Error: &protocol}, nil
	}
	return &Response{ID: id, Result: result}, nil
}

func marshalToRaw(obj any) (json.RawMessage, error) {
	if obj == nil {
		return nil, nil
	}
	data, err := jsonMarshal(obj)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// jsonMarshal marshals obj to JSON like json.Marshal but without HTML escaping.
func jsonMarshal(obj any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		return nil, err
	}
	// json.Encoder.Encode adds a trailing newline. Trim it to be consistent with json.Marshal.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

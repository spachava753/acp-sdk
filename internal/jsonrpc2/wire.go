// Copyright 2018 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package jsonrpc2

import (
	"bytes"
	"encoding/json"
	"fmt"

	internaljson "github.com/spachava753/acp-sdk/internal/json"
)

// This file contains the go forms of the wire specification.
// see http://www.jsonrpc.org/specification for details

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// CodeRequestCancelled is the ACP/LSP per-request cancellation error code.
const CodeRequestCancelled = -32800

var (
	// ErrParse is used when invalid JSON was received by the server.
	ErrParse = NewError(CodeParseError, "parse error")
	// ErrInvalidRequest is used when the JSON sent is not a valid Request object.
	ErrInvalidRequest = NewError(CodeInvalidRequest, "invalid request")
	// ErrMethodNotFound should be returned by the handler when the method does
	// not exist / is not available.
	ErrMethodNotFound = NewError(CodeMethodNotFound, "method not found")
	// ErrInvalidParams should be returned by the handler when method
	// parameter(s) were invalid.
	ErrInvalidParams = NewError(CodeInvalidParams, "invalid params")
	// ErrInternal indicates a failure to process a call correctly
	ErrInternal = NewError(CodeInternalError, "internal error")
	// ErrRequestCancelled reports cancellation before an inbound handler runs.
	ErrRequestCancelled = NewError(CodeRequestCancelled, "Request cancelled")

	// The following errors are not part of the json specification, but
	// compliant extensions specific to this implementation.

	// ErrUnknown should be used for all non coded errors.
	ErrUnknown = NewError(-32001, "unknown error")
	// ErrServerClosing is returned for calls that arrive while the server is closing.
	ErrServerClosing = NewError(-32004, "server is closing")
	// ErrClientClosing is a dummy error returned for calls initiated while the client is closing.
	ErrClientClosing = NewError(-32003, "client is closing")

	// ErrRejected may be wrapped to return errors from calls to Writer.Write
	// that signal that the request was rejected by the transport layer as
	// invalid.
	//
	// Such failures do not indicate that the connection is broken, but rather
	// should be returned to the caller to indicate that the specific request is
	// invalid in the current context.
	ErrRejected = NewError(-32005, "rejected by transport")
)

const wireVersion = "2.0"

// wireCombined is the outgoing envelope. Pointers preserve present null IDs
// and empty methods independently of omitted notification/response members.
type wireCombined struct {
	VersionTag string          `json:"jsonrpc"`
	ID         *ID             `json:"id,omitempty"`
	Method     *string         `json:"method,omitempty"`
	Params     json.RawMessage `json:"params,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      *WireError      `json:"error,omitempty"`
}

// WireError represents a structured error in a Response.
type WireError struct {
	// Code is an error code indicating the type of failure.
	Code int64 `json:"code"`
	// Message is a short description of the error.
	Message string `json:"message"`
	// Data is optional structured data containing additional information about the error.
	Data json.RawMessage `json:"data,omitempty"`

	// Unknown error fields survive raw transport/runtime forwarding. This is
	// not a raw-versus-typed runtime flag; schema projection is an explicit codec.
	extra map[string]json.RawMessage
}

// MarshalJSON retains unknown error fields without allowing them to override
// the public code, message, or data values.
func (err WireError) MarshalJSON() ([]byte, error) {
	type plain WireError
	if len(err.extra) == 0 {
		return jsonMarshal(plain(err))
	}
	fields := make(map[string]any, len(err.extra)+3)
	for name, value := range err.extra {
		fields[name] = value
	}
	fields["code"] = err.Code
	fields["message"] = err.Message
	if len(err.Data) > 0 {
		fields["data"] = err.Data
	}
	return jsonMarshal(fields)
}

// UnmarshalJSON validates required members and keeps omitted data distinct
// from explicit null. Decode into a temporary so a failure cannot leave a
// partially updated error value.
func (err *WireError) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if e := internaljson.Unmarshal(data, &fields); e != nil {
		return e
	}
	if fields == nil {
		return fmt.Errorf("JSON-RPC error must be an object")
	}
	code, hasCode := fields["code"]
	message, hasMessage := fields["message"]
	if !hasCode || bytes.Equal(bytes.TrimSpace(code), []byte("null")) || !hasMessage || !isJSONString(message) {
		return fmt.Errorf("JSON-RPC error requires an integer code and string message")
	}
	var decoded WireError
	if e := internaljson.Unmarshal(code, &decoded.Code); e != nil {
		return e
	}
	if e := internaljson.Unmarshal(message, &decoded.Message); e != nil {
		return e
	}
	decoded.Data = fields["data"]
	delete(fields, "code")
	delete(fields, "message")
	delete(fields, "data")
	if len(fields) > 0 {
		decoded.extra = fields
	}
	*err = decoded
	return nil
}

// NewError returns an error that will encode on the wire correctly.
// The standard codes are made available from this package, this function should
// only be used to build errors for application specific codes as allowed by the
// specification.
func NewError(code int64, message string) error {
	return &WireError{
		Code:    code,
		Message: message,
	}
}

func (err *WireError) Error() string {
	return err.Message
}

func (err *WireError) Is(other error) bool {
	w, ok := other.(*WireError)
	if !ok {
		return false
	}
	return err.Code == w.Code
}

// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// Package jsonrpc exposes JSON-RPC v2 message helpers for custom ACP transports.
package jsonrpc

import "github.com/spachava753/acp-sdk/internal/jsonrpc2"

type (
	// ID is a JSON-RPC request ID.
	ID = jsonrpc2.ID
	// Message is a JSON-RPC message.
	Message = jsonrpc2.Message
	// Request is a JSON-RPC request.
	Request = jsonrpc2.Request
	// Response is a JSON-RPC response.
	Response = jsonrpc2.Response
	// Error is a structured error in a JSON-RPC response.
	Error = jsonrpc2.WireError
)

// MakeID constructs a present request ID from a string, int, int64, json.Number,
// or finite integral float64 in the int64 range. A nil value is an explicit
// JSON null ID, valid for a call; a zero ID value represents no ID/notification.
//
// Decode IDs using json.Number to avoid precision loss before calling MakeID.
// Already-rounded float64 inputs cannot recover the original integer value.
// Unsupported types, fractional values and out-of-range numbers return errors.
func MakeID(v any) (ID, error) {
	return jsonrpc2.MakeID(v)
}

// EncodeMessage serializes a JSON-RPC message to its wire format.
func EncodeMessage(msg Message) ([]byte, error) {
	return jsonrpc2.EncodeMessage(msg)
}

// DecodeMessage deserializes exactly one complete JSON-RPC wire message.
// It distinguishes requests from notifications by ID-member presence, including
// explicit null, and validates envelope/error shapes. Errors expose a structured
// *Error through errors.As and implement interface{ Response() *Response }.
// Custom transports can use that method's non-nil response for an error reply;
// nil means no reply should be sent. SDK connections handle these frame errors
// internally without treating them as transport failures.
func DecodeMessage(data []byte) (Message, error) {
	return jsonrpc2.DecodeMessage(data)
}

// Standard JSON-RPC 2.0 error codes.
// See https://www.jsonrpc.org/specification#error_object
const (
	// CodeParseError indicates invalid JSON was received by the server.
	CodeParseError = jsonrpc2.CodeParseError
	// CodeInvalidRequest indicates the JSON sent is not a valid Request object.
	CodeInvalidRequest = jsonrpc2.CodeInvalidRequest
	// CodeMethodNotFound indicates the method does not exist or is not available.
	CodeMethodNotFound = jsonrpc2.CodeMethodNotFound
	// CodeInvalidParams indicates invalid method parameter(s).
	CodeInvalidParams = jsonrpc2.CodeInvalidParams
	// CodeInternalError indicates an internal JSON-RPC error.
	CodeInternalError = jsonrpc2.CodeInternalError
)

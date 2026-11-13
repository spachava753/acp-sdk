// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

package acptest

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"testing"
	"time"
)

// EqualJSON reports an error unless got and want hold the same single JSON
// value. Numbers compare by their exact text, so large integers stay exact.
func EqualJSON(t testing.TB, got []byte, want string) {
	t.Helper()
	g, ok := decodeOne(t, got)
	if !ok {
		return
	}
	w, ok := decodeOne(t, []byte(want))
	if !ok {
		return
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("JSON = %s\nwant   %s", got, want)
	}
}

// EqualMarshaled marshals v with encoding/json and compares it with want.
func EqualMarshaled(t testing.TB, v any, want string) {
	t.Helper()
	EqualJSON(t, Marshal(t, v), want)
}

// RoundTrip decodes raw into target and checks that encoding it again yields
// the same JSON value.
func RoundTrip(t testing.TB, raw string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		t.Fatalf("decoding %T from %s: %v", target, raw, err)
	}
	EqualMarshaled(t, target, raw)
}

// Marshal encodes v with encoding/json, failing the test on error.
func Marshal(t testing.TB, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encoding %T: %v", v, err)
	}
	return data
}

func decodeOne(t testing.TB, data []byte) (any, bool) {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		t.Errorf("not JSON: %q: %v", data, err)
		return nil, false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		t.Errorf("more than one JSON value: %q", data)
		return nil, false
	}
	return v, true
}

// Result is the outcome of a call started with Go.
type Result[T any] struct {
	Value T
	Err   error
}

// Go runs call in a goroutine. The channel is buffered, so call can finish
// even if the test stops waiting.
func Go[T any](call func() (T, error)) <-chan Result[T] {
	result := make(chan Result[T], 1)
	go func() {
		value, err := call()
		result <- Result[T]{Value: value, Err: err}
	}()
	return result
}

// Await returns the next value from ch, failing the test after Timeout.
func Await[T any](t testing.TB, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(Timeout):
		t.Fatal("timed out waiting for the operation")
		var zero T
		return zero
	}
}

// Value awaits a call started with Go and fails the test if it returned an
// error.
func Value[T any](t testing.TB, result <-chan Result[T]) T {
	t.Helper()
	r := Await(t, result)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	return r.Value
}

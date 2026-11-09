// Copyright 2026 The ACP Go SDK Authors.
// SPDX-License-Identifier: Apache-2.0

// Package schema_test checks the SDK's typed ACP payloads and methods against
// literal wire JSON. Codec tests decode and encode the generated types directly:
// they cover round trips, rejected payloads, recovery from invalid optional
// data, and the distinction between omitted, null and present fields. Route
// tests connect an SDK client or agent to an independent peer and check that
// each typed method sends its own method name and params, returns the typed
// result, and that each incoming message reaches exactly the matching handler.
package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/conformance/internal/acptest"
)

func ptr[T any](v T) *T { return &v }

// alloc returns a new zero T, for tables that decode into several types.
func alloc[T any]() any { return new(T) }

// decode unmarshals input into target, failing the test on error.
func decode(t *testing.T, input string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(input), target); err != nil {
		t.Fatalf("decoding %T from %s: %v", target, input, err)
	}
}

// A decodeCase decodes input into a value from new. If reject is set, decoding
// must fail. Otherwise the decoded value must encode to want, or to input when
// want is empty.
type decodeCase struct {
	name   string
	new    func() any
	input  string
	want   string
	reject bool
}

func runDecodeCases(t *testing.T, cases []decodeCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			value := tt.new()
			err := json.Unmarshal([]byte(tt.input), value)
			if tt.reject {
				if err == nil {
					t.Errorf("%T accepted %s, decoding it as %s", value, tt.input, acptest.Marshal(t, value))
				}
				return
			}
			if err != nil {
				t.Fatalf("decoding %T from %s: %v", value, tt.input, err)
			}
			want := tt.want
			if want == "" {
				want = tt.input
			}
			acptest.EqualMarshaled(t, value, want)
		})
	}
}

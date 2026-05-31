// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package conformance_test

import (
	"encoding/json"
	"testing"
)

func TestGoldenFixturesMatchPublicTypes(t *testing.T) {
	for name, tt := range goldenFixtures() {
		t.Run(name, func(t *testing.T) {
			var raw any
			if err := json.Unmarshal([]byte(tt.raw), &raw); err != nil {
				t.Fatalf("golden fixture is invalid JSON: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.raw), tt.decode()); err != nil {
				t.Fatalf("golden fixture does not decode into public type: %v", err)
			}
			data, err := json.Marshal(tt.value())
			if err != nil {
				t.Fatalf("marshal public type: %v", err)
			}
			assertJSONEqual(t, string(data), tt.raw)
		})
	}
}

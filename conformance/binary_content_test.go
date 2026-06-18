// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package conformance_test

import (
	"encoding/json"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
)

func TestBinaryContentMarshalsAsBase64Strings(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "image data",
			in:   acp.ContentBlock{Type: acp.ContentBlockTypeImage, MimeType: stringPtr("image/png"), Data: "AQID"},
			want: `{"type":"image","data":"AQID","mimeType":"image/png"}`,
		},
		{
			name: "audio data",
			in:   acp.ContentBlock{Type: acp.ContentBlockTypeAudio, MimeType: stringPtr("audio/wav"), Data: "BAUG"},
			want: `{"type":"audio","data":"BAUG","mimeType":"audio/wav"}`,
		},
		{
			name: "resource blob",
			in: acp.ContentBlock{Type: acp.ContentBlockTypeResource, Resource: acp.EmbeddedResourceResource{
				URI:      "file:///data.bin",
				MimeType: stringPtr("application/octet-stream"),
				Blob:     "BwgJ",
			}},
			want: `{"type":"resource","resource":{"uri":"file:///data.bin","mimeType":"application/octet-stream","blob":"BwgJ"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			assertJSONEqual(t, string(data), tt.want)
		})
	}
}

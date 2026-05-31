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
			in:   acp.ContentBlock{Type: acp.ContentTypeImage, MIMEType: "image/png", Data: []byte{1, 2, 3}},
			want: `{"type":"image","data":"AQID","mimeType":"image/png"}`,
		},
		{
			name: "audio data",
			in:   acp.ContentBlock{Type: acp.ContentTypeAudio, MIMEType: "audio/wav", Data: []byte{4, 5, 6}},
			want: `{"type":"audio","data":"BAUG","mimeType":"audio/wav"}`,
		},
		{
			name: "resource blob",
			in: acp.ContentBlock{Type: acp.ContentTypeResource, Resource: &acp.ResourceContents{
				URI:      "file:///data.bin",
				MIMEType: "application/octet-stream",
				Blob:     []byte{7, 8, 9},
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

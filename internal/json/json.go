// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// Package json provides internal JSON utilities.

package json

import (
	"bytes"
	"fmt"
	"io"

	"github.com/segmentio/encoding/json"
)

type Decoder struct {
	dec *json.Decoder
}

func NewDecoder(r io.Reader) *Decoder {
	dec := json.NewDecoder(r)
	dec.DontMatchCaseInsensitiveStructFields()
	return &Decoder{dec: dec}
}

func (d *Decoder) UseNumber() {
	d.dec.UseNumber()
}

func (d *Decoder) Decode(v any) error {
	return d.dec.Decode(v)
}

func Unmarshal(data []byte, v any) error {
	dec := NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return err
	}
	// Unlike the streaming Decoder, Unmarshal must consume exactly one value.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

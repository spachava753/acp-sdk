package acp

import (
	"bytes"
	"encoding/json"
)

// unmarshalJSON preserves numbers in interface values without changing typed numeric fields.
func unmarshalJSON(data []byte, value any) error {
	if !json.Valid(data) {
		return json.Unmarshal(data, value)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(value)
}

package acp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func isJSONNull(data []byte) bool {
	return bytes.Equal(bytes.TrimSpace(data), []byte("null"))
}

// requireJSONProperties checks a nested object's required carrier properties.
func requireJSONProperties(data []byte, required, nonNull []string) error {
	var fields map[string]json.RawMessage
	if err := unmarshalJSON(data, &fields); err != nil {
		return err
	}
	for _, name := range required {
		if len(fields[name]) == 0 {
			return fmt.Errorf("missing required property %q", name)
		}
	}
	for _, name := range nonNull {
		if isJSONNull(fields[name]) {
			return fmt.Errorf("property %q must not be null", name)
		}
	}
	return nil
}

// unmarshalJSON preserves numbers in interface values without changing typed numeric fields.
func unmarshalJSON(data []byte, value any) error {
	if !json.Valid(data) {
		return json.Unmarshal(data, value)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(value)
}

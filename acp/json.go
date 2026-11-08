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

// captureJSONExtraFields retains opaque extension values without decoding them
// through interface/float64, and never restores an invalid salvaged known field.
func captureJSONExtraFields(data []byte, known []string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := unmarshalJSON(data, &fields); err != nil {
		return nil, err
	}
	for _, name := range known {
		delete(fields, name)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return fields, nil
}

func marshalJSONWithExtraFields(value any, extra map[string]json.RawMessage, known []string) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || len(extra) == 0 {
		return data, err
	}
	var fields map[string]json.RawMessage
	if err := unmarshalJSON(data, &fields); err != nil {
		return nil, err
	}
	reserved := make(map[string]bool, len(known))
	for _, name := range known {
		reserved[name] = true
	}
	for name, raw := range extra {
		if !reserved[name] {
			fields[name] = raw
		}
	}
	return json.Marshal(fields)
}

// validateJSONType is used only when generated schema constraints require a
// non-null primitive. encoding/json alone accepts null for scalar Go values.
func validateJSONType[T any](data []byte) error {
	if isJSONNull(data) {
		return fmt.Errorf("value must not be null")
	}
	var value T
	return unmarshalJSON(data, &value)
}

// validateJSONNumber checks the JSON token kind without imposing a float64
// range on opaque values. Numeric strings are strings, not numbers.
func validateJSONNumber(data []byte) error {
	var value any
	if err := unmarshalJSON(data, &value); err != nil {
		return err
	}
	if _, ok := value.(json.Number); !ok {
		return fmt.Errorf("value must be a number")
	}
	return nil
}

func validateJSONNull(data []byte) error {
	if !isJSONNull(data) {
		return fmt.Errorf("value must be null")
	}
	return nil
}

func validateJSONUnion(data []byte, validators ...func([]byte) error) error {
	for _, validate := range validators {
		if validate(data) == nil {
			return nil
		}
	}
	return fmt.Errorf("value does not match any allowed JSON representation")
}

func validateJSONMapValues(data []byte, validate func([]byte) error) error {
	if isJSONNull(data) {
		return fmt.Errorf("value must be an object")
	}
	var values map[string]json.RawMessage
	if err := unmarshalJSON(data, &values); err != nil {
		return err
	}
	for name, value := range values {
		if err := validate(value); err != nil {
			return fmt.Errorf("property %q: %w", name, err)
		}
	}
	return nil
}

func validateJSONArrayValues(data []byte, validate func([]byte) error) error {
	if isJSONNull(data) {
		return fmt.Errorf("value must be an array")
	}
	var values []json.RawMessage
	if err := unmarshalJSON(data, &values); err != nil {
		return err
	}
	for i, value := range values {
		if err := validate(value); err != nil {
			return fmt.Errorf("item %d: %w", i, err)
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

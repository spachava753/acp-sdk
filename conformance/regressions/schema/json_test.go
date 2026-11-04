package schema_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func equalJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	decode := func(data []byte) any {
		if !json.Valid(data) {
			t.Fatalf("not one complete JSON value: %q", data)
		}
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		var value any
		if err := d.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if !reflect.DeepEqual(decode(got), decode([]byte(want))) {
		t.Errorf("JSON value changed\n got: %s\nwant: %s", got, want)
	}
}

func decode(t *testing.T, input string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(input), target); err != nil {
		t.Fatalf("decode %T: %v", target, err)
	}
}

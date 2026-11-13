package typegen

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestNullableRawUnionDecoding(t *testing.T) {
	testGeneratedObjectFixture(t, "nullable_raw_union", nullableRawUnionBehavior)
}

func TestNestedScopeCarrierDecoding(t *testing.T) {
	testGeneratedObjectFixture(t, "nested_scope_carriers", nestedScopeBehavior)
}

func TestMarkerDiscriminatorCollisions(t *testing.T) {
	testGeneratedObjectFixture(t, "marker_discriminator_collisions", markerDiscriminatorBehavior)
}

func testGeneratedObjectFixture(t *testing.T, fixture, behavior string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../testdata", fixture, "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	helpers, err := os.ReadFile("../../../acp/json.go")
	if err != nil {
		t.Fatal(err)
	}

	// Exercise the generated JSON boundary with the real generic helpers.
	// Source snapshots alone cannot prove the generated decoders enforce it.
	dir := t.TempDir()
	for name, source := range map[string][]byte{
		"types_gen.go":  Generate(&schema),
		"json.go":       helpers,
		"types_test.go": []byte(behavior),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), source, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-timeout=10s", ".")
	cmd.Dir = dir
	// The generated fixture imports only standard-library packages. GOPATH mode
	// lets the isolated package compile without inventing a module or fetching.
	cmd.Env = append(os.Environ(), "GO111MODULE=off", "GOTOOLCHAIN=local")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated %s behavior: %v\n%s", fixture, err, output)
	}
}

const nullableRawUnionBehavior = `package acp

import (
 "encoding/json"
 "testing"
)

func TestNullableRawUnion(t *testing.T) {
 // Null is a valid alternative, not an omitted required field. The integer
 // must remain exact rather than passing through a float64 representation.
 for _, id := range []string{"null", "0", "\"\"", "9223372036854775807"} {
  input := []byte("{\"id\":" + id + ",\"label\":\"\"}")
  var scope Scope
  if err := json.Unmarshal(input, &scope); err != nil {
   t.Errorf("valid scope %s: %v", input, err)
  } else if string(scope.ID) != id {
   t.Errorf("raw scope ID = %s, want %s", scope.ID, id)
  }
  var tagged TaggedScope
  input = []byte("{\"kind\":\"request\",\"id\":" + id + "}")
  if err := json.Unmarshal(input, &tagged); err != nil {
   t.Errorf("valid nested union %s: %v", input, err)
  } else if string(tagged.ID) != id {
   t.Errorf("raw tagged ID = %s, want %s", tagged.ID, id)
  }
 }
 // Opaque numbers are not float64 fields. Preserve a valid exponent beyond
 // float64 range without accepting a quoted numeric token as a number.
 var large Scope
 if err := json.Unmarshal([]byte("{\"id\":null,\"label\":\"\",\"number\":1e1000}"), &large); err != nil {
  t.Fatal(err)
 } else if string(large.Number) != "1e1000" {
  t.Errorf("opaque number = %s, want 1e1000", large.Number)
 }
 if err := json.Unmarshal([]byte("{\"id\":null,\"label\":\"\",\"number\":\"1e1000\"}"), &large); err == nil {
  t.Error("quoted numeric token accepted as a number")
 }
 // Adding null must not silently disable the other alternatives' checks.
 for _, id := range []string{"true", "0.5", "9223372036854775808"} {
  var scope Scope
  if err := json.Unmarshal([]byte("{\"id\":" + id + ",\"label\":\"\"}"), &scope); err == nil {
   t.Errorf("invalid scope ID %s accepted", id)
  }
  var tagged TaggedScope
  if err := json.Unmarshal([]byte("{\"kind\":\"request\",\"id\":" + id + "}"), &tagged); err == nil {
   t.Errorf("invalid nested union ID %s accepted", id)
  }
 }
 for _, input := range []string{"{\"label\":\"\"}", "{\"id\":null,\"label\":null}"} {
  var scope Scope
  if err := json.Unmarshal([]byte(input), &scope); err == nil {
   t.Errorf("required/non-null guard relaxed: %s", input)
  }
 }
}
`

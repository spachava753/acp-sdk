package typegen

const nestedScopeBehavior = `package acp

import (
 "encoding/json"
 "reflect"
 "testing"
)

func checkJSON(t *testing.T, value any, want string) {
 t.Helper()
 data, err := json.Marshal(value)
 if err != nil { t.Fatal(err) }
 var gotValue, wantValue any
 if err := json.Unmarshal(data, &gotValue); err != nil { t.Fatal(err) }
 if err := json.Unmarshal([]byte(want), &wantValue); err != nil { t.Fatal(err) }
 if !reflect.DeepEqual(gotValue, wantValue) { t.Errorf("got %s, want %s", data, want) }
}

func TestNestedScope(t *testing.T) {
 // Root-level and allOf-wrapped alternatives must retain the same scope rule.
 for _, mode := range []string{"known", "_vendor"} {
  for _, scope := range []string{"\"sessionId\":\"\"", "\"requestId\":null"} {
   input := "{\"mode\":\""+mode+"\",\"message\":\"\","+scope+"}"
   var request Request
   if err := json.Unmarshal([]byte(input), &request); err != nil { t.Fatal(err) }
   checkJSON(t, request, input)
  }
  var request Request
  if err := json.Unmarshal([]byte("{\"mode\":\""+mode+"\",\"message\":\"\"}"), &request); err == nil {
   t.Error("missing nested scope accepted")
  }
 }
 for _, input := range []string{"{\"message\":\"\"}", "{\"message\":\"\",\"sessionId\":null,\"requestId\":true}"} {
  var body Body
  if err := json.Unmarshal([]byte(input), &body); err == nil { t.Errorf("invalid body accepted: %s", input) }
 }
 // Try full candidates, not just the first present key. Failed/unused carrier
 // fields must not leak into the selected candidate or its re-encoded payload.
 var body Body
 if err := json.Unmarshal([]byte("{\"message\":\"\",\"sessionId\":null,\"toolCallId\":false,\"requestId\":null}"), &body); err != nil { t.Fatal(err) }
 checkJSON(t, body, "{\"message\":\"\",\"requestId\":null}")
 if err := json.Unmarshal([]byte("{\"message\":\"\",\"sessionId\":\"s\",\"requestId\":true}"), &body); err != nil { t.Fatal(err) }
 checkJSON(t, body, "{\"message\":\"\",\"sessionId\":\"s\"}")
}
`

const markerDiscriminatorBehavior = `package acp

import (
 "encoding/json"
 "reflect"
 "slices"
 "testing"
)

func checkJSON(t *testing.T, value any, want string) {
 t.Helper()
 data, err := json.Marshal(value)
 if err != nil { t.Fatal(err) }
 var gotValue, wantValue any
 if err := json.Unmarshal(data, &gotValue); err != nil { t.Fatal(err) }
 if err := json.Unmarshal([]byte(want), &wantValue); err != nil { t.Fatal(err) }
 if !reflect.DeepEqual(gotValue, wantValue) { t.Errorf("got %s, want %s", data, want) }
}

func TestMarkerDiscriminators(t *testing.T) {
 var list List
 input := "{\"closed\":[{\"nullFields\":\"known\",\"title\":null}],\"open\":[{\"extraFields\":\"_vendor\",\"items\":\"opaque\"}]}"
 if err := json.Unmarshal([]byte(input), &list); err != nil { t.Fatal(err) }
 checkJSON(t, list, input)
 // Both synthetic marker names can also be real schema discriminators.
 for _, input := range []string{"{\"extraFields\":\"known\",\"items\":[]}", "{\"extraFields\":\"_vendor\",\"items\":\"opaque\"}"} {
  var value ExtraTag
  if err := json.Unmarshal([]byte(input), &value); err != nil { t.Fatal(err) }
  checkJSON(t, value, input)
 }
 for _, input := range []string{"{\"nullFields\":\"known\",\"title\":null}", "{\"nullFields\":\"_vendor\",\"title\":\"opaque\"}"} {
  var value NullTag
  if err := json.Unmarshal([]byte(input), &value); err != nil { t.Fatal(err) }
  if value.NullFields2 == "known" && !slices.Contains(value.NullFields, "title") { t.Error("null marker lost") }
  checkJSON(t, value, input)
 }
}
`

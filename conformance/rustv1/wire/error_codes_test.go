package wire_test

import (
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/acp-sdk/jsonrpc"
)

// Independent literals from error.rs::sample_codes() and v1_expected(), not
// values computed by either Go SDK representation.
const (
	upstreamParseErrorCode       = -32700
	upstreamInvalidRequestCode   = -32600
	upstreamMethodNotFoundCode   = -32601
	upstreamInvalidParamsCode    = -32602
	upstreamInternalErrorCode    = -32603
	upstreamRequestCancelledCode = -32800
	upstreamAuthRequiredCode     = -32000
	upstreamResourceNotFoundCode = -32002
	upstreamZeroErrorCode        = 0
)

// Rust src/agent-client-protocol/src/error.rs::error_codes_compare_with_schema_codes and the code-coherence
// portion of ::shared_constructors_match_both_schema_versions. These five codes
// have independent public runtime and schema constants in Go. Rust constructor
// messages, Display/Debug and ErrorCode::From traits have separate dispositions.
func TestErrorCodeAgreement(t *testing.T) {
	for _, pair := range []struct {
		runtime int64
		schema  acp.ErrorCode
		want    int64
	}{
		{jsonrpc.CodeParseError, acp.ErrorCodeParseError, upstreamParseErrorCode},
		{jsonrpc.CodeInvalidRequest, acp.ErrorCodeInvalidRequest, upstreamInvalidRequestCode},
		{jsonrpc.CodeMethodNotFound, acp.ErrorCodeMethodNotFound, upstreamMethodNotFoundCode},
		{jsonrpc.CodeInvalidParams, acp.ErrorCodeInvalidParams, upstreamInvalidParamsCode},
		{jsonrpc.CodeInternalError, acp.ErrorCodeInternalError, upstreamInternalErrorCode},
	} {
		if pair.runtime != pair.want || int64(pair.schema) != pair.want {
			t.Errorf("runtime/schema codes = %d/%d, want upstream %d", pair.runtime, pair.schema, pair.want)
		}
	}
	for _, pair := range []struct {
		schema acp.ErrorCode
		want   int64
	}{
		{acp.ErrorCodeRequestCancelled, upstreamRequestCancelledCode},
		{acp.ErrorCodeAuthenticationRequired, upstreamAuthRequiredCode},
		{acp.ErrorCodeResourceNotFound, upstreamResourceNotFoundCode},
	} {
		if int64(pair.schema) != pair.want {
			t.Errorf("schema code = %d, want upstream %d", pair.schema, pair.want)
		}
	}
	if acp.ErrorCodeInvalidParams == acp.ErrorCodeInvalidRequest {
		t.Error("InvalidParams and InvalidRequest must differ")
	}
}

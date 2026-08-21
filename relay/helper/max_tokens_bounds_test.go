package helper

import (
	"bytes"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// hugeMaxTokens exceeds maxTokensLimit but still fits in a uint (and JSON
// number), reproducing a value that binds fine but would overflow when
// multiplied into pre-consume quota math.
const hugeMaxTokensJSON = `4000000000`

func newJSONContext(t *testing.T, path, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

// TestGetAndValidateResponsesRequest_RejectsOversizedMaxOutputTokens
// reproduces a real billing-overflow bug: unlike the Chat Completions
// validator, the Responses/Claude/Gemini request validators never bounded
// their max-tokens fields, even though those values feed the same
// pre-consume quota multiplication.
func TestGetAndValidateResponsesRequest_RejectsOversizedMaxOutputTokens(t *testing.T) {
	c := newJSONContext(t, "/v1/responses", `{"model":"gpt-4.1","input":"hi","max_output_tokens":`+hugeMaxTokensJSON+`}`)
	_, err := GetAndValidateResponsesRequest(c)
	require.Error(t, err)
}

func TestGetAndValidateClaudeRequest_RejectsOversizedMaxTokens(t *testing.T) {
	c := newJSONContext(t, "/v1/messages", `{"model":"claude-3-7-sonnet","messages":[{"role":"user","content":"hi"}],"max_tokens":`+hugeMaxTokensJSON+`}`)
	_, err := GetAndValidateClaudeRequest(c)
	require.Error(t, err)
}

func TestGetAndValidateGeminiRequest_RejectsOversizedMaxOutputTokens(t *testing.T) {
	c := newJSONContext(t, "/v1/models/gemini-3-flash-preview:generateContent",
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":`+hugeMaxTokensJSON+`}}`)
	_, err := GetAndValidateGeminiRequest(c)
	require.Error(t, err)
}

func TestGetAndValidateTextRequest_RejectsOversizedMaxCompletionTokens(t *testing.T) {
	c := newJSONContext(t, "/v1/chat/completions", `{"model":"gpt-4.1","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":`+hugeMaxTokensJSON+`}`)
	_, err := GetAndValidateTextRequest(c, 0)
	require.Error(t, err, "max_completion_tokens must be bounded the same way max_tokens already is")
}

func TestExceedsMaxTokensLimit_BoundaryValues(t *testing.T) {
	within := uint(math.MaxInt32 / 2)
	over := uint(math.MaxInt32/2) + 1
	require.False(t, exceedsMaxTokensLimit(&within))
	require.True(t, exceedsMaxTokensLimit(&over))
	require.False(t, exceedsMaxTokensLimit(nil))
}

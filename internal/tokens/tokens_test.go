package tokens_test

import (
	"testing"

	"github.com/airelay/airelay/internal/tokens"
	"github.com/stretchr/testify/require"
)

func TestParseOpenAIChunk_WithUsage(t *testing.T) {
	line := []byte(`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	u, err := tokens.ParseOpenAIChunk(line)
	require.NoError(t, err)
	require.NotNil(t, u)
	require.Equal(t, 10, u.PromptTokens)
	require.Equal(t, 5, u.CompletionTokens)
}

func TestParseOpenAIChunk_NoUsage(t *testing.T) {
	line := []byte(`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","choices":[{"delta":{"content":"hello"}}]}`)
	u, err := tokens.ParseOpenAIChunk(line)
	require.NoError(t, err)
	require.Nil(t, u)
}

func TestParseOpenAIChunk_Done(t *testing.T) {
	u, err := tokens.ParseOpenAIChunk([]byte("data: [DONE]"))
	require.NoError(t, err)
	require.Nil(t, u)
}

func TestParseAnthropicMessageStart(t *testing.T) {
	event := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":42}}}")
	n := tokens.ParseAnthropicMessageStart(event)
	require.Equal(t, 42, n)
}

func TestParseAnthropicEvent_MessageDelta(t *testing.T) {
	event := []byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":17}}")
	u, err := tokens.ParseAnthropicEvent(event, 42)
	require.NoError(t, err)
	require.NotNil(t, u)
	require.Equal(t, 42, u.PromptTokens)
	require.Equal(t, 17, u.CompletionTokens)
}

func TestParseAnthropicEvent_OtherEvent(t *testing.T) {
	event := []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}")
	u, err := tokens.ParseAnthropicEvent(event, 0)
	require.NoError(t, err)
	require.Nil(t, u)
}

func TestParseGoogleChunk_WithUsage(t *testing.T) {
	chunk := []byte(`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3}}`)
	u, err := tokens.ParseGoogleChunk(chunk)
	require.NoError(t, err)
	require.NotNil(t, u)
	require.Equal(t, 8, u.PromptTokens)
	require.Equal(t, 3, u.CompletionTokens)
}

func TestParseGoogleChunk_NoUsage(t *testing.T) {
	chunk := []byte(`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`)
	u, err := tokens.ParseGoogleChunk(chunk)
	require.NoError(t, err)
	require.Nil(t, u)
}

func TestParseGoogleChunk_Empty(t *testing.T) {
	u, err := tokens.ParseGoogleChunk([]byte(""))
	require.NoError(t, err)
	require.Nil(t, u)
}

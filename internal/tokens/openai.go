package tokens

import (
	"bytes"
	"encoding/json"
)

type openAIChunk struct {
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// ParseOpenAIChunk extracts token usage from an OpenAI SSE data line.
// Returns nil if the chunk contains no usage data (most chunks won't).
func ParseOpenAIChunk(data []byte) (*Usage, error) {
	data = bytes.TrimPrefix(data, []byte("data: "))
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return nil, nil
	}
	var chunk openAIChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, nil // non-fatal: skip malformed chunks
	}
	if chunk.Usage == nil {
		return nil, nil
	}
	return &Usage{
		PromptTokens:     chunk.Usage.PromptTokens,
		CompletionTokens: chunk.Usage.CompletionTokens,
	}, nil
}

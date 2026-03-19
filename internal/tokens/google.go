package tokens

import (
	"bytes"
	"encoding/json"
)

type googleChunk struct {
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

// ParseGoogleChunk extracts token usage from a Google Gemini streaming chunk.
// Returns nil if this chunk contains no usageMetadata.
func ParseGoogleChunk(data []byte) (*Usage, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var chunk googleChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, nil
	}
	if chunk.UsageMetadata == nil {
		return nil, nil
	}
	return &Usage{
		PromptTokens:     chunk.UsageMetadata.PromptTokenCount,
		CompletionTokens: chunk.UsageMetadata.CandidatesTokenCount,
	}, nil
}

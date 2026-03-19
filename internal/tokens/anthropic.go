package tokens

import (
	"bytes"
	"encoding/json"
)

type anthropicDelta struct {
	Type  string `json:"type"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicMessageStart struct {
	Type    string `json:"type"`
	Message *struct {
		Usage *struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// ParseAnthropicMessageStart extracts input tokens from a message_start event block.
// Returns 0 if not a message_start event or no token data is present.
func ParseAnthropicMessageStart(data []byte) int {
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimPrefix(line, []byte("data: "))
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var start anthropicMessageStart
		if err := json.Unmarshal(line, &start); err != nil {
			continue
		}
		if start.Type == "message_start" && start.Message != nil && start.Message.Usage != nil {
			return start.Message.Usage.InputTokens
		}
	}
	return 0
}

// ParseAnthropicEvent extracts token usage from an Anthropic SSE event block.
// inputTokens must be tracked separately from the message_start event.
// Returns nil for all events except message_delta which carries output token count.
func ParseAnthropicEvent(data []byte, inputTokens int) (*Usage, error) {
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimPrefix(line, []byte("data: "))
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var delta anthropicDelta
		if err := json.Unmarshal(line, &delta); err != nil {
			continue
		}
		if delta.Type == "message_delta" && delta.Usage != nil {
			return &Usage{
				PromptTokens:     inputTokens,
				CompletionTokens: delta.Usage.OutputTokens,
			}, nil
		}
	}
	return nil, nil
}

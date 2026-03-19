package tokens

// Usage holds token counts extracted from a streaming response.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

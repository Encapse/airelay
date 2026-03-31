package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/airelay/airelay/internal/models"
	"github.com/airelay/airelay/internal/tokens"
)

// ProviderURLs maps providers to their base API URLs.
var ProviderURLs = map[models.AIProvider]string{
	models.ProviderOpenAI:    "https://api.openai.com",
	models.ProviderAnthropic: "https://api.anthropic.com",
	models.ProviderGoogle:    "https://generativelanguage.googleapis.com",
}

// ForwardResult contains the outcome of a forwarded request.
type ForwardResult struct {
	StatusCode           int
	Usage                *tokens.Usage
	DurationMS           int
	AnthropicInputTokens int
}

var proxyHTTPClient = &http.Client{Timeout: 5 * time.Minute}

// Forward proxies the request to the provider and streams the response to w.
// It extracts token usage from the final SSE chunk for cost accounting.
// body is the pre-read request body; the caller is responsible for reading it
// once and passing the same slice here to avoid double allocation.
func Forward(
	w http.ResponseWriter,
	r *http.Request,
	body []byte,
	providerBase string,
	providerKey string,
	provider models.AIProvider,
	pathSuffix string,
) (*ForwardResult, error) {
	start := time.Now()

	upstreamURL := providerBase + pathSuffix
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	for k, vs := range r.Header {
		switch k {
		case "Authorization", "X-Api-Key", "X-Airelay-Meta":
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	switch provider {
	case models.ProviderOpenAI, models.ProviderGoogle:
		req.Header.Set("Authorization", "Bearer "+providerKey)
	case models.ProviderAnthropic:
		req.Header.Set("x-api-key", providerKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	resp, err := proxyHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	result := &ForwardResult{StatusCode: resp.StatusCode}

	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		result.Usage = streamSSE(w, resp.Body, provider, &result.AnthropicInputTokens)
	} else {
		io.Copy(w, resp.Body)
	}

	result.DurationMS = int(time.Since(start).Milliseconds())
	return result, nil
}

// streamSSE forwards SSE chunks to the client and extracts token usage.
func streamSSE(w http.ResponseWriter, body io.Reader, provider models.AIProvider, anthropicInput *int) *tokens.Usage {
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	var lastUsage *tokens.Usage
	var eventBuf bytes.Buffer

	for scanner.Scan() {
		line := scanner.Bytes()
		fmt.Fprintf(w, "%s\n", line)
		if flusher != nil {
			flusher.Flush()
		}

		eventBuf.Write(line)
		eventBuf.WriteByte('\n')

		switch provider {
		case models.ProviderOpenAI:
			if u, _ := tokens.ParseOpenAIChunk(line); u != nil {
				lastUsage = u
			}
		case models.ProviderAnthropic:
			if n := tokens.ParseAnthropicMessageStart(eventBuf.Bytes()); n > 0 {
				*anthropicInput = n
			}
			if u, _ := tokens.ParseAnthropicEvent(eventBuf.Bytes(), *anthropicInput); u != nil {
				lastUsage = u
			}
		case models.ProviderGoogle:
			if u, _ := tokens.ParseGoogleChunk(line); u != nil {
				lastUsage = u
			}
		}

		if len(bytes.TrimSpace(line)) == 0 {
			eventBuf.Reset()
		}
	}
	return lastUsage
}

package jobs_test

import (
	"testing"

	"github.com/airelay/airelay/internal/jobs"
	"github.com/stretchr/testify/require"
)

func TestParseLiteLLMEntry(t *testing.T) {
	entry := jobs.LiteLLMEntry{
		InputCostPerToken:  0.0000025,
		OutputCostPerToken: 0.00001,
		LiteLLMProvider:    "openai",
	}
	in1k, out1k := entry.Per1kCosts()
	require.InDelta(t, 0.0025, in1k, 0.000001)
	require.InDelta(t, 0.01, out1k, 0.000001)
}

func TestParseOpenRouterEntry(t *testing.T) {
	model := jobs.OpenRouterModel{
		ID: "openai/gpt-4o",
		Pricing: jobs.OpenRouterPricing{
			Prompt:     "0.0000025",
			Completion: "0.00001",
		},
	}
	provider, name := model.ProviderAndModel()
	require.Equal(t, "openai", provider)
	require.Equal(t, "gpt-4o", name)
	in1k, out1k := model.Per1kCosts()
	require.InDelta(t, 0.0025, in1k, 0.000001)
	require.InDelta(t, 0.01, out1k, 0.000001)
}

func TestFilterKnownProvider(t *testing.T) {
	require.True(t, jobs.IsKnownProvider("openai"))
	require.True(t, jobs.IsKnownProvider("anthropic"))
	require.True(t, jobs.IsKnownProvider("google"))
	require.False(t, jobs.IsKnownProvider("cohere"))
}

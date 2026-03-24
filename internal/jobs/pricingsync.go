package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const liteLLMURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
const openRouterURL = "https://openrouter.ai/api/v1/models"

// LiteLLMEntry is a single model entry from the LiteLLM pricing JSON.
type LiteLLMEntry struct {
	InputCostPerToken  float64 `json:"input_cost_per_token"`
	OutputCostPerToken float64 `json:"output_cost_per_token"`
	LiteLLMProvider    string  `json:"litellm_provider"`
}

// Per1kCosts returns input and output cost per 1k tokens.
func (e LiteLLMEntry) Per1kCosts() (float64, float64) {
	return e.InputCostPerToken * 1000, e.OutputCostPerToken * 1000
}

// OpenRouterPricing holds prompt/completion pricing from OpenRouter.
type OpenRouterPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

// OpenRouterModel is a single model from the OpenRouter models API.
type OpenRouterModel struct {
	ID      string            `json:"id"`
	Pricing OpenRouterPricing `json:"pricing"`
}

// ProviderAndModel splits "provider/model-name" into (provider, model).
func (m OpenRouterModel) ProviderAndModel() (string, string) {
	parts := strings.SplitN(m.ID, "/", 2)
	if len(parts) != 2 {
		return "", m.ID
	}
	return parts[0], parts[1]
}

// Per1kCosts parses OpenRouter's string pricing to per-1k float64 values.
func (m OpenRouterModel) Per1kCosts() (float64, float64) {
	parsePerToken := func(s string) float64 {
		f, _ := strconv.ParseFloat(s, 64)
		return f * 1000
	}
	return parsePerToken(m.Pricing.Prompt), parsePerToken(m.Pricing.Completion)
}

// IsKnownProvider returns true for the three providers we support.
func IsKnownProvider(provider string) bool {
	switch provider {
	case "openai", "anthropic", "google":
		return true
	}
	return false
}

// RunPricingSync fetches LiteLLM and OpenRouter pricing and upserts into model_pricing.
// Rows with manual_override=true are skipped.
func RunPricingSync(ctx context.Context, db *pgxpool.Pool) error {
	client := &http.Client{Timeout: 30 * time.Second}

	// --- LiteLLM ---
	resp, err := client.Get(liteLLMURL)
	if err != nil {
		return fmt.Errorf("fetch litellm: %w", err)
	}
	defer resp.Body.Close()

	var liteLLMData map[string]LiteLLMEntry
	if err := json.NewDecoder(resp.Body).Decode(&liteLLMData); err != nil {
		return fmt.Errorf("decode litellm: %w", err)
	}

	upserted := 0
	for modelName, entry := range liteLLMData {
		if !IsKnownProvider(entry.LiteLLMProvider) {
			continue
		}
		if entry.InputCostPerToken == 0 && entry.OutputCostPerToken == 0 {
			continue
		}
		in1k, out1k := entry.Per1kCosts()
		_, err := db.Exec(ctx, `
			INSERT INTO model_pricing (provider, model, input_cost_per_1k, output_cost_per_1k, synced_from, last_synced_at)
			VALUES ($1,$2,$3,$4,'litellm',NOW())
			ON CONFLICT (provider, model) DO UPDATE
			  SET input_cost_per_1k=$3, output_cost_per_1k=$4, synced_from='litellm', last_synced_at=NOW()
			  WHERE model_pricing.manual_override = false`,
			entry.LiteLLMProvider, modelName, in1k, out1k,
		)
		if err == nil {
			upserted++
		}
	}
	log.Printf("pricing sync: upserted %d rows from LiteLLM", upserted)

	// --- OpenRouter (secondary source) ---
	resp2, err := client.Get(openRouterURL)
	if err != nil {
		log.Printf("pricing sync: could not reach OpenRouter: %v", err)
		return nil // non-fatal
	}
	defer resp2.Body.Close()

	var orData struct {
		Data []OpenRouterModel `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&orData); err != nil {
		log.Printf("pricing sync: decode openrouter: %v", err)
		return nil
	}

	orUpserted := 0
	for _, m := range orData.Data {
		provider, model := m.ProviderAndModel()
		if !IsKnownProvider(provider) || model == "" {
			continue
		}
		in1k, out1k := m.Per1kCosts()
		if in1k == 0 && out1k == 0 {
			continue
		}
		db.Exec(ctx, `
			INSERT INTO model_pricing (provider, model, input_cost_per_1k, output_cost_per_1k, synced_from, last_synced_at)
			VALUES ($1,$2,$3,$4,'openrouter',NOW())
			ON CONFLICT (provider, model) DO NOTHING`,
			provider, model, in1k, out1k,
		)
		orUpserted++
	}
	log.Printf("pricing sync: %d rows from OpenRouter (new models only)", orUpserted)
	return nil
}

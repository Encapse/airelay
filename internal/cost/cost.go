package cost

import "github.com/airelay/airelay/internal/models"

// Calculate returns total cost in USD for a request.
func Calculate(promptTokens, completionTokens int, pricing *models.ModelPricing) float64 {
	input := float64(promptTokens) / 1000.0 * pricing.InputCostPer1k
	output := float64(completionTokens) / 1000.0 * pricing.OutputCostPer1k
	return input + output
}

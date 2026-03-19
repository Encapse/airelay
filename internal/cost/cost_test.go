package cost_test

import (
	"testing"

	"github.com/airelay/airelay/internal/cost"
	"github.com/airelay/airelay/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCalculate(t *testing.T) {
	pricing := &models.ModelPricing{
		InputCostPer1k:  0.00250,
		OutputCostPer1k: 0.01000,
	}
	// (100/1000 * 0.0025) + (50/1000 * 0.01) = 0.00025 + 0.0005 = 0.00075
	c := cost.Calculate(100, 50, pricing)
	require.InDelta(t, 0.00075, c, 0.000001)
}

func TestCalculate_ZeroTokens(t *testing.T) {
	pricing := &models.ModelPricing{InputCostPer1k: 0.001, OutputCostPer1k: 0.002}
	require.Equal(t, 0.0, cost.Calculate(0, 0, pricing))
}

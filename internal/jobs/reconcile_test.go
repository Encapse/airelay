package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDriftFraction(t *testing.T) {
	cases := []struct {
		redis    float64
		db       float64
		wantDrift float64
		desc     string
	}{
		{1.0, 1.0, 0.0, "no drift"},
		{1.05, 1.0, 0.05, "exactly 5% over"},
		{0.95, 1.0, 0.05, "exactly 5% under"},
		{1.20, 1.0, 0.20, "exactly 20% over"},
		{0.80, 1.0, 0.20, "exactly 20% under"},
		{1.04, 1.0, 0.04, "under 5% threshold"},
		{1.25, 1.0, 0.25, "over 20% threshold"},
		{0.0, 0.0, 0.0, "zero db and zero redis"},
		{5.0, 0.0, 0.0, "zero db, non-zero redis — no division by zero"},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			got := driftFraction(c.redis, c.db)
			assert.InDelta(t, c.wantDrift, got, 1e-9)
		})
	}
}

func TestDriftThresholds(t *testing.T) {
	// Verify the threshold constants implied by RunReconcile:
	// drift > 0.05 → correct, drift > 0.20 → also alert

	// Under 5%: no correction expected
	require.Less(t, driftFraction(1.04, 1.0), 0.05, "4% should be below correction threshold")

	// Over 5%: correction expected (using a value clearly above the threshold)
	require.True(t, driftFraction(1.06, 1.0) > 0.05, "6% should trigger correction")

	// Over 20%: alert expected
	require.True(t, driftFraction(1.21, 1.0) > 0.20, "21% should trigger alert")

	// Under 20%: correction but no alert
	require.True(t, driftFraction(1.10, 1.0) > 0.05, "10% should trigger correction")
	require.False(t, driftFraction(1.10, 1.0) > 0.20, "10% should not trigger alert")
}

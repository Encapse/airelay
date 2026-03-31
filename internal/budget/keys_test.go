package budget_test

import (
	"testing"
	"time"

	"github.com/airelay/airelay/internal/budget"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestSpendKey_Daily(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2026, 3, 31, 14, 0, 0, 0, time.UTC)
	got := budget.SpendKey(id, "daily", ts)
	assert.Equal(t, "spend:11111111-1111-1111-1111-111111111111:daily:2026-03-31", got)
}

func TestSpendKey_Monthly(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	got := budget.SpendKey(id, "monthly", ts)
	assert.Equal(t, "spend:11111111-1111-1111-1111-111111111111:monthly:2026-03", got)
}

func TestSpendKey_UTCEnforcement(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	loc := time.FixedZone("UTC+10", 10*3600)
	// 2026-04-01 02:00 in UTC+10 is 2026-03-31 16:00 UTC — key should use UTC date
	ts := time.Date(2026, 4, 1, 2, 0, 0, 0, loc)
	got := budget.SpendKey(id, "daily", ts)
	assert.Equal(t, "spend:11111111-1111-1111-1111-111111111111:daily:2026-03-31", got)
}

func TestSpendKeyTTL_Daily(t *testing.T) {
	// Noon on March 31 → expires April 2 midnight = 36 hours
	ts := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	ttl := budget.SpendKeyTTL("daily", ts)
	assert.Equal(t, 36*time.Hour, ttl)
}

func TestSpendKeyTTL_DailyMidnight(t *testing.T) {
	// Midnight March 31 → expires April 2 midnight = 48 hours
	ts := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	ttl := budget.SpendKeyTTL("daily", ts)
	assert.Equal(t, 48*time.Hour, ttl)
}

func TestSpendKeyTTL_Monthly(t *testing.T) {
	// March 15 00:00 → expires April 5 00:00 = 21 days
	ts := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	ttl := budget.SpendKeyTTL("monthly", ts)
	assert.Equal(t, 21*24*time.Hour, ttl)
}

func TestSpendKeyTTL_MonthlyEndOfMonth(t *testing.T) {
	// March 31 00:00 → expires April 5 00:00 = 5 days
	ts := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	ttl := budget.SpendKeyTTL("monthly", ts)
	assert.Equal(t, 5*24*time.Hour, ttl)
}

func TestSpendKeyTTL_YearBoundary(t *testing.T) {
	// December 15 00:00 → expires January 5 00:00 = 21 days
	ts := time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)
	ttl := budget.SpendKeyTTL("monthly", ts)
	assert.Equal(t, 21*24*time.Hour, ttl)
}

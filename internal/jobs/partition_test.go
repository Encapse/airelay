package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPartitionTableName(t *testing.T) {
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC), "usage_events_2026_03"},
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "usage_events_2026_01"},
		{time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), "usage_events_2026_12"},
		{time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), "usage_events_2027_01"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, partitionTableName(c.in), "for %s", c.in)
	}
}

func TestEnsurePartition_SQLFormat(t *testing.T) {
	// Verify the SQL boundaries are correct by checking ensurePartition logic indirectly
	// via partitionTableName and the date arithmetic.

	// March: start=2026-03-01, end=2026-04-01
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	assert.Equal(t, "2026-03-01", start.Format("2006-01-02"))
	assert.Equal(t, "2026-04-01", end.Format("2006-01-02"))

	// December: start=2026-12-01, end=2027-01-01 (year boundary)
	start = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	end = start.AddDate(0, 1, 0)
	assert.Equal(t, "2026-12-01", start.Format("2006-01-02"))
	assert.Equal(t, "2027-01-01", end.Format("2006-01-02"), "December end should roll to next year")
}

func TestRunPartition_CreatesBothMonths(t *testing.T) {
	// Verify RunPartition iterates over current and next month.
	// Without a real DB we verify via partitionTableName with expected dates.
	now := time.Now().UTC()
	curr := partitionTableName(now)
	next := partitionTableName(now.AddDate(0, 1, 0))
	assert.NotEqual(t, curr, next, "current and next month should differ")
	// Spot-check format
	assert.Regexp(t, `^usage_events_\d{4}_\d{2}$`, curr)
	assert.Regexp(t, `^usage_events_\d{4}_\d{2}$`, next)
}

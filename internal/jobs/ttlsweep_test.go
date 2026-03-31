package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsExpiredSpendKey(t *testing.T) {
	today := "2026-03-31"
	thisMonth := "2026-03"

	cases := []struct {
		key     string
		expired bool
		desc    string
	}{
		// Current day — must NOT be deleted
		{"spend:proj-uuid:daily:2026-03-31", false, "current day key"},
		// Past days — must be deleted
		{"spend:proj-uuid:daily:2026-03-30", true, "yesterday key"},
		{"spend:proj-uuid:daily:2026-03-01", true, "old day in same month"},
		{"spend:proj-uuid:daily:2026-02-28", true, "day from prior month"},
		// Current month — must NOT be deleted
		{"spend:proj-uuid:monthly:2026-03", false, "current month key"},
		// Past months — must be deleted
		{"spend:proj-uuid:monthly:2026-02", true, "prior month key"},
		{"spend:proj-uuid:monthly:2025-12", true, "year-ago month key"},
		// Malformed / unknown period — must NOT be deleted (safe default)
		{"spend:proj-uuid:weekly:2026-03-31", false, "unknown period"},
		{"spend:proj-uuid", false, "too few parts"},
		{"not-a-spend-key", false, "unrelated key"},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			assert.Equal(t, c.expired, isExpiredSpendKey(c.key, today, thisMonth))
		})
	}
}

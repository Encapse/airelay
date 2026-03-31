package budget

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SpendKey returns the Redis key for a project's spend in a given period.
// This is the canonical implementation — all packages must use this function
// to ensure key format consistency.
func SpendKey(projectID uuid.UUID, period string, t time.Time) string {
	switch period {
	case "daily":
		return fmt.Sprintf("spend:%s:daily:%s", projectID, t.UTC().Format("2006-01-02"))
	case "monthly":
		return fmt.Sprintf("spend:%s:monthly:%s", projectID, t.UTC().Format("2006-01"))
	default:
		return fmt.Sprintf("spend:%s:%s", projectID, period)
	}
}

// SpendKeyTTL returns how long a spend key should live in Redis, measured from t.
// Daily keys expire 2 days after the key date (midnight).
// Monthly keys expire on the 5th of the following month (buffer for reconciliation).
// Pass time.Now().UTC() in production; pass a fixed time in tests.
func SpendKeyTTL(period string, t time.Time) time.Duration {
	u := t.UTC()
	switch period {
	case "daily":
		end := time.Date(u.Year(), u.Month(), u.Day()+2, 0, 0, 0, 0, time.UTC)
		return end.Sub(u)
	case "monthly":
		end := time.Date(u.Year(), u.Month()+1, 5, 0, 0, 0, 0, time.UTC)
		return end.Sub(u)
	default:
		return 7 * 24 * time.Hour
	}
}

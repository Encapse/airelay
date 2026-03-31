package jobs

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// isExpiredSpendKey reports whether key represents a spend key for a period
// other than today or this month. today is "YYYY-MM-DD", thisMonth is "YYYY-MM".
func isExpiredSpendKey(key, today, thisMonth string) bool {
	// key format: spend:{project_id}:daily:YYYY-MM-DD or spend:{project_id}:monthly:YYYY-MM
	parts := strings.Split(key, ":")
	if len(parts) < 4 {
		return false
	}
	period := parts[2]
	value := parts[3]
	switch period {
	case "daily":
		return value != today
	case "monthly":
		return value != thisMonth
	default:
		return false
	}
}

// RunTTLSweep deletes expired Redis spend keys (prior day and prior month periods).
// Current day and current month keys are preserved.
func RunTTLSweep(ctx context.Context, rdb *redis.Client) {
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	thisMonth := now.Format("2006-01")

	var cursor uint64
	deleted := 0
	for {
		var keys []string
		var err error
		keys, cursor, err = rdb.Scan(ctx, cursor, "spend:*", 100).Result()
		if err != nil {
			log.Printf("ttlsweep: scan error: %v", err)
			break
		}
		for _, key := range keys {
			if !isExpiredSpendKey(key, today, thisMonth) {
				continue
			}
			rdb.Del(ctx, key)
			deleted++
		}
		if cursor == 0 {
			break
		}
	}
	if deleted > 0 {
		log.Printf("ttlsweep: deleted %d expired spend keys", deleted)
	}
}

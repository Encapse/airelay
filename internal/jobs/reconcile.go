package jobs

import (
	"context"
	"log"
	"time"

	"github.com/airelay/airelay/internal/budget"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// RunReconcile checks Redis spend counters against Postgres SUM for all active projects.
// Drift >5% corrects Redis. Drift >20% logs an alert.
func RunReconcile(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client) {
	rows, err := db.Query(ctx, `SELECT id FROM projects WHERE archived_at IS NULL`)
	if err != nil {
		log.Printf("reconcile: query projects: %v", err)
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	periods := []struct {
		period      string
		periodStart time.Time
	}{
		{
			"daily",
			time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
		},
		{
			"monthly",
			time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for rows.Next() {
		var projectID uuid.UUID
		if err := rows.Scan(&projectID); err != nil {
			continue
		}
		for _, p := range periods {
			redisKey := budget.SpendKey(projectID, p.period, now)
			redisVal, err := rdb.Get(ctx, redisKey).Float64()
			if err == redis.Nil {
				continue // no spend recorded yet
			}
			if err != nil {
				continue
			}
			var dbSum float64
			db.QueryRow(ctx,
				`SELECT COALESCE(SUM(cost_usd),0) FROM usage_events
				 WHERE project_id=$1 AND created_at >= $2`,
				projectID, p.periodStart,
			).Scan(&dbSum)

			if dbSum == 0 {
				continue
			}
			drift := driftFraction(redisVal, dbSum)
			if drift > 0.20 {
				log.Printf("ALERT reconcile: project %s %s drift %.0f%% (redis=%.6f db=%.6f)",
					projectID, p.period, drift*100, redisVal, dbSum)
			}
			if drift > 0.05 {
				// Use proper TTL — TTL=0 would leave the key without expiry.
				rdb.Set(ctx, redisKey, dbSum, budget.SpendKeyTTL(p.period, now))
				log.Printf("reconcile: corrected %s %s redis=%.6f → db=%.6f (drift %.1f%%)",
					projectID, p.period, redisVal, dbSum, drift*100)
			}
		}
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// driftFraction returns |redis - db| / db. Returns 0 if db == 0 (no spend recorded).
func driftFraction(redis, db float64) float64 {
	if db == 0 {
		return 0
	}
	return abs((redis - db) / db)
}

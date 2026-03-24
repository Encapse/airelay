package jobs

import (
	"context"
	"fmt"
	"log"
	"time"

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
		keyFmt      string
	}{
		{
			"daily",
			time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
			"daily:" + now.Format("2006-01-02"),
		},
		{
			"monthly",
			time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC),
			"monthly:" + now.Format("2006-01"),
		},
	}

	for rows.Next() {
		var projectID uuid.UUID
		if err := rows.Scan(&projectID); err != nil {
			continue
		}
		for _, p := range periods {
			redisKey := fmt.Sprintf("spend:%s:%s", projectID, p.keyFmt)
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
			drift := abs((redisVal - dbSum) / dbSum)
			if drift > 0.20 {
				log.Printf("ALERT reconcile: project %s %s drift %.0f%% (redis=%.6f db=%.6f)",
					projectID, p.period, drift*100, redisVal, dbSum)
			}
			if drift > 0.05 {
				rdb.Set(ctx, redisKey, dbSum, 0)
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

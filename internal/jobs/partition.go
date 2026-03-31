package jobs

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunPartition ensures the current and next month's usage_events partitions exist.
// Creating both guards against a cold deploy landing on a month with no partition.
// Safe to call multiple times — uses CREATE TABLE IF NOT EXISTS.
func RunPartition(ctx context.Context, db *pgxpool.Pool) {
	now := time.Now().UTC()
	for _, t := range []time.Time{now, now.AddDate(0, 1, 0)} {
		if err := ensurePartition(ctx, db, t); err != nil {
			log.Printf("partition: %v", err)
		}
	}
}

func ensurePartition(ctx context.Context, db *pgxpool.Pool, t time.Time) error {
	// Normalise to first of month
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	tableName := partitionTableName(t)
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF usage_events
		 FOR VALUES FROM ('%s') TO ('%s')`,
		tableName,
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
	)
	if _, err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("could not create %s: %w", tableName, err)
	}
	log.Printf("partition: ensured %s exists (%s to %s)",
		tableName, start.Format("2006-01"), end.Format("2006-01"))
	return nil
}

// partitionTableName returns the table name for the month containing t.
func partitionTableName(t time.Time) string {
	return fmt.Sprintf("usage_events_%04d_%02d", t.Year(), int(t.Month()))
}

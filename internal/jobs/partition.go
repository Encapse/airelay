package jobs

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunPartition ensures next month's usage_events partition exists.
// Safe to call multiple times — uses CREATE TABLE IF NOT EXISTS.
func RunPartition(ctx context.Context, db *pgxpool.Pool) {
	now := time.Now().UTC()
	nextMonth := now.AddDate(0, 1, 0)
	year := nextMonth.Year()
	month := nextMonth.Month()

	partStart := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	partEnd := partStart.AddDate(0, 1, 0)

	tableName := fmt.Sprintf("usage_events_%04d_%02d", year, int(month))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF usage_events
		 FOR VALUES FROM ('%s') TO ('%s')`,
		tableName,
		partStart.Format("2006-01-02"),
		partEnd.Format("2006-01-02"),
	)
	_, err := db.Exec(ctx, sql)
	if err != nil {
		log.Printf("partition: could not create %s: %v", tableName, err)
		return
	}
	log.Printf("partition: ensured %s exists (%s to %s)",
		tableName, partStart.Format("2006-01"), partEnd.Format("2006-01"))
}

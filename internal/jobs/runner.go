package jobs

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Start launches all background jobs. Call once from main — non-blocking.
func Start(db *pgxpool.Pool, rdb *redis.Client) {
	// Reconciliation: every 60 seconds
	go func() {
		for {
			time.Sleep(60 * time.Second)
			RunReconcile(context.Background(), db, rdb)
		}
	}()

	// Pricing sync: run once immediately, then every 24 hours
	go func() {
		if err := RunPricingSync(context.Background(), db); err != nil {
			log.Printf("pricing sync (startup): %v", err)
		}
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := RunPricingSync(context.Background(), db); err != nil {
				log.Printf("pricing sync: %v", err)
			}
		}
	}()

	// Partition management: run once on startup, then daily
	go func() {
		RunPartition(context.Background(), db)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			RunPartition(context.Background(), db)
		}
	}()

	// TTL sweep: run once on startup, then daily
	go func() {
		RunTTLSweep(context.Background(), rdb)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			RunTTLSweep(context.Background(), rdb)
		}
	}()

	log.Println("background jobs started: reconcile (60s), pricing sync (24h), partition (24h), ttl sweep (24h)")
}

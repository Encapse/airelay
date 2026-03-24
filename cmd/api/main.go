package main

import (
	"context"
	"log"
	"net/http"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/dashboard"
	"github.com/airelay/airelay/internal/config"
	"github.com/airelay/airelay/internal/db"
	"github.com/airelay/airelay/internal/jobs"
	redisclient "github.com/airelay/airelay/internal/redis"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	pool, err := db.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	rdb, err := redisclient.Connect(cfg.RedisURL)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()

	jobs.Start(pool, rdb)

	srv, mux := api.NewServer(pool, rdb, cfg)
	dashboard.NewDashboardServer(mux)

	srv.Addr = ":" + cfg.APIPort

	log.Printf("AIRelay management API + dashboard listening on :%s", cfg.APIPort)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

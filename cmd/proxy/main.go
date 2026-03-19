package main

import (
	"context"
	"log"
	"net/http"

	"github.com/airelay/airelay/internal/config"
	"github.com/airelay/airelay/internal/db"
	redisclient "github.com/airelay/airelay/internal/redis"
	"github.com/airelay/airelay/proxy"
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

	srv := proxy.NewServer(pool, rdb, cfg.CredentialEncryptionKey)
	srv.Addr = ":" + cfg.ProxyPort

	log.Printf("AIRelay proxy listening on :%s", cfg.ProxyPort)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("proxy server: %v", err)
	}
}

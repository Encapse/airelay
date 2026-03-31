package proxy

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func NewServer(db *pgxpool.Pool, rdb *redis.Client, encKey string) *http.Server {
	handler := NewHandler(db, rdb, encKey)
	mux := http.NewServeMux()
	mux.Handle("/proxy/", handler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})
	return &http.Server{
		Handler: mux,
		// ReadHeaderTimeout guards against slow-header attacks.
		// ReadTimeout and WriteTimeout are long to accommodate SSE streams
		// (AI completions can run for several minutes).
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
}

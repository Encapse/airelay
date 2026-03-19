package proxy

import (
	"fmt"
	"net/http"

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
	return &http.Server{Handler: mux}
}

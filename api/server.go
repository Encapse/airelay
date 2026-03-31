package api

import (
	"net/http"
	"time"

	"github.com/airelay/airelay/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

// NewServer wires all handlers and returns (*http.Server, *http.ServeMux).
// The mux is returned separately so main can mount dashboard routes without
// a type assertion on srv.Handler.
func NewServer(db *pgxpool.Pool, rdb *redis.Client, cfg *config.Config) (*http.Server, *http.ServeMux) {
	mux := http.NewServeMux()

	auth := NewAuthHandler(db, cfg.JWTSecret)
	projects := NewProjectsHandler(db)
	keys := NewKeysHandler(db)
	creds := NewCredentialsHandler(db, cfg.CredentialEncryptionKey)
	budgets := NewBudgetHandler(db, rdb)
	usage := NewUsageHandler(db)
	models := NewModelsHandler(db)

	authed := RequireAuth(cfg.JWTSecret)
	// 10 req/min burst=10 per IP on auth endpoints; evict stale entries every minute.
	authRate := IPRateLimit(rate.Every(6*time.Second), 10, time.Minute)

	// Auth — rate-limited but no JWT required
	mux.Handle("POST /v1/auth/signup", authRate(http.HandlerFunc(auth.Signup)))
	mux.Handle("POST /v1/auth/login", authRate(http.HandlerFunc(auth.Login)))
	mux.Handle("GET /v1/auth/me", chain(http.HandlerFunc(auth.Me), authed))

	// Projects
	mux.Handle("GET /v1/projects", chain(http.HandlerFunc(projects.List), authed))
	mux.Handle("POST /v1/projects", chain(http.HandlerFunc(projects.Create), authed))
	mux.Handle("GET /v1/projects/{id}", chain(http.HandlerFunc(projects.Get), authed))
	mux.Handle("DELETE /v1/projects/{id}", chain(http.HandlerFunc(projects.Delete), authed))

	// Keys
	mux.Handle("GET /v1/projects/{id}/keys", chain(http.HandlerFunc(keys.List), authed))
	mux.Handle("POST /v1/projects/{id}/keys", chain(http.HandlerFunc(keys.Create), authed))
	mux.Handle("DELETE /v1/projects/{id}/keys/{keyId}", chain(http.HandlerFunc(keys.Revoke), authed))

	// Credentials
	mux.Handle("GET /v1/projects/{id}/credentials", chain(http.HandlerFunc(creds.List), authed))
	mux.Handle("POST /v1/projects/{id}/credentials", chain(http.HandlerFunc(creds.Add), authed))
	mux.Handle("DELETE /v1/projects/{id}/credentials/{credId}", chain(http.HandlerFunc(creds.Revoke), authed))

	// Budget
	mux.Handle("GET /v1/projects/{id}/budget", chain(http.HandlerFunc(budgets.Get), authed))
	mux.Handle("PUT /v1/projects/{id}/budget", chain(http.HandlerFunc(budgets.Upsert), authed))
	mux.Handle("DELETE /v1/projects/{id}/budget", chain(http.HandlerFunc(budgets.Delete), authed))

	// Usage — summary must be registered before list to avoid ambiguity
	mux.Handle("GET /v1/projects/{id}/usage/summary", chain(http.HandlerFunc(usage.Summary), authed))
	mux.Handle("GET /v1/projects/{id}/usage", chain(http.HandlerFunc(usage.List), authed))

	// Models — public, no auth
	mux.HandleFunc("GET /v1/models", models.List)

	// Health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}, mux
}

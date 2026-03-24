package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/airelay/airelay/internal/cost"
	"github.com/airelay/airelay/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Handler is the main proxy HTTP handler.
type Handler struct {
	resolver     *KeyResolver
	budgets      *BudgetChecker
	logger       *Logger
	db           *pgxpool.Pool
	pricingCache sync.Map // map[string]*models.ModelPricing, keyed as "provider:model"
}

func NewHandler(db *pgxpool.Pool, rdb *redis.Client, encKey string) *Handler {
	budgets := NewBudgetChecker(db, rdb)
	return &Handler{
		resolver: NewKeyResolver(db, rdb, encKey),
		budgets:  budgets,
		logger:   NewLogger(db, budgets),
		db:       db,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Path: /proxy/{provider}/{...rest}
	path := strings.TrimPrefix(r.URL.Path, "/proxy/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] == "" {
		writeJSON(w, http.StatusBadRequest, "invalid proxy path: expected /proxy/{provider}/...")
		return
	}
	provider, ok := slugToProvider(parts[0])
	if !ok {
		writeJSON(w, http.StatusBadRequest, "unknown provider: "+parts[0])
		return
	}
	pathSuffix := "/" + parts[1]

	bearerKey := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if bearerKey == "" || !strings.HasPrefix(bearerKey, keyPrefix) {
		writeJSON(w, http.StatusUnauthorized, "missing or invalid AIRelay API key")
		return
	}

	lookup, err := h.resolver.Resolve(r.Context(), bearerKey, provider)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "invalid API key or no credential configured for provider")
		return
	}

	budget, err := h.budgets.CheckBudgets(r.Context(), lookup.ProjectID)
	redisDown := (err != nil) || (budget != nil && budget.RedisDown)
	if err != nil {
		log.Printf("budget check error for project %s: %v", lookup.ProjectID, err)
		// fail open
	} else if budget.Blocked {
		writeJSON(w, http.StatusTooManyRequests, "budget exceeded: "+budget.Reason)
		return
	}

	// Read model from body before Forward consumes it
	model := peekModel(r)
	metadata := parseMetadata(r.Header.Get("X-AIRelay-Meta"))

	providerBase := ProviderURLs[provider]
	fwdResult, err := Forward(w, r, providerBase, lookup.PlainKey, provider, pathSuffix)
	if err != nil {
		log.Printf("forward error for project %s: %v", lookup.ProjectID, err)
		return
	}

	event := UsageEvent{
		ProjectID:  lookup.ProjectID,
		APIKeyID:   lookup.APIKeyID,
		Provider:   string(provider),
		Model:      model,
		DurationMS: fwdResult.DurationMS,
		StatusCode: fwdResult.StatusCode,
		Metadata:   metadata,
	}

	if fwdResult.Usage != nil {
		event.PromptTokens = fwdResult.Usage.PromptTokens
		event.CompletionTokens = fwdResult.Usage.CompletionTokens
		if pricing := h.lookupPricing(r.Context(), string(provider), model); pricing != nil {
			c := cost.Calculate(event.PromptTokens, event.CompletionTokens, pricing)
			event.CostUSD = &c
		}
	}

	if redisDown {
		event.FailOpen = true
		if err := h.logger.LogDirect(r.Context(), event); err != nil {
			log.Printf("fail-open direct write failed for project %s: %v", lookup.ProjectID, err)
		}
	} else {
		h.logger.Log(event)
	}
}

// peekModel reads the model field from the request JSON body without consuming it.
func peekModel(r *http.Request) string {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "unknown"
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var payload struct {
		Model string `json:"model"`
	}
	json.Unmarshal(body, &payload)
	if payload.Model == "" {
		return "unknown"
	}
	return payload.Model
}

// lookupPricing returns pricing for a provider+model, using an in-process cache
// to avoid a Postgres query on every request. Cache is populated lazily and
// cleared on process restart (pricing sync runs every 24h, so staleness is bounded).
func (h *Handler) lookupPricing(ctx context.Context, provider, model string) *models.ModelPricing {
	key := provider + ":" + model
	if v, ok := h.pricingCache.Load(key); ok {
		return v.(*models.ModelPricing)
	}
	var pricing models.ModelPricing
	err := h.db.QueryRow(ctx,
		`SELECT input_cost_per_1k, output_cost_per_1k FROM model_pricing WHERE provider=$1 AND model=$2`,
		provider, model,
	).Scan(&pricing.InputCostPer1k, &pricing.OutputCostPer1k)
	if err != nil {
		h.pricingCache.Store(key, (*models.ModelPricing)(nil))
		return nil
	}
	pricing.Provider = provider
	pricing.Model = model
	h.pricingCache.Store(key, &pricing)
	return &pricing
}

func slugToProvider(slug string) (models.AIProvider, bool) {
	switch slug {
	case "openai":
		return models.ProviderOpenAI, true
	case "anthropic":
		return models.ProviderAnthropic, true
	case "google":
		return models.ProviderGoogle, true
	}
	return "", false
}

func writeJSON(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func parseMetadata(header string) map[string]any {
	if header == "" {
		return nil
	}
	var m map[string]any
	json.Unmarshal([]byte(header), &m)
	return m
}

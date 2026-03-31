package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

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

	// Read request body once — cap at 100 MiB to prevent memory exhaustion.
	// The same slice is passed to Forward to avoid a second allocation.
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeJSON(w, http.StatusBadRequest, "could not read request body")
		}
		return
	}

	model := extractModel(reqBody)
	metadata := parseMetadata(r.Header.Get("X-AIRelay-Meta"))

	providerBase := ProviderURLs[provider]
	fwdResult, err := Forward(w, r, reqBody, providerBase, lookup.PlainKey, provider, pathSuffix)
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

// extractModel parses the model field from a JSON request body.
// Returns "unknown" if the field is absent or the body is not valid JSON.
func extractModel(body []byte) string {
	var payload struct {
		Model string `json:"model"`
	}
	json.Unmarshal(body, &payload)
	if payload.Model == "" {
		return "unknown"
	}
	return payload.Model
}

const pricingCacheTTL = 24 * time.Hour

type cachedPricing struct {
	pricing   *models.ModelPricing // nil means "not found"
	expiresAt time.Time
}

// lookupPricing returns pricing for a provider+model, using an in-process cache
// with a 24-hour TTL. The pricing sync job also runs every 24h so staleness is
// bounded to at most two cycles in the worst case.
func (h *Handler) lookupPricing(ctx context.Context, provider, model string) *models.ModelPricing {
	key := provider + ":" + model
	if v, ok := h.pricingCache.Load(key); ok {
		c := v.(cachedPricing)
		if time.Now().Before(c.expiresAt) {
			return c.pricing
		}
	}
	var pricing models.ModelPricing
	err := h.db.QueryRow(ctx,
		`SELECT input_cost_per_1k, output_cost_per_1k FROM model_pricing WHERE provider=$1 AND model=$2`,
		provider, model,
	).Scan(&pricing.InputCostPer1k, &pricing.OutputCostPer1k)
	exp := time.Now().Add(pricingCacheTTL)
	if err != nil {
		h.pricingCache.Store(key, cachedPricing{pricing: nil, expiresAt: exp})
		return nil
	}
	pricing.Provider = provider
	pricing.Model = model
	h.pricingCache.Store(key, cachedPricing{pricing: &pricing, expiresAt: exp})
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

# AIRelay Plan 3 — Billing + Infrastructure + Distribution

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship AIRelay to production — Stripe billing for Pro/Team upgrades, Fly.io deployment with Docker, GitHub Actions CI/CD, landing page, signup welcome email, and PostHog analytics.

**Architecture:** Stripe webhooks update `users.plan` in Postgres; the management API handles checkout/portal sessions. Both services deploy as separate Fly.io apps built from a single multi-stage Dockerfile. The landing page is a static HTML file embedded in the API binary alongside the dashboard.

**Tech Stack:** Go 1.22+ (existing), stripe-go/v76, Fly.io, Docker (distroless), GitHub Actions, Resend REST API, PostHog JS snippet

---

## File Structure

```
internal/billing/
  stripe.go             - PriceMap (PriceToPlan, PlanPrice), Client (EnsureCustomer, CreateCheckoutSession, CreatePortalSession)
  stripe_test.go        - unit tests for price mapping

internal/email/
  resend.go             - Sender (send, SendWelcome) — no SDK, plain HTTP
  resend_test.go        - nil-safe no-op test

api/
  billing.go            - Checkout, Portal, Webhook handlers
  billing_test.go       - unit tests (nil client safe)

landing/
  embed.go              - //go:embed directive
  server.go             - Mount(mux, postHogKey) — registers GET /
  templates/
    index.html          - full landing page: hero, how-it-works, features, pricing toggle, CTA

db/migrations/
  010_users_billing.sql - add stripe_subscription_id to users

Dockerfile              - multi-stage: base → proxy target → api target
.dockerignore           - exclude .env, tmp, vendor
fly.proxy.toml          - Fly.io config for proxy service (port 8081)
fly.api.toml            - Fly.io config for api+dashboard service (port 8080)
.github/workflows/
  ci.yml                - test on every push; deploy proxy + api on main
```

---

## Chunk 1: Stripe Billing

**Files:**
- Modify: `internal/config/config.go`
- Create: `db/migrations/010_users_billing.sql`
- Create: `internal/billing/stripe.go` + `internal/billing/stripe_test.go`
- Create: `api/billing.go` + `api/billing_test.go`
- Modify: `api/server.go` — updated signature + billing routes
- Modify: `cmd/api/main.go` — wire billing

### Task 1: Extend config for Stripe, email, and app URL

- [ ] **Step 1: Add new fields to `internal/config/config.go`**

Open `internal/config/config.go`. Add these fields to the `Config` struct:

```go
// Stripe
StripeSecretKey      string
StripeWebhookSecret  string
StripePriceProMonthly  string
StripePriceProAnnual   string
StripePriceTeamMonthly string
StripePriceTeamAnnual  string

// App
AppURL string // e.g. https://api.airelay.dev (no trailing slash)

// Email
ResendAPIKey string
FromEmail    string

// Analytics
PostHogKey string
```

Add these to the `Load()` return:

```go
StripeSecretKey:       os.Getenv("STRIPE_SECRET_KEY"),
StripeWebhookSecret:   os.Getenv("STRIPE_WEBHOOK_SECRET"),
StripePriceProMonthly:  os.Getenv("STRIPE_PRICE_PRO_MONTHLY"),
StripePriceProAnnual:   os.Getenv("STRIPE_PRICE_PRO_ANNUAL"),
StripePriceTeamMonthly: os.Getenv("STRIPE_PRICE_TEAM_MONTHLY"),
StripePriceTeamAnnual:  os.Getenv("STRIPE_PRICE_TEAM_ANNUAL"),
AppURL:      getEnvOrDefault("APP_URL", "http://localhost:8080"),
ResendAPIKey: os.Getenv("RESEND_API_KEY"),
FromEmail:    getEnvOrDefault("FROM_EMAIL", "hello@airelay.dev"),
PostHogKey:  os.Getenv("POSTHOG_API_KEY"),
```

Add helper if not already present:

```go
func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 2: Update `.env.example`**

Add to `.env.example`:
```
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
STRIPE_PRICE_PRO_MONTHLY=price_...
STRIPE_PRICE_PRO_ANNUAL=price_...
STRIPE_PRICE_TEAM_MONTHLY=price_...
STRIPE_PRICE_TEAM_ANNUAL=price_...
APP_URL=http://localhost:8080
RESEND_API_KEY=re_...
FROM_EMAIL=hello@airelay.dev
POSTHOG_API_KEY=phc_...
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./internal/config/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go .env.example
git commit -m "feat: config — Stripe, email, app URL, PostHog fields"
```

---

### Task 2: Billing schema migration

- [ ] **Step 1: Create `db/migrations/010_users_billing.sql`**

```sql
-- +goose Up
ALTER TABLE users ADD COLUMN IF NOT EXISTS stripe_subscription_id TEXT;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS stripe_subscription_id;
```

- [ ] **Step 2: Run migration**

```bash
make migrate-up
```
Expected: migration 010 applied.

- [ ] **Step 3: Commit**

```bash
git add db/migrations/010_users_billing.sql
git commit -m "feat: migration 010 — stripe_subscription_id on users"
```

---

### Task 3: Stripe billing package

- [ ] **Step 1: Install Stripe Go SDK**

```bash
go get github.com/stripe/stripe-go/v76
```

- [ ] **Step 2: Write failing tests**

Create `internal/billing/stripe_test.go`:

```go
package billing_test

import (
	"testing"

	"github.com/airelay/airelay/internal/billing"
	"github.com/airelay/airelay/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPriceToPlan(t *testing.T) {
	prices := billing.PriceMap{
		ProMonthly:  "price_pro_m",
		ProAnnual:   "price_pro_a",
		TeamMonthly: "price_team_m",
		TeamAnnual:  "price_team_a",
	}
	require.Equal(t, models.PlanPro, prices.PriceToPlan("price_pro_m"))
	require.Equal(t, models.PlanPro, prices.PriceToPlan("price_pro_a"))
	require.Equal(t, models.PlanTeam, prices.PriceToPlan("price_team_m"))
	require.Equal(t, models.PlanTeam, prices.PriceToPlan("price_team_a"))
	require.Equal(t, models.PlanFree, prices.PriceToPlan("unknown"))
}

func TestPlanPrice_Monthly(t *testing.T) {
	prices := billing.PriceMap{ProMonthly: "price_pro_m", TeamMonthly: "price_team_m"}
	id, ok := prices.PlanPrice(models.PlanPro, "monthly")
	require.True(t, ok)
	require.Equal(t, "price_pro_m", id)

	id, ok = prices.PlanPrice(models.PlanTeam, "monthly")
	require.True(t, ok)
	require.Equal(t, "price_team_m", id)
}

func TestPlanPrice_Annual(t *testing.T) {
	prices := billing.PriceMap{ProMonthly: "pm", ProAnnual: "pa"}
	id, ok := prices.PlanPrice(models.PlanPro, "annual")
	require.True(t, ok)
	require.Equal(t, "pa", id)
}

func TestPlanPrice_Free(t *testing.T) {
	prices := billing.PriceMap{}
	_, ok := prices.PlanPrice(models.PlanFree, "monthly")
	require.False(t, ok)
}
```

- [ ] **Step 3: Run test — expect compilation failure**

```bash
go test ./internal/billing/... -v
```
Expected: compilation error — package not defined.

- [ ] **Step 4: Implement `internal/billing/stripe.go`**

Create `internal/billing/stripe.go`:

```go
package billing

import (
	"fmt"

	"github.com/airelay/airelay/internal/models"
	stripe "github.com/stripe/stripe-go/v76"
	checkoutsession "github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/customer"
	portalsession "github.com/stripe/stripe-go/v76/billingportal/session"
)

// PriceMap maps Stripe price IDs to plan tiers. Populated from config.
type PriceMap struct {
	ProMonthly  string
	ProAnnual   string
	TeamMonthly string
	TeamAnnual  string
}

// PriceToPlan returns the plan tier for a Stripe price ID.
// Returns PlanFree for unknown price IDs.
func (p PriceMap) PriceToPlan(priceID string) models.UserPlan {
	switch priceID {
	case p.ProMonthly, p.ProAnnual:
		return models.PlanPro
	case p.TeamMonthly, p.TeamAnnual:
		return models.PlanTeam
	}
	return models.PlanFree
}

// PlanPrice returns the Stripe price ID for the given plan and interval.
// interval is "monthly" or "annual". Returns ("", false) for free/unconfigured.
func (p PriceMap) PlanPrice(plan models.UserPlan, interval string) (string, bool) {
	switch plan {
	case models.PlanPro:
		if interval == "annual" && p.ProAnnual != "" {
			return p.ProAnnual, true
		}
		if p.ProMonthly != "" {
			return p.ProMonthly, true
		}
	case models.PlanTeam:
		if interval == "annual" && p.TeamAnnual != "" {
			return p.TeamAnnual, true
		}
		if p.TeamMonthly != "" {
			return p.TeamMonthly, true
		}
	}
	return "", false
}

// Client wraps Stripe API operations.
type Client struct {
	prices PriceMap
	appURL string
}

// NewClient configures the Stripe SDK and returns a client.
func NewClient(secretKey, appURL string, prices PriceMap) *Client {
	stripe.Key = secretKey
	return &Client{prices: prices, appURL: appURL}
}

// EnsureCustomer returns the existing stripeCustomerID, or creates a new Stripe
// customer if it is empty. Returns the customer ID to store in the database.
func (c *Client) EnsureCustomer(email, stripeCustomerID string) (string, error) {
	if stripeCustomerID != "" {
		return stripeCustomerID, nil
	}
	cust, err := customer.New(&stripe.CustomerParams{Email: stripe.String(email)})
	if err != nil {
		return "", fmt.Errorf("create stripe customer: %w", err)
	}
	return cust.ID, nil
}

// CreateCheckoutSession creates a Stripe Checkout session for the given plan
// and interval ("monthly" or "annual"). Returns the hosted checkout URL.
func (c *Client) CreateCheckoutSession(customerID, planStr, interval string) (string, error) {
	priceID, ok := c.prices.PlanPrice(models.UserPlan(planStr), interval)
	if !ok {
		return "", fmt.Errorf("no price configured for plan %q interval %q", planStr, interval)
	}
	s, err := checkoutsession.New(&stripe.CheckoutSessionParams{
		Customer: stripe.String(customerID),
		Mode:     stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(priceID), Quantity: stripe.Int64(1)},
		},
		SuccessURL: stripe.String(c.appURL + "/dashboard/projects?upgraded=1"),
		CancelURL:  stripe.String(c.appURL + "/dashboard/projects"),
	})
	if err != nil {
		return "", fmt.Errorf("create checkout session: %w", err)
	}
	return s.URL, nil
}

// CreatePortalSession creates a Stripe billing portal session for managing
// an existing subscription. Returns the hosted portal URL.
func (c *Client) CreatePortalSession(customerID string) (string, error) {
	s, err := portalsession.New(&stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(c.appURL + "/dashboard/projects"),
	})
	if err != nil {
		return "", fmt.Errorf("create portal session: %w", err)
	}
	return s.URL, nil
}
```

- [ ] **Step 5: Run tests — expect pass**

```bash
go test ./internal/billing/... -v
```
Expected: PASS (4 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/billing/ go.mod go.sum
git commit -m "feat: Stripe billing package — PriceMap, checkout session, billing portal"
```

---

### Task 4: Billing API handlers

- [ ] **Step 1: Write failing tests**

Create `api/billing_test.go`:

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCheckout_NoClaims(t *testing.T) {
	h := api.NewBillingHandler(nil, nil, nil, "")
	body, _ := json.Marshal(map[string]string{"plan": "pro", "interval": "monthly"})
	req := httptest.NewRequest(http.MethodPost, "/v1/billing/checkout", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Checkout(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCheckout_InvalidBody(t *testing.T) {
	h := api.NewBillingHandler(nil, nil, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/v1/billing/checkout", bytes.NewBufferString("bad"))
	req = injectClaims(req, models.PlanFree)
	w := httptest.NewRecorder()
	h.Checkout(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCheckout_InvalidPlan(t *testing.T) {
	h := api.NewBillingHandler(nil, nil, nil, "")
	body, _ := json.Marshal(map[string]string{"plan": "enterprise", "interval": "monthly"})
	req := httptest.NewRequest(http.MethodPost, "/v1/billing/checkout", bytes.NewReader(body))
	req = injectClaims(req, models.PlanFree)
	w := httptest.NewRecorder()
	h.Checkout(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPortal_NoClaims(t *testing.T) {
	h := api.NewBillingHandler(nil, nil, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/v1/billing/portal", nil)
	w := httptest.NewRecorder()
	h.Portal(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestWebhook_BadSignature(t *testing.T) {
	h := api.NewBillingHandler(nil, nil, nil, "whsec_test")
	req := httptest.NewRequest(http.MethodPost, "/v1/billing/webhook", bytes.NewBufferString(`{}`))
	req.Header.Set("Stripe-Signature", "bad")
	w := httptest.NewRecorder()
	h.Webhook(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
```

- [ ] **Step 2: Run test — expect compilation failure**

```bash
go test ./api/... -run "TestCheckout|TestPortal|TestWebhook" -v
```
Expected: compilation error.

- [ ] **Step 3: Implement `api/billing.go`**

Create `api/billing.go`:

```go
package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/airelay/airelay/internal/billing"
	"github.com/airelay/airelay/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
	stripe "github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/webhook"
)

// BillingHandler handles Stripe checkout, portal, and webhook endpoints.
type BillingHandler struct {
	db            *pgxpool.Pool
	stripe        *billing.Client
	prices        billing.PriceMap
	webhookSecret string
}

// NewBillingHandler constructs the handler. stripeClient and prices may be nil
// when Stripe is unconfigured (dev/test); affected endpoints return 503.
func NewBillingHandler(db *pgxpool.Pool, stripeClient *billing.Client, prices *billing.PriceMap, webhookSecret string) *BillingHandler {
	h := &BillingHandler{db: db, stripe: stripeClient, webhookSecret: webhookSecret}
	if prices != nil {
		h.prices = *prices
	}
	return h
}

// POST /v1/billing/checkout
// Body: {"plan":"pro","interval":"monthly"}
// Returns: {"checkout_url":"https://checkout.stripe.com/..."}
func (h *BillingHandler) Checkout(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var req struct {
		Plan     string `json:"plan"`
		Interval string `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch models.UserPlan(req.Plan) {
	case models.PlanPro, models.PlanTeam:
	default:
		writeError(w, http.StatusBadRequest, "plan must be pro or team")
		return
	}
	if req.Interval != "annual" {
		req.Interval = "monthly"
	}
	if h.stripe == nil {
		writeError(w, http.StatusServiceUnavailable, "billing not configured")
		return
	}
	// Look up user email and existing stripe_customer_id
	var email, stripeCustomerID string
	h.db.QueryRow(r.Context(),
		`SELECT email, COALESCE(stripe_customer_id, '') FROM users WHERE id=$1`,
		claims.UserID,
	).Scan(&email, &stripeCustomerID)

	// Lazy Stripe customer creation — no API call on signup
	newCustomerID, err := h.stripe.EnsureCustomer(email, stripeCustomerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create billing customer")
		return
	}
	if newCustomerID != stripeCustomerID {
		h.db.Exec(r.Context(),
			`UPDATE users SET stripe_customer_id=$1 WHERE id=$2`,
			newCustomerID, claims.UserID,
		)
	}
	checkoutURL, err := h.stripe.CreateCheckoutSession(newCustomerID, req.Plan, req.Interval)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create checkout session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"checkout_url": checkoutURL})
}

// GET /v1/billing/portal
// Returns: {"portal_url":"https://billing.stripe.com/..."}
func (h *BillingHandler) Portal(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if h.stripe == nil {
		writeError(w, http.StatusServiceUnavailable, "billing not configured")
		return
	}
	var stripeCustomerID string
	h.db.QueryRow(r.Context(),
		`SELECT COALESCE(stripe_customer_id, '') FROM users WHERE id=$1`,
		claims.UserID,
	).Scan(&stripeCustomerID)
	if stripeCustomerID == "" {
		writeError(w, http.StatusBadRequest, "no billing account — upgrade first")
		return
	}
	portalURL, err := h.stripe.CreatePortalSession(stripeCustomerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create portal session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"portal_url": portalURL})
}

// POST /v1/billing/webhook
// No auth — verified via Stripe-Signature header.
// Handled events: checkout.session.completed, customer.subscription.updated,
// customer.subscription.deleted
func (h *BillingHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read body")
		return
	}
	event, err := webhook.ConstructEvent(body, r.Header.Get("Stripe-Signature"), h.webhookSecret)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook signature")
		return
	}

	switch event.Type {
	case "checkout.session.completed":
		var s stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &s); err != nil {
			break
		}
		if s.Subscription != nil && s.Customer != nil {
			h.db.Exec(r.Context(),
				`UPDATE users SET stripe_subscription_id=$1 WHERE stripe_customer_id=$2`,
				s.Subscription.ID, s.Customer.ID,
			)
		}

	case "customer.subscription.updated":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			break
		}
		if len(sub.Items.Data) > 0 {
			priceID := sub.Items.Data[0].Price.ID
			plan := h.prices.PriceToPlan(priceID)
			if sub.Status == stripe.SubscriptionStatusActive || sub.Status == stripe.SubscriptionStatusTrialing {
				h.db.Exec(r.Context(),
					`UPDATE users SET plan=$1 WHERE stripe_customer_id=$2`,
					string(plan), sub.Customer.ID,
				)
			}
		}

	case "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			break
		}
		h.db.Exec(r.Context(),
			`UPDATE users SET plan='free', stripe_subscription_id=NULL WHERE stripe_customer_id=$1`,
			sub.Customer.ID,
		)
	}

	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./api/... -run "TestCheckout|TestPortal|TestWebhook" -v
```
Expected: PASS (5 tests).

- [ ] **Step 5: Update `api/server.go` — new signature + billing routes**

`NewServer` now accepts stripe client and prices. Open `api/server.go` and update:

```go
// Updated signature:
func NewServer(db *pgxpool.Pool, rdb *redis.Client, cfg *config.Config,
	stripeClient *billing.Client, prices *billing.PriceMap) (*http.Server, *http.ServeMux) {
```

Add the billing import at the top:
```go
"github.com/airelay/airelay/internal/billing"
```

Inside `NewServer`, after `models := NewModelsHandler(db)`, add:
```go
billingH := NewBillingHandler(db, stripeClient, prices, cfg.StripeWebhookSecret)
```

Add these routes after the models route:
```go
// Billing
mux.Handle("POST /v1/billing/checkout", chain(http.HandlerFunc(billingH.Checkout), authed))
mux.Handle("GET /v1/billing/portal", chain(http.HandlerFunc(billingH.Portal), authed))
mux.HandleFunc("POST /v1/billing/webhook", billingH.Webhook) // no auth — Stripe signature
```

- [ ] **Step 6: Update `cmd/api/main.go`**

After the `rdb` setup, add:

```go
var stripeClient *billing.Client
var prices *billing.PriceMap
if cfg.StripeSecretKey != "" {
	prices = &billing.PriceMap{
		ProMonthly:  cfg.StripePriceProMonthly,
		ProAnnual:   cfg.StripePriceProAnnual,
		TeamMonthly: cfg.StripePriceTeamMonthly,
		TeamAnnual:  cfg.StripePriceTeamAnnual,
	}
	stripeClient = billing.NewClient(cfg.StripeSecretKey, cfg.AppURL, *prices)
}
```

Update the `api.NewServer` call:
```go
srv, mux := api.NewServer(pool, rdb, cfg, stripeClient, prices)
```

Add import: `"github.com/airelay/airelay/internal/billing"`

- [ ] **Step 7: Build and verify**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add api/billing.go api/billing_test.go api/server.go cmd/api/main.go
git commit -m "feat: Stripe billing — checkout, portal, webhook with subscription plan sync"
```

---

## Chunk 2: Docker + Fly.io Deployment

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`
- Create: `fly.proxy.toml`
- Create: `fly.api.toml`
- Modify: `Makefile` — add docker and deploy targets

### Task 5: Dockerfile

- [ ] **Step 1: Create `.dockerignore`**

```
.env
.env.*
tmp/
*.test
docs/
```

- [ ] **Step 2: Create `Dockerfile`**

Multi-stage build. The `base` stage downloads dependencies once. `proxy` and `api` are separate final images.

```dockerfile
# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS base
RUN apk add --no-cache ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# --- Proxy binary ---
FROM base AS proxy-build
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /proxy ./cmd/proxy

FROM gcr.io/distroless/static-debian12 AS proxy
COPY --from=proxy-build /proxy /proxy
EXPOSE 8081
ENTRYPOINT ["/proxy"]

# --- API + dashboard binary ---
FROM base AS api-build
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /api ./cmd/api

FROM gcr.io/distroless/static-debian12 AS api
COPY --from=api-build /api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
```

- [ ] **Step 3: Build and test both images**

```bash
docker build --target proxy -t airelay-proxy:local .
docker build --target api -t airelay-api:local .
```
Expected: both images build successfully.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile .dockerignore
git commit -m "feat: multi-stage Dockerfile — distroless proxy and api images"
```

---

### Task 6: Fly.io configuration

- [ ] **Step 1: Create `fly.proxy.toml`**

```toml
app = "airelay-proxy"
primary_region = "iad"
kill_signal = "SIGINT"
kill_timeout = "5s"

[build]
  dockerfile = "Dockerfile"
  target = "proxy"

[http_service]
  internal_port = 8081
  force_https = true
  auto_stop_machines = true
  auto_start_machines = true
  min_machines_running = 1

  [http_service.concurrency]
    type = "connections"
    hard_limit = 250
    soft_limit = 200

[checks]
  [checks.health]
    grace_period = "10s"
    interval = "30s"
    method = "GET"
    path = "/health"
    port = 8081
    timeout = "5s"
    type = "http"
```

- [ ] **Step 2: Create `fly.api.toml`**

```toml
app = "airelay-api"
primary_region = "iad"
kill_signal = "SIGINT"
kill_timeout = "5s"

[build]
  dockerfile = "Dockerfile"
  target = "api"

[http_service]
  internal_port = 8080
  force_https = true
  auto_stop_machines = true
  auto_start_machines = true
  min_machines_running = 1

[checks]
  [checks.health]
    grace_period = "10s"
    interval = "30s"
    method = "GET"
    path = "/health"
    port = 8080
    timeout = "5s"
    type = "http"
```

- [ ] **Step 3: Add deploy targets to Makefile**

Open `Makefile`, add after the existing targets:

```makefile
deploy-proxy:
	fly deploy --config fly.proxy.toml --remote-only

deploy-api:
	fly deploy --config fly.api.toml --remote-only

deploy: deploy-proxy deploy-api
```

- [ ] **Step 4: Commit**

```bash
git add fly.proxy.toml fly.api.toml Makefile
git commit -m "feat: Fly.io deployment config — proxy (8081) and api (8080) apps"
```

---

## Chunk 3: GitHub Actions CI/CD

**Files:**
- Create: `.github/workflows/ci.yml`

### Task 7: CI/CD pipeline

- [ ] **Step 1: Create `.github/workflows/ci.yml`**

```yaml
name: CI/CD

on:
  push:
    branches: ["*"]
  pull_request:
    branches: [main]

jobs:
  test:
    name: Test
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:15
        env:
          POSTGRES_USER: airelay
          POSTGRES_PASSWORD: airelay
          POSTGRES_DB: airelay_test
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
        ports:
          - 5432:5432
      redis:
        image: redis:7
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
        ports:
          - 6379:6379
    env:
      DATABASE_URL: postgres://airelay:airelay@localhost:5432/airelay_test?sslmode=disable
      REDIS_URL: redis://localhost:6379
      JWT_SECRET: testsecret_ci
      CREDENTIAL_ENCRYPTION_KEY: abcdefghijklmnopqrstuvwxyz123456
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache: true
      - name: Install goose
        run: go install github.com/pressly/goose/v3/cmd/goose@latest
      - name: Run migrations
        run: goose -dir db/migrations postgres "$DATABASE_URL" up
      - name: Run tests
        run: go test ./... -timeout 60s

  deploy-proxy:
    name: Deploy Proxy
    needs: test
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: superfly/flyctl-actions/setup-flyctl@master
      - name: Deploy proxy
        run: fly deploy --config fly.proxy.toml --remote-only
        env:
          FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}

  deploy-api:
    name: Deploy API
    needs: test
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: superfly/flyctl-actions/setup-flyctl@master
      - name: Deploy API
        run: fly deploy --config fly.api.toml --remote-only
        env:
          FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
```

- [ ] **Step 2: Create Fly deploy token and add GitHub secret**

```bash
fly tokens create deploy -x 999999h
```
Copy the output. In GitHub: repository → Settings → Secrets → Actions → New secret named `FLY_API_TOKEN`.

- [ ] **Step 3: Commit and verify CI runs**

```bash
git add .github/
git commit -m "feat: GitHub Actions CI — test on push, deploy proxy + api on main"
git push
```
Expected: CI workflow appears in GitHub → Actions tab.

---

## Chunk 4: Landing Page

**Files:**
- Create: `landing/embed.go`
- Create: `landing/server.go`
- Create: `landing/templates/index.html`
- Modify: `cmd/api/main.go` — mount landing routes

### Task 8: Landing page

- [ ] **Step 1: Create `landing/embed.go`**

```go
package landing

import "embed"

//go:embed templates
var TemplateFS embed.FS
```

- [ ] **Step 2: Create `landing/server.go`**

```go
package landing

import (
	"html/template"
	"log"
	"net/http"
)

type pageData struct {
	PostHogKey string
}

// Mount registers the landing page at GET /. Requires exact path match
// so it does not capture dashboard or API routes.
func Mount(mux *http.ServeMux, postHogKey string) {
	tmpl, err := template.ParseFS(TemplateFS, "templates/index.html")
	if err != nil {
		log.Fatalf("landing: parse templates: %v", err)
	}
	data := pageData{PostHogKey: postHogKey}
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
			log.Printf("landing: render: %v", err)
		}
	})
}
```

- [ ] **Step 3: Create `landing/templates/index.html`**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>AIRelay — Stop surprise AI bills</title>
  <meta name="description" content="AIRelay enforces real-time budgets across OpenAI, Anthropic, and Google. One environment variable. No code changes.">
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #fff; color: #111; }
    a { color: inherit; }
    nav { max-width: 1100px; margin: 0 auto; padding: 20px 24px; display: flex; align-items: center; justify-content: space-between; }
    .nav-brand { font-weight: 800; font-size: 20px; text-decoration: none; }
    .nav-links { display: flex; gap: 24px; align-items: center; }
    .nav-links a { text-decoration: none; color: #555; font-size: 15px; }
    .nav-links a:hover { color: #111; }
    .btn { display: inline-block; padding: 10px 20px; border-radius: 7px; font-size: 15px; font-weight: 600; cursor: pointer; text-decoration: none; border: none; }
    .btn-primary { background: #2563eb; color: #fff; }
    .btn-primary:hover { background: #1d4ed8; }
    .btn-outline { background: transparent; color: #2563eb; border: 1.5px solid #2563eb; }
    .btn-outline:hover { background: #eff6ff; }
    .hero { max-width: 760px; margin: 80px auto 60px; text-align: center; padding: 0 24px; }
    .hero h1 { font-size: clamp(34px, 6vw, 54px); font-weight: 800; line-height: 1.1; margin-bottom: 20px; }
    .hero h1 span { color: #2563eb; }
    .hero p { font-size: 19px; color: #555; line-height: 1.6; margin-bottom: 36px; max-width: 560px; margin-left: auto; margin-right: auto; }
    .hero-cta { display: flex; gap: 12px; justify-content: center; flex-wrap: wrap; }
    .hero-code { background: #f1f5f9; border: 1px solid #e2e8f0; border-radius: 8px; padding: 14px 20px; font-family: "SF Mono","Fira Code",monospace; font-size: 14px; text-align: left; max-width: 520px; margin: 40px auto 0; line-height: 1.8; }
    .hero-code .comment { color: #64748b; }
    .hero-code .key { color: #7c3aed; font-weight: 600; }
    .hero-code .val { color: #059669; }
    .section { max-width: 900px; margin: 0 auto; padding: 80px 24px; }
    .section h2 { font-size: 32px; font-weight: 700; text-align: center; margin-bottom: 48px; }
    .steps { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 32px; }
    .step { text-align: center; }
    .step-num { width: 40px; height: 40px; border-radius: 50%; background: #2563eb; color: #fff; font-weight: 700; font-size: 16px; display: flex; align-items: center; justify-content: center; margin: 0 auto 16px; }
    .step h3 { font-size: 17px; font-weight: 700; margin-bottom: 8px; }
    .step p { font-size: 14px; color: #555; line-height: 1.6; }
    .features-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 24px; }
    .feature-card { background: #f9fafb; border: 1px solid #e5e7eb; border-radius: 10px; padding: 24px; }
    .feature-card h3 { font-size: 16px; font-weight: 700; margin-bottom: 8px; }
    .feature-card p { font-size: 14px; color: #555; line-height: 1.6; }
    .pricing-section { background: #f9fafb; }
    .pricing-toggle { display: flex; align-items: center; justify-content: center; gap: 10px; margin-bottom: 40px; }
    .toggle-btn { padding: 7px 18px; border-radius: 20px; border: 1.5px solid #d1d5db; cursor: pointer; font-size: 14px; font-weight: 500; background: #fff; }
    .toggle-btn.active { background: #2563eb; color: #fff; border-color: #2563eb; }
    .badge-save { background: #d1fae5; color: #065f46; font-size: 11px; font-weight: 700; padding: 2px 7px; border-radius: 12px; vertical-align: middle; }
    .pricing-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 24px; max-width: 820px; margin: 0 auto; }
    .price-card { background: #fff; border: 1.5px solid #e5e7eb; border-radius: 12px; padding: 28px; }
    .price-card.featured { border-color: #2563eb; }
    .plan-name { font-size: 12px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.06em; color: #6b7280; margin-bottom: 8px; }
    .price { font-size: 38px; font-weight: 800; margin-bottom: 2px; }
    .price-sub { font-size: 13px; color: #6b7280; margin-bottom: 22px; }
    .price-card ul { list-style: none; margin-bottom: 24px; }
    .price-card ul li { font-size: 14px; color: #374151; padding: 5px 0 5px 22px; position: relative; }
    .price-card ul li::before { content: "✓"; position: absolute; left: 0; color: #2563eb; font-weight: 700; }
    .price-card ul li.dim { color: #9ca3af; }
    .price-card ul li.dim::before { color: #9ca3af; content: "–"; }
    footer { border-top: 1px solid #e5e7eb; padding: 32px 24px; max-width: 900px; margin: 0 auto; display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 16px; font-size: 13px; color: #6b7280; }
    footer a { color: #6b7280; text-decoration: none; }
    footer a:hover { color: #111; }
  </style>
</head>
<body>

<nav>
  <a href="/" class="nav-brand">AIRelay</a>
  <div class="nav-links">
    <a href="/dashboard/login">Sign in</a>
    <a href="/dashboard/signup" class="btn btn-primary" style="padding:8px 16px;font-size:14px;">Get started free</a>
  </div>
</nav>

<section class="hero">
  <h1>Stop <span>surprise</span><br>AI bills</h1>
  <p>Real-time budget enforcement across OpenAI, Anthropic, and Google. One environment variable change. No code rewrites.</p>
  <div class="hero-cta">
    <a href="/dashboard/signup" class="btn btn-primary">Get started free</a>
    <a href="https://github.com/airelay/airelay" class="btn btn-outline">View source</a>
  </div>
  <div class="hero-code">
    <div class="comment"># Before</div>
    <div><span class="key">OPENAI_BASE_URL</span>=<span class="val">https://api.openai.com/v1</span></div>
    <br>
    <div class="comment"># After — that's it</div>
    <div><span class="key">OPENAI_BASE_URL</span>=<span class="val">https://api.airelay.dev/proxy/openai</span></div>
  </div>
</section>

<section class="section">
  <h2>How it works</h2>
  <div class="steps">
    <div class="step">
      <div class="step-num">1</div>
      <h3>Point your SDK</h3>
      <p>Change one environment variable. Your OpenAI, Anthropic, or Google SDK routes through AIRelay — no SDK changes, no code rewrites.</p>
    </div>
    <div class="step">
      <div class="step-num">2</div>
      <h3>Set a budget</h3>
      <p>Create daily or monthly spending limits per project. Hard limits block requests at 100%. Soft alerts warn you at 75% and 90%.</p>
    </div>
    <div class="step">
      <div class="step-num">3</div>
      <h3>Stay in control</h3>
      <p>Watch spend in real time on the dashboard. No more checking your AI invoice at end of month in horror.</p>
    </div>
  </div>
</section>

<section class="section" style="padding-top:0;">
  <h2>Built for developers</h2>
  <div class="features-grid">
    <div class="feature-card">
      <h3>Real-time enforcement</h3>
      <p>Budget checks run synchronously on every request. A $5 limit means you never exceed $5, even under high concurrency.</p>
    </div>
    <div class="feature-card">
      <h3>Multi-provider</h3>
      <p>One proxy, three providers. OpenAI, Anthropic, and Google Gemini. Single dashboard shows spend across all of them.</p>
    </div>
    <div class="feature-card">
      <h3>Fail-open by design</h3>
      <p>If AIRelay has an outage, your requests pass through. We never become a single point of failure for your product.</p>
    </div>
    <div class="feature-card">
      <h3>Per-user attribution</h3>
      <p>Pass <code style="background:#f3f4f6;padding:1px 5px;border-radius:3px;font-size:12px;">X-AIRelay-Meta</code> headers to track cost per end-user. Know exactly which features cost money. Pro+.</p>
    </div>
    <div class="feature-card">
      <h3>No request volume gates</h3>
      <p>We charge for the intelligence layer, not your traffic. Route as many requests as you need on any plan.</p>
    </div>
    <div class="feature-card">
      <h3>Source-available</h3>
      <p>The proxy engine is published under BSL 1.1. Audit exactly what happens to your API calls. No black boxes.</p>
    </div>
  </div>
</section>

<section class="section pricing-section">
  <h2>Simple pricing</h2>
  <div class="pricing-toggle">
    <button class="toggle-btn active" id="btn-monthly" onclick="setPricing('monthly')">Monthly</button>
    <button class="toggle-btn" id="btn-annual" onclick="setPricing('annual')">Annual <span class="badge-save">2 months free</span></button>
  </div>
  <div class="pricing-grid">
    <div class="price-card">
      <div class="plan-name">Free</div>
      <div class="price">$0</div>
      <div class="price-sub">forever</div>
      <ul>
        <li>1 project, 1 API key</li>
        <li>7-day usage history</li>
        <li>Email alerts</li>
        <li class="dim">Webhooks</li>
        <li class="dim">Metadata attribution</li>
        <li class="dim">Team seats</li>
      </ul>
      <a href="/dashboard/signup" class="btn btn-outline" style="width:100%;text-align:center;display:block;">Get started</a>
    </div>
    <div class="price-card featured">
      <div class="plan-name">Pro</div>
      <div class="price" id="pro-price">$79</div>
      <div class="price-sub" id="pro-sub">/month</div>
      <ul>
        <li>Unlimited projects &amp; keys</li>
        <li>90-day usage history</li>
        <li>Email + webhook alerts</li>
        <li>Metadata cost attribution</li>
        <li class="dim">Team seats</li>
        <li class="dim">Billing webhooks</li>
      </ul>
      <a href="/dashboard/signup" class="btn btn-primary" style="width:100%;text-align:center;display:block;">Start free trial</a>
    </div>
    <div class="price-card">
      <div class="plan-name">Team</div>
      <div class="price" id="team-price">$199</div>
      <div class="price-sub" id="team-sub">/month</div>
      <ul>
        <li>Everything in Pro</li>
        <li>5 team seats</li>
        <li>Unlimited history</li>
        <li>Billing webhooks</li>
        <li>99.9% uptime SLA</li>
        <li>Slack alerts</li>
      </ul>
      <a href="/dashboard/signup" class="btn btn-outline" style="width:100%;text-align:center;display:block;">Get started</a>
    </div>
  </div>
</section>

<section class="section" style="text-align:center; padding:80px 24px;">
  <h2 style="margin-bottom:16px;">Ready to stop worrying about your AI bill?</h2>
  <p style="color:#555; margin-bottom:32px; font-size:17px;">Free forever. No credit card required.</p>
  <a href="/dashboard/signup" class="btn btn-primary" style="font-size:17px;padding:14px 32px;">Create your free account</a>
</section>

<footer>
  <div>© 2026 AIRelay</div>
  <div style="display:flex;gap:20px;">
    <a href="https://github.com/airelay/airelay">GitHub</a>
    <a href="/dashboard/login">Sign in</a>
    <a href="mailto:hello@airelay.dev">Contact</a>
  </div>
</footer>

<script>
var pricingData = {
  monthly: {pro: '$79', team: '$199', sub: '/month'},
  annual:  {pro: '$790', team: '$1,990', sub: '/year'}
};
function setPricing(mode) {
  document.getElementById('btn-monthly').className = 'toggle-btn' + (mode === 'monthly' ? ' active' : '');
  document.getElementById('btn-annual').className  = 'toggle-btn' + (mode === 'annual'  ? ' active' : '');
  document.getElementById('pro-price').textContent  = pricingData[mode].pro;
  document.getElementById('team-price').textContent = pricingData[mode].team;
  document.getElementById('pro-sub').textContent    = pricingData[mode].sub;
  document.getElementById('team-sub').textContent   = pricingData[mode].sub;
}
</script>

{{if .PostHogKey}}
<script>
!function(t,e){var o,n,p,r;e.__SV||(window.posthog=e,e._i=[],e.init=function(i,s,a){function g(t,e){var o=e.split(".");2==o.length&&(t=t[o[0]],e=o[1]),t[e]=function(){t.push([e].concat(Array.prototype.slice.call(arguments,0)))}}(p=t.createElement("script")).type="text/javascript",p.crossOrigin="anonymous",p.async=!0,p.src=s.api_host+"/static/array.js",(r=t.getElementsByTagName("script")[0]).parentNode.insertBefore(p,r);var u=e;for(void 0!==a?u=e[a]=[]:a="posthog",u.people=u.people||[],u.toString=function(t){var e="posthog";return"posthog"!==a&&(e+="."+a),t||(e+=" (stub)"),e},u.people.toString=function(){return u.toString(1)+".people (stub)"},o="capture identify alias people.set people.set_once set_config register register_once unregister opt_out_capturing has_opted_out_capturing opt_in_capturing reset isFeatureEnabled onFeatureFlags getFeatureFlag getFeatureFlagPayload reloadFeatureFlags group updateEarlyAccessFeatureEnrollment getEarlyAccessFeatures getActiveMatchingSurveys getSurveys onSessionId".split(" "),n=0;n<o.length;n++)g(u,o[n]);e._i.push([i,s,a])},e.__SV=1)}(document,window.posthog||[]);
posthog.init('{{.PostHogKey}}', {api_host: 'https://app.posthog.com'});
</script>
{{end}}

</body>
</html>
```

- [ ] **Step 4: Mount landing in `cmd/api/main.go`**

Add import: `"github.com/airelay/airelay/landing"`

After `dashboard.NewDashboardServer(mux, cfg.PostHogKey)`, add:
```go
landing.Mount(mux, cfg.PostHogKey)
```

- [ ] **Step 5: Build and verify**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 6: Smoke test landing page**

```bash
export $(cat .env | xargs) && go run ./cmd/api/ &
curl -s http://localhost:8080/ | grep -o "Stop surprise"
```
Expected: `Stop surprise`

- [ ] **Step 7: Commit**

```bash
git add landing/ cmd/api/main.go
git commit -m "feat: landing page — hero, how-it-works, features, pricing toggle with annual option"
```

---

## Chunk 5: Welcome Email + PostHog Dashboard Analytics

**Files:**
- Create: `internal/email/resend.go` + `internal/email/resend_test.go`
- Modify: `api/auth.go` — send welcome email after signup
- Modify: `api/auth_test.go` — update `NewAuthHandler` call signature
- Modify: `api/server.go` — pass email sender
- Modify: `dashboard/handler.go` — thread PostHogKey through templates
- Modify: `dashboard/server.go` — accept postHogKey
- Modify: `dashboard/templates/layout.html` — PostHog snippet
- Modify: `cmd/api/main.go` — pass PostHogKey to dashboard

### Task 9: Welcome email via Resend

- [ ] **Step 1: Write failing test**

Create `internal/email/resend_test.go`:

```go
package email_test

import (
	"testing"

	"github.com/airelay/airelay/internal/email"
	"github.com/stretchr/testify/require"
)

func TestSender_NilSafe(t *testing.T) {
	// An unconfigured Sender (empty API key) must not panic — it's a silent no-op.
	s := email.NewSender("", "noreply@example.com")
	err := s.SendWelcome("user@example.com")
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run test — expect compilation failure**

```bash
go test ./internal/email/... -v
```
Expected: compilation error.

- [ ] **Step 3: Implement `internal/email/resend.go`**

Create `internal/email/resend.go`:

```go
package email

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Sender sends transactional email via the Resend REST API.
// If APIKey is empty, all operations are no-ops (useful in dev/test).
type Sender struct {
	apiKey    string
	fromEmail string
	client    *http.Client
}

// NewSender creates an email sender. Pass empty apiKey to disable.
func NewSender(apiKey, fromEmail string) *Sender {
	return &Sender{
		apiKey:    apiKey,
		fromEmail: fromEmail,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

type resendPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

func (s *Sender) send(to, subject, html string) error {
	if s.apiKey == "" {
		return nil
	}
	payload := resendPayload{From: s.fromEmail, To: []string{to}, Subject: subject, HTML: html}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("email: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("email: send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("email: resend returned %d", resp.StatusCode)
	}
	return nil
}

// SendWelcome sends the onboarding email to a new user.
func (s *Sender) SendWelcome(to string) error {
	return s.send(to, "Welcome to AIRelay — get started in 2 minutes", welcomeHTML)
}

const welcomeHTML = `<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;color:#111;">
  <h1 style="font-size:24px;font-weight:800;margin-bottom:8px;">Welcome to AIRelay</h1>
  <p style="color:#555;margin-bottom:24px;">You're one environment variable away from never worrying about surprise AI bills again.</p>
  <h2 style="font-size:16px;font-weight:700;margin-bottom:12px;">Quick start (60 seconds)</h2>
  <div style="background:#f1f5f9;border-radius:8px;padding:16px;font-family:monospace;font-size:13px;margin-bottom:24px;line-height:1.8;">
    <div style="color:#64748b;"># 1. Create a project and add your OpenAI key</div>
    <div style="color:#64748b;"># 2. Generate an AIRelay API key</div>
    <div style="color:#64748b;"># 3. Update your env:</div>
    <div><span style="color:#7c3aed;">OPENAI_API_KEY</span>=air_sk_your_key_here</div>
    <div><span style="color:#7c3aed;">OPENAI_BASE_URL</span>=<span style="color:#059669;">https://api.airelay.dev/proxy/openai</span></div>
  </div>
  <p style="color:#555;margin-bottom:24px;">Set a monthly budget to start blocking overspend. Hard limits stop requests at 100%; soft alerts email you at 75% and 90%.</p>
  <a href="https://api.airelay.dev/dashboard/projects" style="display:inline-block;background:#2563eb;color:#fff;padding:12px 24px;border-radius:7px;text-decoration:none;font-weight:600;">Open your dashboard →</a>
  <p style="color:#9ca3af;font-size:12px;margin-top:32px;">AIRelay · Reply with any questions</p>
</div>`
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/email/... -v
```
Expected: PASS (1 test).

- [ ] **Step 5: Update `api/auth.go` to send welcome email**

Add `*email.Sender` to `AuthHandler` and update `NewAuthHandler`:

```go
// Add import: "github.com/airelay/airelay/internal/email"
// Add import: "log"

type AuthHandler struct {
	db     *pgxpool.Pool
	secret string
	email  *email.Sender // nil-safe — no-op when unconfigured
}

func NewAuthHandler(db *pgxpool.Pool, secret string, emailSender *email.Sender) *AuthHandler {
	return &AuthHandler{db: db, secret: secret, email: emailSender}
}
```

At the end of the successful `Signup` path (after `writeJSON`), add:

```go
// Send welcome email — non-blocking, failure logged not returned
if h.email != nil {
	go func() {
		if err := h.email.SendWelcome(req.Email); err != nil {
			log.Printf("welcome email failed for %s: %v", req.Email, err)
		}
	}()
}
```

- [ ] **Step 6: Update `api/auth_test.go`**

All `api.NewAuthHandler(nil, "secret")` calls become:
```go
api.NewAuthHandler(nil, "secret", nil)
```
(nil email sender = no-op, tests are unaffected)

- [ ] **Step 7: Update `api/server.go`**

Add import: `"github.com/airelay/airelay/internal/email"`

In `NewServer`, replace:
```go
auth := NewAuthHandler(db, cfg.JWTSecret)
```
with:
```go
emailSender := email.NewSender(cfg.ResendAPIKey, cfg.FromEmail)
auth := NewAuthHandler(db, cfg.JWTSecret, emailSender)
```

- [ ] **Step 8: Run tests — expect pass**

```bash
go test ./api/... -run "TestSignup|TestLogin|TestMe" -v
```
Expected: PASS (5 tests).

- [ ] **Step 9: Commit**

```bash
git add internal/email/ api/auth.go api/auth_test.go api/server.go
git commit -m "feat: welcome email via Resend — sent non-blocking after signup"
```

---

### Task 10: PostHog analytics in dashboard

- [ ] **Step 1: Update `dashboard/handler.go`**

Add `postHogKey` field and `pageData` struct:

```go
type Handler struct {
	tmpl       *template.Template
	postHogKey string
}

type pageData struct {
	PostHogKey string
}

func NewHandler(tmpl *template.Template, postHogKey string) *Handler {
	return &Handler{tmpl: tmpl, postHogKey: postHogKey}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "login.html", pageData{PostHogKey: h.postHogKey})
}

func (h *Handler) Projects(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "projects.html", pageData{PostHogKey: h.postHogKey})
}

func (h *Handler) Project(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "project.html", pageData{PostHogKey: h.postHogKey})
}
```

- [ ] **Step 2: Update `dashboard/server.go` signature**

Change `NewDashboardServer` to accept `postHogKey string`:

```go
func NewDashboardServer(mux *http.ServeMux, postHogKey string) {
	tmpl, err := template.ParseFS(TemplateFS, "templates/*.html")
	if err != nil {
		log.Fatalf("dashboard: parse templates: %v", err)
	}
	h := NewHandler(tmpl, postHogKey)
	// rest of route registration unchanged
}
```

- [ ] **Step 3: Update `dashboard/templates/layout.html`**

Add PostHog snippet after the existing `</style>` tag and before the JWT `<script>` block:

```html
{{if .PostHogKey}}
<script>
!function(t,e){var o,n,p,r;e.__SV||(window.posthog=e,e._i=[],e.init=function(i,s,a){function g(t,e){var o=e.split(".");2==o.length&&(t=t[o[0]],e=o[1]),t[e]=function(){t.push([e].concat(Array.prototype.slice.call(arguments,0)))}}(p=t.createElement("script")).type="text/javascript",p.crossOrigin="anonymous",p.async=!0,p.src=s.api_host+"/static/array.js",(r=t.getElementsByTagName("script")[0]).parentNode.insertBefore(p,r);var u=e;for(void 0!==a?u=e[a]=[]:a="posthog",u.people=u.people||[],u.toString=function(t){var e="posthog";return"posthog"!==a&&(e+="."+a),t||(e+=" (stub)"),e},u.people.toString=function(){return u.toString(1)+".people (stub)"},o="capture identify alias people.set people.set_once set_config register register_once unregister opt_out_capturing has_opted_out_capturing opt_in_capturing reset isFeatureEnabled onFeatureFlags getFeatureFlag getFeatureFlagPayload reloadFeatureFlags group updateEarlyAccessFeatureEnrollment getEarlyAccessFeatures getActiveMatchingSurveys getSurveys onSessionId".split(" "),n=0;n<o.length;n++)g(u,o[n]);e._i.push([i,s,a])},e.__SV=1)}(document,window.posthog||[]);
posthog.init('{{.PostHogKey}}', {api_host: 'https://app.posthog.com'});
</script>
{{end}}
```

- [ ] **Step 4: Update `cmd/api/main.go`**

Change:
```go
dashboard.NewDashboardServer(mux)
```
to:
```go
dashboard.NewDashboardServer(mux, cfg.PostHogKey)
```

- [ ] **Step 5: Build and verify**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add dashboard/ cmd/api/main.go
git commit -m "feat: PostHog analytics in dashboard — conditional on POSTHOG_API_KEY env var"
```

---

### Task 11: Launch readiness check

- [ ] **Step 1: Run full test suite**

```bash
export $(cat .env | xargs) && make test
```
Expected: all tests pass.

- [ ] **Step 2: Build both binaries**

```bash
go build ./cmd/proxy/ && go build ./cmd/api/
```
Expected: both binaries produced, no errors.

- [ ] **Step 3: Build Docker images**

```bash
docker build --target proxy -t airelay-proxy:release .
docker build --target api -t airelay-api:release .
```
Expected: both images build. Check sizes:
```bash
docker images | grep airelay
```
Expected: proxy and api images each under ~30MB (distroless Go binary).

- [ ] **Step 4: Smoke test Docker API image locally**

```bash
docker run --rm -p 8080:8080 \
  -e DATABASE_URL="$DATABASE_URL" \
  -e REDIS_URL="$REDIS_URL" \
  -e JWT_SECRET="$JWT_SECRET" \
  -e CREDENTIAL_ENCRYPTION_KEY="$CREDENTIAL_ENCRYPTION_KEY" \
  airelay-api:release &
sleep 2
curl -s http://localhost:8080/health
```
Expected: `ok`

Kill the container after test.

- [ ] **Step 5: End-to-end flow test**

```bash
# Full signup → project → key → credential → budget → proxy flow
export $(cat .env | xargs)
go run ./cmd/api/ &
go run ./cmd/proxy/ &

TOKEN=$(curl -s -X POST http://localhost:8080/v1/auth/signup \
  -H "Content-Type: application/json" \
  -d '{"email":"launch@test.com","password":"password123"}' | jq -r .token // empty)
# Login if signup returns no token (user may already exist)
TOKEN=$(curl -s -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"launch@test.com","password":"password123"}' | jq -r .token)

PROJECT=$(curl -s -X POST http://localhost:8080/v1/projects \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Launch Test"}' | jq -r .id)

echo "Project: $PROJECT"

KEY=$(curl -s -X POST http://localhost:8080/v1/projects/$PROJECT/keys \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"test"}' | jq -r .key)

echo "Key starts with air_sk_: $(echo $KEY | grep -c air_sk_)"

curl -s http://localhost:8080/ | grep -o "Stop surprise"
```
Expected: project UUID returned, key starts with `air_sk_`, landing page contains "Stop surprise".

- [ ] **Step 6: Final commit**

```bash
git add .
git commit -m "feat: Plan 3 complete — Stripe billing, Docker, CI/CD, landing page, email, PostHog"
```

---

## Summary

Plan 3 delivers production readiness:

- ✅ Stripe billing — checkout sessions (monthly + annual), billing portal, webhook-driven plan sync
- ✅ Lazy Stripe customer creation — no API call at signup, only when upgrading
- ✅ Migration 010 — `stripe_subscription_id` on users
- ✅ Multi-stage Dockerfile — distroless proxy + api images, both under ~30MB
- ✅ Fly.io config — `fly.proxy.toml` and `fly.api.toml` with health checks and auto-scaling
- ✅ GitHub Actions CI/CD — test on every push with real Postgres + Redis, deploy on main
- ✅ Landing page — hero, how-it-works, features, pricing toggle (monthly/annual), CTAs
- ✅ Welcome email — non-blocking Resend call after signup, no-op when unconfigured
- ✅ PostHog analytics — conditional JS snippet in dashboard and landing page

**Next:** Launch — create Fly.io apps, set secrets, push to main, write and post Show HN.

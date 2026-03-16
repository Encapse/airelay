# AIRelay — Design Spec

**Date:** 2026-03-16
**Status:** Design complete, pending implementation planning
**Domain:** airelay.dev
**Stack:** Go, Postgres (Neon), Redis (Upstash), Cloudflare, Fly.io

---

## 1. Problem

Developers building with AI APIs (OpenAI, Anthropic, Google, etc.) have no reliable way to control costs per project, enforce budgets in real time, or know exactly what each feature or end-user is costing them. A single runaway request loop or power user can generate a $5k bill overnight. Existing tools (Helicone, Portkey) focus on observability and developer tooling — budget enforcement is an afterthought.

---

## 2. Product

AIRelay is a transparent HTTP proxy that sits between a developer's app and AI providers. One environment variable change — `OPENAI_BASE_URL=https://api.airelay.dev/proxy/openai` — and the integration is complete. No SDK changes, no code rewrites.

**Core value:** prevent surprise AI bills via real-time budget enforcement. Observability (cost charts, usage history, per-model breakdowns) is the intelligence layer built on top.

**Positioning:** The simplest way to protect your AI costs.

---

## 3. Architecture

Two separate Go services sharing Postgres and Redis:

### 3.1 Proxy Service (hot path)
Handles all `/proxy/*` traffic. Stateless, horizontally scalable, zero management API code.

**Request flow:**
1. Receive request with AIRelay key in `Authorization: Bearer air_sk_...` header
2. Look up project + provider credential from Redis cache (fallback: Postgres)
3. Check current spend against budget via Redis `GET spend:{project_id}`
4. If budget exceeded and `hard_limit = true`: return `429` immediately — no tokens burned
5. Forward request to provider with real API key substituted
6. Stream response back to client transparently (SSE pass-through)
7. On stream close: read token counts from final chunk, compute `cost_usd`
8. Push usage event to buffered Go channel (non-blocking)
9. Background goroutine: atomic Redis `INCRBY spend:{project_id}`, batch-write to Postgres

### 3.2 Management API
Handles all `/v1/*` traffic. Dashboard backend, authentication, project/budget/key management.

### 3.3 Infrastructure
```
Client → Cloudflare (DDoS, TLS, edge routing)
       → Fly.io Load Balancer (health checks, auto-failover)
       → N stateless Proxy instances (us-east, us-west, scale as needed)
       → Redis (Upstash, replicated) — budget state, rate limits
       → Postgres (Neon, primary + standby) — source of truth
```

---

## 4. Consistency Model

**Redis is the enforcement layer. Postgres is the source of truth.**

- Every proxied request does a synchronous `INCRBY` on Redis before returning — always current.
- Usage events are written to a buffered in-memory channel (50k cap), flushed to Postgres every 1 second or 100 events, whichever comes first.
- On Redis cold start: rebuild spend counters from `SELECT project_id, SUM(cost_usd) FROM usage_events WHERE period = current GROUP BY project_id`. Completes in < 500ms.
- Reconciliation job runs every 60s: compares Redis spend to Postgres SUM. Drift > 5% corrects Redis and emits a metric. Drift > 20% triggers an alert.

### 4.1 Fail-Open Design

If Redis is unreachable, requests pass through without budget enforcement. Usage is still captured:
1. Response is still buffered and tokens are still counted
2. Usage event queued to dead letter queue (in-memory, 50k cap)
3. Dead letter queue retries with exponential backoff: 5s → 30s → 5min
4. On recovery: DLQ flushes to Postgres, Redis rebuilds from Postgres

Events written during fail-open are flagged with `fail_open = true`. Budget may be temporarily exceeded during infrastructure failures — disclosed in ToS.

---

## 5. Data Model

### users
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| email | text unique | |
| plan | enum | free / pro / team |
| stripe_customer_id | text | nullable |
| created_at | timestamptz | |

### projects
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| user_id | uuid FK → users | |
| name | text | |
| slug | text unique | |
| created_at | timestamptz | |
| archived_at | timestamptz | nullable |

### api_keys
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| project_id | uuid FK → projects | |
| key_hash | text | bcrypt, never stored plain |
| key_prefix | text | `air_sk_ab12...` display only |
| name | text | |
| last_used_at | timestamptz | nullable |
| revoked_at | timestamptz | nullable |

### provider_credentials
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| project_id | uuid FK → projects | |
| provider | enum | openai / anthropic / google |
| encrypted_key | text | AES-256, encryption key in env var |
| created_at | timestamptz | |
| revoked_at | timestamptz | nullable |

### budgets
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| project_id | uuid FK → projects | |
| amount_usd | numeric(10,4) | |
| period | enum | daily / monthly |
| hard_limit | bool | block at 100% or alert only |
| created_at | timestamptz | |

### alert_thresholds
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| budget_id | uuid FK → budgets | |
| threshold_pct | int | 75 / 90 / 100 |
| notify_email | bool | |
| notify_webhook_url | text | nullable |
| last_fired_at | timestamptz | nullable |

### usage_events *(partitioned by month on created_at)*
| Column | Type | Notes |
|---|---|---|
| id | uuid PK | |
| project_id | uuid FK → projects | |
| api_key_id | uuid FK → api_keys | |
| provider | text | openai / anthropic / google |
| model | text | gpt-4o / claude-3-5-sonnet / etc |
| prompt_tokens | int | |
| completion_tokens | int | |
| cost_usd | numeric(10,8) | calculated at write time |
| duration_ms | int | |
| status_code | int | |
| metadata | jsonb | user-defined tags via `X-AIRelay-Meta` header |
| fail_open | bool | flagged if written during outage |
| created_at | timestamptz | partition key |

### model_pricing
| Column | Type | Notes |
|---|---|---|
| provider | text PK | |
| model | text PK | |
| input_cost_per_1k | numeric(10,8) | |
| output_cost_per_1k | numeric(10,8) | |
| synced_from | text | "litellm" / "openrouter" / "manual" |
| manual_override | bool | if true, sync job skips this row |
| last_synced_at | timestamptz | |

**Key design decisions:**
- `cost_usd` stored at write time — model prices change; historical accuracy requires storing cost when the event is recorded, not when it's queried
- `metadata` jsonb — developers pass `X-AIRelay-Meta: {"user_id":"u_123"}` for per-end-user cost attribution
- Partitioned by month — usage_events is the largest table; range partitioning keeps queries fast and old data easy to archive

---

## 6. Model Pricing Sync

Background job runs every 24 hours:
1. Fetch `https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json`
2. Fetch OpenRouter `/api/v1/models` as secondary source
3. Upsert into `model_pricing` — skip rows where `manual_override = true`
4. New models added automatically; price changes update existing rows

If a proxied request references an unknown model: store `cost_usd = NULL`, flag event, alert admin. Do not block the request.

If LiteLLM becomes unmaintained: fall back to OpenRouter as primary, then manual entry via admin panel. Long-term: open source AIRelay's own pricing file as a community resource.

---

## 7. API Surface

### 7.1 Proxy — `api.airelay.dev/proxy`

Drop-in SDK replacement. Pass AIRelay key as `Authorization: Bearer air_sk_...`.

| Path prefix | Provider |
|---|---|
| `/proxy/openai/*` | OpenAI |
| `/proxy/anthropic/*` | Anthropic |
| `/proxy/google/*` | Google Gemini |

All paths under each prefix pass through unchanged. SSE streaming is transparent — token counts read from final chunk (OpenAI `usage` field, Anthropic `message_stop` event).

### 7.2 Management API — `api.airelay.dev/v1`

**Auth**
- `POST /v1/auth/signup`
- `POST /v1/auth/login` → JWT
- `GET /v1/auth/me` → user + plan

**Projects**
- `GET /v1/projects`
- `POST /v1/projects`
- `GET /v1/projects/:id`
- `DELETE /v1/projects/:id`

**API Keys**
- `GET /v1/projects/:id/keys` — list (prefix only, never full key)
- `POST /v1/projects/:id/keys` — create (full key returned once only)
- `DELETE /v1/projects/:id/keys/:keyId` — revoke

**Provider Credentials**
- `GET /v1/projects/:id/credentials` — list (masked)
- `POST /v1/projects/:id/credentials` — add
- `DELETE /v1/projects/:id/credentials/:credId` — revoke

**Budgets**
- `GET /v1/projects/:id/budget` — config + current spend
- `PUT /v1/projects/:id/budget` — upsert limit + thresholds
- `DELETE /v1/projects/:id/budget` — remove (unlimited)

**Usage**
- `GET /v1/projects/:id/usage` — raw events, paginated
- `GET /v1/projects/:id/usage/summary` — cost by model / date / key
- `GET /v1/models` — supported models + current pricing

---

## 8. Open Core Split

**Open source (`github.com/airelay/proxy`):**
- Proxy engine binary
- Token counting logic per provider
- Model pricing sync job
- Docker image for self-hosting

**Private (hosted product):**
- Dashboard (cost charts, budget UI, usage breakdowns)
- Management API
- Alert delivery (email + webhooks)
- Multi-region infrastructure + uptime SLA
- Team features (seats, roles, shared projects)
- Billing and plan management (Stripe)
- Reconciliation and operational reliability jobs

---

## 9. Pricing & Pay Gates

| | Free | Pro $79/mo | Team $199/mo |
|---|---|---|---|
| Projects | 1 | Unlimited | Unlimited |
| API keys | 1 | Unlimited | Unlimited |
| Provider credentials | 1 | Unlimited | Unlimited |
| Usage history | 7 days | 90 days | Unlimited |
| Alerts | Email only | Email + webhooks | Email + webhooks + Slack |
| Metadata cost attribution | — | ✓ | ✓ |
| Team seats | 1 | 1 | 5 |
| Billing webhooks | — | — | ✓ |
| Uptime SLA | — | — | 99.9% in writing |

**Natural upgrade triggers:**
1. Second project → immediate Pro upgrade
2. Need last month's data → history gate → Pro
3. Teammate needs access → Team
4. Need Slack/PagerDuty alerts → webhooks gate → Pro
5. Building an AI product, need to pass costs to customers → billing webhooks → Team

Request volume is not gated — proxy everything, charge for the intelligence layer on top.

---

## 10. Infrastructure Cost

| Stage | Monthly cost |
|---|---|
| Launch (0 customers) | $0 (free tiers: Fly.io, Neon, Upstash, Cloudflare) |
| Early (20-100 customers) | ~$30/mo |
| Production (100-300 customers) | ~$100-150/mo |

Margins at scale: 85-90%. The proxy is compute-efficient Go; the marginal cost per additional customer is near zero.

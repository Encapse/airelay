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
2. Look up project + provider credential from Redis cache (fallback: Postgres, SHA-256 key hash comparison)
3. Read all active budgets for the project. For each: check `spend:{project_id}:{period_key}` against limit via Redis Lua script that atomically checks AND increments a reservation counter
4. If any budget exceeded and `hard_limit = true`: return `429` immediately — no tokens burned, no reservation held
5. Forward request to provider with real API key substituted
6. Stream response back to client transparently (SSE pass-through)
7. On stream close: read exact token counts from final chunk (OpenAI `usage` field, Anthropic `message_stop` event, Gemini `usageMetadata` field), compute `cost_usd`
8. Correct the reservation: `INCRBYFLOAT spend:{project_id}:{period_key}` by actual cost (reservation from step 3 used `0` — actual deduction happens here atomically)
9. Push usage event to buffered Go channel (non-blocking), batch-write to Postgres every 1s or 100 events

**Concurrency model:** budget check and spend increment are two separate operations — there is an inherent race window equal to the duration of in-flight requests. Under high concurrency a project may marginally exceed its budget by the cost of concurrent in-flight requests. This is documented behavior, not a bug. For the use cases AIRelay targets (developer cost protection, not financial-grade enforcement), this margin is acceptable and bounded.

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

**Spend key structure:** `spend:{project_id}:{period_key}` where `period_key` is `daily:{YYYY-MM-DD}` or `monthly:{YYYY-MM}`. Keys are naturally scoped to their period — no reset job needed. A new day/month = a new key. Expired keys (prior periods) are cleaned up by a daily TTL sweep job.

- Every proxied request does a synchronous `INCRBYFLOAT` on the period-scoped Redis key — always current for the active period.
- Usage events are written to a buffered in-memory channel (50k cap), flushed to Postgres every 1 second or 100 events, whichever comes first.
- On Redis cold start: rebuild spend counters from `SELECT project_id, SUM(cost_usd) FROM usage_events WHERE created_at >= period_start GROUP BY project_id`. Completes in < 500ms.
- Reconciliation job runs every 60s: compares Redis spend to Postgres SUM for the current period. Drift > 5% corrects Redis and emits a metric. Drift > 20% triggers an alert.

### 4.1 Fail-Open Design

If Redis is unreachable, requests pass through without budget enforcement. Usage is still captured with a two-tier fallback:

**Tier 1 — Redis down, Postgres available (common case):**
1. Response is buffered and tokens are counted as normal
2. Usage event written directly to Postgres (bypassing Redis and the async channel)
3. Event flagged with `fail_open = true`
4. On Redis recovery: rebuild spend keys from Postgres SUM — no data loss

**Tier 2 — Both Redis and Postgres unreachable (rare):**
1. Usage event queued to in-memory dead letter queue (50k cap)
2. DLQ retries Postgres write with exponential backoff: 5s → 30s → 5min
3. On Postgres recovery: DLQ flushes, then Redis rebuilds from Postgres
4. If proxy restarts while DLQ has pending events: events are lost. This is the irreducible risk of in-process buffering and is disclosed in ToS.

Budget may be temporarily exceeded during infrastructure failures. The Tier 1 path (Redis only down) produces zero data loss. The Tier 2 path (both down) has a bounded loss window equal to the time between the last successful Postgres flush and the process restart.

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
| key_hash | text | SHA-256, never stored plain — bcrypt is too slow for the hot path lookup |
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

**Constraint:** `UNIQUE(project_id, period)` — one budget per period type per project. A project may have both a daily and a monthly budget simultaneously. The proxy enforces all active budgets — if either is exceeded with `hard_limit = true`, the request is blocked.

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

**Source-available under BSL (`github.com/airelay/proxy`):**
- Proxy engine binary
- Token counting logic per provider
- Model pricing sync job
- Docker image for self-hosting

**License: Business Source License (BSL 1.1)**
Source code is publicly readable and auditable. Personal and non-commercial use is permitted. Running a competing commercial managed service using this code is explicitly prohibited. Converts to Apache 2.0 after 4 years.

**Why BSL over Apache 2.0:** Developers routing production AI traffic through AIRelay need to trust what the proxy does with their requests — source visibility provides that trust. BSL gives us that benefit without handing a free business to competitors. This is the same approach used by HashiCorp (Terraform), Sentry, and MariaDB.

**Private (hosted product):**
- Dashboard (cost charts, budget UI, usage breakdowns)
- Management API
- Alert delivery (email + webhooks)
- Multi-region infrastructure + uptime SLA
- Team features (seats, roles, shared projects)
- Billing and plan management (Stripe)
- Reconciliation and operational reliability jobs

**Note on self-hosted deployments:** The BSL proxy binary does not include the reconciliation job (Redis↔Postgres drift correction) or the alert delivery system. Self-hosters operate without drift detection. This is an accepted and documented trade-off — the hosted product's reliability layer is part of the paid value proposition.

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

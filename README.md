# AIRelay

An AI API cost-protection proxy. Drop-in replacement for OpenAI, Anthropic, and Google AI SDKs — one env var change, real-time budget enforcement, full usage tracking.

```
OPENAI_BASE_URL=https://proxy.airelay.dev/proxy/openai
```

---

## Architecture

```
Client → AIRelay Proxy → OpenAI / Anthropic / Google
              ↓
         Redis (budget enforcement, key cache)
              ↓
         Postgres (usage events, async batch write)
```

Two services share one codebase:
- `cmd/proxy` — hot path, handles all AI requests
- `cmd/api` — management API (Plan 2)

---

## Prerequisites

- Go 1.22+
- Docker (for local Postgres + Redis)
- `goose` for migrations: `go install github.com/pressly/goose/v3/cmd/goose@latest`

---

## Local Setup

### 1. Clone and configure

```bash
git clone https://github.com/airelay/airelay
cd airelay
cp .env.example .env
```

Edit `.env` — the only required change for local testing is adding your AI provider keys:

```bash
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
```

The other values work out of the box for local Docker:

```bash
DATABASE_URL=postgres://airelay:airelay@localhost:5432/airelay?sslmode=disable
REDIS_URL=redis://localhost:6379
CREDENTIAL_ENCRYPTION_KEY=airelay-dev-enckey-32bytesexactly!  # exactly 32 bytes
JWT_SECRET=airelay-dev-jwt-secret-change-in-prod
```

### 2. Start infrastructure

```bash
make dev        # docker compose up -d (postgres:5432, redis:6379)
```

### 3. Run migrations

```bash
export $(cat .env | xargs)
make migrate-up
```

Expected output:
```
goose: successfully migrated database to version: 8
```

### 4. Seed local data

```bash
make seed
```

Expected output:
```
Seed complete

  Email:      dev@airelay.dev
  Password:   password123
  Project ID: <uuid>
  API Key:    air_sk_<hex>
```

Save the API key — you'll use it for all proxy requests.

---

## Running the Proxy

```bash
make proxy      # go run ./cmd/proxy/ — listens on :8081
```

---

## Manual Testing

### Health check

```bash
curl http://localhost:8081/health
# → ok
```

### OpenAI via proxy

```bash
export AIR_KEY=air_sk_<your key from seed>

curl http://localhost:8081/proxy/openai/v1/models \
  -H "Authorization: Bearer $AIR_KEY"
```

Expected: JSON list of OpenAI models.

### Anthropic via proxy

```bash
curl http://localhost:8081/proxy/anthropic/v1/messages \
  -H "Authorization: Bearer $AIR_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-3-5-haiku-20241022","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'
```

### 401 — bad key

```bash
curl -w "\n%{http_code}" http://localhost:8081/proxy/openai/v1/models \
  -H "Authorization: Bearer air_sk_wrongkey"
# → 401
```

### 429 — budget enforcement

```bash
export PROJECT_ID=<uuid from seed output>

# Simulate exceeding the $10 monthly budget
redis-cli SET "spend:$PROJECT_ID:monthly:$(date +%Y-%m)" 11.00

# This request should now be blocked
curl -w "\n%{http_code}" http://localhost:8081/proxy/openai/v1/models \
  -H "Authorization: Bearer $AIR_KEY"
# → 429 {"error":"budget exceeded: ..."}

# Reset spend to unblock
redis-cli DEL "spend:$PROJECT_ID:monthly:$(date +%Y-%m)"
```

### Per-end-user cost attribution

Pass metadata via `X-AIRelay-Meta` header (any JSON):

```bash
curl http://localhost:8081/proxy/openai/v1/chat/completions \
  -H "Authorization: Bearer $AIR_KEY" \
  -H "X-AIRelay-Meta: {\"user_id\": \"usr_123\", \"session\": \"abc\"}" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'
```

---

## Automated Tests

```bash
export $(cat .env | xargs)
make test
```

Current coverage (30 tests, no external dependencies required):

| Package | Tests | Covers |
|---|---|---|
| `internal/config` | 6 | Env loading, validation, defaults |
| `internal/encrypt` | 4 | AES-256-GCM roundtrip, key length guard |
| `internal/tokens` | 9 | OpenAI/Anthropic/Google SSE token parsing |
| `internal/cost` | 2 | Cost calculation from token counts |
| `proxy` | 9 | Key hashing/generation, spend key format, handler auth/routing |

**Known gap:** No integration tests for DB/Redis-dependent paths (key resolution, budget enforcement write path, usage event persistence). These require a live database and will be added in Plan 2.

---

## Makefile Reference

| Target | Command | Description |
|---|---|---|
| `make dev` | `docker compose up -d` | Start Postgres + Redis |
| `make stop` | `docker compose down` | Stop infrastructure |
| `make migrate-up` | `goose ... up` | Apply all pending migrations |
| `make migrate-down` | `goose ... down` | Roll back one migration |
| `make test` | `go test ./...` | Run all unit tests |
| `make build` | `go build -o bin/...` | Build proxy and api binaries to `bin/` |
| `make proxy` | `go run ./cmd/proxy/` | Run proxy in dev mode |
| `make seed` | `go run ./cmd/seed/` | Seed local dev data |
| `make lint` | `go vet ./...` | Run Go vet |

---

## Project Structure

```
cmd/
  proxy/        — Proxy service entry point
  seed/         — Local dev seed script
  api/          — Management API entry point (Plan 2)
db/
  migrations/   — goose migration files (001–008)
internal/
  config/       — Environment config loading
  cost/         — Token cost calculation
  db/           — Postgres connection pool
  encrypt/      — AES-256-GCM credential encryption
  models/       — Domain types
  redis/        — Redis client
  tokens/       — SSE token parsing (OpenAI, Anthropic, Google)
proxy/          — Core proxy package (auth, budget, handler, logger, DLQ)
```

---

## Plan Status

- [x] **Plan 1** — Core Proxy (this branch: `plan1/core-proxy`)
- [ ] **Plan 2** — Management API + Background Jobs + Dashboard
- [ ] **Plan 3** — Billing + Infrastructure + Distribution

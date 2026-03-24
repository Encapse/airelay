# AIRelay — Claude Instructions

## Project

AI API cost-protection proxy. Go monorepo, two services:
- `cmd/proxy` (:8081) — hot path, all AI requests
- `cmd/api` (:8080) — management API + dashboard + background jobs

## Stack

Go 1.25 · pgx/v5 · go-redis/v9 · golang-jwt/jwt/v5 · bcrypt · HTMX · goose · testify

## Development Workflow

### After implementing each plan

Run a code audit using the `feature-dev:code-reviewer` agent before merging. The audit must cover:

1. **Operational correctness** — TTLs, error handling, failure modes, resource cleanup
2. **Hot path performance** — any synchronous DB/Redis calls on the proxy request path
3. **Concurrency** — race conditions, shared state, goroutine lifecycle
4. **Data correctness** — missing indexes, partition coverage, constraint gaps
5. **Security** — credential handling, input validation, injection vectors

Do not skip this step. The build-then-audit pattern catches issues that plan reviews miss.

### Commit discipline

- Main session handles all `git commit` and `git push` — subagents do not commit
- Subagents return "DONE" when implementation is complete
- Two-stage review: spec compliance first, then code quality

### Running locally

```bash
make dev          # start Postgres + Redis
export $(cat .env | xargs)
make migrate-up   # apply all migrations
make seed         # seed dev user + project + API key
make proxy        # :8081
make api          # :8080
```

### Tests

```bash
make test                  # unit tests (no Docker)
make test-integration      # requires make dev + migrate-up
make test-all              # both
```

## Architecture notes

- Spend keys in Redis must always have a TTL (daily: 2d, monthly: 35d via `spendKeyTTL`)
- Pricing cache lives in `Handler.pricingCache` (sync.Map) — cleared on restart, refreshed by 24h pricing sync job
- DLQ preserves in-flight events during retry: `d.queue = append(failed, newlyEnqueued...)`
- `usage_events` is partitioned by `created_at` (RANGE). Migration 010 adds DEFAULT partition + 2027 partitions. Partition job creates future months automatically.
- JWT: HS256, 7-day expiry, Claims{UserID, Email, Plan}
- Plan limits: Free (1 project, 1 key, 7d history), Pro (unlimited, 90d), Team (unlimited, unlimited)

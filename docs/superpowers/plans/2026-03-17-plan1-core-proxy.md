# AIRelay Plan 1 — Core Proxy

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working AI API proxy that authenticates requests, enforces budgets in real time, and forwards traffic to OpenAI, Anthropic, and Google — including full SSE streaming support.

**Architecture:** Two Go services share Postgres and Redis. This plan builds the proxy service only. The management API (Plan 2) populates the database; for local dev we seed directly. The proxy is stateless, horizontally scalable, and fail-open by design.

**Tech Stack:** Go 1.22+, pgx/v5, go-redis/v9, pressly/goose/v3, testify, godotenv, Docker Compose

---

## Chunk 1: Repository Foundation

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `docker-compose.yml`
- Create: `.env.example`
- Create: `.gitignore`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

### Task 1: Initialise the Go module and install dependencies

- [ ] **Step 1: Create the module**

```bash
cd ~/projects/airelay
go mod init github.com/airelay/airelay
```

- [ ] **Step 2: Install dependencies**

```bash
go get github.com/jackc/pgx/v5
go get github.com/jackc/pgx/v5/pgxpool
go get github.com/redis/go-redis/v9
go get github.com/pressly/goose/v3
go get github.com/golang-jwt/jwt/v5
go get github.com/stretchr/testify
go get github.com/joho/godotenv
go get github.com/google/uuid
go get golang.org/x/crypto
```

- [ ] **Step 3: Verify go.mod has correct module path**

```bash
head -3 go.mod
```
Expected:
```
module github.com/airelay/airelay

go 1.22
```

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "feat: initialise Go module and dependencies"
```

---

### Task 2: Write the config package

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:
```go
package config_test

import (
	"os"
	"testing"

	"github.com/airelay/airelay/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLoad_RequiredVars(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/airelay")
	os.Setenv("REDIS_URL", "redis://localhost:6379")
	os.Setenv("CREDENTIAL_ENCRYPTION_KEY", "abcdefghijklmnopqrstuvwxyz123456") // exactly 32 bytes
	os.Setenv("JWT_SECRET", "testsecret")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("REDIS_URL")
		os.Unsetenv("CREDENTIAL_ENCRYPTION_KEY")
		os.Unsetenv("JWT_SECRET")
	}()

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, "postgres://user:pass@localhost/airelay", cfg.DatabaseURL)
	require.Equal(t, "redis://localhost:6379", cfg.RedisURL)
	require.Len(t, cfg.CredentialEncryptionKey, 32)
}

func TestLoad_MissingRequired(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	_, err := config.Load()
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test — expect compilation failure**

```bash
go test ./internal/config/... -v
```
Expected: compilation error — package does not exist yet.

- [ ] **Step 3: Implement config**

Create `internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL             string
	RedisURL                string
	CredentialEncryptionKey string // must be exactly 32 bytes
	JWTSecret               string
	Port                    string
	ProxyPort               string
	AdminEmail              string
	ResendAPIKey            string
	Env                     string
	OpenAIKey               string // optional, used by seed script
	AnthropicKey            string // optional, used by seed script
}

func Load() (*Config, error) {
	required := []string{"DATABASE_URL", "REDIS_URL", "CREDENTIAL_ENCRYPTION_KEY", "JWT_SECRET"}
	for _, key := range required {
		if os.Getenv(key) == "" {
			return nil, fmt.Errorf("required env var %s is not set", key)
		}
	}
	encKey := os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
	if len(encKey) != 32 {
		return nil, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be exactly 32 bytes, got %d", len(encKey))
	}
	return &Config{
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		RedisURL:                os.Getenv("REDIS_URL"),
		CredentialEncryptionKey: encKey,
		JWTSecret:               os.Getenv("JWT_SECRET"),
		Port:                    getEnvOrDefault("PORT", "8080"),
		ProxyPort:               getEnvOrDefault("PROXY_PORT", "8081"),
		AdminEmail:              os.Getenv("ADMIN_EMAIL"),
		ResendAPIKey:            os.Getenv("RESEND_API_KEY"),
		Env:                     getEnvOrDefault("ENV", "development"),
		OpenAIKey:               os.Getenv("OPENAI_API_KEY"),
		AnthropicKey:            os.Getenv("ANTHROPIC_API_KEY"),
	}, nil
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/config/... -v
```
Expected: PASS (2 tests)

- [ ] **Step 5: Create .env.example**

```bash
cat > .env.example << 'EOF'
DATABASE_URL=postgres://airelay:airelay@localhost:5432/airelay?sslmode=disable
REDIS_URL=redis://localhost:6379
CREDENTIAL_ENCRYPTION_KEY=abcdefghijklmnopqrstuvwxyz123456
JWT_SECRET=change-me-jwt-secret-min-32-chars
PORT=8080
PROXY_PORT=8081
ADMIN_EMAIL=you@example.com
RESEND_API_KEY=
ENV=development
OPENAI_API_KEY=sk-your-key-here
ANTHROPIC_API_KEY=sk-ant-your-key-here
EOF
```

Note: `abcdefghijklmnopqrstuvwxyz123456` is exactly 32 bytes. Change it before any real deployment.

- [ ] **Step 6: Create .gitignore**

```bash
cat > .gitignore << 'EOF'
.env
*.env.local
bin/
tmp/
*.test
coverage.out
.superpowers/
EOF
```

- [ ] **Step 7: Commit**

```bash
git add internal/config/ .env.example .gitignore
git commit -m "feat: config package with required env var validation"
```

---

### Task 3: Docker Compose and Makefile

- [ ] **Step 1: Create docker-compose.yml**

```yaml
version: "3.9"
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: airelay
      POSTGRES_PASSWORD: airelay
      POSTGRES_DB: airelay
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

volumes:
  pgdata:
```

- [ ] **Step 2: Create Makefile**

```makefile
.PHONY: dev stop migrate-up migrate-down test build-proxy build-api seed proxy lint

dev:
	docker compose up -d
	@echo "Postgres: localhost:5432 | Redis: localhost:6379"

stop:
	docker compose down

migrate-up:
	go run github.com/pressly/goose/v3/cmd/goose@latest \
		-dir db/migrations postgres \
		"$$(grep DATABASE_URL .env | cut -d= -f2-)" up

migrate-down:
	go run github.com/pressly/goose/v3/cmd/goose@latest \
		-dir db/migrations postgres \
		"$$(grep DATABASE_URL .env | cut -d= -f2-)" down

test:
	go test ./... -v -count=1

build-proxy:
	go build -o bin/proxy ./cmd/proxy

build-api:
	go build -o bin/api ./cmd/api

seed:
	go run ./cmd/seed

proxy:
	go run ./cmd/proxy

lint:
	go vet ./...
```

- [ ] **Step 3: Start services and verify**

```bash
cp .env.example .env
make dev
docker compose ps
```
Expected: postgres and redis containers running (State: Up).

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml Makefile
git commit -m "feat: docker-compose for local postgres and redis"
```

---

## Chunk 2: Database Migrations and Shared Models

**Files:**
- Create: `db/migrations/001_create_users.sql` through `008_create_model_pricing.sql`
- Create: `internal/db/db.go` + `internal/db/db_test.go`
- Create: `internal/redis/redis.go` + `internal/redis/redis_test.go`
- Create: `internal/models/models.go`

### Task 4: Write all migrations

- [ ] **Step 1: Create migrations directory**

```bash
mkdir -p db/migrations
```

- [ ] **Step 2: Create 001_create_users.sql**

```sql
-- db/migrations/001_create_users.sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TYPE user_plan AS ENUM ('free', 'pro', 'team');

CREATE TABLE users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               TEXT NOT NULL UNIQUE,
    password_hash       TEXT NOT NULL,
    plan                user_plan NOT NULL DEFAULT 'free',
    stripe_customer_id  TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE users;
DROP TYPE user_plan;
```

- [ ] **Step 3: Create 002_create_projects.sql**

```sql
-- db/migrations/002_create_projects.sql
-- +goose Up
CREATE TABLE projects (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ
);

CREATE INDEX idx_projects_user_id ON projects(user_id);

-- +goose Down
DROP TABLE projects;
```

- [ ] **Step 4: Create 003_create_api_keys.sql**

```sql
-- db/migrations/003_create_api_keys.sql
-- +goose Up
CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key_hash     TEXT NOT NULL UNIQUE,
    key_prefix   TEXT NOT NULL,
    name         TEXT NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_api_keys_project_id ON api_keys(project_id);
CREATE INDEX idx_api_keys_key_hash   ON api_keys(key_hash);

-- +goose Down
DROP TABLE api_keys;
```

- [ ] **Step 5: Create 004_create_provider_credentials.sql**

```sql
-- db/migrations/004_create_provider_credentials.sql
-- +goose Up
CREATE TYPE ai_provider AS ENUM ('openai', 'anthropic', 'google');

CREATE TABLE provider_credentials (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    provider      ai_provider NOT NULL,
    encrypted_key TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at    TIMESTAMPTZ,
    UNIQUE(project_id, provider)
);

CREATE INDEX idx_provider_credentials_project_id ON provider_credentials(project_id);

-- +goose Down
DROP TABLE provider_credentials;
DROP TYPE ai_provider;
```

- [ ] **Step 6: Create 005_create_budgets.sql**

```sql
-- db/migrations/005_create_budgets.sql
-- +goose Up
CREATE TYPE budget_period AS ENUM ('daily', 'monthly');

CREATE TABLE budgets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    amount_usd  NUMERIC(10,4) NOT NULL,
    period      budget_period NOT NULL,
    hard_limit  BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, period)
);

-- +goose Down
DROP TABLE budgets;
DROP TYPE budget_period;
```

- [ ] **Step 7: Create 006_create_alert_thresholds.sql**

```sql
-- db/migrations/006_create_alert_thresholds.sql
-- +goose Up
CREATE TABLE alert_thresholds (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    budget_id          UUID NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
    threshold_pct      INT NOT NULL CHECK (threshold_pct IN (75, 90, 100)),
    notify_email       BOOLEAN NOT NULL DEFAULT true,
    notify_webhook_url TEXT,
    last_fired_at      TIMESTAMPTZ,
    UNIQUE(budget_id, threshold_pct)
);

-- +goose Down
DROP TABLE alert_thresholds;
```

- [ ] **Step 8: Create 007_create_usage_events.sql**

```sql
-- db/migrations/007_create_usage_events.sql
-- +goose Up
CREATE TABLE usage_events (
    id                UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id),
    api_key_id        UUID NOT NULL REFERENCES api_keys(id),
    provider          TEXT NOT NULL,
    model             TEXT NOT NULL,
    prompt_tokens     INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    cost_usd          NUMERIC(10,8),
    duration_ms       INT NOT NULL DEFAULT 0,
    status_code       INT NOT NULL,
    metadata          JSONB,
    fail_open         BOOLEAN NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
) PARTITION BY RANGE (created_at);

-- Initial partitions — add new partitions monthly via the partition management job (Plan 2)
CREATE TABLE usage_events_2026_03 PARTITION OF usage_events
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE usage_events_2026_04 PARTITION OF usage_events
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE usage_events_2026_05 PARTITION OF usage_events
    FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');

CREATE INDEX idx_usage_events_project_created ON usage_events(project_id, created_at);
CREATE INDEX idx_usage_events_api_key_id      ON usage_events(api_key_id);

-- +goose Down
DROP TABLE usage_events;
```

- [ ] **Step 9: Create 008_create_model_pricing.sql**

```sql
-- db/migrations/008_create_model_pricing.sql
-- +goose Up
CREATE TABLE model_pricing (
    provider           TEXT NOT NULL,
    model              TEXT NOT NULL,
    input_cost_per_1k  NUMERIC(10,8) NOT NULL,
    output_cost_per_1k NUMERIC(10,8) NOT NULL,
    synced_from        TEXT NOT NULL DEFAULT 'manual',
    manual_override    BOOLEAN NOT NULL DEFAULT false,
    last_synced_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, model)
);

INSERT INTO model_pricing (provider, model, input_cost_per_1k, output_cost_per_1k, synced_from) VALUES
    ('openai',    'gpt-4o',                          0.00250,  0.01000,  'manual'),
    ('openai',    'gpt-4o-mini',                     0.00015,  0.00060,  'manual'),
    ('openai',    'gpt-4-turbo',                     0.01000,  0.03000,  'manual'),
    ('openai',    'gpt-3.5-turbo',                   0.00050,  0.00150,  'manual'),
    ('anthropic', 'claude-3-5-sonnet-20241022',      0.00300,  0.01500,  'manual'),
    ('anthropic', 'claude-3-5-haiku-20241022',       0.00080,  0.00400,  'manual'),
    ('anthropic', 'claude-3-opus-20240229',          0.01500,  0.07500,  'manual'),
    ('google',    'gemini-1.5-pro',                  0.00125,  0.00500,  'manual'),
    ('google',    'gemini-1.5-flash',                0.000075, 0.00030,  'manual'),
    ('google',    'gemini-2.0-flash',                0.000100, 0.00040,  'manual');

-- +goose Down
DROP TABLE model_pricing;
```

- [ ] **Step 10: Run migrations**

```bash
make migrate-up
```
Expected: `goose: successfully migrated database to version: 8`

- [ ] **Step 11: Verify schema**

```bash
docker exec -it $(docker compose ps -q postgres) psql -U airelay -c "\dt"
```
Expected: 8 tables listed including `usage_events`.

- [ ] **Step 12: Commit**

```bash
git add db/migrations/
git commit -m "feat: all database migrations with seeded model pricing"
```

---

### Task 5: Database connection pool

- [ ] **Step 1: Write the failing test**

Create `internal/db/db_test.go`:
```go
package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/airelay/airelay/internal/db"
	"github.com/stretchr/testify/require"
)

func TestConnect(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := db.Connect(context.Background(), url)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(context.Background()))
}
```

- [ ] **Step 2: Run test — expect skip**

```bash
go test ./internal/db/... -v
```
Expected: SKIP (no DATABASE_URL in env yet).

- [ ] **Step 3: Implement db.Connect**

Create `internal/db/db.go`:
```go
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}
```

- [ ] **Step 4: Run test with live DB**

```bash
export $(cat .env | xargs) && go test ./internal/db/... -v
```
Expected: PASS (1 test)

- [ ] **Step 5: Commit**

```bash
git add internal/db/
git commit -m "feat: postgres connection pool"
```

---

### Task 6: Redis client

- [ ] **Step 1: Write the failing test**

Create `internal/redis/redis_test.go`:
```go
package redis_test

import (
	"context"
	"os"
	"testing"

	redisclient "github.com/airelay/airelay/internal/redis"
	"github.com/stretchr/testify/require"
)

func TestConnect(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set")
	}
	client, err := redisclient.Connect(url)
	require.NoError(t, err)
	defer client.Close()
	require.NoError(t, client.Ping(context.Background()).Err())
}
```

- [ ] **Step 2: Implement redis.Connect**

Create `internal/redis/redis.go`:
```go
package redis

import (
	"fmt"

	"github.com/redis/go-redis/v9"
)

func Connect(redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return redis.NewClient(opts), nil
}
```

- [ ] **Step 3: Run test**

```bash
export $(cat .env | xargs) && go test ./internal/redis/... -v
```
Expected: PASS (1 test)

- [ ] **Step 4: Commit**

```bash
git add internal/redis/
git commit -m "feat: redis client"
```

---

### Task 7: Shared models

- [ ] **Step 1: Create internal/models/models.go**

```go
package models

import (
	"time"

	"github.com/google/uuid"
)

type UserPlan string

const (
	PlanFree UserPlan = "free"
	PlanPro  UserPlan = "pro"
	PlanTeam UserPlan = "team"
)

type User struct {
	ID               uuid.UUID
	Email            string
	PasswordHash     string
	Plan             UserPlan
	StripeCustomerID *string
	CreatedAt        time.Time
}

type Project struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Name       string
	Slug       string
	CreatedAt  time.Time
	ArchivedAt *time.Time
}

type APIKey struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	KeyHash    string
	KeyPrefix  string
	Name       string
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

type AIProvider string

const (
	ProviderOpenAI    AIProvider = "openai"
	ProviderAnthropic AIProvider = "anthropic"
	ProviderGoogle    AIProvider = "google"
)

type ProviderCredential struct {
	ID           uuid.UUID
	ProjectID    uuid.UUID
	Provider     AIProvider
	EncryptedKey string
	CreatedAt    time.Time
	RevokedAt    *time.Time
}

type BudgetPeriod string

const (
	PeriodDaily   BudgetPeriod = "daily"
	PeriodMonthly BudgetPeriod = "monthly"
)

type Budget struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	AmountUSD float64
	Period    BudgetPeriod
	HardLimit bool
	CreatedAt time.Time
}

type AlertThreshold struct {
	ID               uuid.UUID
	BudgetID         uuid.UUID
	ThresholdPct     int
	NotifyEmail      bool
	NotifyWebhookURL *string
	LastFiredAt      *time.Time
}

type UsageEvent struct {
	ID               uuid.UUID
	ProjectID        uuid.UUID
	APIKeyID         uuid.UUID
	Provider         string
	Model            string
	PromptTokens     int
	CompletionTokens int
	CostUSD          *float64
	DurationMS       int
	StatusCode       int
	Metadata         map[string]any
	FailOpen         bool
	CreatedAt        time.Time
}

type ModelPricing struct {
	Provider        string
	Model           string
	InputCostPer1k  float64
	OutputCostPer1k float64
	SyncedFrom      string
	ManualOverride  bool
	LastSyncedAt    time.Time
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./internal/models/...
```
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/models/
git commit -m "feat: shared domain models"
```

---

## Chunk 3: Encryption, Token Counting, and Cost

**Files:**
- Create: `internal/encrypt/encrypt.go` + `encrypt_test.go`
- Create: `internal/tokens/tokens.go`
- Create: `internal/tokens/openai.go`
- Create: `internal/tokens/anthropic.go`
- Create: `internal/tokens/google.go`
- Create: `internal/tokens/tokens_test.go`
- Create: `internal/cost/cost.go` + `cost_test.go`

### Task 8: AES-256 encryption for provider credentials

- [ ] **Step 1: Write the failing test**

Create `internal/encrypt/encrypt_test.go`:
```go
package encrypt_test

import (
	"testing"

	"github.com/airelay/airelay/internal/encrypt"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecrypt(t *testing.T) {
	key := "abcdefghijklmnopqrstuvwxyz123456"
	plaintext := "sk-openai-supersecret-key-12345"

	ciphertext, err := encrypt.Encrypt(key, plaintext)
	require.NoError(t, err)
	require.NotEqual(t, plaintext, ciphertext)

	decrypted, err := encrypt.Decrypt(key, ciphertext)
	require.NoError(t, err)
	require.Equal(t, plaintext, decrypted)
}

func TestEncrypt_DifferentCiphertextEachCall(t *testing.T) {
	key := "abcdefghijklmnopqrstuvwxyz123456"
	c1, _ := encrypt.Encrypt(key, "same-input")
	c2, _ := encrypt.Encrypt(key, "same-input")
	require.NotEqual(t, c1, c2)
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := "abcdefghijklmnopqrstuvwxyz123456"
	key2 := "zyxwvutsrqponmlkjihgfedcba654321"
	ciphertext, _ := encrypt.Encrypt(key1, "secret")
	_, err := encrypt.Decrypt(key2, ciphertext)
	require.Error(t, err)
}

func TestEncrypt_WrongKeyLength(t *testing.T) {
	_, err := encrypt.Encrypt("tooshort", "payload")
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test — expect failure**

```bash
go test ./internal/encrypt/... -v
```
Expected: compilation error.

- [ ] **Step 3: Implement AES-256-GCM encryption**

Create `internal/encrypt/encrypt.go`:
```go
package encrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

func Encrypt(key, plaintext string) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("key must be exactly 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func Decrypt(key, ciphertext string) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("key must be exactly 32 bytes, got %d", len(key))
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	if len(data) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, sealed := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/encrypt/... -v
```
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/encrypt/
git commit -m "feat: AES-256-GCM encryption for provider credentials"
```

---

### Task 9: Token counting per provider

- [ ] **Step 1: Write failing tests**

Create `internal/tokens/tokens_test.go`:
```go
package tokens_test

import (
	"testing"

	"github.com/airelay/airelay/internal/tokens"
	"github.com/stretchr/testify/require"
)

func TestParseOpenAI_WithUsage(t *testing.T) {
	chunk := []byte(`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":25,"total_tokens":35}}`)
	result, err := tokens.ParseOpenAIChunk(chunk)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 10, result.PromptTokens)
	require.Equal(t, 25, result.CompletionTokens)
}

func TestParseOpenAI_NoUsage(t *testing.T) {
	chunk := []byte(`data: {"id":"chatcmpl-123","choices":[{"delta":{"content":"hello"}}]}`)
	result, err := tokens.ParseOpenAIChunk(chunk)
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestParseOpenAI_DoneChunk(t *testing.T) {
	result, err := tokens.ParseOpenAIChunk([]byte("data: [DONE]"))
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestParseAnthropic_MessageStart(t *testing.T) {
	event := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":15,\"output_tokens\":0}}}")
	n := tokens.ParseAnthropicMessageStart(event)
	require.Equal(t, 15, n)
}

func TestParseAnthropic_MessageStart_NonEvent(t *testing.T) {
	event := []byte("event: content_block_start\ndata: {\"type\":\"content_block_start\"}")
	n := tokens.ParseAnthropicMessageStart(event)
	require.Equal(t, 0, n)
}

func TestParseAnthropic_MessageDelta(t *testing.T) {
	event := []byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":42}}")
	result, err := tokens.ParseAnthropicEvent(event, 15)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 15, result.PromptTokens)
	require.Equal(t, 42, result.CompletionTokens)
}

func TestParseAnthropic_NonDeltaEvent(t *testing.T) {
	event := []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hi\"}}")
	result, err := tokens.ParseAnthropicEvent(event, 10)
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestParseGoogle_WithUsage(t *testing.T) {
	chunk := []byte(`{"candidates":[],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":30,"totalTokenCount":38}}`)
	result, err := tokens.ParseGoogleChunk(chunk)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 8, result.PromptTokens)
	require.Equal(t, 30, result.CompletionTokens)
}

func TestParseGoogle_NoUsage(t *testing.T) {
	chunk := []byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`)
	result, err := tokens.ParseGoogleChunk(chunk)
	require.NoError(t, err)
	require.Nil(t, result)
}
```

- [ ] **Step 2: Run test — expect failure**

```bash
go test ./internal/tokens/... -v
```
Expected: compilation error.

- [ ] **Step 3: Create tokens.go**

Create `internal/tokens/tokens.go`:
```go
package tokens

// Usage holds parsed token counts from a provider response.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}
```

- [ ] **Step 4: Create openai.go**

Create `internal/tokens/openai.go`:
```go
package tokens

import (
	"bytes"
	"encoding/json"
)

type openAIChunk struct {
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// ParseOpenAIChunk extracts token usage from an OpenAI SSE data line.
// Returns nil if the chunk contains no usage data (most chunks won't).
func ParseOpenAIChunk(data []byte) (*Usage, error) {
	data = bytes.TrimPrefix(data, []byte("data: "))
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return nil, nil
	}
	var chunk openAIChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, nil // non-fatal: skip malformed chunks
	}
	if chunk.Usage == nil {
		return nil, nil
	}
	return &Usage{
		PromptTokens:     chunk.Usage.PromptTokens,
		CompletionTokens: chunk.Usage.CompletionTokens,
	}, nil
}
```

- [ ] **Step 5: Create anthropic.go**

Create `internal/tokens/anthropic.go`:
```go
package tokens

import (
	"bytes"
	"encoding/json"
)

type anthropicDelta struct {
	Type  string `json:"type"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicMessageStart struct {
	Type    string `json:"type"`
	Message *struct {
		Usage *struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// ParseAnthropicMessageStart extracts input tokens from a message_start event block.
// Returns 0 if not a message_start event or no token data is present.
func ParseAnthropicMessageStart(data []byte) int {
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimPrefix(line, []byte("data: "))
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var start anthropicMessageStart
		if err := json.Unmarshal(line, &start); err != nil {
			continue
		}
		if start.Type == "message_start" && start.Message != nil && start.Message.Usage != nil {
			return start.Message.Usage.InputTokens
		}
	}
	return 0
}

// ParseAnthropicEvent extracts token usage from an Anthropic SSE event block.
// inputTokens must be tracked separately from the message_start event.
// Returns nil for all events except message_delta which carries output token count.
func ParseAnthropicEvent(data []byte, inputTokens int) (*Usage, error) {
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimPrefix(line, []byte("data: "))
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var delta anthropicDelta
		if err := json.Unmarshal(line, &delta); err != nil {
			continue
		}
		if delta.Type == "message_delta" && delta.Usage != nil {
			return &Usage{
				PromptTokens:     inputTokens,
				CompletionTokens: delta.Usage.OutputTokens,
			}, nil
		}
	}
	return nil, nil
}
```

- [ ] **Step 6: Create google.go**

Create `internal/tokens/google.go`:
```go
package tokens

import (
	"bytes"
	"encoding/json"
)

type googleChunk struct {
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

// ParseGoogleChunk extracts token usage from a Google Gemini streaming chunk.
// Returns nil if this chunk contains no usageMetadata.
func ParseGoogleChunk(data []byte) (*Usage, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var chunk googleChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, nil
	}
	if chunk.UsageMetadata == nil {
		return nil, nil
	}
	return &Usage{
		PromptTokens:     chunk.UsageMetadata.PromptTokenCount,
		CompletionTokens: chunk.UsageMetadata.CandidatesTokenCount,
	}, nil
}
```

- [ ] **Step 7: Run all token tests — expect pass**

```bash
go test ./internal/tokens/... -v
```
Expected: PASS (9 tests)

- [ ] **Step 8: Commit**

```bash
git add internal/tokens/
git commit -m "feat: token counting for OpenAI, Anthropic, and Google"
```

---

### Task 10: Cost calculation

- [ ] **Step 1: Write failing test**

Create `internal/cost/cost_test.go`:
```go
package cost_test

import (
	"testing"

	"github.com/airelay/airelay/internal/cost"
	"github.com/airelay/airelay/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCalculate(t *testing.T) {
	pricing := &models.ModelPricing{
		InputCostPer1k:  0.00250,
		OutputCostPer1k: 0.01000,
	}
	// (100/1000 * 0.0025) + (50/1000 * 0.01) = 0.00025 + 0.0005 = 0.00075
	c := cost.Calculate(100, 50, pricing)
	require.InDelta(t, 0.00075, c, 0.000001)
}

func TestCalculate_ZeroTokens(t *testing.T) {
	pricing := &models.ModelPricing{InputCostPer1k: 0.001, OutputCostPer1k: 0.002}
	require.Equal(t, 0.0, cost.Calculate(0, 0, pricing))
}
```

- [ ] **Step 2: Implement**

Create `internal/cost/cost.go`:
```go
package cost

import "github.com/airelay/airelay/internal/models"

// Calculate returns total cost in USD for a request.
func Calculate(promptTokens, completionTokens int, pricing *models.ModelPricing) float64 {
	input := float64(promptTokens) / 1000.0 * pricing.InputCostPer1k
	output := float64(completionTokens) / 1000.0 * pricing.OutputCostPer1k
	return input + output
}
```

- [ ] **Step 3: Run tests — expect pass**

```bash
go test ./internal/cost/... -v
```
Expected: PASS (2 tests)

- [ ] **Step 4: Commit**

```bash
git add internal/cost/
git commit -m "feat: cost calculation from token counts and model pricing"
```

---

## Chunk 4: Proxy Service — Auth, Budget, Logger

**Files:**
- Create: `proxy/auth.go` + `proxy/auth_test.go`
- Create: `proxy/budget.go` + `proxy/budget_test.go`
- Create: `proxy/logger.go`
- Create: `proxy/dlq.go`

### Task 11: API key authentication (proxy-side)

- [ ] **Step 1: Write failing test**

Create `proxy/auth_test.go`:
```go
package proxy_test

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/airelay/airelay/proxy"
	"github.com/stretchr/testify/require"
)

func TestHashKey(t *testing.T) {
	key := "air_sk_testkey123"
	hash := proxy.HashKey(key)
	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	require.Equal(t, expected, hash)
}

func TestGenerateKey(t *testing.T) {
	full, prefix, hash := proxy.GenerateKey()
	require.True(t, len(full) > 20)
	require.True(t, len(full) >= 16)
	require.Equal(t, full[:16], prefix)
	require.Equal(t, proxy.HashKey(full), hash)
}

func TestGenerateKey_Unique(t *testing.T) {
	_, _, h1 := proxy.GenerateKey()
	_, _, h2 := proxy.GenerateKey()
	require.NotEqual(t, h1, h2)
}
```

- [ ] **Step 2: Run test — expect failure**

```bash
go test ./proxy/... -v
```
Expected: compilation error.

- [ ] **Step 3: Implement proxy/auth.go**

Create `proxy/auth.go`:
```go
package proxy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/airelay/airelay/internal/encrypt"
	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const keyPrefix = "air_sk_"
const keyCacheTTL = 5 * time.Minute

// HashKey returns the SHA-256 hex hash of an API key.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// GenerateKey creates a new AIRelay API key.
// Returns (fullKey, displayPrefix, hash).
func GenerateKey() (string, string, string) {
	b := make([]byte, 24)
	rand.Read(b)
	full := keyPrefix + hex.EncodeToString(b)
	prefix := full[:16]
	return full, prefix, HashKey(full)
}

// KeyLookup holds the resolved data for an inbound API key.
type KeyLookup struct {
	APIKeyID  uuid.UUID
	ProjectID uuid.UUID
	Provider  models.AIProvider
	PlainKey  string
}

// KeyResolver resolves AIRelay API keys to projects and decrypted provider credentials.
type KeyResolver struct {
	db     *pgxpool.Pool
	redis  *redis.Client
	encKey string
}

func NewKeyResolver(db *pgxpool.Pool, rdb *redis.Client, encKey string) *KeyResolver {
	return &KeyResolver{db: db, redis: rdb, encKey: encKey}
}

// Resolve looks up an inbound key from Redis cache, falling back to Postgres.
func (r *KeyResolver) Resolve(ctx context.Context, inboundKey string, provider models.AIProvider) (*KeyLookup, error) {
	hash := HashKey(inboundKey)
	cacheKey := fmt.Sprintf("keycache:%s:%s", hash, provider)

	if val, err := r.redis.Get(ctx, cacheKey).Result(); err == nil {
		return parseKeyLookup(val)
	}

	lookup, err := r.resolveFromDB(ctx, hash, provider)
	if err != nil {
		return nil, err
	}

	r.redis.Set(ctx, cacheKey, encodeKeyLookup(lookup), keyCacheTTL)
	return lookup, nil
}

func (r *KeyResolver) resolveFromDB(ctx context.Context, keyHash string, provider models.AIProvider) (*KeyLookup, error) {
	var lookup KeyLookup
	var encryptedKey string
	err := r.db.QueryRow(ctx, `
		SELECT ak.id, ak.project_id, pc.provider, pc.encrypted_key
		FROM api_keys ak
		JOIN provider_credentials pc ON pc.project_id = ak.project_id
		    AND pc.provider = $2
		    AND pc.revoked_at IS NULL
		WHERE ak.key_hash = $1
		  AND ak.revoked_at IS NULL`,
		keyHash, string(provider),
	).Scan(&lookup.APIKeyID, &lookup.ProjectID, &lookup.Provider, &encryptedKey)
	if err != nil {
		return nil, fmt.Errorf("key not found or no credential for provider: %w", err)
	}
	plain, err := encrypt.Decrypt(r.encKey, encryptedKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential: %w", err)
	}
	lookup.PlainKey = plain
	return &lookup, nil
}

// encodeKeyLookup serialises a KeyLookup for Redis caching.
// PlainKey may contain arbitrary characters so we base64 encode the last field.
func encodeKeyLookup(l *KeyLookup) string {
	encodedKey := hex.EncodeToString([]byte(l.PlainKey))
	return fmt.Sprintf("%s|%s|%s|%s", l.APIKeyID, l.ProjectID, l.Provider, encodedKey)
}

func parseKeyLookup(s string) (*KeyLookup, error) {
	parts := strings.SplitN(s, "|", 4)
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid cache format")
	}
	akID, err := uuid.Parse(parts[0])
	if err != nil {
		return nil, fmt.Errorf("parse api key id: %w", err)
	}
	pID, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, fmt.Errorf("parse project id: %w", err)
	}
	plainBytes, err := hex.DecodeString(parts[3])
	if err != nil {
		return nil, fmt.Errorf("decode plain key: %w", err)
	}
	return &KeyLookup{
		APIKeyID:  akID,
		ProjectID: pID,
		Provider:  models.AIProvider(parts[2]),
		PlainKey:  string(plainBytes),
	}, nil
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./proxy/... -run TestHashKey -v
go test ./proxy/... -run TestGenerateKey -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add proxy/auth.go proxy/auth_test.go
git commit -m "feat: proxy API key hashing, generation, and Redis-cached resolution"
```

---

### Task 12: Budget enforcement

- [ ] **Step 1: Write failing test**

Create `proxy/budget_test.go`:
```go
package proxy_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/airelay/airelay/proxy"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSpendKey_Daily(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	day := time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC)
	key := proxy.SpendKey(id, "daily", day)
	require.Equal(t, fmt.Sprintf("spend:%s:daily:2026-03-17", id), key)
}

func TestSpendKey_Monthly(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	day := time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC)
	key := proxy.SpendKey(id, "monthly", day)
	require.Equal(t, fmt.Sprintf("spend:%s:monthly:2026-03", id), key)
}
```

- [ ] **Step 2: Run test — expect failure**

```bash
go test ./proxy/... -run TestSpendKey -v
```

- [ ] **Step 3: Implement proxy/budget.go**

Create `proxy/budget.go`:
```go
package proxy

import (
	"context"
	"fmt"
	"time"

	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// SpendKey returns the period-scoped Redis key for a project's spend.
func SpendKey(projectID uuid.UUID, period string, t time.Time) string {
	switch period {
	case "daily":
		return fmt.Sprintf("spend:%s:daily:%s", projectID, t.UTC().Format("2006-01-02"))
	case "monthly":
		return fmt.Sprintf("spend:%s:monthly:%s", projectID, t.UTC().Format("2006-01"))
	default:
		return fmt.Sprintf("spend:%s:%s", projectID, period)
	}
}

// BudgetResult is returned by CheckBudgets.
type BudgetResult struct {
	Blocked   bool
	Reason    string
	RedisDown bool // true if Redis was unreachable; handler should use fail-open logging path
}

// BudgetChecker checks and records project spend against configured budgets.
type BudgetChecker struct {
	db    *pgxpool.Pool
	redis *redis.Client
}

func NewBudgetChecker(db *pgxpool.Pool, rdb *redis.Client) *BudgetChecker {
	return &BudgetChecker{db: db, redis: rdb}
}

// CheckBudgets returns Blocked=true if any hard-limit budget has been exceeded.
// Fails open on all errors: budget check errors never block a request.
// Sets RedisDown=true if Redis is unreachable so the handler can write directly to Postgres.
func (b *BudgetChecker) CheckBudgets(ctx context.Context, projectID uuid.UUID) (*BudgetResult, error) {
	// Probe Redis with a cheap ping to detect outage before loading budgets
	if err := b.redis.Ping(ctx).Err(); err != nil {
		return &BudgetResult{Blocked: false, RedisDown: true}, nil
	}
	budgets, err := b.loadBudgets(ctx, projectID)
	if err != nil {
		return &BudgetResult{Blocked: false}, nil // fail open
	}
	now := time.Now().UTC()
	for _, budget := range budgets {
		key := SpendKey(projectID, string(budget.Period), now)
		spend, err := b.getSpend(ctx, key, projectID, budget.Period)
		if err != nil {
			continue // fail open per budget
		}
		if budget.HardLimit && spend >= budget.AmountUSD {
			return &BudgetResult{
				Blocked: true,
				Reason:  fmt.Sprintf("%s budget of $%.4f exceeded (spend: $%.4f)", budget.Period, budget.AmountUSD, spend),
			}, nil
		}
	}
	return &BudgetResult{Blocked: false}, nil
}

// RecordSpend adds cost to the Redis spend key for a given period.
func (b *BudgetChecker) RecordSpend(ctx context.Context, projectID uuid.UUID, period models.BudgetPeriod, costUSD float64) {
	key := SpendKey(projectID, string(period), time.Now().UTC())
	b.redis.IncrByFloat(ctx, key, costUSD)
}

func (b *BudgetChecker) loadBudgets(ctx context.Context, projectID uuid.UUID) ([]models.Budget, error) {
	rows, err := b.db.Query(ctx,
		`SELECT id, project_id, amount_usd, period, hard_limit, created_at
		 FROM budgets WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var budgets []models.Budget
	for rows.Next() {
		var bg models.Budget
		if err := rows.Scan(&bg.ID, &bg.ProjectID, &bg.AmountUSD, &bg.Period, &bg.HardLimit, &bg.CreatedAt); err != nil {
			return nil, err
		}
		budgets = append(budgets, bg)
	}
	return budgets, rows.Err()
}

// getSpend returns current spend from Redis, rebuilding from Postgres on cache miss.
func (b *BudgetChecker) getSpend(ctx context.Context, key string, projectID uuid.UUID, period models.BudgetPeriod) (float64, error) {
	val, err := b.redis.Get(ctx, key).Float64()
	if err == nil {
		return val, nil
	}
	if err != redis.Nil {
		return 0, err
	}
	spend, err := b.rebuildFromDB(ctx, projectID, period)
	if err != nil {
		return 0, err
	}
	b.redis.Set(ctx, key, spend, 0)
	return spend, nil
}

func (b *BudgetChecker) rebuildFromDB(ctx context.Context, projectID uuid.UUID, period models.BudgetPeriod) (float64, error) {
	now := time.Now().UTC()
	var periodStart time.Time
	switch period {
	case models.PeriodDaily:
		periodStart = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case models.PeriodMonthly:
		periodStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	var spend float64
	err := b.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM usage_events
		 WHERE project_id = $1 AND created_at >= $2`,
		projectID, periodStart,
	).Scan(&spend)
	return spend, err
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./proxy/... -run TestSpendKey -v
```
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
git add proxy/budget.go proxy/budget_test.go
git commit -m "feat: budget enforcement with period-scoped Redis keys and fail-open"
```

---

### Task 13: Async usage logger and dead letter queue

- [ ] **Step 1: Create proxy/dlq.go**

```go
package proxy

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const dlqCap = 50_000

// DLQ is an in-memory dead letter queue for usage events that failed Postgres writes.
type DLQ struct {
	mu    sync.Mutex
	queue []UsageEvent
	db    *pgxpool.Pool
}

func NewDLQ(db *pgxpool.Pool) *DLQ {
	d := &DLQ{db: db}
	go d.retryLoop()
	return d
}

func (d *DLQ) Enqueue(e UsageEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.queue) >= dlqCap {
		log.Printf("WARN: DLQ full (%d items), dropping oldest event", dlqCap)
		d.queue = d.queue[1:]
	}
	d.queue = append(d.queue, e)
}

func (d *DLQ) retryLoop() {
	backoff := []time.Duration{5 * time.Second, 30 * time.Second, 5 * time.Minute}
	attempt := 0
	for {
		time.Sleep(backoff[min(attempt, len(backoff)-1)])
		d.mu.Lock()
		if len(d.queue) == 0 {
			d.mu.Unlock()
			attempt = 0
			continue
		}
		batch := make([]UsageEvent, len(d.queue))
		copy(batch, d.queue)
		d.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		failed := d.flushBatch(ctx, batch)
		cancel()

		d.mu.Lock()
		d.queue = failed
		d.mu.Unlock()

		if len(failed) > 0 {
			attempt = min(attempt+1, len(backoff)-1)
			log.Printf("WARN: DLQ retry: %d events remaining", len(failed))
		} else {
			attempt = 0
		}
	}
}

func (d *DLQ) flushBatch(ctx context.Context, batch []UsageEvent) []UsageEvent {
	var failed []UsageEvent
	for _, e := range batch {
		if err := writeUsageEvent(ctx, d.db, e); err != nil {
			failed = append(failed, e)
		}
	}
	return failed
}
```

Note: `min` is a builtin in Go 1.21+. Do not redeclare it.

- [ ] **Step 2: Create proxy/logger.go**

```go
package proxy

import (
	"context"
	"log"
	"time"

	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UsageEvent is a record to be written to Postgres.
type UsageEvent struct {
	ProjectID        uuid.UUID
	APIKeyID         uuid.UUID
	Provider         string
	Model            string
	PromptTokens     int
	CompletionTokens int
	CostUSD          *float64
	DurationMS       int
	StatusCode       int
	Metadata         map[string]any
	FailOpen         bool
}

// Logger batches usage events and writes them to Postgres asynchronously.
type Logger struct {
	db      *pgxpool.Pool
	ch      chan UsageEvent
	dlq     *DLQ
	budgets *BudgetChecker
}

func NewLogger(db *pgxpool.Pool, budgets *BudgetChecker) *Logger {
	l := &Logger{
		db:      db,
		ch:      make(chan UsageEvent, 50_000),
		dlq:     NewDLQ(db),
		budgets: budgets,
	}
	go l.run()
	return l
}

// Log queues an event for async write. Non-blocking — drops on full channel.
func (l *Logger) Log(e UsageEvent) {
	select {
	case l.ch <- e:
	default:
		log.Printf("WARN: usage logger channel full, dropping event for project %s", e.ProjectID)
	}
}

// LogDirect writes an event synchronously to Postgres (used during fail-open when Redis is down).
func (l *Logger) LogDirect(ctx context.Context, e UsageEvent) error {
	return writeUsageEvent(ctx, l.db, e)
}

func (l *Logger) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var batch []UsageEvent

	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, e := range batch {
			if err := writeUsageEvent(ctx, l.db, e); err != nil {
				l.dlq.Enqueue(e)
			} else {
				l.recordSpend(ctx, e)
			}
		}
		batch = batch[:0]
	}

	for {
		select {
		case e := <-l.ch:
			batch = append(batch, e)
			if len(batch) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// recordSpend updates Redis spend counters only for periods that have budgets.
func (l *Logger) recordSpend(ctx context.Context, e UsageEvent) {
	if e.CostUSD == nil || l.budgets == nil {
		return
	}
	budgets, err := l.budgets.loadBudgets(ctx, e.ProjectID)
	if err != nil {
		return
	}
	for _, b := range budgets {
		l.budgets.RecordSpend(ctx, e.ProjectID, b.Period, *e.CostUSD)
	}
}

// writeUsageEvent is shared by Logger and DLQ.
func writeUsageEvent(ctx context.Context, db *pgxpool.Pool, e UsageEvent) error {
	_, err := db.Exec(ctx, `
		INSERT INTO usage_events
		    (project_id, api_key_id, provider, model, prompt_tokens, completion_tokens,
		     cost_usd, duration_ms, status_code, metadata, fail_open)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.ProjectID, e.APIKeyID, e.Provider, e.Model,
		e.PromptTokens, e.CompletionTokens, e.CostUSD,
		e.DurationMS, e.StatusCode, e.Metadata, e.FailOpen,
	)
	return err
}

// loadBudgets is exported from BudgetChecker for use by Logger.
// We expose it via a method to avoid duplicating the query.
func (b *BudgetChecker) loadBudgetsExported(ctx context.Context, projectID uuid.UUID) ([]models.Budget, error) {
	return b.loadBudgets(ctx, projectID)
}
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./proxy/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add proxy/logger.go proxy/dlq.go
git commit -m "feat: async usage logger with period-aware spend recording and DLQ"
```

---

## Chunk 5: Proxy Service — Forwarding and Handler

**Files:**
- Create: `proxy/forward.go`
- Create: `proxy/handler.go`
- Create: `proxy/server.go`
- Create: `cmd/proxy/main.go`

### Task 14: Request forwarding and SSE streaming

- [ ] **Step 1: Create proxy/forward.go**

```go
package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/airelay/airelay/internal/models"
	"github.com/airelay/airelay/internal/tokens"
)

// ProviderURLs maps providers to their base API URLs.
var ProviderURLs = map[models.AIProvider]string{
	models.ProviderOpenAI:    "https://api.openai.com",
	models.ProviderAnthropic: "https://api.anthropic.com",
	models.ProviderGoogle:    "https://generativelanguage.googleapis.com",
}

// ForwardResult contains the outcome of a forwarded request.
type ForwardResult struct {
	StatusCode           int
	Usage                *tokens.Usage
	DurationMS           int
	AnthropicInputTokens int
}

var proxyHTTPClient = &http.Client{Timeout: 5 * time.Minute}

// Forward proxies the request to the provider and streams the response to w.
// It extracts token usage from the final SSE chunk for cost accounting.
func Forward(
	w http.ResponseWriter,
	r *http.Request,
	providerBase string,
	providerKey string,
	provider models.AIProvider,
	pathSuffix string,
) (*ForwardResult, error) {
	start := time.Now()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}

	upstreamURL := providerBase + pathSuffix
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	for k, vs := range r.Header {
		switch k {
		case "Authorization", "X-Api-Key", "X-Airelay-Meta":
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	switch provider {
	case models.ProviderOpenAI, models.ProviderGoogle:
		req.Header.Set("Authorization", "Bearer "+providerKey)
	case models.ProviderAnthropic:
		req.Header.Set("x-api-key", providerKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	resp, err := proxyHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	result := &ForwardResult{StatusCode: resp.StatusCode}

	if resp.Header.Get("Content-Type") == "text/event-stream" {
		result.Usage = streamSSE(w, resp.Body, provider, &result.AnthropicInputTokens)
	} else {
		io.Copy(w, resp.Body)
	}

	result.DurationMS = int(time.Since(start).Milliseconds())
	return result, nil
}

// streamSSE forwards SSE chunks to the client and extracts token usage.
func streamSSE(w http.ResponseWriter, body io.Reader, provider models.AIProvider, anthropicInput *int) *tokens.Usage {
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	var lastUsage *tokens.Usage
	var eventBuf bytes.Buffer

	for scanner.Scan() {
		line := scanner.Bytes()
		fmt.Fprintf(w, "%s\n", line)
		if flusher != nil {
			flusher.Flush()
		}

		eventBuf.Write(line)
		eventBuf.WriteByte('\n')

		switch provider {
		case models.ProviderOpenAI:
			if u, _ := tokens.ParseOpenAIChunk(line); u != nil {
				lastUsage = u
			}
		case models.ProviderAnthropic:
			// message_start carries input token count — capture it first
			if n := tokens.ParseAnthropicMessageStart(eventBuf.Bytes()); n > 0 {
				*anthropicInput = n
			}
			if u, _ := tokens.ParseAnthropicEvent(eventBuf.Bytes(), *anthropicInput); u != nil {
				lastUsage = u
			}
		case models.ProviderGoogle:
			if u, _ := tokens.ParseGoogleChunk(line); u != nil {
				lastUsage = u
			}
		}

		if len(bytes.TrimSpace(line)) == 0 {
			eventBuf.Reset()
		}
	}
	return lastUsage
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./proxy/...
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add proxy/forward.go
git commit -m "feat: provider request forwarding with transparent SSE streaming"
```

---

### Task 15: Main proxy handler

- [ ] **Step 1: Create proxy/handler.go**

```go
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/airelay/airelay/internal/cost"
	"github.com/airelay/airelay/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Handler is the main proxy HTTP handler.
type Handler struct {
	resolver *KeyResolver
	budgets  *BudgetChecker
	logger   *Logger
	db       *pgxpool.Pool
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
		// Response may or may not have been started — log and return.
		// writeJSON would corrupt a partially-written response, so we only log here.
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
		if pricing := h.lookupPricing(string(provider), model); pricing != nil {
			c := cost.Calculate(event.PromptTokens, event.CompletionTokens, pricing)
			event.CostUSD = &c
		}
	}

	// Tier 1 fail-open: Redis down → write directly to Postgres, flag the event.
	// This ensures zero data loss when only Redis is unavailable.
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

func (h *Handler) lookupPricing(provider, model string) *models.ModelPricing {
	var pricing models.ModelPricing
	err := h.db.QueryRow(context.Background(),
		`SELECT input_cost_per_1k, output_cost_per_1k FROM model_pricing WHERE provider=$1 AND model=$2`,
		provider, model,
	).Scan(&pricing.InputCostPer1k, &pricing.OutputCostPer1k)
	if err != nil {
		return nil
	}
	pricing.Provider = provider
	pricing.Model = model
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
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./proxy/...
```
Expected: no errors.

- [ ] **Step 3: Write unit tests for the handler's early-exit paths**

Create `proxy/handler_test.go`:
```go
package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/proxy"
	"github.com/stretchr/testify/require"
)

// newTestHandler creates a Handler with nil DB/Redis — safe for tests that
// exercise auth/routing rejection paths that never reach DB or Redis.
func newTestHandler() *proxy.Handler {
	return proxy.NewHandler(nil, nil, "abcdefghijklmnopqrstuvwxyz123456")
}

func TestHandler_MissingKey(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/proxy/openai/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_InvalidKeyPrefix(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/proxy/openai/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-notanairelay key")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_UnknownProvider(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/proxy/badprovider/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer air_sk_testkey123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_MissingPathSuffix(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/proxy/openai", nil)
	req.Header.Set("Authorization", "Bearer air_sk_testkey123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
```

- [ ] **Step 4: Run test**

```bash
go test ./proxy/... -run TestHandler -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add proxy/handler.go proxy/handler_test.go
git commit -m "feat: main proxy handler wiring auth, budget, forwarding, and logging"
```

---

### Task 16: Server entry point

- [ ] **Step 1: Create proxy/server.go**

```go
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
```

- [ ] **Step 2: Create cmd/proxy/main.go**

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/airelay/airelay/internal/config"
	"github.com/airelay/airelay/internal/db"
	redisclient "github.com/airelay/airelay/internal/redis"
	"github.com/airelay/airelay/proxy"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	pool, err := db.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	rdb, err := redisclient.Connect(cfg.RedisURL)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()

	srv := proxy.NewServer(pool, rdb, cfg.CredentialEncryptionKey)
	srv.Addr = ":" + cfg.ProxyPort

	log.Printf("AIRelay proxy listening on :%s", cfg.ProxyPort)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("proxy server: %v", err)
	}
}
```

- [ ] **Step 3: Build**

```bash
go build ./cmd/proxy/
```
Expected: produces `proxy` binary.

- [ ] **Step 4: Run health check**

```bash
export $(cat .env | xargs) && go run ./cmd/proxy/ &
sleep 1
curl http://localhost:8081/health
kill %1
```
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add proxy/server.go cmd/proxy/
git commit -m "feat: proxy server entry point"
```

---

## Chunk 6: Seed Script and End-to-End Test

**Files:**
- Create: `cmd/seed/main.go`

### Task 17: Seed script for local development

- [ ] **Step 1: Create cmd/seed/main.go**

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/airelay/airelay/internal/config"
	"github.com/airelay/airelay/internal/db"
	"github.com/airelay/airelay/internal/encrypt"
	"github.com/airelay/airelay/proxy"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	pool, err := db.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	ctx := context.Background()

	// User
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	var userID string
	err = pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, plan)
		 VALUES ($1, $2, 'pro')
		 ON CONFLICT (email) DO UPDATE SET plan='pro'
		 RETURNING id`,
		"dev@airelay.dev", string(hash),
	).Scan(&userID)
	if err != nil {
		log.Fatalf("create user: %v", err)
	}

	// Project
	var projectID string
	err = pool.QueryRow(ctx,
		`INSERT INTO projects (user_id, name, slug)
		 VALUES ($1, 'Dev Project', 'dev-project')
		 ON CONFLICT (slug) DO UPDATE SET name='Dev Project'
		 RETURNING id`,
		userID,
	).Scan(&projectID)
	if err != nil {
		log.Fatalf("create project: %v", err)
	}

	// API key — always recreate so the printed key is guaranteed to work on re-runs
	fullKey, prefix, keyHash := proxy.GenerateKey()
	pool.Exec(ctx, `DELETE FROM api_keys WHERE project_id=$1 AND name='dev-key'`, projectID)
	_, err = pool.Exec(ctx,
		`INSERT INTO api_keys (project_id, key_hash, key_prefix, name)
		 VALUES ($1, $2, $3, 'dev-key')`,
		projectID, keyHash, prefix,
	)
	if err != nil {
		log.Fatalf("create api key: %v", err)
	}

	// Provider credentials (OpenAI)
	if cfg.OpenAIKey != "" {
		encKey, err := encrypt.Encrypt(cfg.CredentialEncryptionKey, cfg.OpenAIKey)
		if err != nil {
			log.Fatalf("encrypt openai key: %v", err)
		}
		_, err = pool.Exec(ctx,
			`INSERT INTO provider_credentials (project_id, provider, encrypted_key)
			 VALUES ($1, 'openai', $2)
			 ON CONFLICT (project_id, provider) DO UPDATE SET encrypted_key=$2`,
			projectID, encKey,
		)
		if err != nil {
			log.Fatalf("create openai credential: %v", err)
		}
	}

	// Provider credentials (Anthropic)
	if cfg.AnthropicKey != "" {
		encKey, err := encrypt.Encrypt(cfg.CredentialEncryptionKey, cfg.AnthropicKey)
		if err != nil {
			log.Fatalf("encrypt anthropic key: %v", err)
		}
		pool.Exec(ctx,
			`INSERT INTO provider_credentials (project_id, provider, encrypted_key)
			 VALUES ($1, 'anthropic', $2)
			 ON CONFLICT (project_id, provider) DO UPDATE SET encrypted_key=$2`,
			projectID, encKey,
		)
	}

	// Monthly budget: $10 hard limit
	pool.Exec(ctx,
		`INSERT INTO budgets (project_id, amount_usd, period, hard_limit)
		 VALUES ($1, 10.00, 'monthly', true)
		 ON CONFLICT (project_id, period) DO NOTHING`,
		projectID,
	)

	fmt.Printf("✅ Seed complete\n\n")
	fmt.Printf("  Email:      dev@airelay.dev\n")
	fmt.Printf("  Password:   password123\n")
	fmt.Printf("  Project ID: %s\n", projectID)
	fmt.Printf("  API Key:    %s\n\n", fullKey)

	fmt.Printf("Test the proxy:\n")
	fmt.Printf("  export OPENAI_BASE_URL=http://localhost:8081/proxy/openai\n")
	fmt.Printf("  curl http://localhost:8081/proxy/openai/v1/models \\\n")
	fmt.Printf("    -H 'Authorization: Bearer %s'\n\n", fullKey)

	if cfg.OpenAIKey == "" {
		fmt.Fprintln(os.Stderr, "⚠️  OPENAI_API_KEY not set in .env — OpenAI calls won't work")
	}
}
```

- [ ] **Step 2: Run seed**

```bash
export $(cat .env | xargs) && go run ./cmd/seed/
```
Expected:
```
✅ Seed complete

  Email:      dev@airelay.dev
  Project ID: <uuid>
  API Key:    air_sk_<hex>
```

- [ ] **Step 3: Start proxy and test end-to-end**

```bash
# Terminal 1
export $(cat .env | xargs) && go run ./cmd/proxy/

# Terminal 2 — replace <KEY> with key from seed output
curl http://localhost:8081/proxy/openai/v1/models \
  -H "Authorization: Bearer <KEY>"
```
Expected: JSON response from OpenAI listing available models.

- [ ] **Step 4: Test 401 on bad key**

```bash
curl -w "\n%{http_code}" http://localhost:8081/proxy/openai/v1/models \
  -H "Authorization: Bearer air_sk_wrongkey"
```
Expected: `401` with `{"error":"invalid API key..."}`

- [ ] **Step 5: Test budget enforcement**

```bash
# Set spend above the $10 monthly budget
# Replace <PROJECT_ID> with the UUID printed by seed
redis-cli -u redis://localhost:6379 SET "spend:<PROJECT_ID>:monthly:$(date +%Y-%m)" 11.00

# Request should now be blocked
curl -w "\n%{http_code}" http://localhost:8081/proxy/openai/v1/models \
  -H "Authorization: Bearer <KEY>"
```
Expected: `429` with `{"error":"budget exceeded: ..."}`

- [ ] **Step 6: Run all tests**

```bash
export $(cat .env | xargs) && make test
```
Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add cmd/seed/
git commit -m "feat: seed script for local dev and end-to-end proxy test"
```

---

## Summary

Plan 1 delivers a fully working proxy service:

- ✅ Go module, Docker Compose, Makefile
- ✅ 8 database migrations with seeded model pricing
- ✅ Config (with 32-byte key validation), DB pool, Redis client
- ✅ AES-256-GCM credential encryption
- ✅ Token counting for OpenAI, Anthropic, and Google (including Anthropic `message_start` input token extraction)
- ✅ Cost calculation
- ✅ SHA-256 API key hashing, generation, and Redis-cached resolution (pipe-safe encoding)
- ✅ Period-scoped budget enforcement (`spend:{id}:daily:YYYY-MM-DD`, `spend:{id}:monthly:YYYY-MM`)
- ✅ Async usage logger: 50k-cap channel, 1s/100-event flush, period-aware spend recording
- ✅ Dead letter queue with exponential backoff (uses Go 1.21+ builtin `min`)
- ✅ Transparent SSE streaming with token extraction
- ✅ `peekModel` reads model from body before forwarding
- ✅ Error path does not double-write response headers
- ✅ Tier 1 fail-open: Redis down → direct Postgres write with `fail_open=true` flag
- ✅ Real handler unit tests for all early-exit paths (401, 400 — no DB/Redis required)
- ✅ Seed script always produces a working key on re-run (delete-then-insert dev key)

**Next:** Plan 2 — Management API + Background Jobs + Dashboard

//go:build integration

package proxy_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/airelay/airelay/internal/db"
	"github.com/airelay/airelay/internal/encrypt"
	"github.com/airelay/airelay/internal/models"
	redisclient "github.com/airelay/airelay/internal/redis"
	"github.com/airelay/airelay/proxy"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const testEncKey = "integration-test-enckey-32bytes!"

func requireEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("skipping integration test: %s not set", key)
	}
	return v
}

func TestIntegration_KeyResolver(t *testing.T) {
	ctx := context.Background()
	dbURL := requireEnv(t, "DATABASE_URL")
	redisURL := requireEnv(t, "REDIS_URL")

	pool, err := db.Connect(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	rdb, err := redisclient.Connect(redisURL)
	require.NoError(t, err)
	defer rdb.Close()

	// Insert test user
	var userID uuid.UUID
	err = pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, plan)
		 VALUES ('integration-test@airelay.dev', 'x', 'pro')
		 ON CONFLICT (email) DO UPDATE SET plan='pro'
		 RETURNING id`,
	).Scan(&userID)
	require.NoError(t, err)

	// Insert test project
	var projectID uuid.UUID
	err = pool.QueryRow(ctx,
		`INSERT INTO projects (user_id, name, slug)
		 VALUES ($1, 'Integration Test', 'integration-test-keyresolver')
		 ON CONFLICT (slug) DO UPDATE SET name='Integration Test'
		 RETURNING id`,
		userID,
	).Scan(&projectID)
	require.NoError(t, err)

	// Generate a real API key
	fullKey, keyPrefix, keyHash := proxy.GenerateKey()

	// Insert API key
	var keyID uuid.UUID
	err = pool.QueryRow(ctx,
		`INSERT INTO api_keys (project_id, key_hash, key_prefix, name)
		 VALUES ($1, $2, $3, 'integration-test-key')
		 RETURNING id`,
		projectID, keyHash, keyPrefix,
	).Scan(&keyID)
	require.NoError(t, err)

	// Encrypt and insert OpenAI credential
	encryptedKey, err := encrypt.Encrypt(testEncKey, "sk-fake-openai-key-for-testing")
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO provider_credentials (project_id, provider, encrypted_key)
		 VALUES ($1, 'openai', $2)`,
		projectID, encryptedKey,
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, keyID)
		pool.Exec(context.Background(), `DELETE FROM provider_credentials WHERE project_id=$1`, projectID)
		pool.Exec(context.Background(), `DELETE FROM projects WHERE id=$1`, projectID)
		pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
		rdb.Del(context.Background(), "keycache:"+proxy.HashKey(fullKey)+":openai")
	})

	resolver := proxy.NewKeyResolver(pool, rdb, testEncKey)

	// First call — DB lookup
	lookup, err := resolver.Resolve(ctx, fullKey, models.ProviderOpenAI)
	require.NoError(t, err)
	require.Equal(t, projectID, lookup.ProjectID)
	require.Equal(t, keyID, lookup.APIKeyID)
	require.Equal(t, "sk-fake-openai-key-for-testing", lookup.PlainKey)

	// Second call — should hit Redis cache
	lookup2, err := resolver.Resolve(ctx, fullKey, models.ProviderOpenAI)
	require.NoError(t, err)
	require.Equal(t, lookup.ProjectID, lookup2.ProjectID)
	require.Equal(t, lookup.PlainKey, lookup2.PlainKey)
}

func TestIntegration_BudgetEnforcement(t *testing.T) {
	ctx := context.Background()
	dbURL := requireEnv(t, "DATABASE_URL")
	redisURL := requireEnv(t, "REDIS_URL")

	pool, err := db.Connect(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	rdb, err := redisclient.Connect(redisURL)
	require.NoError(t, err)
	defer rdb.Close()

	// Insert test user + project
	var userID, projectID uuid.UUID
	pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, plan)
		 VALUES ('integration-budget@airelay.dev', 'x', 'pro')
		 ON CONFLICT (email) DO UPDATE SET plan='pro'
		 RETURNING id`,
	).Scan(&userID)
	pool.QueryRow(ctx,
		`INSERT INTO projects (user_id, name, slug)
		 VALUES ($1, 'Budget Test', 'integration-test-budget')
		 ON CONFLICT (slug) DO UPDATE SET name='Budget Test'
		 RETURNING id`,
		userID,
	).Scan(&projectID)

	// Insert $5 monthly hard limit
	_, err = pool.Exec(ctx,
		`INSERT INTO budgets (project_id, amount_usd, period, hard_limit)
		 VALUES ($1, 5.00, 'monthly', true)
		 ON CONFLICT (project_id, period) DO UPDATE SET amount_usd=5.00, hard_limit=true`,
		projectID,
	)
	require.NoError(t, err)

	spendKey := proxy.SpendKey(projectID, "monthly", time.Now().UTC())

	t.Cleanup(func() {
		rdb.Del(context.Background(), spendKey)
		pool.Exec(context.Background(), `DELETE FROM budgets WHERE project_id=$1`, projectID)
		pool.Exec(context.Background(), `DELETE FROM projects WHERE id=$1`, projectID)
		pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	checker := proxy.NewBudgetChecker(pool, rdb)

	// Under budget — should pass
	rdb.Set(ctx, spendKey, 4.99, 0)
	result, err := checker.CheckBudgets(ctx, projectID)
	require.NoError(t, err)
	require.False(t, result.Blocked, "should not be blocked at $4.99 of $5.00")

	// Over budget — should block
	rdb.Set(ctx, spendKey, 5.01, 0)
	result, err = checker.CheckBudgets(ctx, projectID)
	require.NoError(t, err)
	require.True(t, result.Blocked, "should be blocked at $5.01 of $5.00")
	require.Contains(t, result.Reason, "monthly")
}

func TestIntegration_UsageEventWrite(t *testing.T) {
	ctx := context.Background()
	dbURL := requireEnv(t, "DATABASE_URL")

	pool, err := db.Connect(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	rdb, err := redisclient.Connect(requireEnv(t, "REDIS_URL"))
	require.NoError(t, err)
	defer rdb.Close()

	// Insert test user + project + api key
	var userID, projectID, keyID uuid.UUID
	pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, plan)
		 VALUES ('integration-usage@airelay.dev', 'x', 'pro')
		 ON CONFLICT (email) DO UPDATE SET plan='pro'
		 RETURNING id`,
	).Scan(&userID)
	pool.QueryRow(ctx,
		`INSERT INTO projects (user_id, name, slug)
		 VALUES ($1, 'Usage Test', 'integration-test-usage')
		 ON CONFLICT (slug) DO UPDATE SET name='Usage Test'
		 RETURNING id`,
		userID,
	).Scan(&projectID)
	pool.QueryRow(ctx,
		`INSERT INTO api_keys (project_id, key_hash, key_prefix, name)
		 VALUES ($1, 'integration-usage-hash', 'air_sk_integra', 'integration-usage-key')
		 ON CONFLICT (key_hash) DO UPDATE SET name='integration-usage-key'
		 RETURNING id`,
		projectID,
	).Scan(&keyID)

	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM usage_events WHERE api_key_id=$1`, keyID)
		pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, keyID)
		pool.Exec(context.Background(), `DELETE FROM projects WHERE id=$1`, projectID)
		pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	cost := 0.00075
	logger := proxy.NewLogger(pool, nil)

	err = logger.LogDirect(ctx, proxy.UsageEvent{
		ProjectID:        projectID,
		APIKeyID:         keyID,
		Provider:         "openai",
		Model:            "gpt-4o-mini",
		PromptTokens:     100,
		CompletionTokens: 50,
		CostUSD:          &cost,
		DurationMS:       234,
		StatusCode:       200,
	})
	require.NoError(t, err)

	// Verify the row was written
	var count int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM usage_events
		 WHERE api_key_id=$1 AND model='gpt-4o-mini' AND status_code=200`,
		keyID,
	).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "usage event should be written to Postgres")
}

func TestIntegration_BudgetNoBudgetsConfigured(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Connect(ctx, requireEnv(t, "DATABASE_URL"))
	require.NoError(t, err)
	defer pool.Close()
	rdb, err := redisclient.Connect(requireEnv(t, "REDIS_URL"))
	require.NoError(t, err)
	defer rdb.Close()

	// A project with no budget rows should never be blocked.
	var userID, projectID uuid.UUID
	pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, plan)
		 VALUES ('integration-nobudget@airelay.dev', 'x', 'pro')
		 ON CONFLICT (email) DO UPDATE SET plan='pro'
		 RETURNING id`,
	).Scan(&userID)
	pool.QueryRow(ctx,
		`INSERT INTO projects (user_id, name, slug)
		 VALUES ($1, 'No Budget Project', 'integration-test-nobudget')
		 ON CONFLICT (slug) DO UPDATE SET name='No Budget Project'
		 RETURNING id`,
		userID,
	).Scan(&projectID)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM projects WHERE id=$1`, projectID)
		pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	checker := proxy.NewBudgetChecker(pool, rdb)
	result, err := checker.CheckBudgets(ctx, projectID)
	require.NoError(t, err)
	require.False(t, result.Blocked, "project with no budgets should never be blocked")
}

func TestIntegration_RecordSpendSetsTTL(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Connect(ctx, requireEnv(t, "DATABASE_URL"))
	require.NoError(t, err)
	defer pool.Close()
	rdb, err := redisclient.Connect(requireEnv(t, "REDIS_URL"))
	require.NoError(t, err)
	defer rdb.Close()

	projectID := uuid.New()
	spendKey := proxy.SpendKey(projectID, "daily", time.Now().UTC())
	rdb.Del(ctx, spendKey) // ensure clean state
	t.Cleanup(func() { rdb.Del(context.Background(), spendKey) })

	checker := proxy.NewBudgetChecker(pool, rdb)
	checker.RecordSpend(ctx, projectID, models.PeriodDaily, 0.001)

	// Key must exist with a positive TTL — never TTL=-1 (no expiry).
	ttl := rdb.TTL(ctx, spendKey).Val()
	require.Positive(t, int64(ttl), "RecordSpend must set a positive TTL on the spend key")
}

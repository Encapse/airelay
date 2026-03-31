//go:build integration

package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/db"
	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func requireEnvAPI(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("skipping integration test: %s not set", key)
	}
	return v
}

// TestIntegration_ConcurrentProjectCreate fires 5 simultaneous POST /v1/projects
// for the same free-plan user. Only 1 should succeed (HTTP 201); the rest must
// be rejected (HTTP 403). The advisory lock in projects.go prevents the TOCTOU
// race where two requests both read count=0 and both insert.
func TestIntegration_ConcurrentProjectCreate(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Connect(ctx, requireEnvAPI(t, "DATABASE_URL"))
	require.NoError(t, err)
	defer pool.Close()

	// Create a dedicated test user
	var userID uuid.UUID
	err = pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, plan)
		 VALUES ('integration-concurrent-proj@airelay.dev', 'x', 'free')
		 ON CONFLICT (email) DO UPDATE SET plan='free'
		 RETURNING id`,
	).Scan(&userID)
	require.NoError(t, err)

	t.Cleanup(func() {
		pool.Exec(context.Background(),
			`UPDATE projects SET archived_at=NOW() WHERE user_id=$1 AND archived_at IS NULL`, userID)
		pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	handler := api.NewProjectsHandler(pool)
	claims := &api.Claims{UserID: userID, Email: "integration-concurrent-proj@airelay.dev", Plan: models.PlanFree}

	const concurrency = 5
	codes := make([]int, concurrency)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := range concurrency {
		go func(i int) {
			defer wg.Done()
			body := bytes.NewBufferString(`{"name":"concurrent project"}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/projects", body)
			req = req.WithContext(api.ContextWithClaims(req.Context(), claims))
			w := httptest.NewRecorder()
			handler.Create(w, req)
			codes[i] = w.Code
		}(i)
	}
	wg.Wait()

	created := 0
	for _, code := range codes {
		if code == http.StatusCreated {
			created++
		}
	}
	require.Equal(t, 1, created,
		"exactly 1 project should be created for a free user (codes: %v)", codes)
}

// TestIntegration_ConcurrentKeyCreate fires 5 simultaneous POST .../keys
// for the same free-plan project. Only 1 should succeed (HTTP 201).
func TestIntegration_ConcurrentKeyCreate(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Connect(ctx, requireEnvAPI(t, "DATABASE_URL"))
	require.NoError(t, err)
	defer pool.Close()

	// Create test user + project
	var userID, projectID uuid.UUID
	err = pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, plan)
		 VALUES ('integration-concurrent-key@airelay.dev', 'x', 'free')
		 ON CONFLICT (email) DO UPDATE SET plan='free'
		 RETURNING id`,
	).Scan(&userID)
	require.NoError(t, err)
	err = pool.QueryRow(ctx,
		`INSERT INTO projects (user_id, name, slug)
		 VALUES ($1, 'Concurrent Key Test', 'integration-concurrent-key-proj')
		 ON CONFLICT (slug) DO UPDATE SET name='Concurrent Key Test'
		 RETURNING id`,
		userID,
	).Scan(&projectID)
	require.NoError(t, err)

	t.Cleanup(func() {
		pool.Exec(context.Background(),
			`UPDATE api_keys SET revoked_at=NOW() WHERE project_id=$1 AND revoked_at IS NULL`, projectID)
		pool.Exec(context.Background(), `DELETE FROM projects WHERE id=$1`, projectID)
		pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	handler := api.NewKeysHandler(pool)
	claims := &api.Claims{UserID: userID, Email: "integration-concurrent-key@airelay.dev", Plan: models.PlanFree}

	const concurrency = 5
	codes := make([]int, concurrency)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := range concurrency {
		go func(i int) {
			defer wg.Done()
			body := bytes.NewBufferString(`{"name":"concurrent key"}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+projectID.String()+"/keys", body)
			req.SetPathValue("id", projectID.String())
			req = req.WithContext(api.ContextWithClaims(req.Context(), claims))
			// projectFromPath verifies ownership — we need the project to belong to userID
			w := httptest.NewRecorder()
			handler.Create(w, req)
			codes[i] = w.Code
		}(i)
	}
	wg.Wait()

	created := 0
	for _, code := range codes {
		if code == http.StatusCreated {
			created++
		}
	}
	require.Equal(t, 1, created,
		"exactly 1 key should be created for a free user (codes: %v)", codes)
}

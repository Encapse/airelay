package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-jwt-secret"

func TestIssueAndValidateToken(t *testing.T) {
	userID := uuid.New()
	token, err := api.IssueToken(userID, "user@test.com", models.PlanPro, testSecret)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	// Validate via RequireAuth middleware
	called := false
	handler := api.RequireAuth(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := api.ClaimsFromContext(r.Context())
		require.NotNil(t, claims)
		require.Equal(t, userID, claims.UserID)
		require.Equal(t, models.PlanPro, claims.Plan)
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRequireAuth_Missing(t *testing.T) {
	handler := api.RequireAuth(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireAuth_BadToken(t *testing.T) {
	handler := api.RequireAuth(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLimits_Free(t *testing.T) {
	lim := api.Limits(models.PlanFree)
	require.Equal(t, 1, lim.MaxProjects)
	require.Equal(t, 1, lim.MaxKeys)
	require.Equal(t, 7, lim.HistoryDays)
}

func TestLimits_Pro(t *testing.T) {
	lim := api.Limits(models.PlanPro)
	require.Equal(t, 0, lim.MaxProjects) // 0 = unlimited
	require.Equal(t, 0, lim.MaxKeys)
	require.Equal(t, 90, lim.HistoryDays)
}

func TestLimits_Team(t *testing.T) {
	lim := api.Limits(models.PlanTeam)
	require.Equal(t, -1, lim.HistoryDays) // -1 = unlimited
}

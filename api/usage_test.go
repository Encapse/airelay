package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/stretchr/testify/require"
)

func TestUsage_ListNoClaims(t *testing.T) {
	h := api.NewUsageHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/anything/usage", nil)
	w := httptest.NewRecorder()
	h.List(w, req)
	// projectFromPath checks claims first → 401
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUsage_HistoryStart_Free(t *testing.T) {
	start := api.HistoryStart(models.PlanFree)
	require.False(t, start.IsZero())
}

func TestUsage_HistoryStart_Team(t *testing.T) {
	start := api.HistoryStart(models.PlanTeam)
	require.True(t, start.IsZero())
}

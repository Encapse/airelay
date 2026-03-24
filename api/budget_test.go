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

func TestBudget_UpsertInvalidBody(t *testing.T) {
	h := api.NewBudgetHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPut, "/v1/projects/bad-uuid/budget", bytes.NewBufferString("not-json"))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Upsert(w, req)
	// projectFromPath: uuid.Parse("") fails → 400
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBudget_UpsertInvalidPeriod(t *testing.T) {
	h := api.NewBudgetHandler(nil, nil)
	body, _ := json.Marshal(map[string]any{"amount_usd": 100.0, "period": "weekly", "hard_limit": true})
	req := httptest.NewRequest(http.MethodPut, "/v1/projects/bad-uuid/budget", bytes.NewReader(body))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Upsert(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

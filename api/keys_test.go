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

func TestKeys_ListBadProjectUUID(t *testing.T) {
	h := api.NewKeysHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/not-a-uuid/keys", nil)
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.List(w, req)
	// projectFromPath: PathValue("id") returns "" for httptest → uuid.Parse("") fails → 400
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestKeys_CreateMissingName(t *testing.T) {
	h := api.NewKeysHandler(nil)
	body, _ := json.Marshal(map[string]string{"name": ""})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/not-a-uuid/keys", bytes.NewReader(body))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Create(w, req)
	// projectFromPath: uuid.Parse("") fails → 400
	require.Equal(t, http.StatusBadRequest, w.Code)
}

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

func TestCredentials_AddInvalidProvider(t *testing.T) {
	h := api.NewCredentialsHandler(nil, "abcdefghijklmnopqrstuvwxyz123456")
	body, _ := json.Marshal(map[string]string{"provider": "badprovider", "key": "sk-test"})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/bad-uuid/credentials", bytes.NewReader(body))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Add(w, req)
	// projectFromPath: PathValue("id") = "" → uuid.Parse fails → 400
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCredentials_AddMissingKey(t *testing.T) {
	h := api.NewCredentialsHandler(nil, "abcdefghijklmnopqrstuvwxyz123456")
	body, _ := json.Marshal(map[string]string{"provider": "openai", "key": ""})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/bad-uuid/credentials", bytes.NewReader(body))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Add(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

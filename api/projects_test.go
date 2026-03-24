package api_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/stretchr/testify/require"
)

func TestProjects_ListNoClaims(t *testing.T) {
	h := api.NewProjectsHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	w := httptest.NewRecorder()
	h.List(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestProjects_CreateNoClaims(t *testing.T) {
	h := api.NewProjectsHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects",
		bytes.NewBufferString(`{"name":"test"}`))
	w := httptest.NewRecorder()
	h.Create(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestProjects_CreateMissingName(t *testing.T) {
	h := api.NewProjectsHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects",
		bytes.NewBufferString(`{"name":""}`))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Create(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestProjects_GetBadUUID(t *testing.T) {
	h := api.NewProjectsHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/not-a-uuid", nil)
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Get(w, req)
	// projectFromPath parses PathValue("id") — with httptest, PathValue won't be set
	// so uuid.Parse("") will fail → 400
	require.Equal(t, http.StatusBadRequest, w.Code)
}

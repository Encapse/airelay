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

func TestAuth_SignupBadBody(t *testing.T) {
	h := api.NewAuthHandler(nil, "secret")
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()
	h.Signup(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuth_SignupMissingEmail(t *testing.T) {
	h := api.NewAuthHandler(nil, "secret")
	body := bytes.NewBufferString(`{"email":"","password":"pass123"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", body)
	w := httptest.NewRecorder()
	h.Signup(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuth_LoginBadBody(t *testing.T) {
	h := api.NewAuthHandler(nil, "secret")
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()
	h.Login(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuth_Me_NoClaims(t *testing.T) {
	h := api.NewAuthHandler(nil, "secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	w := httptest.NewRecorder()
	h.Me(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuth_Me_WithClaims(t *testing.T) {
	h := api.NewAuthHandler(nil, "secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Me(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

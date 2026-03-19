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

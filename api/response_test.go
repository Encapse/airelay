package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"hello": "world"})
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	var out map[string]string
	json.NewDecoder(w.Body).Decode(&out)
	require.Equal(t, "world", out["hello"])
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "bad input")
	require.Equal(t, http.StatusBadRequest, w.Code)
	var out map[string]string
	json.NewDecoder(w.Body).Decode(&out)
	require.Equal(t, "bad input", out["error"])
}

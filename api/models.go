package api

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ModelsHandler returns supported model pricing. No auth required.
type ModelsHandler struct {
	db *pgxpool.Pool
}

func NewModelsHandler(db *pgxpool.Pool) *ModelsHandler {
	return &ModelsHandler{db: db}
}

// GET /v1/models
func (h *ModelsHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT provider, model, input_cost_per_1k, output_cost_per_1k, synced_from, last_synced_at
		 FROM model_pricing ORDER BY provider, model`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()
	type modelRow struct {
		Provider        string    `json:"provider"`
		Model           string    `json:"model"`
		InputCostPer1k  float64   `json:"input_cost_per_1k"`
		OutputCostPer1k float64   `json:"output_cost_per_1k"`
		SyncedFrom      string    `json:"synced_from"`
		LastSyncedAt    time.Time `json:"last_synced_at"`
	}
	var models []modelRow
	for rows.Next() {
		var m modelRow
		rows.Scan(&m.Provider, &m.Model, &m.InputCostPer1k, &m.OutputCostPer1k, &m.SyncedFrom, &m.LastSyncedAt)
		models = append(models, m)
	}
	if models == nil {
		models = []modelRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UsageHandler handles usage event queries.
type UsageHandler struct {
	db *pgxpool.Pool
}

func NewUsageHandler(db *pgxpool.Pool) *UsageHandler {
	return &UsageHandler{db: db}
}

// HistoryStart returns the earliest allowed created_at for the given plan.
// Returns zero time for unlimited plans (Team: HistoryDays == -1).
func HistoryStart(plan models.UserPlan) time.Time {
	lim := Limits(plan)
	if lim.HistoryDays < 0 {
		return time.Time{} // zero = no restriction
	}
	return time.Now().UTC().AddDate(0, 0, -lim.HistoryDays)
}

// GET /v1/projects/{id}/usage?page=1&limit=50
func (h *UsageHandler) List(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := (page - 1) * limit

	histStart := HistoryStart(claims.Plan)

	type eventRow struct {
		ID               string   `json:"id"`
		Provider         string   `json:"provider"`
		Model            string   `json:"model"`
		PromptTokens     int      `json:"prompt_tokens"`
		CompletionTokens int      `json:"completion_tokens"`
		CostUSD          *float64 `json:"cost_usd"`
		DurationMS       int      `json:"duration_ms"`
		StatusCode       int      `json:"status_code"`
		FailOpen         bool     `json:"fail_open"`
		CreatedAt        time.Time `json:"created_at"`
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT id, provider, model, prompt_tokens, completion_tokens,
		        cost_usd, duration_ms, status_code, fail_open, created_at
		 FROM usage_events
		 WHERE project_id=$1 AND created_at >= $2
		 ORDER BY created_at DESC LIMIT $3 OFFSET $4`,
		projectID, histStart, limit, offset,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()
	var events []eventRow
	for rows.Next() {
		var e eventRow
		var id uuid.UUID
		if err := rows.Scan(&id, &e.Provider, &e.Model, &e.PromptTokens,
			&e.CompletionTokens, &e.CostUSD, &e.DurationMS, &e.StatusCode,
			&e.FailOpen, &e.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		e.ID = id.String()
		events = append(events, e)
	}
	if events == nil {
		events = []eventRow{}
	}

	var total int
	h.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM usage_events WHERE project_id=$1 AND created_at >= $2`,
		projectID, histStart,
	).Scan(&total)

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"total":  total,
		"page":   page,
		"limit":  limit,
	})
}

// GET /v1/projects/{id}/usage/summary
func (h *UsageHandler) Summary(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	histStart := HistoryStart(claims.Plan)

	type modelCost struct {
		Model   string  `json:"model"`
		CostUSD float64 `json:"cost_usd"`
		Calls   int     `json:"calls"`
	}
	modelRows, err := h.db.Query(r.Context(),
		`SELECT model, COALESCE(SUM(cost_usd),0), COUNT(*)
		 FROM usage_events
		 WHERE project_id=$1 AND created_at >= $2
		 GROUP BY model ORDER BY SUM(cost_usd) DESC NULLS LAST`,
		projectID, histStart,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer modelRows.Close()
	var byModel []modelCost
	for modelRows.Next() {
		var m modelCost
		modelRows.Scan(&m.Model, &m.CostUSD, &m.Calls)
		byModel = append(byModel, m)
	}
	if byModel == nil {
		byModel = []modelCost{}
	}

	type dayCost struct {
		Date    string  `json:"date"`
		CostUSD float64 `json:"cost_usd"`
	}
	dayRows, _ := h.db.Query(r.Context(),
		`SELECT DATE(created_at) as day, COALESCE(SUM(cost_usd),0)
		 FROM usage_events
		 WHERE project_id=$1 AND created_at >= $2
		 GROUP BY day ORDER BY day DESC LIMIT 30`,
		projectID, histStart,
	)
	var byDay []dayCost
	if dayRows != nil {
		defer dayRows.Close()
		for dayRows.Next() {
			var d dayCost
			dayRows.Scan(&d.Date, &d.CostUSD)
			byDay = append(byDay, d)
		}
	}
	if byDay == nil {
		byDay = []dayCost{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"by_model": byModel,
		"by_day":   byDay,
	})
}

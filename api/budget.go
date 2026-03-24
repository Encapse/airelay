package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// BudgetHandler handles budget and alert threshold management.
type BudgetHandler struct {
	db    *pgxpool.Pool
	redis *redis.Client
}

func NewBudgetHandler(db *pgxpool.Pool, rdb *redis.Client) *BudgetHandler {
	return &BudgetHandler{db: db, redis: rdb}
}

// GET /v1/projects/{id}/budget
func (h *BudgetHandler) Get(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	type budgetResp struct {
		ID           string    `json:"id"`
		AmountUSD    float64   `json:"amount_usd"`
		Period       string    `json:"period"`
		HardLimit    bool      `json:"hard_limit"`
		CurrentSpend float64   `json:"current_spend"`
		CreatedAt    time.Time `json:"created_at"`
	}
	rows, err := h.db.Query(r.Context(),
		`SELECT id, amount_usd, period, hard_limit, created_at FROM budgets WHERE project_id=$1`,
		projectID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()
	var budgets []budgetResp
	now := time.Now().UTC()
	for rows.Next() {
		var b budgetResp
		var id uuid.UUID
		var period models.BudgetPeriod
		if err := rows.Scan(&id, &b.AmountUSD, &period, &b.HardLimit, &b.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		b.ID = id.String()
		b.Period = string(period)
		if h.redis != nil {
			key := spendKey(projectID, string(period), now)
			val, _ := h.redis.Get(r.Context(), key).Float64()
			b.CurrentSpend = val
		}
		budgets = append(budgets, b)
	}
	if budgets == nil {
		budgets = []budgetResp{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"budgets": budgets})
}

// PUT /v1/projects/{id}/budget
func (h *BudgetHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	var req struct {
		AmountUSD float64 `json:"amount_usd"`
		Period    string  `json:"period"`
		HardLimit bool    `json:"hard_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch models.BudgetPeriod(req.Period) {
	case models.PeriodDaily, models.PeriodMonthly:
	default:
		writeError(w, http.StatusBadRequest, "period must be daily or monthly")
		return
	}
	if req.AmountUSD <= 0 {
		writeError(w, http.StatusBadRequest, "amount_usd must be positive")
		return
	}
	var budgetID uuid.UUID
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO budgets (project_id, amount_usd, period, hard_limit)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (project_id, period) DO UPDATE
		   SET amount_usd=$2, hard_limit=$4
		 RETURNING id`,
		projectID, req.AmountUSD, req.Period, req.HardLimit,
	).Scan(&budgetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not upsert budget")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         budgetID.String(),
		"amount_usd": req.AmountUSD,
		"period":     req.Period,
		"hard_limit": req.HardLimit,
	})
}

// DELETE /v1/projects/{id}/budget?period=monthly
func (h *BudgetHandler) Delete(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	period := r.URL.Query().Get("period")
	if period == "" {
		h.db.Exec(r.Context(), `DELETE FROM budgets WHERE project_id=$1`, projectID)
	} else {
		h.db.Exec(r.Context(), `DELETE FROM budgets WHERE project_id=$1 AND period=$2`, projectID, period)
	}
	w.WriteHeader(http.StatusNoContent)
}

// spendKey mirrors the proxy's SpendKey format for reading Redis spend.
func spendKey(projectID uuid.UUID, period string, t time.Time) string {
	switch period {
	case "daily":
		return fmt.Sprintf("spend:%s:daily:%s", projectID, t.Format("2006-01-02"))
	case "monthly":
		return fmt.Sprintf("spend:%s:monthly:%s", projectID, t.Format("2006-01"))
	}
	return fmt.Sprintf("spend:%s:%s", projectID, period)
}

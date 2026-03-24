package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/airelay/airelay/internal/encrypt"
	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CredentialsHandler handles provider credential management.
type CredentialsHandler struct {
	db     *pgxpool.Pool
	encKey string
}

func NewCredentialsHandler(db *pgxpool.Pool, encKey string) *CredentialsHandler {
	return &CredentialsHandler{db: db, encKey: encKey}
}

// GET /v1/projects/{id}/credentials
func (h *CredentialsHandler) List(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	rows, err := h.db.Query(r.Context(),
		`SELECT id, provider, created_at, revoked_at FROM provider_credentials
		 WHERE project_id=$1 ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()
	type credRow struct {
		ID        string     `json:"id"`
		Provider  string     `json:"provider"`
		CreatedAt time.Time  `json:"created_at"`
		RevokedAt *time.Time `json:"revoked_at"`
	}
	var creds []credRow
	for rows.Next() {
		var c credRow
		var id uuid.UUID
		if err := rows.Scan(&id, &c.Provider, &c.CreatedAt, &c.RevokedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		c.ID = id.String()
		creds = append(creds, c)
	}
	if creds == nil {
		creds = []credRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": creds})
}

// POST /v1/projects/{id}/credentials
func (h *CredentialsHandler) Add(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Key      string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	switch models.AIProvider(req.Provider) {
	case models.ProviderOpenAI, models.ProviderAnthropic, models.ProviderGoogle:
	default:
		writeError(w, http.StatusBadRequest, "provider must be openai, anthropic, or google")
		return
	}
	encryptedKey, err := encrypt.Encrypt(h.encKey, req.Key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not encrypt credential")
		return
	}
	var credID uuid.UUID
	var createdAt time.Time
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO provider_credentials (project_id, provider, encrypted_key)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (project_id, provider) WHERE revoked_at IS NULL
		   DO UPDATE SET encrypted_key=$3, revoked_at=NULL
		 RETURNING id, created_at`,
		projectID, req.Provider, encryptedKey,
	).Scan(&credID, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not store credential")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         credID.String(),
		"provider":   req.Provider,
		"created_at": createdAt,
	})
}

// DELETE /v1/projects/{id}/credentials/{credId}
func (h *CredentialsHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	credID, err := uuid.Parse(r.PathValue("credId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid credential id")
		return
	}
	tag, err := h.db.Exec(r.Context(),
		`UPDATE provider_credentials SET revoked_at=NOW()
		 WHERE id=$1 AND project_id=$2 AND revoked_at IS NULL`,
		credID, projectID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "credential not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

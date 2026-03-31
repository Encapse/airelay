package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/airelay/airelay/proxy"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// KeysHandler handles API key management.
type KeysHandler struct {
	db *pgxpool.Pool
}

func NewKeysHandler(db *pgxpool.Pool) *KeysHandler {
	return &KeysHandler{db: db}
}

// GET /v1/projects/{id}/keys
func (h *KeysHandler) List(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	rows, err := h.db.Query(r.Context(),
		`SELECT id, key_prefix, name, last_used_at, revoked_at, created_at
		 FROM api_keys WHERE project_id=$1 ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()
	type keyRow struct {
		ID         string     `json:"id"`
		KeyPrefix  string     `json:"key_prefix"`
		Name       string     `json:"name"`
		LastUsedAt *time.Time `json:"last_used_at"`
		RevokedAt  *time.Time `json:"revoked_at"`
		CreatedAt  time.Time  `json:"created_at"`
	}
	var keys []keyRow
	for rows.Next() {
		var k keyRow
		var id uuid.UUID
		if err := rows.Scan(&id, &k.KeyPrefix, &k.Name, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		k.ID = id.String()
		keys = append(keys, k)
	}
	if keys == nil {
		keys = []keyRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// POST /v1/projects/{id}/keys
func (h *KeysHandler) Create(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	claims := ClaimsFromContext(r.Context())
	lim := Limits(claims.Plan)

	// Wrap count-check + insert in a transaction with a per-project advisory lock.
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	if lim.MaxKeys > 0 {
		// Lock on project ID to serialise concurrent key creates for the same project.
		if _, err := tx.Exec(r.Context(),
			`SELECT pg_advisory_xact_lock(hashtext($1::text)::bigint)`, projectID.String()); err != nil {
			writeError(w, http.StatusInternalServerError, "could not acquire lock")
			return
		}
		var count int
		tx.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM api_keys WHERE project_id=$1 AND revoked_at IS NULL`,
			projectID,
		).Scan(&count)
		if count >= lim.MaxKeys {
			writeError(w, http.StatusForbidden,
				fmt.Sprintf("free plan limited to %d key per project — upgrade to Pro for unlimited", lim.MaxKeys))
			return
		}
	}

	fullKey, prefix, keyHash := proxy.GenerateKey()
	var keyID uuid.UUID
	var createdAt time.Time
	err = tx.QueryRow(r.Context(),
		`INSERT INTO api_keys (project_id, key_hash, key_prefix, name)
		 VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
		projectID, keyHash, prefix, req.Name,
	).Scan(&keyID, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create key")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not commit")
		return
	}
	// Full key returned once only — never stored in plaintext
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         keyID.String(),
		"key":        fullKey,
		"key_prefix": prefix,
		"name":       req.Name,
		"created_at": createdAt,
	})
}

// DELETE /v1/projects/{id}/keys/{keyId}
func (h *KeysHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	keyID, err := uuid.Parse(r.PathValue("keyId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}
	tag, err := h.db.Exec(r.Context(),
		`UPDATE api_keys SET revoked_at=NOW()
		 WHERE id=$1 AND project_id=$2 AND revoked_at IS NULL`,
		keyID, projectID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

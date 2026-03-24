package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProjectsHandler handles project CRUD.
type ProjectsHandler struct {
	db *pgxpool.Pool
}

func NewProjectsHandler(db *pgxpool.Pool) *ProjectsHandler {
	return &ProjectsHandler{db: db}
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// generateSlug converts a name to a URL-safe slug with a short UUID suffix.
func generateSlug(name string) string {
	slug := nonAlphanumeric.ReplaceAllString(strings.ToLower(name), "-")
	slug = strings.Trim(slug, "-")
	suffix := strings.ReplaceAll(uuid.New().String(), "-", "")[:6]
	return slug + "-" + suffix
}

// projectFromPath parses the {id} path value and verifies the authenticated user owns the project.
// Returns (projectID, true) on success, writes error and returns (zero, false) on failure.
func projectFromPath(db *pgxpool.Pool, w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return uuid.Nil, false
	}
	idStr := r.PathValue("id")
	projectID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return uuid.Nil, false
	}
	var exists bool
	db.QueryRow(r.Context(),
		`SELECT true FROM projects WHERE id=$1 AND user_id=$2 AND archived_at IS NULL`,
		projectID, claims.UserID,
	).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "project not found")
		return uuid.Nil, false
	}
	return projectID, true
}

// GET /v1/projects
func (h *ProjectsHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	rows, err := h.db.Query(r.Context(),
		`SELECT id, name, slug, created_at FROM projects
		 WHERE user_id=$1 AND archived_at IS NULL ORDER BY created_at DESC`,
		claims.UserID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()
	type projectRow struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		Slug      string    `json:"slug"`
		CreatedAt time.Time `json:"created_at"`
	}
	var projects []projectRow
	for rows.Next() {
		var p projectRow
		var id uuid.UUID
		if err := rows.Scan(&id, &p.Name, &p.Slug, &p.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		p.ID = id.String()
		projects = append(projects, p)
	}
	if projects == nil {
		projects = []projectRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

// POST /v1/projects
func (h *ProjectsHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Plan limit check
	lim := Limits(claims.Plan)
	if lim.MaxProjects > 0 {
		var count int
		h.db.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM projects WHERE user_id=$1 AND archived_at IS NULL`,
			claims.UserID,
		).Scan(&count)
		if count >= lim.MaxProjects {
			writeError(w, http.StatusForbidden,
				"free plan limited to 1 project — upgrade to Pro for unlimited")
			return
		}
	}

	slug := generateSlug(req.Name)
	var projectID uuid.UUID
	var createdAt time.Time
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO projects (user_id, name, slug) VALUES ($1,$2,$3) RETURNING id, created_at`,
		claims.UserID, req.Name, slug,
	).Scan(&projectID, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create project")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         projectID.String(),
		"name":       req.Name,
		"slug":       slug,
		"created_at": createdAt,
	})
}

// GET /v1/projects/{id}
func (h *ProjectsHandler) Get(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	var name, slug string
	var createdAt time.Time
	err := h.db.QueryRow(r.Context(),
		`SELECT name, slug, created_at FROM projects WHERE id=$1`,
		projectID,
	).Scan(&name, &slug, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         projectID.String(),
		"name":       name,
		"slug":       slug,
		"created_at": createdAt,
	})
}

// DELETE /v1/projects/{id}
func (h *ProjectsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	projectID, ok := projectFromPath(h.db, w, r)
	if !ok {
		return
	}
	h.db.Exec(r.Context(),
		`UPDATE projects SET archived_at=NOW() WHERE id=$1`,
		projectID,
	)
	w.WriteHeader(http.StatusNoContent)
}

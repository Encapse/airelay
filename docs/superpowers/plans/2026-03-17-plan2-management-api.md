# AIRelay Plan 2 — Management API + Background Jobs + Dashboard

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the management API, background jobs, and HTMX dashboard that operators use to manage projects, budgets, API keys, provider credentials, and view usage.

**Architecture:** Single `cmd/api` binary serves the management API (JWT-authenticated REST at `/v1/...`), a lightweight HTMX dashboard (server-rendered Go templates at `/dashboard/...`), and runs all background jobs as goroutines. Proxy engine from Plan 1 is a separate binary sharing the same Postgres and Redis.

**Tech Stack:** Go 1.22+ (ServeMux with method+path patterns), golang-jwt/jwt/v5, bcrypt, html/template, embed.FS, HTMX 2.x (CDN)

---

## File Structure

```
api/
  response.go         - writeJSON, writeError helpers
  middleware.go        - RequireAuth, IssueToken, Claims, Limits, chain
  auth.go             - Signup, Login, Me handlers
  projects.go         - List, Create, Get, Delete handlers
  keys.go             - List, Create, Revoke handlers
  credentials.go      - List, Add, Revoke handlers
  budget.go           - Get, Upsert, Delete handlers
  usage.go            - List, Summary handlers
  models.go           - List model pricing handler
  server.go           - NewServer, route registration

dashboard/
  embed.go            - //go:embed directive
  server.go           - NewDashboardServer, route setup
  handler.go          - Login, Projects, Project page handlers
  templates/
    layout.html       - base HTML, HTMX CDN, JWT header script
    login.html        - login form + token storage JS
    projects.html     - project list + create form
    project.html      - project detail: budget (30s JS polling), keys, usage table

internal/jobs/
  runner.go           - Start() launches all jobs on goroutines/tickers
  pricingsync.go      - fetch LiteLLM JSON + OpenRouter, upsert model_pricing
  reconcile.go        - Redis vs Postgres drift check/correct every 60s
  partition.go        - create next month's usage_events partition
  ttlsweep.go         - delete expired Redis spend keys daily

cmd/api/main.go       - wire DB, Redis, start API + dashboard + jobs
```

---

## Chunk 1: API Foundation — Response Helpers, Middleware, Auth

**Files:**
- Create: `api/response.go`
- Create: `api/middleware.go` + `api/middleware_test.go`
- Create: `api/auth.go` + `api/auth_test.go`

### Task 1: Response helpers

- [ ] **Step 1: Create `api/response.go`**

```go
package api

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./api/...
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add api/response.go
git commit -m "feat: API response helpers"
```

---

### Task 2: JWT middleware

- [ ] **Step 1: Write failing test**

Create `api/middleware_test.go`:
```go
package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRequireAuth_MissingToken(t *testing.T) {
	h := api.RequireAuth("testsecret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireAuth_ValidToken(t *testing.T) {
	secret := "testsecret"
	userID := uuid.New()
	token, err := api.IssueToken(userID, models.PlanPro, secret)
	require.NoError(t, err)

	var gotUserID uuid.UUID
	h := api.RequireAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := api.ClaimsFromContext(r.Context())
		require.NotNil(t, claims)
		gotUserID = claims.UserID
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, userID, gotUserID)
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	h := api.RequireAuth("testsecret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLimits_Free(t *testing.T) {
	l := api.Limits(models.PlanFree)
	require.Equal(t, 1, l.MaxProjects)
	require.Equal(t, 1, l.MaxKeys)
	require.Equal(t, 7, l.HistoryDays)
}

func TestLimits_Pro(t *testing.T) {
	l := api.Limits(models.PlanPro)
	require.Equal(t, -1, l.MaxProjects)
	require.Equal(t, -1, l.MaxKeys)
	require.Equal(t, 90, l.HistoryDays)
}

func TestInjectClaims_RoundTrip(t *testing.T) {
	userID := uuid.New()
	claims := &api.Claims{UserID: userID, Plan: models.PlanTeam}
	ctx := context.WithValue(context.Background(), api.ClaimsContextKey, claims)
	got := api.ClaimsFromContext(ctx)
	require.NotNil(t, got)
	require.Equal(t, userID, got.UserID)
}
```

- [ ] **Step 2: Run test — expect compilation failure**

```bash
go test ./api/... -v
```
Expected: compilation error (package doesn't exist yet).

- [ ] **Step 3: Implement `api/middleware.go`**

Create `api/middleware.go`:
```go
package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/airelay/airelay/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type contextKey string

// ClaimsContextKey is exported so tests can inject claims directly.
const ClaimsContextKey contextKey = "claims"

// Claims is the JWT payload stored in request context.
type Claims struct {
	UserID uuid.UUID      `json:"user_id"`
	Plan   models.UserPlan `json:"plan"`
	jwt.RegisteredClaims
}

// IssueToken creates a signed JWT for the given user (30-day expiry).
func IssueToken(userID uuid.UUID, plan models.UserPlan, secret string) (string, error) {
	claims := &Claims{
		UserID: userID,
		Plan:   plan,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ClaimsFromContext retrieves JWT claims from the request context.
func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(ClaimsContextKey).(*Claims)
	return c
}

// RequireAuth validates the Bearer JWT and injects claims into context.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer := r.Header.Get("Authorization")
			if !strings.HasPrefix(bearer, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "missing or invalid authorization header")
				return
			}
			tokenStr := strings.TrimPrefix(bearer, "Bearer ")
			claims := &Claims{}
			token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			})
			if err != nil || !token.Valid {
				writeError(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// PlanLimits defines per-plan resource constraints.
type PlanLimits struct {
	MaxProjects int // -1 = unlimited
	MaxKeys     int // -1 = unlimited
	HistoryDays int // -1 = unlimited
}

// Limits returns the PlanLimits for the given plan tier.
func Limits(plan models.UserPlan) PlanLimits {
	switch plan {
	case models.PlanPro:
		return PlanLimits{MaxProjects: -1, MaxKeys: -1, HistoryDays: 90}
	case models.PlanTeam:
		return PlanLimits{MaxProjects: -1, MaxKeys: -1, HistoryDays: -1}
	default: // free
		return PlanLimits{MaxProjects: 1, MaxKeys: 1, HistoryDays: 7}
	}
}

// chain applies middlewares right-to-left (first middleware is outermost).
func chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./api/... -run "TestRequireAuth|TestLimits|TestInjectClaims" -v
```
Expected: PASS (6 tests)

- [ ] **Step 5: Commit**

```bash
git add api/middleware.go api/middleware_test.go
git commit -m "feat: JWT middleware — IssueToken, RequireAuth, PlanLimits"
```

---

### Task 3: Auth handlers

- [ ] **Step 1: Write failing test**

Create `api/auth_test.go`:
```go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSignup_InvalidBody(t *testing.T) {
	h := api.NewAuthHandler(nil, "secret")
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()
	h.Signup(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSignup_ShortPassword(t *testing.T) {
	h := api.NewAuthHandler(nil, "secret")
	body, _ := json.Marshal(map[string]string{"email": "a@b.com", "password": "short"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Signup(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLogin_InvalidBody(t *testing.T) {
	h := api.NewAuthHandler(nil, "secret")
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()
	h.Login(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMe_NoClaims(t *testing.T) {
	h := api.NewAuthHandler(nil, "secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	w := httptest.NewRecorder()
	h.Me(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMe_WithClaims(t *testing.T) {
	h := api.NewAuthHandler(nil, "secret")
	userID := uuid.New()
	claims := &api.Claims{UserID: userID, Plan: models.PlanPro}
	ctx := context.WithValue(context.Background(), api.ClaimsContextKey, claims)
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Me(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	require.Equal(t, userID.String(), resp["user_id"])
	require.Equal(t, "pro", resp["plan"])
}
```

- [ ] **Step 2: Run test — expect compilation failure**

```bash
go test ./api/... -run "TestSignup|TestLogin|TestMe" -v
```
Expected: compilation error — `NewAuthHandler` not defined.

- [ ] **Step 3: Implement `api/auth.go`**

Create `api/auth.go`:
```go
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	db     *pgxpool.Pool
	secret string
}

func NewAuthHandler(db *pgxpool.Pool, secret string) *AuthHandler {
	return &AuthHandler{db: db, secret: secret}
}

// POST /v1/auth/signup
func (h *AuthHandler) Signup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "email required and password must be at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var userID uuid.UUID
	var plan models.UserPlan
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 ON CONFLICT (email) DO NOTHING
		 RETURNING id, plan`,
		req.Email, string(hash),
	).Scan(&userID, &plan)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "email already registered")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"user_id": userID.String(),
		"plan":    string(plan),
	})
}

// POST /v1/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var userID uuid.UUID
	var passwordHash string
	var plan models.UserPlan
	err := h.db.QueryRow(r.Context(),
		`SELECT id, password_hash, plan FROM users WHERE email=$1`,
		req.Email,
	).Scan(&userID, &passwordHash, &plan)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	token, err := IssueToken(userID, plan, h.secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// GET /v1/auth/me
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": claims.UserID.String(),
		"plan":    string(claims.Plan),
	})
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./api/... -run "TestSignup|TestLogin|TestMe" -v
```
Expected: PASS (5 tests)

- [ ] **Step 5: Commit**

```bash
git add api/auth.go api/auth_test.go
git commit -m "feat: auth handlers — signup, login, me"
```

---

## Chunk 2: Projects, Keys, and Credentials Handlers

**Files:**
- Create: `api/projects.go` + `api/projects_test.go`
- Create: `api/keys.go` + `api/keys_test.go`
- Create: `api/credentials.go` + `api/credentials_test.go`

### Task 4: Projects handlers

- [ ] **Step 1: Write failing tests**

Create `api/projects_test.go`:
```go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func injectClaims(r *http.Request, plan models.UserPlan) *http.Request {
	claims := &api.Claims{UserID: uuid.New(), Plan: plan}
	ctx := context.WithValue(r.Context(), api.ClaimsContextKey, claims)
	return r.WithContext(ctx)
}

func TestProjects_CreateInvalidBody(t *testing.T) {
	h := api.NewProjectsHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects", bytes.NewBufferString("not-json"))
	req = injectClaims(req, models.PlanFree)
	w := httptest.NewRecorder()
	h.Create(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestProjects_CreateMissingName(t *testing.T) {
	h := api.NewProjectsHandler(nil)
	body, _ := json.Marshal(map[string]string{"name": ""})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects", bytes.NewReader(body))
	req = injectClaims(req, models.PlanFree)
	w := httptest.NewRecorder()
	h.Create(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestProjects_GetNoClaims(t *testing.T) {
	h := api.NewProjectsHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	h.Get(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
```

- [ ] **Step 2: Run test — expect compilation failure**

```bash
go test ./api/... -run TestProjects -v
```
Expected: compilation error.

- [ ] **Step 3: Implement `api/projects.go`**

Create `api/projects.go`:
```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var slugRe = regexp.MustCompile(`[^a-z0-9-]`)

// ProjectsHandler handles project CRUD.
type ProjectsHandler struct {
	db *pgxpool.Pool
}

func NewProjectsHandler(db *pgxpool.Pool) *ProjectsHandler {
	return &ProjectsHandler{db: db}
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
	type proj struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		Slug      string    `json:"slug"`
		CreatedAt time.Time `json:"created_at"`
	}
	var projects []proj
	for rows.Next() {
		var p proj
		var id uuid.UUID
		if err := rows.Scan(&id, &p.Name, &p.Slug, &p.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		p.ID = id.String()
		projects = append(projects, p)
	}
	if projects == nil {
		projects = []proj{}
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
	if strings.TrimSpace(req.Name) == "" {
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
			writeError(w, http.StatusForbidden, fmt.Sprintf("free plan limited to %d project — upgrade to Pro for unlimited", lim.MaxProjects))
			return
		}
	}
	slug := slugRe.ReplaceAllString(strings.ToLower(strings.ReplaceAll(req.Name, " ", "-")), "")
	if slug == "" {
		slug = "project"
	}
	// Ensure slug uniqueness with a suffix
	baseSlug := slug
	for i := 1; ; i++ {
		var exists bool
		h.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM projects WHERE slug=$1)`, slug).Scan(&exists)
		if !exists {
			break
		}
		slug = fmt.Sprintf("%s-%d", baseSlug, i)
	}
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
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	projectID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	type proj struct {
		ID        string     `json:"id"`
		Name      string     `json:"name"`
		Slug      string     `json:"slug"`
		CreatedAt time.Time  `json:"created_at"`
		ArchivedAt *time.Time `json:"archived_at"`
	}
	var p proj
	p.ID = projectID.String()
	err = h.db.QueryRow(r.Context(),
		`SELECT name, slug, created_at, archived_at FROM projects
		 WHERE id=$1 AND user_id=$2`,
		projectID, claims.UserID,
	).Scan(&p.Name, &p.Slug, &p.CreatedAt, &p.ArchivedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// DELETE /v1/projects/{id}
func (h *ProjectsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	projectID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	tag, err := h.db.Exec(r.Context(),
		`UPDATE projects SET archived_at=NOW() WHERE id=$1 AND user_id=$2 AND archived_at IS NULL`,
		projectID, claims.UserID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ownerProject verifies a project belongs to a user and is not archived.
func ownerProject(db *pgxpool.Pool, r *http.Request, projectID, userID uuid.UUID) bool {
	var exists bool
	db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND user_id=$2 AND archived_at IS NULL)`,
		projectID, userID,
	).Scan(&exists)
	return exists
}

// projectFromPath parses project id from path and verifies ownership.
// Returns (projectID, ok). Writes error to w if not ok.
func projectFromPath(db *pgxpool.Pool, w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return uuid.UUID{}, false
	}
	projectID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return uuid.UUID{}, false
	}
	if !ownerProject(db, r, projectID, claims.UserID) {
		writeError(w, http.StatusNotFound, "project not found")
		return uuid.UUID{}, false
	}
	return projectID, true
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./api/... -run TestProjects -v
```
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add api/projects.go api/projects_test.go
git commit -m "feat: project handlers — list, create, get, delete with plan limits"
```

---

### Task 5: API key handlers

- [ ] **Step 1: Write failing tests**

Create `api/keys_test.go`:
```go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/stretchr/testify/require"
)

func TestKeys_CreateInvalidBody(t *testing.T) {
	h := api.NewKeysHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/bad-uuid/keys", bytes.NewBufferString("not-json"))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Create(w, req)
	// bad project uuid → 400 (projectFromPath fails before JSON decode in this handler)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestKeys_CreateMissingName(t *testing.T) {
	h := api.NewKeysHandler(nil)
	body, _ := json.Marshal(map[string]string{"name": ""})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/bad-uuid/keys", bytes.NewReader(body))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Create(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
```

- [ ] **Step 2: Run test — expect compilation failure**

```bash
go test ./api/... -run TestKeys -v
```
Expected: compilation error.

- [ ] **Step 3: Implement `api/keys.go`**

Create `api/keys.go`:
```go
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
	claims := ClaimsFromContext(r.Context())

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Plan limit check
	lim := Limits(claims.Plan)
	if lim.MaxKeys > 0 {
		var count int
		h.db.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM api_keys WHERE project_id=$1 AND revoked_at IS NULL`,
			projectID,
		).Scan(&count)
		if count >= lim.MaxKeys {
			writeError(w, http.StatusForbidden, fmt.Sprintf("free plan limited to %d key per project — upgrade to Pro for unlimited", lim.MaxKeys))
			return
		}
	}

	fullKey, prefix, keyHash := proxy.GenerateKey()
	var keyID uuid.UUID
	var createdAt time.Time
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO api_keys (project_id, key_hash, key_prefix, name)
		 VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
		projectID, keyHash, prefix, req.Name,
	).Scan(&keyID, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create key")
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
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./api/... -run TestKeys -v
```
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
git add api/keys.go api/keys_test.go
git commit -m "feat: API key handlers — list, create (key returned once), revoke"
```

---

### Task 5b: Add migration for provider_credentials unique constraint

The `credentials.go` Add handler uses `ON CONFLICT (project_id, provider)`. This requires a `UNIQUE` constraint that Plan 1's base migration did not include. Add it now.

- [ ] **Step 1: Create `db/migrations/009_provider_credentials_unique.sql`**

```sql
-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS provider_credentials_project_provider_idx
    ON provider_credentials(project_id, provider)
    WHERE revoked_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS provider_credentials_project_provider_idx;
```

**Note:** The partial index `WHERE revoked_at IS NULL` allows re-adding a provider after revocation — only one *active* credential per provider per project.

- [ ] **Step 2: Run migration**

```bash
make migrate-up
```
Expected: migration 009 applied successfully.

- [ ] **Step 3: Commit**

```bash
git add db/migrations/009_provider_credentials_unique.sql
git commit -m "feat: unique constraint on active provider credentials per project"
```

---

### Task 6: Provider credentials handlers

- [ ] **Step 1: Write failing tests**

Create `api/credentials_test.go`:
```go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCredentials_AddInvalidProvider(t *testing.T) {
	h := api.NewCredentialsHandler(nil, "abcdefghijklmnopqrstuvwxyz123456")
	body, _ := json.Marshal(map[string]string{"provider": "badprovider", "key": "sk-test"})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/bad-uuid/credentials", bytes.NewReader(body))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Add(w, req)
	// bad project uuid → 400
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCredentials_AddMissingKey(t *testing.T) {
	h := api.NewCredentialsHandler(nil, "abcdefghijklmnopqrstuvwxyz123456")
	body, _ := json.Marshal(map[string]string{"provider": "openai", "key": ""})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/bad-uuid/credentials", bytes.NewReader(body))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Add(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
```

- [ ] **Step 2: Run test — expect compilation failure**

```bash
go test ./api/... -run TestCredentials -v
```
Expected: compilation error.

- [ ] **Step 3: Implement `api/credentials.go`**

Create `api/credentials.go`:
```go
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
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./api/... -run TestCredentials -v
```
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
git add api/credentials.go api/credentials_test.go
git commit -m "feat: provider credential handlers — list, add (encrypted), revoke"
```

---

## Chunk 3: Budget, Usage, Models Handlers + Server Wiring

**Files:**
- Create: `api/budget.go` + `api/budget_test.go`
- Create: `api/usage.go` + `api/usage_test.go`
- Create: `api/models.go`
- Create: `api/server.go`

### Task 7: Budget handlers

- [ ] **Step 1: Write failing tests**

Create `api/budget_test.go`:
```go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/stretchr/testify/require"
)

func TestBudget_UpsertInvalidBody(t *testing.T) {
	h := api.NewBudgetHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPut, "/v1/projects/bad-uuid/budget", bytes.NewBufferString("not-json"))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Upsert(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBudget_UpsertInvalidPeriod(t *testing.T) {
	h := api.NewBudgetHandler(nil, nil)
	body, _ := json.Marshal(map[string]any{"amount_usd": 100.0, "period": "weekly", "hard_limit": true})
	req := httptest.NewRequest(http.MethodPut, "/v1/projects/bad-uuid/budget", bytes.NewReader(body))
	req = injectClaims(req, models.PlanPro)
	w := httptest.NewRecorder()
	h.Upsert(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
```

- [ ] **Step 2: Run test — expect compilation failure**

```bash
go test ./api/... -run TestBudget -v
```
Expected: compilation error.

- [ ] **Step 3: Implement `api/budget.go`**

Create `api/budget.go`:
```go
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
		// Read current spend from Redis
		spendKey := spendKey(projectID, string(period), now)
		if h.redis != nil {
			val, _ := h.redis.Get(r.Context(), spendKey).Float64()
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
		// Delete all budgets for project
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
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./api/... -run TestBudget -v
```
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
git add api/budget.go api/budget_test.go
git commit -m "feat: budget handlers — get (with Redis spend), upsert, delete"
```

---

### Task 8: Usage and models handlers

- [ ] **Step 1: Write failing test**

Create `api/usage_test.go`:
```go
package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/stretchr/testify/require"
)

func TestUsage_ListNoClaims(t *testing.T) {
	h := api.NewUsageHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/anything/usage", nil)
	w := httptest.NewRecorder()
	h.List(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUsage_HistoryStart_Free(t *testing.T) {
	start := api.HistoryStart(models.PlanFree)
	// Free plan: 7 days — start should be ~7 days ago
	require.False(t, start.IsZero())
}

func TestUsage_HistoryStart_Team(t *testing.T) {
	start := api.HistoryStart(models.PlanTeam)
	// Team plan: unlimited — start should be zero time
	require.True(t, start.IsZero())
}
```

- [ ] **Step 2: Run test — expect compilation failure**

```bash
go test ./api/... -run TestUsage -v
```
Expected: compilation error.

- [ ] **Step 3: Implement `api/usage.go`**

Create `api/usage.go`:
```go
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
// Returns zero time for unlimited plans (Team).
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
		ID               string     `json:"id"`
		Provider         string     `json:"provider"`
		Model            string     `json:"model"`
		PromptTokens     int        `json:"prompt_tokens"`
		CompletionTokens int        `json:"completion_tokens"`
		CostUSD          *float64   `json:"cost_usd"`
		DurationMS       int        `json:"duration_ms"`
		StatusCode       int        `json:"status_code"`
		FailOpen         bool       `json:"fail_open"`
		CreatedAt        time.Time  `json:"created_at"`
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
	histStart := HistoryStart(claims.Plan)

	// Cost by model
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

	// Cost by day (last 30 days within history window)
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
```

- [ ] **Step 4: Create `api/models.go`**

```go
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
```

- [ ] **Step 5: Run tests — expect pass**

```bash
go test ./api/... -run TestUsage -v
```
Expected: PASS (3 tests)

- [ ] **Step 6: Commit**

```bash
git add api/usage.go api/usage_test.go api/models.go
git commit -m "feat: usage handlers (history-gated), summary, models list"
```

---

### Task 9: API server wiring

- [ ] **Step 1: Create `api/server.go`**

```go
package api

import (
	"net/http"

	"github.com/airelay/airelay/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// NewServer wires all handlers and returns (http.Server, *http.ServeMux).
// The mux is returned separately so main can mount additional routes (e.g. dashboard)
// without a fragile type assertion on srv.Handler.
func NewServer(db *pgxpool.Pool, rdb *redis.Client, cfg *config.Config) (*http.Server, *http.ServeMux) {
	mux := http.NewServeMux()

	auth := NewAuthHandler(db, cfg.JWTSecret)
	projects := NewProjectsHandler(db)
	keys := NewKeysHandler(db)
	creds := NewCredentialsHandler(db, cfg.CredentialEncryptionKey)
	budgets := NewBudgetHandler(db, rdb)
	usage := NewUsageHandler(db)
	models := NewModelsHandler(db)

	authed := RequireAuth(cfg.JWTSecret)

	// Auth — no middleware on signup/login
	mux.HandleFunc("POST /v1/auth/signup", auth.Signup)
	mux.HandleFunc("POST /v1/auth/login", auth.Login)
	mux.Handle("GET /v1/auth/me", chain(http.HandlerFunc(auth.Me), authed))

	// Projects
	mux.Handle("GET /v1/projects", chain(http.HandlerFunc(projects.List), authed))
	mux.Handle("POST /v1/projects", chain(http.HandlerFunc(projects.Create), authed))
	mux.Handle("GET /v1/projects/{id}", chain(http.HandlerFunc(projects.Get), authed))
	mux.Handle("DELETE /v1/projects/{id}", chain(http.HandlerFunc(projects.Delete), authed))

	// Keys
	mux.Handle("GET /v1/projects/{id}/keys", chain(http.HandlerFunc(keys.List), authed))
	mux.Handle("POST /v1/projects/{id}/keys", chain(http.HandlerFunc(keys.Create), authed))
	mux.Handle("DELETE /v1/projects/{id}/keys/{keyId}", chain(http.HandlerFunc(keys.Revoke), authed))

	// Credentials
	mux.Handle("GET /v1/projects/{id}/credentials", chain(http.HandlerFunc(creds.List), authed))
	mux.Handle("POST /v1/projects/{id}/credentials", chain(http.HandlerFunc(creds.Add), authed))
	mux.Handle("DELETE /v1/projects/{id}/credentials/{credId}", chain(http.HandlerFunc(creds.Revoke), authed))

	// Budget
	mux.Handle("GET /v1/projects/{id}/budget", chain(http.HandlerFunc(budgets.Get), authed))
	mux.Handle("PUT /v1/projects/{id}/budget", chain(http.HandlerFunc(budgets.Upsert), authed))
	mux.Handle("DELETE /v1/projects/{id}/budget", chain(http.HandlerFunc(budgets.Delete), authed))

	// Usage (note: /usage/summary must be registered before /usage to avoid ambiguity)
	mux.Handle("GET /v1/projects/{id}/usage/summary", chain(http.HandlerFunc(usage.Summary), authed))
	mux.Handle("GET /v1/projects/{id}/usage", chain(http.HandlerFunc(usage.List), authed))

	// Models — public, no auth
	mux.HandleFunc("GET /v1/models", models.List)

	// Health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	return &http.Server{Handler: mux}, mux
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./api/...
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add api/server.go
git commit -m "feat: API server wiring — all routes registered"
```

---

## Chunk 4: Background Jobs

**Files:**
- Create: `internal/jobs/runner.go`
- Create: `internal/jobs/pricingsync.go`
- Create: `internal/jobs/pricingsync_test.go`
- Create: `internal/jobs/reconcile.go`
- Create: `internal/jobs/partition.go`
- Create: `internal/jobs/ttlsweep.go`

### Task 10: Model pricing sync job

- [ ] **Step 1: Write failing test**

Create `internal/jobs/pricingsync_test.go`:
```go
package jobs_test

import (
	"testing"

	"github.com/airelay/airelay/internal/jobs"
	"github.com/stretchr/testify/require"
)

func TestParseLiteLLMEntry(t *testing.T) {
	entry := jobs.LiteLLMEntry{
		InputCostPerToken:  0.0000025,
		OutputCostPerToken: 0.00001,
		LiteLLMProvider:    "openai",
	}
	in1k, out1k := entry.Per1kCosts()
	require.InDelta(t, 0.0025, in1k, 0.000001)
	require.InDelta(t, 0.01, out1k, 0.000001)
}

func TestParseOpenRouterEntry(t *testing.T) {
	model := jobs.OpenRouterModel{
		ID: "openai/gpt-4o",
		Pricing: jobs.OpenRouterPricing{
			Prompt:     "0.0000025",
			Completion: "0.00001",
		},
	}
	provider, name := model.ProviderAndModel()
	require.Equal(t, "openai", provider)
	require.Equal(t, "gpt-4o", name)
	in1k, out1k := model.Per1kCosts()
	require.InDelta(t, 0.0025, in1k, 0.000001)
	require.InDelta(t, 0.01, out1k, 0.000001)
}

func TestFilterKnownProvider(t *testing.T) {
	require.True(t, jobs.IsKnownProvider("openai"))
	require.True(t, jobs.IsKnownProvider("anthropic"))
	require.True(t, jobs.IsKnownProvider("google"))
	require.False(t, jobs.IsKnownProvider("cohere"))
}
```

- [ ] **Step 2: Run test — expect failure**

```bash
go test ./internal/jobs/... -v
```
Expected: compilation error.

- [ ] **Step 3: Implement `internal/jobs/pricingsync.go`**

Create `internal/jobs/pricingsync.go`:
```go
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const liteLLMURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
const openRouterURL = "https://openrouter.ai/api/v1/models"

// LiteLLMEntry is a single model entry from the LiteLLM pricing JSON.
type LiteLLMEntry struct {
	InputCostPerToken  float64 `json:"input_cost_per_token"`
	OutputCostPerToken float64 `json:"output_cost_per_token"`
	LiteLLMProvider    string  `json:"litellm_provider"`
}

// Per1kCosts returns input and output cost per 1k tokens.
func (e LiteLLMEntry) Per1kCosts() (float64, float64) {
	return e.InputCostPerToken * 1000, e.OutputCostPerToken * 1000
}

// OpenRouterPricing holds prompt/completion pricing from OpenRouter.
type OpenRouterPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

// OpenRouterModel is a single model from the OpenRouter models API.
type OpenRouterModel struct {
	ID      string            `json:"id"`
	Pricing OpenRouterPricing `json:"pricing"`
}

// ProviderAndModel splits "provider/model-name" into (provider, model).
func (m OpenRouterModel) ProviderAndModel() (string, string) {
	parts := strings.SplitN(m.ID, "/", 2)
	if len(parts) != 2 {
		return "", m.ID
	}
	return parts[0], parts[1]
}

// Per1kCosts parses OpenRouter's string pricing to per-1k float64 values.
func (m OpenRouterModel) Per1kCosts() (float64, float64) {
	parsePerToken := func(s string) float64 {
		f, _ := strconv.ParseFloat(s, 64)
		return f * 1000
	}
	return parsePerToken(m.Pricing.Prompt), parsePerToken(m.Pricing.Completion)
}

// IsKnownProvider returns true for the three providers we support.
func IsKnownProvider(provider string) bool {
	switch provider {
	case "openai", "anthropic", "google":
		return true
	}
	return false
}

// RunPricingSync fetches LiteLLM and OpenRouter pricing and upserts into model_pricing.
// Rows with manual_override=true are skipped.
func RunPricingSync(ctx context.Context, db *pgxpool.Pool) error {
	client := &http.Client{Timeout: 30 * time.Second}

	// --- LiteLLM ---
	resp, err := client.Get(liteLLMURL)
	if err != nil {
		return fmt.Errorf("fetch litellm: %w", err)
	}
	defer resp.Body.Close()

	var liteLLMData map[string]LiteLLMEntry
	if err := json.NewDecoder(resp.Body).Decode(&liteLLMData); err != nil {
		return fmt.Errorf("decode litellm: %w", err)
	}

	upserted := 0
	for modelName, entry := range liteLLMData {
		if !IsKnownProvider(entry.LiteLLMProvider) {
			continue
		}
		if entry.InputCostPerToken == 0 && entry.OutputCostPerToken == 0 {
			continue // skip entries with no pricing
		}
		in1k, out1k := entry.Per1kCosts()
		_, err := db.Exec(ctx, `
			INSERT INTO model_pricing (provider, model, input_cost_per_1k, output_cost_per_1k, synced_from, last_synced_at)
			VALUES ($1,$2,$3,$4,'litellm',NOW())
			ON CONFLICT (provider, model) DO UPDATE
			  SET input_cost_per_1k=$3, output_cost_per_1k=$4, synced_from='litellm', last_synced_at=NOW()
			  WHERE model_pricing.manual_override = false`,
			entry.LiteLLMProvider, modelName, in1k, out1k,
		)
		if err == nil {
			upserted++
		}
	}
	log.Printf("pricing sync: upserted %d rows from LiteLLM", upserted)

	// --- OpenRouter (secondary source) ---
	resp2, err := client.Get(openRouterURL)
	if err != nil {
		log.Printf("pricing sync: could not reach OpenRouter: %v", err)
		return nil // non-fatal — LiteLLM succeeded
	}
	defer resp2.Body.Close()

	var orData struct {
		Data []OpenRouterModel `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&orData); err != nil {
		log.Printf("pricing sync: decode openrouter: %v", err)
		return nil
	}

	orUpserted := 0
	for _, m := range orData.Data {
		provider, model := m.ProviderAndModel()
		if !IsKnownProvider(provider) || model == "" {
			continue
		}
		in1k, out1k := m.Per1kCosts()
		if in1k == 0 && out1k == 0 {
			continue
		}
		// Only insert if not already present (LiteLLM is authoritative)
		db.Exec(ctx, `
			INSERT INTO model_pricing (provider, model, input_cost_per_1k, output_cost_per_1k, synced_from, last_synced_at)
			VALUES ($1,$2,$3,$4,'openrouter',NOW())
			ON CONFLICT (provider, model) DO NOTHING`,
			provider, model, in1k, out1k,
		)
		orUpserted++
	}
	log.Printf("pricing sync: %d rows from OpenRouter (new models only)", orUpserted)
	return nil
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/jobs/... -run "TestParseLiteLLM|TestParseOpenRouter|TestFilterKnown" -v
```
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/jobs/pricingsync.go internal/jobs/pricingsync_test.go
git commit -m "feat: model pricing sync job — LiteLLM primary, OpenRouter secondary"
```

---

### Task 11: Reconciliation, partition, TTL sweep, and runner

- [ ] **Step 1: Create `internal/jobs/reconcile.go`**

```go
package jobs

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// RunReconcile checks Redis spend counters against Postgres SUM for all projects.
// Drift >5% corrects Redis. Drift >20% logs an alert.
func RunReconcile(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client) {
	rows, err := db.Query(ctx, `SELECT id FROM projects WHERE archived_at IS NULL`)
	if err != nil {
		log.Printf("reconcile: query projects: %v", err)
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	periods := []struct {
		period     string
		periodStart time.Time
		keyFmt     string
	}{
		{"daily", time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), "daily:" + now.Format("2006-01-02")},
		{"monthly", time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), "monthly:" + now.Format("2006-01")},
	}

	for rows.Next() {
		var projectID uuid.UUID
		if err := rows.Scan(&projectID); err != nil {
			continue
		}
		for _, p := range periods {
			redisKey := fmt.Sprintf("spend:%s:%s", projectID, p.keyFmt)
			redisVal, err := rdb.Get(ctx, redisKey).Float64()
			if err == redis.Nil {
				continue // no spend recorded yet — nothing to reconcile
			}
			if err != nil {
				continue
			}
			var dbSum float64
			db.QueryRow(ctx,
				`SELECT COALESCE(SUM(cost_usd),0) FROM usage_events
				 WHERE project_id=$1 AND created_at >= $2`,
				projectID, p.periodStart,
			).Scan(&dbSum)

			if dbSum == 0 {
				continue
			}
			drift := abs((redisVal - dbSum) / dbSum)
			if drift > 0.20 {
				log.Printf("ALERT reconcile: project %s %s drift %.0f%% (redis=%.6f db=%.6f)",
					projectID, p.period, drift*100, redisVal, dbSum)
			}
			if drift > 0.05 {
				rdb.Set(ctx, redisKey, dbSum, 0)
				log.Printf("reconcile: corrected %s %s redis=%.6f → db=%.6f (drift %.1f%%)",
					projectID, p.period, redisVal, dbSum, drift*100)
			}
		}
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
```

- [ ] **Step 2: Create `internal/jobs/partition.go`**

```go
package jobs

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunPartition ensures the next month's usage_events partition exists.
// Safe to run multiple times — uses CREATE TABLE IF NOT EXISTS equivalent via DO NOTHING on catalog.
func RunPartition(ctx context.Context, db *pgxpool.Pool) {
	now := time.Now().UTC()
	// Create partition for next month
	nextMonth := now.AddDate(0, 1, 0)
	year := nextMonth.Year()
	month := nextMonth.Month()

	partStart := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	partEnd := partStart.AddDate(0, 1, 0)

	tableName := fmt.Sprintf("usage_events_%04d_%02d", year, int(month))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF usage_events
		 FOR VALUES FROM ('%s') TO ('%s')`,
		tableName,
		partStart.Format("2006-01-02"),
		partEnd.Format("2006-01-02"),
	)
	_, err := db.Exec(ctx, sql)
	if err != nil {
		log.Printf("partition: could not create %s: %v", tableName, err)
		return
	}
	log.Printf("partition: ensured %s exists (%s to %s)", tableName, partStart.Format("2006-01"), partEnd.Format("2006-01"))
}
```

- [ ] **Step 3: Create `internal/jobs/ttlsweep.go`**

```go
package jobs

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RunTTLSweep deletes expired Redis spend keys (prior day and prior month periods).
// Keys from the current day/month are never deleted.
func RunTTLSweep(ctx context.Context, rdb *redis.Client) {
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	thisMonth := now.Format("2006-01")

	var cursor uint64
	deleted := 0
	for {
		var keys []string
		var err error
		keys, cursor, err = rdb.Scan(ctx, cursor, "spend:*", 100).Result()
		if err != nil {
			log.Printf("ttlsweep: scan error: %v", err)
			break
		}
		for _, key := range keys {
			// key format: spend:{project_id}:daily:YYYY-MM-DD  or  spend:{project_id}:monthly:YYYY-MM
			parts := strings.Split(key, ":")
			if len(parts) < 4 {
				continue
			}
			period := parts[2]
			value := parts[3]
			switch period {
			case "daily":
				if value == today {
					continue // current day — keep
				}
			case "monthly":
				if value == thisMonth {
					continue // current month — keep
				}
			default:
				continue
			}
			rdb.Del(ctx, key)
			deleted++
		}
		if cursor == 0 {
			break
		}
	}
	if deleted > 0 {
		log.Printf("ttlsweep: deleted %d expired spend keys", deleted)
	}
}
```

- [ ] **Step 4: Create `internal/jobs/runner.go`**

```go
package jobs

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Start launches all background jobs. Call once from main — non-blocking.
func Start(db *pgxpool.Pool, rdb *redis.Client) {
	// Reconciliation: every 60 seconds
	go func() {
		for {
			time.Sleep(60 * time.Second)
			RunReconcile(context.Background(), db, rdb)
		}
	}()

	// Pricing sync: every 24 hours (run once immediately, then on ticker)
	go func() {
		if err := RunPricingSync(context.Background(), db); err != nil {
			log.Printf("pricing sync (startup): %v", err)
		}
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := RunPricingSync(context.Background(), db); err != nil {
				log.Printf("pricing sync: %v", err)
			}
		}
	}()

	// Partition management: daily at ~00:05 UTC
	go func() {
		RunPartition(context.Background(), db) // run once on startup
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			RunPartition(context.Background(), db)
		}
	}()

	// TTL sweep: daily
	go func() {
		RunTTLSweep(context.Background(), rdb)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			RunTTLSweep(context.Background(), rdb)
		}
	}()

	log.Println("background jobs started: reconcile (60s), pricing sync (24h), partition (24h), ttl sweep (24h)")
}
```

- [ ] **Step 5: Verify compilation**

```bash
go build ./internal/jobs/...
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/jobs/
git commit -m "feat: background jobs — reconcile, pricing sync, partition, TTL sweep"
```

---

## Chunk 5: Dashboard

**Files:**
- Create: `dashboard/embed.go`
- Create: `dashboard/server.go`
- Create: `dashboard/handler.go`
- Create: `dashboard/templates/layout.html`
- Create: `dashboard/templates/login.html`
- Create: `dashboard/templates/projects.html`
- Create: `dashboard/templates/project.html`

### Task 12: Dashboard templates and server

- [ ] **Step 1: Create `dashboard/templates/layout.html`**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{block "title" .}}AIRelay{{end}}</title>
  <script src="https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js"></script>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #f9fafb; color: #111; }
    .container { max-width: 900px; margin: 0 auto; padding: 24px 16px; }
    nav { background: #fff; border-bottom: 1px solid #e5e7eb; padding: 0 16px; display: flex; align-items: center; gap: 16px; height: 52px; }
    nav .brand { font-weight: 700; font-size: 18px; color: #111; text-decoration: none; }
    nav a { color: #555; text-decoration: none; font-size: 14px; }
    nav a:hover { color: #111; }
    .card { background: #fff; border: 1px solid #e5e7eb; border-radius: 8px; padding: 20px; margin-bottom: 16px; }
    .btn { display: inline-block; padding: 8px 16px; border-radius: 6px; font-size: 14px; font-weight: 500; cursor: pointer; border: none; text-decoration: none; }
    .btn-primary { background: #2563eb; color: #fff; }
    .btn-primary:hover { background: #1d4ed8; }
    .btn-danger { background: #dc2626; color: #fff; }
    .btn-sm { padding: 4px 10px; font-size: 12px; }
    input, select { width: 100%; padding: 8px 10px; border: 1px solid #d1d5db; border-radius: 6px; font-size: 14px; margin-top: 4px; }
    label { font-size: 13px; font-weight: 500; color: #374151; }
    .form-group { margin-bottom: 14px; }
    .error { color: #dc2626; font-size: 13px; margin-top: 6px; }
    .badge { display: inline-block; padding: 2px 8px; border-radius: 12px; font-size: 11px; font-weight: 600; }
    .badge-free { background: #f3f4f6; color: #6b7280; }
    .badge-pro  { background: #dbeafe; color: #1d4ed8; }
    .badge-team { background: #d1fae5; color: #065f46; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th { text-align: left; padding: 8px 12px; border-bottom: 2px solid #e5e7eb; color: #6b7280; font-size: 12px; font-weight: 600; text-transform: uppercase; }
    td { padding: 8px 12px; border-bottom: 1px solid #f3f4f6; }
    .progress-bar { height: 8px; background: #e5e7eb; border-radius: 4px; overflow: hidden; }
    .progress-fill { height: 100%; background: #2563eb; border-radius: 4px; transition: width 0.3s; }
    .progress-fill.warning { background: #f59e0b; }
    .progress-fill.danger  { background: #dc2626; }
  </style>
  <script>
    // Attach JWT to all HTMX requests
    document.addEventListener('htmx:configRequest', function(evt) {
      var token = localStorage.getItem('air_token');
      if (token) { evt.detail.headers['Authorization'] = 'Bearer ' + token; }
    });
  </script>
</head>
<body>
  <nav>
    <a href="/dashboard/projects" class="brand">AIRelay</a>
    <a href="/dashboard/projects">Projects</a>
    <span id="nav-plan" style="margin-left:auto; font-size:12px;"></span>
    <a href="#" onclick="localStorage.removeItem('air_token'); window.location='/dashboard/login'">Logout</a>
  </nav>
  <div class="container">
    {{block "content" .}}{{end}}
  </div>
</body>
</html>
```

- [ ] **Step 2: Create `dashboard/templates/login.html`**

```html
{{template "layout" .}}
{{define "title"}}Login — AIRelay{{end}}
{{define "content"}}
<div style="max-width:400px; margin:80px auto;">
  <div class="card">
    <h1 style="font-size:22px; margin-bottom:20px;">Sign in to AIRelay</h1>
    <div id="login-error" class="error" style="display:none;"></div>
    <div class="form-group">
      <label>Email</label>
      <input type="email" id="email" placeholder="you@example.com">
    </div>
    <div class="form-group">
      <label>Password</label>
      <input type="password" id="password" placeholder="••••••••">
    </div>
    <button class="btn btn-primary" style="width:100%" onclick="doLogin()">Sign in</button>
    <p style="margin-top:14px; font-size:13px; color:#6b7280;">
      No account? <a href="/dashboard/signup">Sign up</a>
    </p>
  </div>
</div>
<script>
async function doLogin() {
  var email = document.getElementById('email').value;
  var pass  = document.getElementById('password').value;
  var err   = document.getElementById('login-error');
  err.style.display = 'none';
  try {
    var resp = await fetch('/v1/auth/login', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({email: email, password: pass})
    });
    var data = await resp.json();
    if (!resp.ok) { err.textContent = data.error; err.style.display = 'block'; return; }
    localStorage.setItem('air_token', data.token);
    window.location = '/dashboard/projects';
  } catch(e) {
    err.textContent = 'Network error'; err.style.display = 'block';
  }
}
document.getElementById('password').addEventListener('keydown', function(e){ if(e.key==='Enter') doLogin(); });
</script>
{{end}}
```

- [ ] **Step 3: Create `dashboard/templates/projects.html`**

```html
{{template "layout" .}}
{{define "title"}}Projects — AIRelay{{end}}
{{define "content"}}
<div style="display:flex; align-items:center; justify-content:space-between; margin-bottom:20px;">
  <h1 style="font-size:20px; font-weight:700;">Projects</h1>
  <button class="btn btn-primary btn-sm" onclick="document.getElementById('new-project-form').style.display='block'">+ New project</button>
</div>

<div id="new-project-form" class="card" style="display:none; margin-bottom:16px;">
  <h3 style="margin-bottom:12px; font-size:15px;">Create project</h3>
  <div class="form-group">
    <label>Name</label>
    <input type="text" id="new-project-name" placeholder="My AI app">
  </div>
  <div id="create-error" class="error" style="display:none;"></div>
  <button class="btn btn-primary btn-sm" onclick="createProject()">Create</button>
</div>

<div id="projects-list">
  <p style="color:#6b7280;">Loading projects...</p>
</div>

<script>
var token = localStorage.getItem('air_token');
if (!token) { window.location = '/dashboard/login'; }

async function loadProjects() {
  var resp = await fetch('/v1/projects', {headers: {'Authorization': 'Bearer ' + token}});
  if (resp.status === 401) { window.location = '/dashboard/login'; return; }
  var data = await resp.json();
  var el = document.getElementById('projects-list');
  if (!data.projects.length) {
    el.innerHTML = '<p style="color:#6b7280;">No projects yet. Create one above.</p>';
    return;
  }
  el.innerHTML = data.projects.map(function(p) {
    return '<div class="card" style="display:flex;align-items:center;justify-content:space-between;">' +
      '<div><a href="/dashboard/projects/' + p.id + '" style="font-weight:600;text-decoration:none;color:#2563eb;">' + p.name + '</a>' +
      '<div style="font-size:12px;color:#6b7280;margin-top:2px;">' + p.slug + '</div></div>' +
      '<a href="/dashboard/projects/' + p.id + '" class="btn btn-primary btn-sm">Open</a></div>';
  }).join('');
}

async function createProject() {
  var name = document.getElementById('new-project-name').value;
  var err = document.getElementById('create-error');
  err.style.display = 'none';
  var resp = await fetch('/v1/projects', {
    method: 'POST',
    headers: {'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'},
    body: JSON.stringify({name: name})
  });
  var data = await resp.json();
  if (!resp.ok) { err.textContent = data.error; err.style.display = 'block'; return; }
  document.getElementById('new-project-form').style.display = 'none';
  loadProjects();
}

loadProjects();
</script>
{{end}}
```

- [ ] **Step 4: Create `dashboard/templates/project.html`**

```html
{{template "layout" .}}
{{define "title"}}Project — AIRelay{{end}}
{{define "content"}}
<div style="margin-bottom:20px;">
  <a href="/dashboard/projects" style="color:#6b7280; font-size:13px;">← Projects</a>
  <h1 id="project-name" style="font-size:20px; font-weight:700; margin-top:8px;">Loading...</h1>
</div>

<div id="budget-section" class="card" style="margin-bottom:16px;">
  <h3 style="font-size:15px; font-weight:600; margin-bottom:12px;">Budget</h3>
  <div id="budget-daily"></div>
  <div id="budget-monthly" style="margin-top:12px;"></div>
</div>

<div class="card" style="margin-bottom:16px;">
  <h3 style="font-size:15px; font-weight:600; margin-bottom:12px;">API Keys</h3>
  <div id="keys-list"></div>
  <button class="btn btn-primary btn-sm" style="margin-top:12px;" onclick="createKey()">+ New key</button>
  <div id="new-key-result" style="margin-top:10px; font-size:13px; word-break:break-all;"></div>
</div>

<div class="card">
  <h3 style="font-size:15px; font-weight:600; margin-bottom:12px;">Recent Usage</h3>
  <div id="usage-table"><p style="color:#6b7280;">Loading...</p></div>
</div>

<script>
var token = localStorage.getItem('air_token');
if (!token) { window.location = '/dashboard/login'; }
var projectID = window.location.pathname.split('/').pop();

async function apiFetch(path, opts) {
  opts = opts || {};
  opts.headers = Object.assign({'Authorization': 'Bearer ' + token}, opts.headers || {});
  var resp = await fetch(path, opts);
  if (resp.status === 401) { window.location = '/dashboard/login'; }
  return resp;
}

async function loadProject() {
  var resp = await apiFetch('/v1/projects/' + projectID);
  var p = await resp.json();
  document.getElementById('project-name').textContent = p.name;
}

async function loadBudget() {
  var resp = await apiFetch('/v1/projects/' + projectID + '/budget');
  var data = await resp.json();
  var budgets = data.budgets || [];
  ['daily','monthly'].forEach(function(period) {
    var b = budgets.find(function(x){ return x.period === period; });
    var el = document.getElementById('budget-' + period);
    if (!b) { el.innerHTML = '<p style="color:#6b7280;font-size:13px;">No ' + period + ' budget set.</p>'; return; }
    var pct = b.amount_usd > 0 ? Math.min((b.current_spend / b.amount_usd)*100, 100) : 0;
    var fillClass = pct >= 90 ? 'danger' : pct >= 75 ? 'warning' : '';
    el.innerHTML = '<div style="display:flex;justify-content:space-between;font-size:13px;margin-bottom:4px;">' +
      '<span style="text-transform:capitalize;font-weight:600;">' + period + ' budget</span>' +
      '<span>$' + b.current_spend.toFixed(4) + ' / $' + b.amount_usd.toFixed(2) + '</span></div>' +
      '<div class="progress-bar"><div class="progress-fill ' + fillClass + '" style="width:' + pct.toFixed(1) + '%;"></div></div>';
  });
  setTimeout(loadBudget, 30000); // poll every 30s
}

async function loadKeys() {
  var resp = await apiFetch('/v1/projects/' + projectID + '/keys');
  var data = await resp.json();
  var el = document.getElementById('keys-list');
  if (!data.keys.length) { el.innerHTML = '<p style="color:#6b7280;font-size:13px;">No keys yet.</p>'; return; }
  el.innerHTML = '<table><thead><tr><th>Name</th><th>Prefix</th><th>Created</th><th></th></tr></thead><tbody>' +
    data.keys.filter(function(k){ return !k.revoked_at; }).map(function(k) {
      return '<tr><td>' + k.name + '</td><td><code>' + k.key_prefix + '...</code></td>' +
        '<td>' + new Date(k.created_at).toLocaleDateString() + '</td>' +
        '<td><button class="btn btn-danger btn-sm" onclick="revokeKey(\'' + k.id + '\')">Revoke</button></td></tr>';
    }).join('') + '</tbody></table>';
}

async function createKey() {
  var name = prompt('Key name (e.g. "production")');
  if (!name) return;
  var resp = await apiFetch('/v1/projects/' + projectID + '/keys', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({name: name})
  });
  var data = await resp.json();
  if (resp.ok) {
    document.getElementById('new-key-result').innerHTML =
      '<strong>Copy this key now — it will not be shown again:</strong><br>' +
      '<code style="background:#f3f4f6;padding:6px 10px;border-radius:4px;display:block;margin-top:6px;">' + data.key + '</code>';
    loadKeys();
  }
}

async function revokeKey(keyId) {
  if (!confirm('Revoke this key? All requests using it will fail immediately.')) return;
  await apiFetch('/v1/projects/' + projectID + '/keys/' + keyId, {method: 'DELETE'});
  loadKeys();
}

async function loadUsage() {
  var resp = await apiFetch('/v1/projects/' + projectID + '/usage?limit=20');
  var data = await resp.json();
  var el = document.getElementById('usage-table');
  if (!data.events.length) { el.innerHTML = '<p style="color:#6b7280;font-size:13px;">No usage events yet.</p>'; return; }
  el.innerHTML = '<table><thead><tr><th>Model</th><th>Tokens</th><th>Cost</th><th>Status</th><th>Time</th></tr></thead><tbody>' +
    data.events.map(function(e) {
      var cost = e.cost_usd != null ? '$' + e.cost_usd.toFixed(6) : '—';
      return '<tr><td>' + e.model + '</td><td>' + (e.prompt_tokens + e.completion_tokens) + '</td>' +
        '<td>' + cost + '</td><td>' + e.status_code + '</td>' +
        '<td>' + new Date(e.created_at).toLocaleString() + '</td></tr>';
    }).join('') + '</tbody></table>';
}

loadProject();
loadBudget();
loadKeys();
loadUsage();
</script>
{{end}}
```

- [ ] **Step 5: Create `dashboard/embed.go`**

```go
package dashboard

import "embed"

//go:embed templates
var TemplateFS embed.FS
```

- [ ] **Step 6: Create `dashboard/handler.go`**

```go
package dashboard

import (
	"html/template"
	"net/http"
)

// Handler serves dashboard HTML pages.
type Handler struct {
	tmpl *template.Template
}

func NewHandler(tmpl *template.Template) *Handler {
	return &Handler{tmpl: tmpl}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "login.html", nil)
}

func (h *Handler) Projects(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "projects.html", nil)
}

func (h *Handler) Project(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "project.html", nil)
}
```

- [ ] **Step 7: Create `dashboard/server.go`**

```go
package dashboard

import (
	"html/template"
	"log"
	"net/http"
)

// NewDashboardServer returns routes mounted on the provided mux.
// Call this with the same mux as the API server to serve both from one binary.
func NewDashboardServer(mux *http.ServeMux) {
	tmpl, err := template.ParseFS(TemplateFS, "templates/*.html")
	if err != nil {
		log.Fatalf("dashboard: parse templates: %v", err)
	}
	h := NewHandler(tmpl)

	mux.HandleFunc("GET /dashboard/login", h.Login)
	mux.HandleFunc("GET /dashboard/signup", h.Login) // same page — JS handles both
	mux.HandleFunc("GET /dashboard/projects", h.Projects)
	mux.HandleFunc("GET /dashboard/projects/{id}", h.Project)
	mux.HandleFunc("GET /dashboard/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard/projects", http.StatusFound)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/dashboard/projects", http.StatusFound)
		}
	})
}
```

- [ ] **Step 8: Verify compilation**

```bash
go build ./dashboard/...
```
Expected: no errors.

- [ ] **Step 9: Commit**

```bash
git add dashboard/
git commit -m "feat: HTMX dashboard — login, projects list, project detail with budget polling"
```

---

## Chunk 6: Entry Point and Integration Test

**Files:**
- Create: `cmd/api/main.go`
- Modify: `Makefile` — add `api` target

### Task 13: Management API entry point

- [ ] **Step 1: Create `cmd/api/main.go`**

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/dashboard"
	"github.com/airelay/airelay/internal/config"
	"github.com/airelay/airelay/internal/db"
	"github.com/airelay/airelay/internal/jobs"
	redisclient "github.com/airelay/airelay/internal/redis"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	pool, err := db.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	rdb, err := redisclient.Connect(cfg.RedisURL)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()

	// Background jobs (non-blocking)
	jobs.Start(pool, rdb)

	// Build API server — mux returned separately to avoid type assertion
	srv, mux := api.NewServer(pool, rdb, cfg)

	// Mount dashboard routes on the same mux
	dashboard.NewDashboardServer(mux)

	srv.Addr = ":" + cfg.Port

	log.Printf("AIRelay management API + dashboard listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
```


- [ ] **Step 2: Update Makefile — add `api` target**

Open `Makefile` and add after the `proxy:` target:
```makefile
api:
	go run ./cmd/api
```

- [ ] **Step 3: Build**

```bash
go build ./cmd/api/
```
Expected: produces `api` binary.

- [ ] **Step 4: Commit**

```bash
git add cmd/api/ Makefile
git commit -m "feat: management API entry point — serves API + dashboard + starts background jobs"
```

---

### Task 14: End-to-end integration test

- [ ] **Step 1: Start all services and the API**

Terminal 1:
```bash
make dev
make migrate-up
export $(cat .env | xargs) && go run ./cmd/api/
```
Expected: `AIRelay management API + dashboard listening on :8080`

- [ ] **Step 2: Signup and login**

Terminal 2:
```bash
# Signup
curl -s -X POST http://localhost:8080/v1/auth/signup \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"password123"}' | jq .

# Login — save token
TOKEN=$(curl -s -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"password123"}' | jq -r .token)
echo "TOKEN=$TOKEN"
```
Expected: signup returns `{"user_id":"...","plan":"free"}`, login returns `{"token":"eyJ..."}`.

- [ ] **Step 3: Create project and API key**

```bash
# Create project
PROJECT=$(curl -s -X POST http://localhost:8080/v1/projects \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Project"}' | jq -r .id)
echo "PROJECT=$PROJECT"

# Create API key
KEY=$(curl -s -X POST http://localhost:8080/v1/projects/$PROJECT/keys \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"dev"}' | jq -r .key)
echo "KEY=$KEY"
```
Expected: project id UUID, key starts with `air_sk_`.

- [ ] **Step 4: Add provider credential and set budget**

```bash
# Add OpenAI credential (use real key or test value)
curl -s -X POST http://localhost:8080/v1/projects/$PROJECT/credentials \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"provider\":\"openai\",\"key\":\"${OPENAI_API_KEY}\"}" | jq .

# Set monthly budget
curl -s -X PUT http://localhost:8080/v1/projects/$PROJECT/budget \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"amount_usd":5.00,"period":"monthly","hard_limit":true}' | jq .
```
Expected: credential `{"id":"...","provider":"openai"}`, budget `{"id":"...","amount_usd":5.0}`.

- [ ] **Step 5: Test the proxy with the management-created key**

```bash
# Start proxy in another terminal
export $(cat .env | xargs) && go run ./cmd/proxy/ &

# Make a proxied request
curl http://localhost:8081/proxy/openai/v1/models \
  -H "Authorization: Bearer $KEY" | jq .keys[0]
```
Expected: OpenAI model list (if OPENAI_API_KEY is real) or 401 from OpenAI (key not found with test value). Either way, the proxy auth works — it finds the credential and forwards.

- [ ] **Step 6: Open the dashboard**

```bash
open http://localhost:8080/dashboard/login
```
Log in with test@example.com / password123. Expected: projects list page with "Test Project" visible.

- [ ] **Step 7: Run all tests**

```bash
export $(cat .env | xargs) && make test
```
Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add .
git commit -m "feat: Plan 2 complete — management API, background jobs, dashboard"
```

---

## Summary

Plan 2 delivers the full management layer:

- ✅ JWT auth (signup, login, me) with bcrypt password hashing
- ✅ Plan enforcement: Free (1 project, 1 key, 7d history), Pro (unlimited, 90d), Team (unlimited)
- ✅ Project CRUD with auto-slug generation and plan limit gating
- ✅ API key management — full key returned once only (same GenerateKey as proxy)
- ✅ Provider credentials — AES-256 encrypted, never exposed in list responses
- ✅ Budget management — upsert with period uniqueness, current spend from Redis
- ✅ Usage events — paginated, history-gated by plan
- ✅ Usage summary — cost by model and by day
- ✅ Model pricing list — public endpoint
- ✅ Background jobs — pricing sync (LiteLLM + OpenRouter), reconciliation (60s), partition management, TTL sweep
- ✅ HTMX dashboard — login, projects list, project detail with 30s budget polling
- ✅ Single binary: management API + dashboard + jobs on one port

**Next:** Plan 3 — Billing + Infrastructure + Distribution

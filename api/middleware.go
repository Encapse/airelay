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

const claimsKey contextKey = "claims"

// Claims holds the JWT payload.
type Claims struct {
	UserID uuid.UUID       `json:"user_id"`
	Email  string          `json:"email"`
	Plan   models.UserPlan `json:"plan"`
	jwt.RegisteredClaims
}

// IssueToken creates a signed HS256 JWT valid for 7 days.
func IssueToken(userID uuid.UUID, email string, plan models.UserPlan, secret string) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		Plan:   plan,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// RequireAuth returns middleware that validates Bearer JWTs and injects Claims into context.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "missing or invalid authorization header")
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			var claims Claims
			token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			})
			if err != nil || !token.Valid {
				writeError(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, &claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext retrieves Claims from context. Returns nil if not present.
func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(claimsKey).(*Claims)
	return c
}

// ContextWithClaims injects Claims into context (used by tests).
func ContextWithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, c)
}

// PlanLimits holds enforcement limits for a plan tier.
// MaxProjects/MaxKeys == 0 means unlimited.
// HistoryDays == -1 means unlimited.
type PlanLimits struct {
	MaxProjects int
	MaxKeys     int
	HistoryDays int
}

// Limits returns the enforcement limits for the given plan.
func Limits(plan models.UserPlan) PlanLimits {
	switch plan {
	case models.PlanPro:
		return PlanLimits{MaxProjects: 0, MaxKeys: 0, HistoryDays: 90}
	case models.PlanTeam:
		return PlanLimits{MaxProjects: 0, MaxKeys: 0, HistoryDays: -1}
	default: // free
		return PlanLimits{MaxProjects: 1, MaxKeys: 1, HistoryDays: 7}
	}
}

// chain applies middlewares right-to-left: chain(h, m1, m2) → m1(m2(h)).
func chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

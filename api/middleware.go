package api

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/airelay/airelay/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
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

// ipLimiterEntry combines a rate limiter with the last time it was accessed,
// so the cleanup goroutine can evict stale entries without leaking memory.
type ipLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPRateLimit returns middleware that limits requests per remote IP.
// r is the sustained rate (e.g. rate.Every(12*time.Second) for 5/min),
// burst is the burst allowance, and cleanupInterval controls how often stale
// entries are purged from the in-memory map.
func IPRateLimit(r rate.Limit, burst int, cleanupInterval time.Duration) func(http.Handler) http.Handler {
	var mu sync.Mutex
	visitors := make(map[string]*ipLimiterEntry)

	// Background goroutine: evict entries not seen in 2x cleanupInterval.
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-2 * cleanupInterval)
			mu.Lock()
			for ip, e := range visitors {
				if e.lastSeen.Before(cutoff) {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	limiterFor := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		e, ok := visitors[ip]
		if !ok {
			e = &ipLimiterEntry{limiter: rate.NewLimiter(r, burst)}
			visitors[ip] = e
		}
		e.lastSeen = time.Now()
		return e.limiter
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// Use just the host portion of RemoteAddr; ignore port.
			ip, _, err := net.SplitHostPort(req.RemoteAddr)
			if err != nil {
				ip = req.RemoteAddr
			}
			if !limiterFor(ip).Allow() {
				writeError(w, http.StatusTooManyRequests, "too many requests")
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

// chain applies middlewares right-to-left: chain(h, m1, m2) → m1(m2(h)).
func chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

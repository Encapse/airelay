package api_test

import (
	"context"
	"net/http"

	"github.com/airelay/airelay/api"
	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
)

func injectClaims(r *http.Request, plan models.UserPlan) *http.Request {
	claims := &api.Claims{
		UserID: uuid.New(),
		Email:  "test@example.com",
		Plan:   plan,
	}
	return r.WithContext(api.ContextWithClaims(r.Context(), claims))
}

// injectClaimsCtx injects claims into a context directly (for non-request tests).
func injectClaimsCtx(ctx context.Context, plan models.UserPlan) context.Context {
	claims := &api.Claims{
		UserID: uuid.New(),
		Email:  "test@example.com",
		Plan:   plan,
	}
	return api.ContextWithClaims(ctx, claims)
}

package proxy

import (
	"context"
	"fmt"
	"time"

	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// SpendKey returns the period-scoped Redis key for a project's spend.
func SpendKey(projectID uuid.UUID, period string, t time.Time) string {
	switch period {
	case "daily":
		return fmt.Sprintf("spend:%s:daily:%s", projectID, t.UTC().Format("2006-01-02"))
	case "monthly":
		return fmt.Sprintf("spend:%s:monthly:%s", projectID, t.UTC().Format("2006-01"))
	default:
		return fmt.Sprintf("spend:%s:%s", projectID, period)
	}
}

// BudgetResult is returned by CheckBudgets.
type BudgetResult struct {
	Blocked   bool
	Reason    string
	RedisDown bool // true if Redis was unreachable; handler should use fail-open logging path
}

// BudgetChecker checks and records project spend against configured budgets.
type BudgetChecker struct {
	db    *pgxpool.Pool
	redis *redis.Client
}

func NewBudgetChecker(db *pgxpool.Pool, rdb *redis.Client) *BudgetChecker {
	return &BudgetChecker{db: db, redis: rdb}
}

// CheckBudgets returns Blocked=true if any hard-limit budget has been exceeded.
// Fails open on all errors: budget check errors never block a request.
// Sets RedisDown=true if Redis is unreachable so the handler can write directly to Postgres.
func (b *BudgetChecker) CheckBudgets(ctx context.Context, projectID uuid.UUID) (*BudgetResult, error) {
	// Probe Redis with a cheap ping to detect outage before loading budgets
	if err := b.redis.Ping(ctx).Err(); err != nil {
		return &BudgetResult{Blocked: false, RedisDown: true}, nil
	}
	budgets, err := b.loadBudgets(ctx, projectID)
	if err != nil {
		return &BudgetResult{Blocked: false}, nil // fail open
	}
	now := time.Now().UTC()
	for _, budget := range budgets {
		key := SpendKey(projectID, string(budget.Period), now)
		spend, err := b.getSpend(ctx, key, projectID, budget.Period)
		if err != nil {
			continue // fail open per budget
		}
		if budget.HardLimit && spend >= budget.AmountUSD {
			return &BudgetResult{
				Blocked: true,
				Reason:  fmt.Sprintf("%s budget of $%.4f exceeded (spend: $%.4f)", budget.Period, budget.AmountUSD, spend),
			}, nil
		}
	}
	return &BudgetResult{Blocked: false}, nil
}

// RecordSpend adds cost to the Redis spend key for a given period.
func (b *BudgetChecker) RecordSpend(ctx context.Context, projectID uuid.UUID, period models.BudgetPeriod, costUSD float64) {
	key := SpendKey(projectID, string(period), time.Now().UTC())
	b.redis.IncrByFloat(ctx, key, costUSD)
}

func (b *BudgetChecker) loadBudgets(ctx context.Context, projectID uuid.UUID) ([]models.Budget, error) {
	rows, err := b.db.Query(ctx,
		`SELECT id, project_id, amount_usd, period, hard_limit, created_at
		 FROM budgets WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var budgets []models.Budget
	for rows.Next() {
		var bg models.Budget
		if err := rows.Scan(&bg.ID, &bg.ProjectID, &bg.AmountUSD, &bg.Period, &bg.HardLimit, &bg.CreatedAt); err != nil {
			return nil, err
		}
		budgets = append(budgets, bg)
	}
	return budgets, rows.Err()
}

// getSpend returns current spend from Redis, rebuilding from Postgres on cache miss.
func (b *BudgetChecker) getSpend(ctx context.Context, key string, projectID uuid.UUID, period models.BudgetPeriod) (float64, error) {
	val, err := b.redis.Get(ctx, key).Float64()
	if err == nil {
		return val, nil
	}
	if err != redis.Nil {
		return 0, err
	}
	spend, err := b.rebuildFromDB(ctx, projectID, period)
	if err != nil {
		return 0, err
	}
	b.redis.Set(ctx, key, spend, 0)
	return spend, nil
}

func (b *BudgetChecker) rebuildFromDB(ctx context.Context, projectID uuid.UUID, period models.BudgetPeriod) (float64, error) {
	now := time.Now().UTC()
	var periodStart time.Time
	switch period {
	case models.PeriodDaily:
		periodStart = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case models.PeriodMonthly:
		periodStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return 0, fmt.Errorf("unknown budget period: %s", period)
	}
	var spend float64
	err := b.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM usage_events
		 WHERE project_id = $1 AND created_at >= $2`,
		projectID, periodStart,
	).Scan(&spend)
	return spend, err
}

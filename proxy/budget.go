package proxy

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/airelay/airelay/internal/budget"
	"github.com/airelay/airelay/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// SpendKey is a package-level alias kept for integration tests that import it directly.
func SpendKey(projectID uuid.UUID, period string, t time.Time) string {
	return budget.SpendKey(projectID, period, t)
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
	for _, bg := range budgets {
		key := budget.SpendKey(projectID, string(bg.Period), now)
		spend, err := b.getSpend(ctx, key, projectID, bg.Period)
		if err != nil {
			continue // fail open per budget
		}
		if bg.HardLimit && spend >= bg.AmountUSD {
			return &BudgetResult{
				Blocked: true,
				Reason:  fmt.Sprintf("%s budget of $%.4f exceeded (spend: $%.4f)", bg.Period, bg.AmountUSD, spend),
			}, nil
		}
	}
	return &BudgetResult{Blocked: false}, nil
}

// RecordSpend adds cost to the Redis spend key for a given period.
// Logs a warning if the increment fails so drift is visible in logs.
func (b *BudgetChecker) RecordSpend(ctx context.Context, projectID uuid.UUID, period models.BudgetPeriod, costUSD float64) {
	now := time.Now().UTC()
	key := budget.SpendKey(projectID, string(period), now)
	if err := b.redis.IncrByFloat(ctx, key, costUSD).Err(); err != nil {
		log.Printf("WARN: RecordSpend failed for project %s period %s: %v", projectID, period, err)
		return
	}
	// Set TTL if not already set (first write for this period).
	ttl, err := b.redis.TTL(ctx, key).Result()
	if err == nil && ttl < 0 {
		b.redis.Expire(ctx, key, budget.SpendKeyTTL(string(period), now))
	}
}

func (b *BudgetChecker) loadBudgets(ctx context.Context, projectID uuid.UUID) ([]models.Budget, error) {
	rows, err := b.db.Query(ctx,
		`SELECT id, project_id, amount_usd, period, hard_limit, created_at
		 FROM budgets WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bgs []models.Budget
	for rows.Next() {
		var bg models.Budget
		if err := rows.Scan(&bg.ID, &bg.ProjectID, &bg.AmountUSD, &bg.Period, &bg.HardLimit, &bg.CreatedAt); err != nil {
			return nil, err
		}
		bgs = append(bgs, bg)
	}
	return bgs, rows.Err()
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
	b.redis.Set(ctx, key, spend, budget.SpendKeyTTL(string(period), time.Now().UTC()))
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

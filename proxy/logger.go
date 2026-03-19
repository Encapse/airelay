package proxy

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UsageEvent is a record to be written to Postgres.
type UsageEvent struct {
	ProjectID        uuid.UUID
	APIKeyID         uuid.UUID
	Provider         string
	Model            string
	PromptTokens     int
	CompletionTokens int
	CostUSD          *float64
	DurationMS       int
	StatusCode       int
	Metadata         map[string]any
	FailOpen         bool
}

// Logger batches usage events and writes them to Postgres asynchronously.
type Logger struct {
	db      *pgxpool.Pool
	ch      chan UsageEvent
	dlq     *DLQ
	budgets *BudgetChecker
}

func NewLogger(db *pgxpool.Pool, budgets *BudgetChecker) *Logger {
	l := &Logger{
		db:      db,
		ch:      make(chan UsageEvent, 50_000),
		dlq:     NewDLQ(db),
		budgets: budgets,
	}
	go l.run()
	return l
}

// Log queues an event for async write. Non-blocking — drops on full channel.
func (l *Logger) Log(e UsageEvent) {
	select {
	case l.ch <- e:
	default:
		log.Printf("WARN: usage logger channel full, dropping event for project %s", e.ProjectID)
	}
}

// LogDirect writes an event synchronously to Postgres (used during fail-open when Redis is down).
func (l *Logger) LogDirect(ctx context.Context, e UsageEvent) error {
	return writeUsageEvent(ctx, l.db, e)
}

func (l *Logger) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var batch []UsageEvent

	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, e := range batch {
			if err := writeUsageEvent(ctx, l.db, e); err != nil {
				l.dlq.Enqueue(e)
			} else {
				l.recordSpend(ctx, e)
			}
		}
		batch = batch[:0]
	}

	for {
		select {
		case e := <-l.ch:
			batch = append(batch, e)
			if len(batch) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// recordSpend updates Redis spend counters only for periods that have budgets.
func (l *Logger) recordSpend(ctx context.Context, e UsageEvent) {
	if e.CostUSD == nil || l.budgets == nil {
		return
	}
	budgets, err := l.budgets.loadBudgets(ctx, e.ProjectID)
	if err != nil {
		return
	}
	for _, b := range budgets {
		l.budgets.RecordSpend(ctx, e.ProjectID, b.Period, *e.CostUSD)
	}
}

// writeUsageEvent is shared by Logger and DLQ.
func writeUsageEvent(ctx context.Context, db *pgxpool.Pool, e UsageEvent) error {
	_, err := db.Exec(ctx, `
		INSERT INTO usage_events
		    (project_id, api_key_id, provider, model, prompt_tokens, completion_tokens,
		     cost_usd, duration_ms, status_code, metadata, fail_open)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.ProjectID, e.APIKeyID, e.Provider, e.Model,
		e.PromptTokens, e.CompletionTokens, e.CostUSD,
		e.DurationMS, e.StatusCode, e.Metadata, e.FailOpen,
	)
	return err
}

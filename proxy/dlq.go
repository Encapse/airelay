package proxy

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const dlqCap = 50_000

// DLQ is an in-memory dead letter queue for usage events that failed Postgres writes.
type DLQ struct {
	mu    sync.Mutex
	queue []UsageEvent
	db    *pgxpool.Pool
}

func NewDLQ(db *pgxpool.Pool) *DLQ {
	d := &DLQ{db: db}
	go d.retryLoop()
	return d
}

func (d *DLQ) Enqueue(e UsageEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.queue) >= dlqCap {
		log.Printf("WARN: DLQ full (%d items), dropping oldest event", dlqCap)
		d.queue = d.queue[1:]
	}
	d.queue = append(d.queue, e)
}

func (d *DLQ) retryLoop() {
	backoff := []time.Duration{5 * time.Second, 30 * time.Second, 5 * time.Minute}
	attempt := 0
	for {
		time.Sleep(backoff[min(attempt, len(backoff)-1)])
		d.mu.Lock()
		if len(d.queue) == 0 {
			d.mu.Unlock()
			attempt = 0
			continue
		}
		batch := make([]UsageEvent, len(d.queue))
		copy(batch, d.queue)
		d.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		failed := d.flushBatch(ctx, batch)
		cancel()

		d.mu.Lock()
		d.queue = failed
		d.mu.Unlock()

		if len(failed) > 0 {
			attempt = min(attempt+1, len(backoff)-1)
			log.Printf("WARN: DLQ retry: %d events remaining", len(failed))
		} else {
			attempt = 0
		}
	}
}

func (d *DLQ) flushBatch(ctx context.Context, batch []UsageEvent) []UsageEvent {
	var failed []UsageEvent
	for _, e := range batch {
		if err := writeUsageEvent(ctx, d.db, e); err != nil {
			failed = append(failed, e)
		}
	}
	return failed
}

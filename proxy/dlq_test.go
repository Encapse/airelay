package proxy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeEvent(model string) UsageEvent {
	return UsageEvent{
		ProjectID: uuid.New(),
		APIKeyID:  uuid.New(),
		Provider:  "openai",
		Model:     model,
	}
}

func newTestDLQ(fn func(context.Context, UsageEvent) error) *DLQ {
	return &DLQ{
		writeFn: func(ctx context.Context, _ *pgxpool.Pool, e UsageEvent) error {
			return fn(ctx, e)
		},
	}
}

func TestDLQ_FlushSuccess(t *testing.T) {
	var written []UsageEvent
	var mu sync.Mutex
	d := newTestDLQ(func(_ context.Context, e UsageEvent) error {
		mu.Lock()
		written = append(written, e)
		mu.Unlock()
		return nil
	})

	batch := []UsageEvent{makeEvent("gpt-4o"), makeEvent("claude-3-5-haiku")}
	failed := d.flushBatch(context.Background(), batch)

	assert.Empty(t, failed)
	assert.Len(t, written, 2)
}

func TestDLQ_FailedEventsReturned(t *testing.T) {
	calls := 0
	d := newTestDLQ(func(_ context.Context, _ UsageEvent) error {
		calls++
		if calls == 1 {
			return errors.New("db error")
		}
		return nil
	})

	e := makeEvent("gpt-4o-mini")
	ctx := context.Background()

	failed := d.flushBatch(ctx, []UsageEvent{e})
	require.Len(t, failed, 1, "first call should fail and return event")

	failed2 := d.flushBatch(ctx, failed)
	assert.Empty(t, failed2, "second call should succeed")
}

func TestDLQ_CapEnforcement(t *testing.T) {
	d := newTestDLQ(func(_ context.Context, _ UsageEvent) error { return nil })

	for i := 0; i < dlqCap; i++ {
		d.Enqueue(makeEvent("model"))
	}
	assert.Equal(t, dlqCap, len(d.queue))

	newest := makeEvent("newest")
	d.Enqueue(newest)
	assert.Equal(t, dlqCap, len(d.queue), "should stay at cap")
	assert.Equal(t, newest, d.queue[dlqCap-1], "newest should be last")
}

func TestDLQ_NewlyEnqueuedPreservedDuringRetry(t *testing.T) {
	d := newTestDLQ(func(_ context.Context, _ UsageEvent) error { return nil })

	existing := makeEvent("existing")
	d.Enqueue(existing)

	// Take a snapshot (as retryLoop would)
	d.mu.Lock()
	batch := make([]UsageEvent, len(d.queue))
	copy(batch, d.queue)
	d.mu.Unlock()

	// Simulate new arrival during retry
	newArrival := makeEvent("arrived-during-retry")
	d.mu.Lock()
	d.queue = append(d.queue, newArrival)
	d.mu.Unlock()

	// Apply merge (all succeeded → failed is empty)
	d.mu.Lock()
	cutoff := min(len(batch), len(d.queue))
	newlyEnqueued := d.queue[cutoff:]
	d.queue = append([]UsageEvent{}, newlyEnqueued...)
	d.mu.Unlock()

	require.Len(t, d.queue, 1)
	assert.Equal(t, newArrival, d.queue[0])
}

func TestDLQ_NoPanicWhenQueueShrinksBelowBatch(t *testing.T) {
	d := newTestDLQ(func(_ context.Context, _ UsageEvent) error { return nil })
	for i := 0; i < 10; i++ {
		d.Enqueue(makeEvent("model"))
	}

	d.mu.Lock()
	batch := make([]UsageEvent, len(d.queue))
	copy(batch, d.queue)
	// Simulate drops reducing queue below batch size
	d.queue = d.queue[:3]
	d.mu.Unlock()

	require.NotPanics(t, func() {
		d.mu.Lock()
		cutoff := min(len(batch), len(d.queue))
		newlyEnqueued := d.queue[cutoff:]
		d.queue = append([]UsageEvent{}, newlyEnqueued...)
		d.mu.Unlock()
	})
}

func TestDLQ_ConcurrentEnqueue(t *testing.T) {
	d := newTestDLQ(func(_ context.Context, _ UsageEvent) error { return nil })

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Enqueue(makeEvent("concurrent"))
		}()
	}
	wg.Wait()

	d.mu.Lock()
	n := len(d.queue)
	d.mu.Unlock()
	assert.LessOrEqual(t, n, 200)
	assert.Greater(t, n, 0)
}

func TestDLQ_EnqueueTimeout(t *testing.T) {
	// Verify flushBatch respects context cancellation
	d := newTestDLQ(func(ctx context.Context, _ UsageEvent) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return nil
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	batch := []UsageEvent{makeEvent("slow")}
	failed := d.flushBatch(ctx, batch)
	// Context expired — event should be re-queued as failed
	assert.Len(t, failed, 1)
}

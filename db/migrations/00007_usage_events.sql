-- +goose Up
CREATE TABLE usage_events (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL,
    api_key_id UUID NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd NUMERIC(12,8),
    duration_ms INTEGER NOT NULL DEFAULT 0,
    status_code INTEGER NOT NULL DEFAULT 0,
    metadata JSONB,
    fail_open BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Create monthly partitions for 2026
CREATE TABLE usage_events_2026_01 PARTITION OF usage_events
    FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE usage_events_2026_02 PARTITION OF usage_events
    FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE usage_events_2026_03 PARTITION OF usage_events
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE usage_events_2026_04 PARTITION OF usage_events
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE usage_events_2026_05 PARTITION OF usage_events
    FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE usage_events_2026_06 PARTITION OF usage_events
    FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
CREATE TABLE usage_events_2026_07 PARTITION OF usage_events
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
CREATE TABLE usage_events_2026_08 PARTITION OF usage_events
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE usage_events_2026_09 PARTITION OF usage_events
    FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE usage_events_2026_10 PARTITION OF usage_events
    FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE usage_events_2026_11 PARTITION OF usage_events
    FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE usage_events_2026_12 PARTITION OF usage_events
    FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');

-- +goose Down
DROP TABLE usage_events CASCADE;

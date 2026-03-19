-- +goose Up
CREATE TABLE alert_thresholds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    threshold_pct INTEGER NOT NULL CHECK (threshold_pct BETWEEN 1 AND 100),
    channel TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE alert_thresholds;

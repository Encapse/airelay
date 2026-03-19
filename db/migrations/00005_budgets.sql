-- +goose Up
CREATE TABLE budgets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    amount_usd NUMERIC(12,6) NOT NULL,
    period TEXT NOT NULL CHECK (period IN ('daily', 'monthly')),
    hard_limit BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, period)
);

-- +goose Down
DROP TABLE budgets;

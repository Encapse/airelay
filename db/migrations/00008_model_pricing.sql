-- +goose Up
CREATE TABLE model_pricing (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    input_cost_per_1k NUMERIC(12,8) NOT NULL,
    output_cost_per_1k NUMERIC(12,8) NOT NULL,
    manual_override BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(provider, model)
);

INSERT INTO model_pricing (provider, model, input_cost_per_1k, output_cost_per_1k) VALUES
    ('openai', 'gpt-4o', 0.00250000, 0.01000000),
    ('openai', 'gpt-4o-mini', 0.00015000, 0.00060000),
    ('anthropic', 'claude-3-5-sonnet-20241022', 0.00300000, 0.01500000),
    ('anthropic', 'claude-3-5-haiku-20241022', 0.00100000, 0.00500000),
    ('google', 'gemini-2.0-flash', 0.00010000, 0.00040000);

-- +goose Down
DROP TABLE model_pricing;

-- +goose Up
CREATE TABLE provider_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    encrypted_key TEXT NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX provider_credentials_project_provider_active
    ON provider_credentials(project_id, provider)
    WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE provider_credentials;

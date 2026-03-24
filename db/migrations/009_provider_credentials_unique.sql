-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS provider_credentials_project_provider_idx
    ON provider_credentials(project_id, provider)
    WHERE revoked_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS provider_credentials_project_provider_idx;

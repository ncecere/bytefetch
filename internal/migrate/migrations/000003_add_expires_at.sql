-- +goose Up
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE jobs DROP COLUMN IF EXISTS expires_at;

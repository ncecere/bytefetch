-- +goose Up
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS deadline_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE jobs DROP COLUMN IF EXISTS deadline_at;

-- +goose Up
ALTER TABLE crawl_tasks ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE crawl_tasks DROP COLUMN IF EXISTS attempts;

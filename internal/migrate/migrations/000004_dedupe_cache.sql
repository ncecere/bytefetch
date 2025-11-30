-- +goose Up
CREATE TABLE IF NOT EXISTS dedupe_cache (
    canonical_url TEXT PRIMARY KEY,
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS dedupe_cache;

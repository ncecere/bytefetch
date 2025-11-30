-- +goose Up
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status TEXT NOT NULL CHECK (status IN ('queued','running','completed','completed_with_errors','failed','cancelled')),
    type TEXT NOT NULL CHECK (type IN ('scrape','batch_scrape','crawl','map')),
    params JSONB NOT NULL DEFAULT '{}'::jsonb,
    deadline_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS crawl_tasks (
    id BIGSERIAL PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    requested_url TEXT NOT NULL,
    final_url TEXT NOT NULL,
    depth INT NOT NULL DEFAULT 0,
    status TEXT NOT NULL CHECK (status IN ('queued','processing','done','failed','skipped')),
    error TEXT,
    attempts INT NOT NULL DEFAULT 0,
    processing_started_at TIMESTAMPTZ,
    heartbeat_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(job_id, final_url)
);
CREATE INDEX IF NOT EXISTS idx_crawl_tasks_job_status ON crawl_tasks(job_id, status);

CREATE TABLE IF NOT EXISTS documents (
    id BIGSERIAL PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    requested_url TEXT NOT NULL,
    final_url TEXT NOT NULL,
    title TEXT,
    content_md TEXT,
    content_raw TEXT,
    meta JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_documents_job ON documents(job_id);

CREATE TABLE IF NOT EXISTS mapped_urls (
    id BIGSERIAL PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    source_url TEXT NOT NULL,
    target_url TEXT NOT NULL,
    depth INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_mapped_urls_job ON mapped_urls(job_id);
CREATE INDEX IF NOT EXISTS idx_mapped_urls_job_depth ON mapped_urls(job_id, depth);

-- +goose Down
DROP TABLE IF EXISTS mapped_urls;
DROP TABLE IF EXISTS documents;
DROP TABLE IF EXISTS crawl_tasks;
DROP TABLE IF EXISTS jobs;

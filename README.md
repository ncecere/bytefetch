# Bullnose

Bullnose is a Go-based scraping/crawling engine inspired by Firecrawl. A single binary (`crawler`) can run API, worker, or combined “all” mode, exposing synchronous scrape plus asynchronous batch/crawl/map jobs with PostgreSQL-backed queues, JS rendering via Rod, and Prometheus metrics.

## Getting Started

1. **Install dependencies**
   ```bash
   make deps  # or go env -w GOPRIVATE ... etc if you have custom tooling
   ```
2. **Run services for local dev (API + worker + cleanup)**
   ```bash
   make run-all
   ```
3. **Run unit tests**
   ```bash
   go test ./...
   ```

PostgreSQL is required; `docker-compose up -d db` (if provided) bootstraps a local instance.

## Configuration

Configuration is YAML-driven (see `config.example.yaml`). Example:

```yaml
server:
  addr: ":8080"

database:
  url: "postgres://bullnose:bullnose@localhost:5432/bullnose?sslmode=disable"

http:
  timeout: 10s
  max_redirects: 5
  max_body_bytes: 5MB
  user_agent: BullnoseCrawler/0.1

worker:
  pool_size: 4
  per_host_limit: 2
  per_host_rps: 2
  max_retries: 3
  backoff_base: 500ms
  backoff_max: 10s
  lease_timeout: 60s
  heartbeat_interval: 10s

job_defaults:
  max_tasks: 5000
  max_depth: 2
  max_duration: 15m
  max_bytes: 100MB
  ttl: 24h
  deadline: 15m

crawl:
  respect_robots: true
  robots_fail_open: true
  respect_nofollow: false
  crawl_delay_ms: 0
  strip_query: true
  block_extensions: [".pdf", ".jpg", ".png", ".zip"]
  allowed_schemes: ["http", "https"]
  dedupe_cache: true
  include_sitemaps: true
  sitemap_url_limit: 500
  sitemap_max_nested: 2

browser:
  enabled: true
  headless: true
  pool_size: 2
  per_host_limit: 2
  nav_timeout: 15s
  wait_idle_timeout: 3s
  max_page_lifetime: 60s

cleanup:
  interval: 1h
  enabled: true
  run_in: ["api", "worker", "all"]
  api_interval: 30m
  worker_interval: 30m
```

Launch the binary with `--config=/path/to/config.yaml`. Environment variables prefixed with `BULLNOSE_` override values (e.g., `BULLNOSE_DATABASE_URL`).

### Server
- `addr` *(default `:8080`)*: Address the Fiber API server listens on, e.g., `0.0.0.0:8080`.

### Database
- `url` *(required)*: PostgreSQL connection string; can also be provided via `DATABASE_URL`.
- `max_open_conns` *(default `10`)*: Upper bound for open connections in the sql.DB pool.
- `max_idle_conns` *(default `5`)*: Idle connection limit.
- `conn_max_idle_time` *(default `5m`)*: Time after which idle connections are closed.
- `conn_max_lifetime` *(default `30m`)*: Maximum lifetime for each connection.

### HTTP (Fetcher)
- `timeout` *(default `10s`)*: HTTP client timeout for network fetches.
- `max_redirects` *(default `5`)*: Redirects followed before aborting.
- `max_body_bytes` *(default `5MB`)*: Response size limit.
- `user_agent` *(default `BullnoseCrawler/0.1`)*: UA header for fetcher and robots.

### Worker
- `pool_size` *(default `8`)*: Number of goroutines processing tasks.
- `per_host_limit` *(default `2`)*: Concurrent requests per host (HTTP fetcher limiter).
- `per_host_rps` *(default `2`)*: Token bucket rate limit per host.
- `max_retries` *(default `3`)*: Attempts before a task is marked failed.
- `backoff_base` *(default `500ms`)* / `backoff_max` *(default `10s`)*: Exponential backoff window with jitter for retries.
- `lease_timeout` *(default `60s`)*: Duration before a stuck task can be claimed by another worker.
- `heartbeat_interval` *(default `10s`)*: Frequency for heartbeat updates while processing.

### Job Defaults
These apply when a request does not override them.

- `max_tasks` *(default `5000`)*: Cap on number of tasks per job.
- `max_depth` *(default `2`)*: Maximum crawl depth.
- `max_duration` *(default `15m`)*: Maximum runtime window before auto-cancel.
- `max_bytes` *(default `100MB`)*: Maximum content size per document.
- `ttl` *(default `24h`)*: Time-to-live for job data before cleanup.
- `deadline` *(default `15m`)*: Deadline tracked on job rows.

### Crawl
- `respect_robots` *(default `true`)*: If false, robots.txt is ignored.
- `robots_fail_open` *(default `true`)*: When robots fetch fails, allow crawling if true.
- `respect_nofollow` *(default `false`)*: If true, pages with meta `nofollow` have links dropped.
- `crawl_delay_ms` *(default `0`)*: Base delay between requests per host; robots delay overrides if larger.
- `strip_query` *(default `true`)*: Remove query strings when canonicalizing.
- `block_extensions` *(default `[".pdf", ...]`)*: Links with these extensions are skipped.
- `allowed_schemes` *(default `["http","https"]`)*: Schemes to follow.
- `dedupe_cache` *(default `false`)*: Global canonical URL cache. When enabled, seed URLs return `409` if already processed and crawl/map workers skip canonical duplicates across jobs.
- `include_sitemaps` *(default `false`)*: Worker seeds sitemap URLs at depth 0 when enabled.
- `sitemap_url_limit` *(default `1000`)*: Max sitemap URLs per job.
- `sitemap_max_nested` *(default `3`)*: Depth when traversing nested sitemap indexes.

### Browser
- `enabled` *(default `true`)*: Toggles Rod/Chromium rendering; when false, JS rendering is unavailable.
- `headless` *(default `true`)*: Whether to run Chromium headless.
- `pool_size` *(default `4`)*: Number of pre-created pages (render contexts).
- `per_host_limit` *(default `2`)*: Concurrent renderings per host; metrics expose in-flight counts per host.
- `nav_timeout` *(default `15s`)*: Navigation timeout for Rod; render failures increment Prometheus counters by host/stage.
- `wait_idle_timeout` *(default `3s`)*: Additional idle wait after load to allow content rendering.
- `executable_path` *(default empty)*: Custom Chromium/Chrome binary path.
- `user_agent` *(default empty)*: UA override for Rod sessions.
- `viewport.width`/`height` *(default `1280x720`)*: Browser viewport dimensions.
- `max_page_lifetime` *(default `60s`)*: Recycle/health-check interval for pool pages.

### Cleanup
- `interval` *(default `1h`)*: Base sweep interval for expiring jobs/data.
- `enabled` *(default `true`)*: Turns the cleanup loop on/off globally.
- `run_in` *(default empty list)*: Restricts cleanup to specific modes (`api`, `worker`, `all`). Empty means all.
- `api_interval` / `worker_interval` *(default `0s` => fallback to `interval`)*: Mode-specific overrides.

### Auth / Ratelimit (placeholders)
- `auth.enabled` *(default `false`)*: Reserved for future auth support.
- `ratelimit.enabled` *(default `false`)*: Reserved for future rate limiting.

### Misc
- Additional sections (e.g., future Redis or observability toggles) should follow the same pattern; refer to `config.example.yaml` for a complete list and defaults sourced from `internal/config/config.go`.

## API Examples

Assuming the API server listens on `localhost:8080`.

### Output Formats

All endpoints that accept `output_format` now support either a single string or an array of formats. Supported values are `markdown` (default), `html`, and `text`. Responses return an `outputs` array ordered by your requested preference (first entry is the primary). Each entry is `{format, content}`. Stored documents and export streams embed the same structure so downstream tools can choose whichever representation they need without re-rendering.

### Scrape (synchronous)

- `url` *(required)*: page to fetch.
- `output_format` *(optional)*: string or list (see above); defaults to `markdown`.
- `use_js` *(optional, default `false`)*: render with headless Chromium before extraction.

**Minimal request**
```bash
curl -X POST http://localhost:8080/v1/scrape \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com","output_format":"text"}' | jq
```

**Full request (multiple outputs + JS)**
```bash
curl -X POST http://localhost:8080/v1/scrape \
  -H "Content-Type: application/json" \
  -d '{
        "url":"https://example.com",
        "output_format":["markdown","html","text"],
        "use_js":true
      }' | jq
```

**Response shape**
```json
{
  "outputs": [
    {"format":"markdown","content":"..."},
    {"format":"html","content":"<body>...</body>"},
    {"format":"text","content":"...plain text..."}
  ],
  "meta": {
    "title": "Example Domain",
    "site_name": "",
    "language": "en",
    "description": "",
    "created_at": "2025-11-30T04:32:21Z"
  }
}
```

### Batch Scrape (asynchronous)

- `urls` *(required)*: array of URLs to scrape independently.
- `output_format` *(optional)*: single format or list; defaults to `markdown`.
- `use_js` *(optional)*: render each URL using the browser pool.

**Minimal request**
```bash
curl -X POST http://localhost:8080/v1/batch/scrape \
  -H "Content-Type: application/json" \
  -d '{"urls":["https://example.com","https://example.com/about"],"output_format":"text"}' | jq
```

**Full request (multi-format)**
```bash
curl -X POST http://localhost:8080/v1/batch/scrape \
  -H "Content-Type: application/json" \
  -d '{
        "urls":["https://example.com","https://example.com/about"],
        "output_format":["markdown","text"],
        "use_js":true
      }' | jq
```

**Job status**
```bash
curl -s http://localhost:8080/v1/jobs/{job_id} | jq
```
Sample:
```json
{
  "id": "479f0f9d-4875-4781-85c5-8d2ca3996c5d",
  "type": "batch_scrape",
  "status": "completed",
  "created_at": "2025-11-29T23:15:49.766528-05:00",
  "expires_at": "2025-11-30T23:15:49.766528-05:00",
  "stats": {
    "queued": 0,
    "processing": 0,
    "done": 2,
    "failed": 0,
    "skipped": 0,
    "total": 2,
    "attempts": 0
  }
}
```

**Documents (results)**
```bash
curl -s "http://localhost:8080/v1/jobs/{job_id}/documents?limit=50&sort=asc&sort_by=created_at" | jq
```
Response:
```json
{
  "items": [
    {
      "id": 9,
      "job_id": "{job_id}",
      "outputs": [
        {"format":"text","content":"..."},
        {"format":"markdown","content":"..."}
      ],
      "meta": {
        "crawl_url": "https://example.com",
        "created_at": "2025-11-30T04:45:41Z",
        "expires_at": "2025-11-30T23:45:40.159217-05:00",
        "site_name": "",
        "description": "",
        "language": "en",
        "title": "Example Domain"
      }
    }
  ],
  "next_cursor": 9
}
```
Use `after={next_cursor}` to paginate forward.

### Crawl (asynchronous traversal)

- `url` *(required)*: initial seed URL.
- `max_depth` *(optional, default `job_defaults.max_depth`)*: traversal depth.
- `same_domain` *(optional, default `false`)*: restrict links to same domain/subdomain.
- `output_format` *(optional)*: single or multiple formats (default `markdown`).
- `use_js` *(optional)*: render each page.
- `include_sitemaps` *(optional, default `false`)*: enqueue sitemap URLs at depth 0.

**Minimal request**
```bash
curl -X POST http://localhost:8080/v1/crawl \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com","output_format":["markdown","text"]}' | jq
```

**Full request**
```bash
curl -X POST http://localhost:8080/v1/crawl \
  -H "Content-Type: application/json" \
  -d '{
        "url":"https://example.com",
        "max_depth":2,
        "same_domain":true,
        "output_format":["markdown","text","html"],
        "use_js":true,
        "include_sitemaps":true
      }' | jq
```

**Job status & documents**
Use the same endpoints shown under batch scrape (`/v1/jobs/{id}` and `/v1/jobs/{id}/documents`)—crawl jobs include `crawl_url` plus `requested_url`/`final_url` metadata per document.

**NDJSON export (documents)**
```bash
curl -s "http://localhost:8080/v1/jobs/{job_id}/export?type=documents&all=true" \
  -H "Accept: application/x-ndjson"
```
Sample lines (pretty-printed for clarity):
```json
{"id":9,"job_id":"{job_id}","outputs":[{"format":"markdown","content":"..."},{"format":"text","content":"..."}],"meta":{"created_at":"...","title":"..."}}
{"type":"cursor","next_cursor":123,"sort":"asc","sort_by":"id"}
```
Each document line mirrors `/documents`, and the final line is always the cursor marker.

### Map (asynchronous link discovery)

- `url` *(required)*: seed page.
- `max_depth` *(optional)*: link discovery depth.
- `same_domain` *(optional)*: keep links scoped to seed host.
- `include_sitemaps` *(optional)*: ingest sitemap URLs at depth 0.

**Minimal request**
```bash
curl -X POST http://localhost:8080/v1/map \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com","max_depth":1}' | jq
```

**Full request**
```bash
curl -X POST http://localhost:8080/v1/map \
  -H "Content-Type: application/json" \
  -d '{
        "url":"https://example.com",
        "max_depth":2,
        "same_domain":true,
        "include_sitemaps":true
      }' | jq
```

**Job status**
Same `/v1/jobs/{id}` endpoint (type will be `map`).

**Map results**
```bash
curl -s "http://localhost:8080/v1/jobs/{job_id}/map?limit=50&sort=asc" | jq
```
Response:
```json
{
  "items": [
    {
      "id": 1,
      "job_id": "{job_id}",
      "source_url": "https://example.com",
      "target_url": "https://example.com/docs",
      "depth": 1,
      "created_at": "2025-11-30T12:55:46.107027-05:00"
    }
  ],
  "next_cursor": 50,
  "links": [
    "https://example.com/",
    "https://example.com/docs"
  ]
}
```

## Observability

- `/metrics` exposes Prometheus metrics (HTTP, worker, fetch/render timings, browser render failures, per-host pool usage, cleanup sweeps).
- `/healthz` is a simple liveness endpoint, `/readyz` checks DB + browser readiness.

## Development Notes

- Refer to `Tasks.md` for ongoing refactor items. When adding new config fields, keep `config.example.yaml`, README tables, and `internal/config/config.go` defaults in sync. Consider generating docs from struct tags to reduce drift (TODO).

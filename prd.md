## **1. Product Overview**

This product is a Go-based web data ingestion system that provides:

1. **Scrape** – Synchronous scrape of a single URL.
2. **Batch Scrape** – Asynchronous scraping of multiple URLs (job-based).
3. **Crawl** – Asynchronous website crawl with content extraction (job-based).
4. **Map** – Asynchronous website link discovery without content extraction (job-based).

Supports multiple output formats:

* Markdown
* HTML
* Plain text

Provides integrated HTML extraction, metadata capture, JS rendering (Rod), job management, and worker processing.
Uses a **single binary** (`crawler`) that runs in either **API** or **WORKER** mode.

---

# **2. Tech Stack**

* **Language:** Go 1.25+
* **Web Framework:** Fiber
* **Database:** PostgreSQL
* **Migrations:** Goose
* **DB Access:** sqlc (no ORM)
* **HTML Parsing:** goquery
* **JS Rendering:** Rod + headless Chromium
* **Queue:** PostgreSQL (`crawl_tasks` table + SKIP LOCKED)
* **Deployment:** Single binary with `--mode` flag

  * `crawler --mode=api`
  * `crawler --mode=worker`

---

# **3. System Architecture**

### **3.1 Components**

* **API service (Fiber)**
  Accepts requests, creates jobs, exposes job status, and exposes results.

* **Worker service**
  Continuously dequeues tasks, fetches URLs, renders pages, extracts content, creates documents, and enqueues new crawl tasks.

* **Common subsystems**

  * DB access layer (sqlc)
  * Extractor (HTML → Extracted struct)
  * Formatter (Extracted → markdown | html | text)
  * Crawler (net/http + Rod)
  * Job/task lifecycle manager

### **3.2 Single Binary Design**

* `crawler --mode=api`
* `crawler --mode=worker`
* `crawler --mode=all` (optional dev mode)

Binary selects which subsystem to start.

---

# **4. Feature Requirements**

## **4.1 Scrape (Synchronous)**

### **Endpoint**

`POST /v1/scrape`

### **Request**

```json
{
  "url": "https://example.com",
  "output_format": "markdown",
  "use_js": false
}
```

### **Behavior**

* Fetch a single page.
* Use Rod only if `use_js=true`.
* Extract content using extractor.
* Return formatted output immediately (no job created).

### **Response**

```json
{
  "url": "...",
  "title": "...",
  "content": "...",
  "meta": { ... }
}
```

---

## **4.2 Batch Scrape (Async Job)**

### **Endpoint**

`POST /v1/batch/scrape`

### **Request**

```json
{
  "urls": ["https://a.com", "https://b.com"],
  "output_format": "markdown",
  "use_js": false
}
```

### **Behavior**

* Create a `job` record (`type=batch_scrape`).
* Add each URL as a `crawl_task` with depth=0.
* Worker processes tasks until done.
* No link traversal.

### **Results Endpoints**

* `GET /v1/jobs/{id}`
* `GET /v1/jobs/{id}/documents`

---

## **4.3 Crawl (Async, website traversal)**

### **Endpoint**

`POST /v1/crawl`

### **Request**

```json
{
  "url": "https://example.com",
  "max_depth": 2,
  "same_domain": true,
  "output_format": "markdown",
  "use_js": false
}
```

### **Behavior**

* Create a `job` (`type=crawl`).
* Seed initial `crawl_task`.
* Worker:

  * Fetch page
  * Extract content + metadata
  * Store in `documents`
  * Discover links
  * Filter by:

    * same domain?
    * depth < max_depth?
  * Enqueue discovered links as new tasks.

### **Results**

* Same endpoints as batch scrape.

---

## **4.4 Map (Async, link discovery only)**

### **Endpoint**

`POST /v1/map`

### **Request**

```json
{
  "url": "https://example.com",
  "max_depth": 2,
  "same_domain": true
}
```

### **Behavior**

* Create `job` (`type=map`)
* Worker fetches pages
* Extract links only
* Insert into `mapped_urls`
* Enqueue new crawl_tasks for discovery
* No document extraction

### **Results Endpoint**

`GET /v1/jobs/{id}/map`

---

# **5. Data Model (Postgres)**

### **jobs**

```
id (uuid PK)
status (queued | running | completed | completed_with_errors | failed | cancelled)
type (scrape | batch_scrape | crawl | map)
params (jsonb)
error (text)
created_at
updated_at
```

### **crawl_tasks**

```
id (bigserial PK)
job_id (uuid FK)
requested_url (text)
final_url (text)
depth (int)
status (queued | processing | done | failed | skipped)
error (text)
processing_started_at (timestamp)
heartbeat_at (timestamp)
timestamps
UNIQUE(job_id, final_url)
```

### **documents**

```
id (bigserial PK)
job_id (uuid FK)
requested_url (text)
final_url (text)
title (text)
content_md (text)
content_raw (text)
meta (jsonb)
timestamps
```

### **mapped_urls**

```
id (bigserial PK)
job_id (uuid FK)
source_url (text)
target_url (text)
depth (int)
timestamps
```

---

# **6. Crawler Behavior**

### **6.1 Plain HTTP Fetch**

* Custom `http.Client`
* Timeouts
* Redirect limit
* Custom User-Agent
* Respect robots.txt (later)

### **6.2 Rod JS Rendering**

* Chromium headless
* Load page
* Wait for network idle
* Get final HTML snapshot
* Extract rendered DOM
* Configurable pool size, timeouts, viewport, and user agent via `config.yaml`
* Used when `use_js=true` or when forced by config; otherwise fall back to plain HTTP

### **6.3 Link Discovery**

* Normalize URLs
* Absolute URL resolution
* Filter out mailto/tel/javascript links
* Filter by domain/scope rules
* Deduplicate via `crawl_tasks(job_id,final_url)` unique constraint
* Strip query params and fragments for deduping; block common binary/media extensions

---

# **7. Extraction Pipeline**

### **Extractor Output Struct**

```go
type Extracted struct {
    URL      string
    Title    string
    Text     string
    HTML     string
    Meta     map[string]string
    Links    []string
}
```

### **Extraction Steps**

1. Parse HTML DOM with goquery.
2. Remove noise (`script`, `style`, `nav`, `footer`).
3. Extract:

   * title
   * meta description
   * visible text
   * cleaned body HTML
   * discovered links
4. Normalize whitespace.

---

# **8. Formatting Engine**

### **Output formats**

1. **markdown**
2. **html**
3. **text**

### **Dispatcher**

```go
Format(extracted, format) -> string
```

### **Markdown rules**

* `#` for title
* Paragraphs separated by blank lines
* No advanced formatting in v1

### **HTML**

* Cleaned HTML only

### **Text**

* Extracted plain text

---

# **9. Worker Logic**

### **Worker Loop**

* Repeatedly call `DequeueTask()` via sqlc:

  * Uses Postgres `FOR UPDATE SKIP LOCKED`
* Processes task:

  * Fetch page (JS or non-JS)
  * Extract content or links
* Save `documents` or `mapped_urls`
* Enqueue new discovered URLs (crawl/map only)
* Update task status → done/failed
* Periodically extend task heartbeat to keep the lease; timed-out tasks are retried by other workers
* After each task, check:

  * If the job has zero pending tasks:

    * Mark job `completed`, `completed_with_errors`, or `failed`

### **Concurrency**

* Configurable worker pool (goroutines) plus per-host concurrency cap and per-host rate limit
* Task lease/heartbeat with configurable timeout and tick interval

---

# **10. API Endpoints**

### **POST /v1/scrape**

Synchronous scrape.

### **POST /v1/batch/scrape**

Create job + tasks.

### **POST /v1/crawl**

Create job + tasks.

### **POST /v1/map**

Create job + tasks.

### **GET /v1/jobs/{id}**

Return job status + counts.

### **GET /v1/jobs/{id}/documents**

Paginated document listing.

### **GET /v1/jobs/{id}/map**

Return discovered URLs.

### **GET /v1/jobs/{id}/export**

Streams NDJSON export of documents or mapped URLs (`type=documents|map`).

---

# **11. Config + Execution**

### **Binary Execution**

```
crawler --mode=api
crawler --mode=worker
crawler --mode=all
```

### **Shared Components**

* DB connection
* Rod crawler instance
* Extractor
* Worker queue manager
* Config loader (`config.yaml` + env + flags)

### **Environment Variables**

* DATABASE_URL
* HTTP_ADDR
* WORKER_CONCURRENCY
* BROWSER_HEADLESS
* BROWSER_TIMEOUT
* USE_JS_DEFAULT
* CONFIG_PATH (optional)

### **Config Defaults (config.yaml)**

* HTTP client: `timeout=10s`, `max_redirects=5`, `max_body_bytes=5MB`, configurable user agent.
* Worker: `pool_size=8`, per-host concurrency=2, per-host rate=2 rps, retries with jittered exponential backoff (base 500ms, max 10s).
* Crawl: respect robots.txt, fail-open on robots fetch errors, optional nofollow, crawl-delay override, strip query params/fragments, block common binaries.
* Browser: enabled, headless, pool size for Rod contexts/tabs, nav/idle timeouts, viewport/user agent configurable.
* Jobs: max_depth, max_tasks, max_duration, max_bytes defaults to prevent runaway crawls.

---

# **12. Non-Goals (v1)**

Explicitly **not** supported in v1:

* PDF generation
* Screenshot capture
* Automatic sitemap ingest
* Cookie/session persistence
* Browser fingerprinting evasion
* Proxy rotation
* Authentication flows
* JavaScript error tolerance logic
* Distributed multi-node browser pools

---

# **13. Success Criteria**

A working system should:

* Correctly scrape single pages synchronously.
* Run batch/crawl/map tasks reliably via worker.
* Handle at least 10–30 pages per second with static fetch.
* Handle JS-rendered pages with Rod.
* Persist results in Postgres.
* Expose stable API endpoints for job management.
* Run as a single binary with mode selection.

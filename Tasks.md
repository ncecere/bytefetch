# Refactor Task List

## 1. Route Handler Cleanup
- [x] Extract a reusable helper for creating jobs + seeding tasks (handles TTL/deadline, dedupe checks, canonicalization) used by `/batch/scrape`, `/crawl`, `/map`.
- [x] Introduce a shared helper for formatting document responses (JSON + NDJSON) to eliminate duplicated code paths.
- [x] Move `jobStore` interface/struct definitions into their own file or reuse `store.Store` directly, simplifying the handler dependencies.

## 2. Worker Modularization
- [x] Split `processDocumentTask` into smaller helpers: robots check, fetch/extract/format, metadata enrichment, content storage, dedupe marking.
- [x] Extract sitemap enqueue logic + dedupe filtering into dedicated functions with unit tests.
- [x] Wrap map job link insertion (`InsertMapped` + task creation) in a single transactional helper to avoid per-link DB calls.

## 3. Store Layer Improvements
- [x] Create helper(s) to assemble `ORDER BY` clauses for pagination to reduce dynamic SQL duplication.
- [x] Add a `ShouldSkipSeed`/`SkipDuplicates` helper centralizing dedupe cache usage for API + worker.
- [x] Batch insert mapped URLs (e.g., new `InsertMappedBatch`) for performance and consistency.

## 4. Fetcher/Renderer Enhancements
- [x] Simplify `Fetcher.Fetch` signature so it reads `browserCfg` from the struct (remove extra parameter).
- [x] Add Prometheus counters/gauges for render failures/timeouts or tie into existing metrics.
- [x] Ensure browser host in-flight metrics cleanly remove labels when hosts go idle (optional if current behavior suffices).

## 5. Configuration/Documentation
- [x] Document new helpers/behavior in README once refactors land (e.g., dedupe helper semantics, batching).
- [x] Consider generating config docs or adding struct comments to avoid drift; at minimum, add TODO with plan.

## 6. Testing
- [x] Add/adjust unit tests covering the new helper functions (document formatter, dedupe helper, order clause builder).
- [x] Re-run `go test ./...` and update any test fixtures impacted by refactors.

# Format Enhancements Roadmap

## 1. Summary Format (`summary`)
- [ ] Add config for OpenAI-compatible summarizer (base URL, API key, default model) and surface via YAML/env.
- [ ] Implement client helper that sends extracted text to the configured API and returns a summary.
- [ ] Extend request validation + formatter to support `"summary"` and persist summarized content.
- [ ] Add tests + README instructions for configuring the summarizer.

## 2. Screenshot Format (`screenshot`)
- [ ] Define API contract for screenshot options (fullPage, quality, viewport) and extend requests schema.
- [ ] Enhance renderer/fetcher to capture screenshots (Rod `Screenshot`), respecting options.
- [ ] Store screenshot blobs/URLs in documents and expose via export endpoints.

## 3. Links Format (`links`)
- [ ] Add extractor logic to capture link metadata (href + text + rel attributes).
- [ ] Implement a formatter that returns only the link list when `output_format="links"`.
- [ ] Document new format and add endpoint tests.

## 4. JSON Format (`json`)
- [ ] Define JSON schema for structured output (content, metadata, links, images, screenshot info).
- [ ] Add formatter that emits JSON per document and update export endpoints to bypass Markdown/text conversion when requested.
- [ ] Update README/export docs to describe the JSON structure.

## 5. Images Format (`images`)
- [ ] Enhance extractor to collect image URLs (src, alt, title, dimensions if available).
- [ ] Add formatter that returns only image data for `output_format="images"` (possibly with download option).
- [ ] Cover with tests and README updates.

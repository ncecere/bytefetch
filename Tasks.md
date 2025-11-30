# Remaining Tasks

- [x] Testing / Benchmarks
  - Added unit tests for fetcher, extractor, worker helpers, and API integration tests for scrape/documents endpoints.

- [x] Scope / Robots Enhancements
  - Added optional sitemap ingestion (config + per-job flag) plus nofollow enforcement wiring.

- [x] Cleanup Controls
  - Cleanup runner now supports per-mode enablement and intervals via config.

- [x] Pagination / Export Polish
  - Added `sort_by` support, improved cursor handling, and emit next cursor metadata inside NDJSON streams.

- [x] Cross-Job Dedupe Seeds
  - Skip job creation when the seed URL already exists in the dedupe cache (returns 409).

- [x] Browser Pool Monitoring
  - Added pool usage gauges, health-check counters, and per-host inflight metrics for the renderer.

package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ncecere/bullnose/internal/config"
	"github.com/ncecere/bullnose/internal/fetch"
	"github.com/ncecere/bullnose/internal/store"
)

func TestScrapeRouteReturnsContent(t *testing.T) {
	app := setupTestApp(t, &fakeStore{job: &store.Job{ID: "job-1"}}, &fakeFetcher{
		resp: &fetch.Result{
			Body:     []byte("<html><body><p>Hello</p></body></html>"),
			FinalURL: "https://example.com",
		},
	})
	body := map[string]any{
		"url":           "https://example.com",
		"output_format": "text",
		"use_js":        false,
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/scrape", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("scrape request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
	var parsed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if parsed["content"] == "" {
		t.Fatalf("expected content in response, got %v", parsed)
	}
}

func TestDocumentsRouteProvidesCursor(t *testing.T) {
	now := time.Now()
	docMeta, _ := json.Marshal(map[string]string{"language": "en"})
	st := &fakeStore{
		job: &store.Job{
			ID:        "job-doc",
			ExpiresAt: sql.NullTime{Valid: true, Time: now.Add(time.Hour)},
		},
		documents: []store.DocumentRow{
			{ID: 1, JobID: "job-doc", RequestedURL: "https://example.com", FinalURL: "https://example.com", ContentMD: "a", ContentRaw: "a", Meta: docMeta},
			{ID: 2, JobID: "job-doc", RequestedURL: "https://example.com/about", FinalURL: "https://example.com/about", ContentMD: "b", ContentRaw: "b", Meta: docMeta},
		},
	}
	app := setupTestApp(t, st, &fakeFetcher{})
	req := httptest.NewRequest("GET", "/v1/jobs/job-doc/documents", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("documents request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
	var parsed struct {
		Items      []map[string]any `json:"items"`
		NextCursor float64          `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if parsed.NextCursor != 2 {
		t.Fatalf("expected next_cursor 2 got %v", parsed.NextCursor)
	}
	if len(parsed.Items) != 2 {
		t.Fatalf("expected 2 items got %d", len(parsed.Items))
	}
}

func setupTestApp(t *testing.T, st jobStore, fetcher contentFetcher) *fiber.App {
	t.Helper()
	app := fiber.New()
	cfg := &config.Config{
		Crawl: config.CrawlConfig{
			StripQuery: true,
		},
		JobDefaults: config.JobDefaults{
			TTL:      time.Hour,
			Deadline: 10 * time.Minute,
		},
	}
	registerRoutes(app, HandlerDeps{
		Store:   st,
		Fetcher: fetcher,
		Config:  cfg,
	})
	return app
}

type fakeStore struct {
	job       *store.Job
	documents []store.DocumentRow
}

func (f *fakeStore) DB() *sql.DB { return nil }

func (f *fakeStore) InsertJob(ctx context.Context, tx *sql.Tx, jobType string, params []byte, expiresAt, deadline time.Time) (string, error) {
	return "job-new", nil
}

func (f *fakeStore) InsertTasks(ctx context.Context, tx *sql.Tx, jobID string, tasks []store.Task) error {
	return nil
}

func (f *fakeStore) HasSeenCanonical(ctx context.Context, url string) (bool, error) {
	return false, nil
}

func (f *fakeStore) GetJob(ctx context.Context, jobID string) (*store.Job, error) {
	if f.job != nil {
		return f.job, nil
	}
	return &store.Job{ID: jobID}, nil
}

func (f *fakeStore) JobStats(ctx context.Context, jobID string) (store.JobStats, error) {
	return store.JobStats{}, nil
}

func (f *fakeStore) ListDocuments(ctx context.Context, jobID string, limit, offset int, sort, orderBy string) ([]store.DocumentRow, error) {
	return f.documents, nil
}

func (f *fakeStore) ListDocumentsAfter(ctx context.Context, jobID string, limit int, afterCursor int64, sort, orderBy string) ([]store.DocumentRow, error) {
	return nil, nil
}

func (f *fakeStore) ListMapped(ctx context.Context, jobID string, limit, offset int, sort, orderBy string) ([]store.MappedRow, error) {
	return nil, nil
}

func (f *fakeStore) ListMappedAfter(ctx context.Context, jobID string, limit int, afterCursor int64, sort, orderBy string) ([]store.MappedRow, error) {
	return nil, nil
}

type fakeFetcher struct {
	resp *fetch.Result
	err  error
}

func (f *fakeFetcher) Fetch(ctx context.Context, rawURL string, useJS bool) (*fetch.Result, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &fetch.Result{FinalURL: rawURL, Body: []byte("<html></html>")}, nil
}

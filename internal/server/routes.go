package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/nickcecere/bullnose/internal/config"
	"github.com/nickcecere/bullnose/internal/extract"
	"github.com/nickcecere/bullnose/internal/fetch"
	"github.com/nickcecere/bullnose/internal/format"
	"github.com/nickcecere/bullnose/internal/store"
	"github.com/nickcecere/bullnose/internal/types"
	"github.com/nickcecere/bullnose/internal/urlutil"
)

type HandlerDeps struct {
	Store   jobStore
	Fetcher contentFetcher
	Config  *config.Config
}

type jobStore interface {
	DB() *sql.DB
	InsertJob(ctx context.Context, tx *sql.Tx, jobType string, params []byte, expiresAt, deadline time.Time) (string, error)
	InsertTasks(ctx context.Context, tx *sql.Tx, jobID string, tasks []store.Task) error
	HasSeenCanonical(ctx context.Context, url string) (bool, error)
	GetJob(ctx context.Context, jobID string) (*store.Job, error)
	JobStats(ctx context.Context, jobID string) (store.JobStats, error)
	ListDocuments(ctx context.Context, jobID string, limit, offset int, sort, orderBy string) ([]store.DocumentRow, error)
	ListDocumentsAfter(ctx context.Context, jobID string, limit int, afterCursor int64, sort, orderBy string) ([]store.DocumentRow, error)
	ListMapped(ctx context.Context, jobID string, limit, offset int, sort, orderBy string) ([]store.MappedRow, error)
	ListMappedAfter(ctx context.Context, jobID string, limit int, afterCursor int64, sort, orderBy string) ([]store.MappedRow, error)
}

type contentFetcher interface {
	Fetch(ctx context.Context, rawURL string, useJS bool) (*fetch.Result, error)
}

func registerRoutes(app *fiber.App, deps HandlerDeps) {
	v1 := app.Group("/v1")

	v1.Post("/scrape", func(c *fiber.Ctx) error {
		var req types.ScrapeRequest
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request")
		}
		if err := validateScrapeRequest(&req); err != nil {
			return badRequest(c, err.Error())
		}
		ctx := c.Context()
		res, err := deps.Fetcher.Fetch(ctx, req.URL, req.UseJS)
		if err != nil {
			return badRequest(c, err.Error())
		}
		ext, err := extract.Extract(res.FinalURL, res.Body)
		if err != nil {
			return internalError(c, err.Error())
		}
		content, err := format.FormatContent(ext, req.OutputFormat)
		if err != nil {
			return badRequest(c, err.Error())
		}
		reqID := reqID(c)
		log.Printf("[API] req_id=%s action=scrape url=%s format=%s js=%t", reqID, req.URL, req.OutputFormat, req.UseJS)
		return c.JSON(fiber.Map{
			"content": content,
			"meta":    ext.Meta,
		})
	})

	v1.Post("/batch/scrape", func(c *fiber.Ctx) error {
		var req types.BatchScrapeRequest
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request")
		}
		if err := validateBatchRequest(&req); err != nil {
			return badRequest(c, err.Error())
		}
		params, _ := json.Marshal(req)
		expiresAt := time.Now().Add(deps.Config.JobDefaults.TTL)
		deadline := time.Now().Add(deps.Config.JobDefaults.Deadline)
		tx, err := deps.Store.DB().BeginTx(c.Context(), nil)
		if err != nil {
			return internalError(c, err.Error())
		}
		jobID, err := deps.Store.InsertJob(c.Context(), tx, "batch_scrape", params, expiresAt, deadline)
		if err != nil {
			tx.Rollback()
			return internalError(c, err.Error())
		}
		tasks := make([]store.Task, 0, len(req.URLs))
		for _, u := range req.URLs {
			canon := u
			if cu, err := urlutil.Canonicalize(u, deps.Config.Crawl.StripQuery); err == nil {
				canon = cu
			}
			if deps.Config.Crawl.DedupeCache {
				if seen, err := deps.Store.HasSeenCanonical(c.Context(), canon); err == nil && seen {
					continue
				}
			}
			tasks = append(tasks, store.Task{
				RequestedURL: u,
				FinalURL:     canon,
				Depth:        0,
				Status:       store.TaskStatusQueued,
			})
		}
		if err := deps.Store.InsertTasks(c.Context(), tx, jobID, tasks); err != nil {
			tx.Rollback()
			return internalError(c, err.Error())
		}
		if err := tx.Commit(); err != nil {
			return internalError(c, err.Error())
		}
		log.Printf("[API] req_id=%s action=batch_scrape job_id=%s urls=%d", reqID(c), jobID, len(req.URLs))
		return c.JSON(fiber.Map{"job_id": jobID})
	})

	v1.Post("/crawl", func(c *fiber.Ctx) error {
		var req types.CrawlRequest
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request")
		}
		if err := validateCrawlRequest(&req); err != nil {
			return badRequest(c, err.Error())
		}
		finalURL := req.URL
		if cu, err := urlutil.Canonicalize(req.URL, deps.Config.Crawl.StripQuery); err == nil {
			finalURL = cu
		}
		if deps.Config.Crawl.DedupeCache {
			if seen, err := deps.Store.HasSeenCanonical(c.Context(), finalURL); err == nil && seen {
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"job_id": "",
					"error":  "seed URL already processed",
				})
			} else if err != nil {
				return internalError(c, err.Error())
			}
		}
		params, _ := json.Marshal(req)
		expiresAt := time.Now().Add(deps.Config.JobDefaults.TTL)
		deadline := time.Now().Add(deps.Config.JobDefaults.Deadline)
		tx, err := deps.Store.DB().BeginTx(c.Context(), nil)
		if err != nil {
			return internalError(c, err.Error())
		}
		jobID, err := deps.Store.InsertJob(c.Context(), tx, "crawl", params, expiresAt, deadline)
		if err != nil {
			tx.Rollback()
			return internalError(c, err.Error())
		}
		task := store.Task{
			RequestedURL: req.URL,
			FinalURL:     finalURL,
			Depth:        0,
			Status:       store.TaskStatusQueued,
		}
		if err := deps.Store.InsertTasks(c.Context(), tx, jobID, []store.Task{task}); err != nil {
			tx.Rollback()
			return internalError(c, err.Error())
		}
		if err := tx.Commit(); err != nil {
			return internalError(c, err.Error())
		}
		log.Printf("[API] req_id=%s action=crawl job_id=%s url=%s depth=%d", reqID(c), jobID, req.URL, req.MaxDepth)
		return c.JSON(fiber.Map{"job_id": jobID})
	})

	v1.Post("/map", func(c *fiber.Ctx) error {
		var req types.MapRequest
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request")
		}
		if err := validateMapRequest(&req); err != nil {
			return badRequest(c, err.Error())
		}
		finalURL := req.URL
		if cu, err := urlutil.Canonicalize(req.URL, deps.Config.Crawl.StripQuery); err == nil {
			finalURL = cu
		}
		if deps.Config.Crawl.DedupeCache {
			if seen, err := deps.Store.HasSeenCanonical(c.Context(), finalURL); err == nil && seen {
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"job_id": "",
					"error":  "seed URL already processed",
				})
			} else if err != nil {
				return internalError(c, err.Error())
			}
		}
		params, _ := json.Marshal(req)
		expiresAt := time.Now().Add(deps.Config.JobDefaults.TTL)
		deadline := time.Now().Add(deps.Config.JobDefaults.Deadline)
		tx, err := deps.Store.DB().BeginTx(c.Context(), nil)
		if err != nil {
			return internalError(c, err.Error())
		}
		jobID, err := deps.Store.InsertJob(c.Context(), tx, "map", params, expiresAt, deadline)
		if err != nil {
			tx.Rollback()
			return internalError(c, err.Error())
		}
		task := store.Task{
			RequestedURL: req.URL,
			FinalURL:     finalURL,
			Depth:        0,
			Status:       store.TaskStatusQueued,
		}
		if err := deps.Store.InsertTasks(c.Context(), tx, jobID, []store.Task{task}); err != nil {
			tx.Rollback()
			return internalError(c, err.Error())
		}
		if err := tx.Commit(); err != nil {
			return internalError(c, err.Error())
		}
		log.Printf("[API] req_id=%s action=map job_id=%s url=%s depth=%d", reqID(c), jobID, req.URL, req.MaxDepth)
		return c.JSON(fiber.Map{"job_id": jobID})
	})

	v1.Get("/jobs/:id", func(c *fiber.Ctx) error {
		jobID := c.Params("id")
		job, err := deps.Store.GetJob(c.Context(), jobID)
		if err != nil {
			return notFound(c, "job not found")
		}
		stats, err := deps.Store.JobStats(c.Context(), jobID)
		if err != nil {
			return internalError(c, err.Error())
		}
		return c.JSON(fiber.Map{
			"id":         job.ID,
			"status":     job.Status,
			"type":       job.Type,
			"expires_at": nullableTime(job.ExpiresAt),
			"deadline":   nullableTime(job.Deadline),
			"created_at": job.CreatedAt,
			"error":      job.Error.String,
			"stats":      stats,
		})
	})

	v1.Get("/jobs/:id/documents", func(c *fiber.Ctx) error {
		jobID := c.Params("id")
		job, err := deps.Store.GetJob(c.Context(), jobID)
		if err != nil {
			return notFound(c, "job not found")
		}
		limit, _, after := parsePaginationAndCursor(c)
		sort := c.Query("sort", "asc")
		orderBy := c.Query("sort_by", "id")
		var rows []store.DocumentRow
		if after > 0 {
			rows, err = deps.Store.ListDocumentsAfter(c.Context(), jobID, limit, after, sort, orderBy)
		} else {
			rows, err = deps.Store.ListDocuments(c.Context(), jobID, limit, 0, sort, orderBy)
		}
		if err != nil {
			return internalError(c, err.Error())
		}
		format := formatFromJob(job)
		resp := make([]documentResponse, 0, len(rows))
		var nextCursor int64
		for _, r := range rows {
			meta := buildMeta(r.Meta, job, r.RequestedURL, r.FinalURL)
			resp = append(resp, documentResponse{
				ID:      r.ID,
				JobID:   r.JobID,
				Content: r.ContentMD,
				Format:  format,
				Meta:    meta,
			})
		}
		if len(rows) > 0 {
			nextCursor = rows[len(rows)-1].ID
		}
		return c.JSON(fiber.Map{
			"items":       resp,
			"next_cursor": nextCursor,
		})
	})

	v1.Get("/jobs/:id/map", func(c *fiber.Ctx) error {
		jobID := c.Params("id")
		limit, _, after := parsePaginationAndCursor(c)
		sort := c.Query("sort", "asc")
		orderBy := c.Query("sort_by", "id")
		var rows []store.MappedRow
		var err error
		if after > 0 {
			rows, err = deps.Store.ListMappedAfter(c.Context(), jobID, limit, after, sort, orderBy)
		} else {
			rows, err = deps.Store.ListMapped(c.Context(), jobID, limit, 0, sort, orderBy)
		}
		if err != nil {
			return internalError(c, err.Error())
		}
		var nextCursor int64
		links := make([]string, 0, len(rows))
		seen := make(map[string]struct{}, len(rows))
		if len(rows) > 0 {
			nextCursor = rows[len(rows)-1].ID
			for _, r := range rows {
				if _, ok := seen[r.TargetURL]; ok {
					continue
				}
				seen[r.TargetURL] = struct{}{}
				links = append(links, r.TargetURL)
			}
		}
		return c.JSON(fiber.Map{
			"items":       rows,
			"next_cursor": nextCursor,
			"links":       links,
		})
	})

	v1.Get("/jobs/:id/export", func(c *fiber.Ctx) error {
		jobID := c.Params("id")
		typ := c.Query("type", "documents")
		limit, _, after := parsePaginationAndCursor(c)
		sort := c.Query("sort", "asc")
		orderBy := c.Query("sort_by", "id")
		all := c.QueryBool("all", false)
		c.Set("Content-Type", "application/x-ndjson")
		job, err := deps.Store.GetJob(c.Context(), jobID)
		if err != nil {
			return notFound(c, "job not found")
		}
		format := formatFromJob(job)

		switch typ {
		case "documents":
			var rows []store.DocumentRow
			cursor := after
			for {
				if cursor > 0 {
					rows, err = deps.Store.ListDocumentsAfter(c.Context(), jobID, limit, cursor, sort, orderBy)
				} else {
					rows, err = deps.Store.ListDocuments(c.Context(), jobID, limit, 0, sort, orderBy)
				}
				if err != nil {
					return internalError(c, err.Error())
				}
				if len(rows) == 0 {
					break
				}
				for _, r := range rows {
					meta := buildMeta(r.Meta, job, r.RequestedURL, r.FinalURL)
					out := documentResponse{
						ID:      r.ID,
						JobID:   r.JobID,
						Content: r.ContentMD,
						Format:  format,
						Meta:    meta,
					}
					line, _ := json.Marshal(out)
					c.Write(line)
					c.Write([]byte("\n"))
					cursor = r.ID
				}
				if !all {
					break
				}
			}
			if cursor > 0 {
				c.Set("X-Next-Cursor", fmt.Sprintf("%d", cursor))
			}
			writeCursorLine(c, cursor, sort, orderBy)
			return nil
		case "map":
			var rows []store.MappedRow
			cursor := after
			for {
				if cursor > 0 {
					rows, err = deps.Store.ListMappedAfter(c.Context(), jobID, limit, cursor, sort, orderBy)
				} else {
					rows, err = deps.Store.ListMapped(c.Context(), jobID, limit, 0, sort, orderBy)
				}
				if err != nil {
					return internalError(c, err.Error())
				}
				if len(rows) == 0 {
					break
				}
				for _, r := range rows {
					line, _ := json.Marshal(r)
					c.Write(line)
					c.Write([]byte("\n"))
					cursor = r.ID
				}
				if !all {
					break
				}
			}
			if cursor > 0 {
				c.Set("X-Next-Cursor", fmt.Sprintf("%d", cursor))
			}
			writeCursorLine(c, cursor, sort, orderBy)
			return nil
		default:
			return badRequest(c, "invalid export type")
		}
	})
}

func parsePaginationAndCursor(c *fiber.Ctx) (limit, offset int, after int64) {
	limit = 50
	offset = 0
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	if v := c.Query("after"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			after = n
		}
	}
	return
}

type documentResponse struct {
	ID      int64  `json:"id"`
	JobID   string `json:"job_id"`
	Content string `json:"content"`
	Format  string `json:"format"`
	Meta    any    `json:"meta"`
}

func formatFromJob(job *store.Job) string {
	var params map[string]any
	if len(job.Params) > 0 {
		_ = json.Unmarshal(job.Params, &params)
	}
	if v, ok := params["output_format"].(string); ok && v != "" {
		return v
	}
	return "markdown"
}

func nullableTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func buildMeta(raw []byte, job *store.Job, requestedURL, finalURL string) map[string]any {
	meta := make(map[string]any)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	meta["requested_url"] = requestedURL
	meta["final_url"] = finalURL
	if t := nullableTime(job.ExpiresAt); t != nil {
		meta["expires_at"] = t.Format(time.RFC3339)
	}
	return meta
}

func writeCursorLine(c *fiber.Ctx, cursor int64, sortDir, orderBy string) {
	meta := map[string]any{
		"type":        "cursor",
		"next_cursor": cursor,
		"sort":        sortDir,
		"sort_by":     orderBy,
	}
	line, _ := json.Marshal(meta)
	c.Write(line)
	c.Write([]byte("\n"))
}

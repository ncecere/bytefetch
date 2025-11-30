package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ncecere/bullnose/internal/extract"
	"github.com/ncecere/bullnose/internal/format"
	"github.com/ncecere/bullnose/internal/store"
	"github.com/ncecere/bullnose/internal/types"
)

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
		seeds := make([]taskSeed, 0, len(req.URLs))
		for _, u := range req.URLs {
			seeds = append(seeds, taskSeed{RequestedURL: u, Depth: 0})
		}
		jobID, err := createJobWithSeeds(c.Context(), deps, "batch_scrape", req, seeds)
		if err != nil {
			if err == errSeedAlreadyProcessed {
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{"job_id": "", "error": err.Error()})
			}
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
		jobID, err := createJobWithSeeds(c.Context(), deps, "crawl", req, []taskSeed{
			{RequestedURL: req.URL, Depth: 0, AbortIfDuplicate: deps.Config.Crawl.DedupeCache},
		})
		if err != nil {
			if err == errSeedAlreadyProcessed {
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"job_id": "",
					"error":  "seed URL already processed",
				})
			}
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
		jobID, err := createJobWithSeeds(c.Context(), deps, "map", req, []taskSeed{
			{RequestedURL: req.URL, Depth: 0, AbortIfDuplicate: deps.Config.Crawl.DedupeCache},
		})
		if err != nil {
			if err == errSeedAlreadyProcessed {
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"job_id": "",
					"error":  "seed URL already processed",
				})
			}
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
			resp = append(resp, documentFromRow(r, job, format))
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
					out := documentFromRow(r, job, format)
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

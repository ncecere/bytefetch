package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	JobStatusQueued              = "queued"
	JobStatusRunning             = "running"
	JobStatusCompleted           = "completed"
	JobStatusCompletedWithErrors = "completed_with_errors"
	JobStatusFailed              = "failed"
	JobStatusCancelled           = "cancelled"
	TaskStatusQueued             = "queued"
	TaskStatusProcessing         = "processing"
	TaskStatusDone               = "done"
	TaskStatusFailed             = "failed"
	TaskStatusSkipped            = "skipped"
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// DB exposes the underlying connection for transactions where needed.
func (s *Store) DB() *sql.DB {
	return s.db
}

type Job struct {
	ID        string
	Status    string
	Type      string
	Params    []byte
	Error     sql.NullString
	ExpiresAt sql.NullTime
	Deadline  sql.NullTime
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Task struct {
	ID                  int64
	JobID               string
	RequestedURL        string
	FinalURL            string
	Depth               int
	Status              string
	Error               sql.NullString
	ProcessingStartedAt sql.NullTime
	HeartbeatAt         sql.NullTime
	Attempts            int
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type Document struct {
	JobID        string
	RequestedURL string
	FinalURL     string
	Title        string
	ContentMD    string
	ContentRaw   string
	Meta         []byte
}

type DocumentRow struct {
	ID           int64     `json:"id"`
	JobID        string    `json:"job_id"`
	RequestedURL string    `json:"requested_url"`
	FinalURL     string    `json:"final_url"`
	Title        string    `json:"title"`
	ContentMD    string    `json:"content_md"`
	ContentRaw   string    `json:"content_raw"`
	Meta         []byte    `json:"meta"`
	CreatedAt    time.Time `json:"created_at"`
}

type MappedInsert struct {
	JobID     string
	SourceURL string
	TargetURL string
	Depth     int
}

type MappedRow struct {
	ID        int64     `json:"id"`
	JobID     string    `json:"job_id"`
	SourceURL string    `json:"source_url"`
	TargetURL string    `json:"target_url"`
	Depth     int       `json:"depth"`
	CreatedAt time.Time `json:"created_at"`
}

type JobStats struct {
	Queued     int `json:"queued"`
	Processing int `json:"processing"`
	Done       int `json:"done"`
	Failed     int `json:"failed"`
	Skipped    int `json:"skipped"`
	Total      int `json:"total"`
	Attempts   int `json:"attempts"`
}

// InsertJob inserts a job with params JSONB.
func (s *Store) InsertJob(ctx context.Context, tx *sql.Tx, jobType string, params []byte, expiresAt, deadline time.Time) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO jobs (status, type, params, expires_at, deadline_at) VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, JobStatusQueued, jobType, params, expiresAt, deadline).Scan(&id)
	return id, err
}

// InsertTasks bulk inserts tasks for a job.
func (s *Store) InsertTasks(ctx context.Context, tx *sql.Tx, jobID string, tasks []Task) error {
	execer := execer{db: s.db, tx: tx}
	stmt, err := execer.PrepareContext(ctx, `
		INSERT INTO crawl_tasks (job_id, requested_url, final_url, depth, status)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (job_id, final_url) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range tasks {
		if _, err := stmt.ExecContext(ctx, jobID, t.RequestedURL, t.FinalURL, t.Depth, t.Status); err != nil {
			return err
		}
	}
	return nil
}

// DequeueTask claims a task using SKIP LOCKED and lease timeout. It returns sql.ErrNoRows when none available.
func (s *Store) DequeueTask(ctx context.Context, leaseTimeout time.Duration) (*Task, error) {
	threshold := time.Now().Add(-leaseTimeout)
	row := s.db.QueryRowContext(ctx, `
		WITH cte AS (
			SELECT id FROM crawl_tasks
			WHERE status IN ($1, $2)
			  AND (status = $1 OR heartbeat_at IS NULL OR heartbeat_at < $3)
			ORDER BY id
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE crawl_tasks AS ct
		SET status = $2,
			processing_started_at = COALESCE(processing_started_at, now()),
			heartbeat_at = now(),
			updated_at = now()
		FROM cte
		WHERE ct.id = cte.id
		RETURNING ct.id, ct.job_id, ct.requested_url, ct.final_url, ct.depth, ct.status, ct.error, ct.processing_started_at, ct.heartbeat_at, ct.attempts, ct.created_at, ct.updated_at
	`, TaskStatusQueued, TaskStatusProcessing, threshold)

	var t Task
	if err := row.Scan(
		&t.ID,
		&t.JobID,
		&t.RequestedURL,
		&t.FinalURL,
		&t.Depth,
		&t.Status,
		&t.Error,
		&t.ProcessingStartedAt,
		&t.HeartbeatAt,
		&t.Attempts,
		&t.CreatedAt,
		&t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateTaskFinalURL updates the final URL after redirects.
func (s *Store) UpdateTaskFinalURL(ctx context.Context, taskID int64, finalURL string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE crawl_tasks AS target
		SET final_url = $1, updated_at = now()
		WHERE target.id = $2
		  AND target.final_url <> $1
		  AND NOT EXISTS (
			SELECT 1 FROM crawl_tasks
			WHERE job_id = target.job_id
			  AND final_url = $1
			  AND id <> target.id
		  )
	`, finalURL, taskID)
	return err
}

// UpdateTaskHeartbeat extends the lease on a task.
func (s *Store) UpdateTaskHeartbeat(ctx context.Context, taskID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE crawl_tasks
		SET heartbeat_at = now(), updated_at = now()
		WHERE id = $1
	`, taskID)
	return err
}

// CompleteTask marks a task as done.
func (s *Store) CompleteTask(ctx context.Context, taskID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE crawl_tasks
		SET status = $1, updated_at = now()
		WHERE id = $2
	`, TaskStatusDone, taskID)
	return err
}

// FailTask marks a task as failed with an error message.
func (s *Store) FailTask(ctx context.Context, taskID int64, reason string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE crawl_tasks
		SET status = $1, error = $2, attempts = attempts + 1, updated_at = now()
		WHERE id = $3
	`, TaskStatusFailed, reason, taskID)
	return err
}

// RetryTask sets task back to queued and increments attempts.
func (s *Store) RetryTask(ctx context.Context, taskID int64, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE crawl_tasks
		SET status = $1, error = NULLIF($2,''), attempts = attempts + 1, heartbeat_at = NULL, updated_at = now()
		WHERE id = $3
	`, TaskStatusQueued, errMsg, taskID)
	return err
}

// StartJob sets job to running.
func (s *Store) StartJob(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET status = $1, updated_at = now() WHERE id = $2
	`, JobStatusRunning, jobID)
	return err
}

// UpdateJobStatus sets final job status and optional error.
func (s *Store) UpdateJobStatus(ctx context.Context, jobID, status, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET status = $1, error = NULLIF($2, ''), updated_at = now() WHERE id = $3
	`, status, errMsg, jobID)
	return err
}

// PendingTaskCounts returns remaining tasks per status for a job.
func (s *Store) PendingTaskCounts(ctx context.Context, jobID string) (queued, processing int, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = $1) AS queued,
			COUNT(*) FILTER (WHERE status = $2) AS processing
		FROM crawl_tasks
		WHERE job_id = $3
	`, TaskStatusQueued, TaskStatusProcessing, jobID).Scan(&queued, &processing)
	return
}

// HasFailedTasks reports whether a job has failed tasks.
func (s *Store) HasFailedTasks(ctx context.Context, jobID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM crawl_tasks WHERE job_id = $1 AND status = $2
	`, jobID, TaskStatusFailed).Scan(&count)
	return count > 0, err
}

// TaskCount returns total tasks for a job.
func (s *Store) TaskCount(ctx context.Context, jobID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM crawl_tasks WHERE job_id = $1
	`, jobID).Scan(&count)
	return count, err
}

// FinishJob determines and sets job completion status.
func (s *Store) FinishJob(ctx context.Context, jobID string) error {
	queued, processing, err := s.PendingTaskCounts(ctx, jobID)
	if err != nil {
		return err
	}
	if queued+processing > 0 {
		return errors.New("job still has pending tasks")
	}

	failed, err := s.HasFailedTasks(ctx, jobID)
	if err != nil {
		return err
	}

	status := JobStatusCompleted
	if failed {
		status = JobStatusCompletedWithErrors
	}
	return s.UpdateJobStatus(ctx, jobID, status, "")
}

// CancelExpiredJobs marks jobs past their expires_at as cancelled.
func (s *Store) CancelExpiredJobs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = $1, error = 'expired', updated_at = now()
		WHERE expires_at IS NOT NULL
		  AND expires_at < now()
		  AND status NOT IN ($1, $2, $3)
	`, JobStatusCancelled, JobStatusCompleted, JobStatusCompletedWithErrors)
	return err
}

// ListDocuments returns documents for a job with pagination.
func (s *Store) ListDocuments(ctx context.Context, jobID string, limit, offset int, sort, orderBy string) ([]DocumentRow, error) {
	order := orderDir(sort)
	orderClause := makeOrderClause(order, documentOrderColumn(orderBy))
	query := fmt.Sprintf(`
		SELECT id, job_id, requested_url, final_url, title, content_md, content_raw, meta, created_at
		FROM documents
		WHERE job_id = $1
		ORDER BY %s
		LIMIT $2 OFFSET $3
	`, orderClause)
	rows, err := s.db.QueryContext(ctx, query, jobID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DocumentRow
	for rows.Next() {
		var r DocumentRow
		if err := rows.Scan(&r.ID, &r.JobID, &r.RequestedURL, &r.FinalURL, &r.Title, &r.ContentMD, &r.ContentRaw, &r.Meta, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListDocumentsAfter returns documents with id > afterCursor (or < when sorting desc).
func (s *Store) ListDocumentsAfter(ctx context.Context, jobID string, limit int, afterCursor int64, sort, orderBy string) ([]DocumentRow, error) {
	order := orderDir(sort)
	orderClause := makeOrderClause(order, documentOrderColumn(orderBy))
	compare := ">"
	if order == "DESC" {
		compare = "<"
	}
	query := fmt.Sprintf(`
		SELECT id, job_id, requested_url, final_url, title, content_md, content_raw, meta, created_at
		FROM documents
		WHERE job_id = $1 AND id %s $2
		ORDER BY %s
		LIMIT $3
	`, compare, orderClause)
	rows, err := s.db.QueryContext(ctx, query, jobID, afterCursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DocumentRow
	for rows.Next() {
		var r DocumentRow
		if err := rows.Scan(&r.ID, &r.JobID, &r.RequestedURL, &r.FinalURL, &r.Title, &r.ContentMD, &r.ContentRaw, &r.Meta, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListMapped returns mapped URLs for a job with pagination.
func (s *Store) ListMapped(ctx context.Context, jobID string, limit, offset int, sort, orderBy string) ([]MappedRow, error) {
	order := orderDir(sort)
	orderClause := makeOrderClause(order, mapOrderColumn(orderBy))
	query := fmt.Sprintf(`
		SELECT id, job_id, source_url, target_url, depth, created_at
		FROM mapped_urls
		WHERE job_id = $1
		ORDER BY %s
		LIMIT $2 OFFSET $3
	`, orderClause)
	rows, err := s.db.QueryContext(ctx, query, jobID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MappedRow
	for rows.Next() {
		var r MappedRow
		if err := rows.Scan(&r.ID, &r.JobID, &r.SourceURL, &r.TargetURL, &r.Depth, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListMappedAfter returns mapped rows with id > afterCursor.
func (s *Store) ListMappedAfter(ctx context.Context, jobID string, limit int, afterCursor int64, sort, orderBy string) ([]MappedRow, error) {
	order := orderDir(sort)
	orderClause := makeOrderClause(order, mapOrderColumn(orderBy))
	compare := ">"
	if order == "DESC" {
		compare = "<"
	}
	query := fmt.Sprintf(`
		SELECT id, job_id, source_url, target_url, depth, created_at
		FROM mapped_urls
		WHERE job_id = $1 AND id %s $2
		ORDER BY %s
		LIMIT $3
	`, compare, orderClause)
	rows, err := s.db.QueryContext(ctx, query, jobID, afterCursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MappedRow
	for rows.Next() {
		var r MappedRow
		if err := rows.Scan(&r.ID, &r.JobID, &r.SourceURL, &r.TargetURL, &r.Depth, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func orderDir(sort string) string {
	if strings.ToLower(sort) == "desc" {
		return "DESC"
	}
	return "ASC"
}

func documentOrderColumn(orderBy string) string {
	switch strings.ToLower(orderBy) {
	case "created_at":
		return "created_at"
	case "final_url":
		return "final_url"
	default:
		return "id"
	}
}

func mapOrderColumn(orderBy string) string {
	switch strings.ToLower(orderBy) {
	case "created_at":
		return "created_at"
	case "target_url":
		return "target_url"
	default:
		return "id"
	}
}

func makeOrderClause(order string, column string) string {
	return fmt.Sprintf("%s %s, id %s", column, order, order)
}

// GetJob fetches a job by ID.
func (s *Store) GetJob(ctx context.Context, jobID string) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, status, type, params, error, expires_at, deadline_at, created_at, updated_at
		FROM jobs WHERE id = $1
	`, jobID)
	var j Job
	if err := row.Scan(&j.ID, &j.Status, &j.Type, &j.Params, &j.Error, &j.ExpiresAt, &j.Deadline, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return nil, err
	}
	return &j, nil
}

// JobStats returns task counts grouped by status for a job.
func (s *Store) JobStats(ctx context.Context, jobID string) (JobStats, error) {
	var stats JobStats
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = $1) AS queued,
			COUNT(*) FILTER (WHERE status = $2) AS processing,
			COUNT(*) FILTER (WHERE status = $3) AS done,
			COUNT(*) FILTER (WHERE status = $4) AS failed,
			COUNT(*) FILTER (WHERE status = $5) AS skipped,
			COUNT(*) AS total,
			COALESCE(SUM(attempts),0) AS attempts
		FROM crawl_tasks
		WHERE job_id = $6
	`, TaskStatusQueued, TaskStatusProcessing, TaskStatusDone, TaskStatusFailed, TaskStatusSkipped, jobID).Scan(
		&stats.Queued, &stats.Processing, &stats.Done, &stats.Failed, &stats.Skipped, &stats.Total, &stats.Attempts,
	)
	return stats, err
}

// InsertDocument stores an extracted document.
func (s *Store) InsertDocument(ctx context.Context, doc Document) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO documents (job_id, requested_url, final_url, title, content_md, content_raw, meta)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, doc.JobID, doc.RequestedURL, doc.FinalURL, doc.Title, doc.ContentMD, doc.ContentRaw, doc.Meta)
	return err
}

// HasSeenCanonical returns true if the canonical URL is recorded in dedupe_cache.
func (s *Store) HasSeenCanonical(ctx context.Context, url string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dedupe_cache WHERE canonical_url = $1`, url).Scan(&count)
	return count > 0, err
}

// MarkCanonical records the canonical URL in dedupe_cache.
func (s *Store) MarkCanonical(ctx context.Context, url string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dedupe_cache (canonical_url, last_seen) VALUES ($1, now())
		ON CONFLICT (canonical_url) DO UPDATE SET last_seen = EXCLUDED.last_seen
	`, url)
	return err
}

// InsertMapped inserts discovered links for map jobs.
func (s *Store) InsertMapped(ctx context.Context, jobID, source, target string, depth int) error {
	return s.InsertMappedBatch(ctx, nil, []MappedInsert{{
		JobID:     jobID,
		SourceURL: source,
		TargetURL: target,
		Depth:     depth,
	}})
}

// InsertMappedBatch bulk inserts mapped URLs, using optional transaction.
func (s *Store) InsertMappedBatch(ctx context.Context, tx *sql.Tx, entries []MappedInsert) error {
	if len(entries) == 0 {
		return nil
	}
	execer := execer{db: s.db, tx: tx}
	stmt, err := execer.PrepareContext(ctx, `
		INSERT INTO mapped_urls (job_id, source_url, target_url, depth)
		VALUES ($1, $2, $3, $4)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		if _, err := stmt.ExecContext(ctx, e.JobID, e.SourceURL, e.TargetURL, e.Depth); err != nil {
			return err
		}
	}
	return nil
}

type execer struct {
	db *sql.DB
	tx *sql.Tx
}

func (e execer) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	if e.tx != nil {
		return e.tx.PrepareContext(ctx, query)
	}
	return e.db.PrepareContext(ctx, query)
}

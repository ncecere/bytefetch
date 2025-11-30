package server

import (
	"context"
	"database/sql"
	"time"

	"github.com/nickcecere/bullnose/internal/config"
	"github.com/nickcecere/bullnose/internal/fetch"
	"github.com/nickcecere/bullnose/internal/store"
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

package server

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ncecere/bytefetch/internal/store"
	"github.com/ncecere/bytefetch/internal/urlutil"
)

type taskSeed struct {
	RequestedURL     string
	Depth            int
	AbortIfDuplicate bool
}

var errSeedAlreadyProcessed = errors.New("seed url already processed")

func createJobWithSeeds(ctx context.Context, deps HandlerDeps, jobType string, params any, seeds []taskSeed) (string, error) {
	paramBytes, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().Add(deps.Config.JobDefaults.TTL)
	deadline := time.Now().Add(deps.Config.JobDefaults.Deadline)
	tx, err := deps.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}

	jobID, err := deps.Store.InsertJob(ctx, tx, jobType, paramBytes, expiresAt, deadline)
	if err != nil {
		tx.Rollback()
		return "", err
	}

	tasks := make([]store.Task, 0, len(seeds))
	for _, seed := range seeds {
		finalURL := seed.RequestedURL
		if canon, err := urlutil.Canonicalize(seed.RequestedURL, deps.Config.Crawl.StripQuery); err == nil {
			finalURL = canon
		}
		if deps.Config.Crawl.DedupeCache {
			seen, err := store.ShouldSkipCanonical(ctx, deps.Store, finalURL)
			if err != nil {
				tx.Rollback()
				return "", err
			}
			if seen {
				if seed.AbortIfDuplicate {
					tx.Rollback()
					return "", errSeedAlreadyProcessed
				}
				continue
			}
		}
		tasks = append(tasks, store.Task{
			RequestedURL: seed.RequestedURL,
			FinalURL:     finalURL,
			Depth:        seed.Depth,
			Status:       store.TaskStatusQueued,
		})
	}

	if len(tasks) > 0 {
		if err := deps.Store.InsertTasks(ctx, tx, jobID, tasks); err != nil {
			tx.Rollback()
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return jobID, nil
}

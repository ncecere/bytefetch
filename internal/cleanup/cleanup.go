package cleanup

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ncecere/bytefetch/internal/metrics"
)

// Run deletes expired jobs and related data periodically.
func Run(ctx context.Context, connString string, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := sweep(ctx, connString); err != nil {
			log.Printf("cleanup sweep error: %v", err)
			metrics.CleanupErrorsTotal.Inc()
		} else {
			metrics.CleanupRunsTotal.Inc()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func sweep(ctx context.Context, connString string) error {
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	// Mark expired jobs as cancelled before delete to surface status if needed.
	if _, err := conn.Exec(ctx, `
		UPDATE jobs
		SET status = 'cancelled', error = 'expired', updated_at = now()
		WHERE expires_at IS NOT NULL
		  AND expires_at < now()
		  AND status NOT IN ('cancelled','completed','completed_with_errors')
	`); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `
		WITH expired_jobs AS (
			SELECT id FROM jobs WHERE expires_at IS NOT NULL AND expires_at < now()
			   OR (deadline_at IS NOT NULL AND deadline_at < now())
		)
		DELETE FROM jobs j USING expired_jobs e WHERE j.id = e.id
	`)
	return err
}

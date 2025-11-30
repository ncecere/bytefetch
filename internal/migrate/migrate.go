package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nickcecere/bullnose/internal/config"
	goose "github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// Run executes database migrations using embedded migration files.
func Run(ctx context.Context, cfg *config.Config) error {
	db, err := sql.Open("pgx", cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxIdleTime(cfg.Database.ConnMaxIdle)
	db.SetConnMaxLifetime(cfg.Database.ConnMaxLife)

	goose.SetBaseFS(embedMigrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}

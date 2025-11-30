package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/nickcecere/bullnose/internal/cleanup"
	"github.com/nickcecere/bullnose/internal/config"
	"github.com/nickcecere/bullnose/internal/migrate"
	"github.com/nickcecere/bullnose/internal/server"
	"github.com/nickcecere/bullnose/internal/worker"
)

func main() {
	mode := flag.String("mode", "api", "mode to run: api | worker | all")
	configPath := flag.String("config", "", "path to config file (default: config.yaml if present)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if err := migrate.Run(ctx, cfg); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	switch *mode {
	case "api":
		errCh := make(chan error, 2)
		go func() { errCh <- server.Start(ctx, cfg) }()
		if cfg.Cleanup.EnabledFor("api") {
			go func() {
				errCh <- cleanup.Run(ctx, cfg.Database.URL, cfg.Cleanup.IntervalFor("api"))
			}()
		}
		waitOrExit(ctx, errCh, "api mode")
	case "worker":
		errCh := make(chan error, 2)
		go func() { errCh <- worker.Start(ctx, cfg) }()
		if cfg.Cleanup.EnabledFor("worker") {
			go func() {
				errCh <- cleanup.Run(ctx, cfg.Database.URL, cfg.Cleanup.IntervalFor("worker"))
			}()
		}
		waitOrExit(ctx, errCh, "worker mode")
	case "all":
		// Run API, worker, and cleanup concurrently; wait for context cancellation.
		errCh := make(chan error, 3)
		go func() { errCh <- server.Start(ctx, cfg) }()
		go func() { errCh <- worker.Start(ctx, cfg) }()
		if cfg.Cleanup.EnabledFor("all") {
			go func() {
				errCh <- cleanup.Run(ctx, cfg.Database.URL, cfg.Cleanup.IntervalFor("all"))
			}()
		}
		waitOrExit(ctx, errCh, "all mode")
	default:
		log.Fatalf("unknown mode: %s", *mode)
	}

	// give goroutines a moment to shut down cleanly
	time.Sleep(200 * time.Millisecond)
}

func waitOrExit(ctx context.Context, errCh chan error, label string) {
	select {
	case <-ctx.Done():
		log.Printf("shutting down (%s)", label)
	case err := <-errCh:
		if err != nil {
			log.Fatalf("error in %s: %v", label, err)
		}
	}
}

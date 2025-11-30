package server

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/google/uuid"
	"github.com/ncecere/bullnose/internal/config"
	"github.com/ncecere/bullnose/internal/db"
	"github.com/ncecere/bullnose/internal/fetch"
	"github.com/ncecere/bullnose/internal/metrics"
	"github.com/ncecere/bullnose/internal/store"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

// Start runs the API server. Routes will be expanded as features land.
func Start(ctx context.Context, cfg *config.Config) error {
	sqlDB, err := db.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	renderer, err := fetch.NewRenderer(cfg.Browser)
	if err != nil {
		return err
	}
	defer renderer.Close()

	st := store.New(sqlDB)
	metrics.MustRegister()

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		reqID := c.Get("X-Request-Id")
		if reqID == "" {
			reqID = uuid.NewString()
			c.Request().Header.Set("X-Request-Id", reqID)
		}
		c.Set("X-Request-Id", reqID)
		c.Locals("req_id", reqID)
		return c.Next()
	})
	app.Use(func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		status := c.Response().StatusCode()
		path := c.Route().Path
		method := c.Method()
		metrics.RequestsTotal.WithLabelValues(path, method, fmt.Sprint(status)).Inc()
		metrics.RequestDuration.WithLabelValues(path, method).Observe(time.Since(start).Seconds())
		log.Printf("[API] req_id=%s method=%s path=%s status=%d duration_ms=%d", reqID(c), method, path, status, time.Since(start).Milliseconds())
		return err
	})
	app.Use(logger.New())
	app.Get("/metrics", func(c *fiber.Ctx) error {
		handler := fasthttpadaptor.NewFastHTTPHandler(promhttp.Handler())
		handler(c.Context())
		return nil
	})
	registerRoutes(app, HandlerDeps{
		Store:   st,
		Fetcher: fetch.NewFetcher(cfg.HTTP, renderer, cfg.Browser),
		Config:  cfg,
	})

	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	app.Get("/readyz", func(c *fiber.Ctx) error {
		if err := db.Ping(c.Context(), sqlDB); err != nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, "db not ready")
		}
		if err := renderer.Ready(c.Context()); err != nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, "browser not ready")
		}
		return c.SendString("ready")
	})

	errCh := make(chan error, 1)
	go func() {
		if err := app.Listen(cfg.Server.Addr); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("api server shutting down")
		return app.Shutdown()
	case err := <-errCh:
		return err
	}
}

func reqID(c *fiber.Ctx) string {
	if v := c.Get("X-Request-Id"); v != "" {
		return v
	}
	if v := c.Response().Header.Peek("X-Request-Id"); len(v) > 0 {
		return string(v)
	}
	return ""
}

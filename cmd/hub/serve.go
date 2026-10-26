package main

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/tereshed/8bitnews/internal/api"
	"github.com/tereshed/8bitnews/internal/config"
	"github.com/tereshed/8bitnews/internal/mcpapi"
	"github.com/tereshed/8bitnews/internal/observability"
	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/sources"
	"github.com/tereshed/8bitnews/internal/storage"
	"github.com/tereshed/8bitnews/internal/store"
)

// readHeaderTimeout, readTimeout and idleTimeout harden the HTTP server
// against slow/stalled clients holding a connection open. There is
// deliberately no WriteTimeout: the MCP streamable HTTP transport's
// standalone SSE stream is a long-lived response, and a global write
// deadline would cut it off mid-stream.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownTimeout   = 15 * time.Second
)

func runServe(_ []string) {
	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}

	logger := observability.NewLogger()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	media, err := storage.New(ctx, cfg.S3)
	if err != nil {
		fatal(err)
	}

	checker := sources.NewChecker(cfg.SourceCheckTimeout, cfg.SourceAllowPrivate)
	wayback := sources.NewWaybackClient(cfg.WaybackBaseURL, cfg.SourceCheckTimeout, cfg.WaybackAccessKey, cfg.WaybackSecretKey, cfg.SourceAllowPrivate)
	availability := sources.NewAvailabilityClient(cfg.WaybackAvailabilityBaseURL, cfg.SourceCheckTimeout, cfg.SourceAllowPrivate)

	svc := service.New(service.Deps{
		Store:                           st,
		Media:                           media,
		Checker:                         checker,
		Wayback:                         wayback,
		Availability:                    availability,
		Log:                             logger,
		SourcesConcurrency:              cfg.SourcesConcurrency,
		SourcesCheckDeadline:            cfg.SourcesCheckDeadline,
		SourcesBackgroundArchiveTimeout: cfg.SourcesBackgroundArchiveTimeout,
	})
	mcpHandler := mcpapi.NewHTTPHandler(svc)
	metrics := observability.NewMetrics()

	// requireMetricsAuth is false when HUB_METRICS_ADDR is set: /metrics is
	// then served unauthenticated on that separate address instead (below),
	// for deployments that scrape it from a private network interface.
	requireMetricsAuth := cfg.MetricsAddr == ""
	router := api.NewRouter(svc, mcpHandler, metrics, logger, cfg.APIKeys, requireMetricsAuth)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}

	if cfg.MetricsAddr != "" {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("GET /metrics", metrics.Handler())
		metricsSrv := &http.Server{
			Addr:              cfg.MetricsAddr,
			Handler:           metricsMux,
			ReadHeaderTimeout: readHeaderTimeout,
		}
		go func() {
			logger.Info("hub metrics listening", "addr", cfg.MetricsAddr)
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("metrics server failed", "error", err)
			}
		}()
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			_ = metricsSrv.Shutdown(shutdownCtx)
		}()
	}

	go func() {
		<-ctx.Done()
		logger.Info("hub shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}()

	logger.Info("hub listening", "addr", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal(err)
	}
}

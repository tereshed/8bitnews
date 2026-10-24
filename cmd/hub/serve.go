package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/tereshed/8bitnews/internal/api"
	"github.com/tereshed/8bitnews/internal/config"
	"github.com/tereshed/8bitnews/internal/mcpapi"
	"github.com/tereshed/8bitnews/internal/observability"
	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/sources"
	"github.com/tereshed/8bitnews/internal/storage"
	"github.com/tereshed/8bitnews/internal/store"
)

func runServe(_ []string) {
	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}

	logger := observability.NewLogger()
	ctx := context.Background()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	media, err := storage.New(ctx, cfg.S3)
	if err != nil {
		fatal(err)
	}

	checker := sources.NewChecker(cfg.SourceCheckTimeout)
	wayback := sources.NewWaybackClient(cfg.WaybackBaseURL, cfg.SourceCheckTimeout)

	svc := service.New(st, media, checker, wayback, logger)
	mcpHandler := mcpapi.NewHTTPHandler(svc)
	metrics := observability.NewMetrics()

	router := api.NewRouter(svc, mcpHandler, metrics, logger, cfg.APIKeys)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: router}
	logger.Info("hub listening", "addr", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal(err)
	}
}

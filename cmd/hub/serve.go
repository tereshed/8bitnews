package main

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/tereshed/8bitnews/internal/admin"
	"github.com/tereshed/8bitnews/internal/api"
	"github.com/tereshed/8bitnews/internal/config"
	"github.com/tereshed/8bitnews/internal/costs"
	"github.com/tereshed/8bitnews/internal/mcpapi"
	"github.com/tereshed/8bitnews/internal/moderation"
	"github.com/tereshed/8bitnews/internal/observability"
	"github.com/tereshed/8bitnews/internal/polymaths"
	"github.com/tereshed/8bitnews/internal/publish"
	"github.com/tereshed/8bitnews/internal/publisher"
	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/sources"
	"github.com/tereshed/8bitnews/internal/storage"
	"github.com/tereshed/8bitnews/internal/store"
	"github.com/tereshed/8bitnews/internal/youtube"
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
	wayback := sources.NewWaybackClient(cfg.WaybackBaseURL, cfg.WaybackSaveTimeout, cfg.WaybackAccessKey, cfg.WaybackSecretKey, cfg.SourceAllowPrivate)
	availability := sources.NewAvailabilityClient(cfg.WaybackAvailabilityBaseURL, cfg.SourceCheckTimeout, cfg.SourceAllowPrivate)

	pub := publish.Config{PublicURL: cfg.PublicURL, CorrectionsContact: cfg.CorrectionsContact}
	// hub uploads to YouTube itself only with the bot: approvals and alerts go through it.
	uploaderOn := cfg.YouTube.Enabled() && cfg.Moderation.BotToken != ""
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
		Publish:                         pub,
		UploaderOn:                      uploaderOn,
	})
	if cfg.PolymathsWriteAPIKey != "" {
		go polymaths.Run(ctx, st, &polymaths.Client{URL: cfg.PolymathsURL, APIKey: cfg.PolymathsWriteAPIKey}, logger)
	} else {
		logger.Info("rework dispatcher off: HUB_POLYMATHS_WRITE_API_KEY is not set; feedback stays queued")
	}
	// The admin page needs the bot, which hands out its login links and is
	// how decisions reach the moderation group, and a public address for them.
	var adminPage http.Handler
	if cfg.Moderation.BotToken != "" {
		tg := moderation.NewTelegram(cfg.Moderation.APIBase, cfg.Moderation.BotToken)
		bot := moderation.New(tg, st, media, cfg.Moderation.ChatID, cfg.Moderation.PollInterval, pub, logger)
		videos := admin.NewVideoChecker()
		bot.SetVideoCheck(videos.Visible)
		var (
			yt     *youtube.Client
			sealer *youtube.Sealer
		)
		if uploaderOn {
			yt = youtube.New(youtube.Config{ClientID: cfg.YouTube.ClientID, ClientSecret: cfg.YouTube.ClientSecret,
				RedirectURL: cfg.PublicURL + admin.YouTubeCallbackPath})
			if sealer, err = youtube.NewSealer(cfg.YouTube.TokenKey); err != nil {
				fatal(err)
			}
			bot.SetUploaderCheck(func(ctx context.Context, channelID string) bool {
				accounts, err := st.ConnectedAccounts(ctx, store.PlatformYouTube)
				if err != nil {
					logger.Error("reading the connected YouTube accounts", "error", err)
					return false
				}
				for _, a := range accounts {
					if a.ChannelID == channelID {
						return true
					}
				}
				return false
			})
			go publisher.New(st, svc, yt, media, bot, sealer, logger).Run(ctx)
		} else {
			logger.Info("YouTube uploader off: uploads stay with the publishing API")
		}
		if cfg.PublicURL != "" {
			auth := admin.NewAuth(cfg.Moderation.BotToken)
			bot.SetAdminLogin(func(userID int64, who string) string {
				return cfg.PublicURL + "/admin/login?t=" + auth.LoginToken(admin.Identity{UserID: userID, Name: who})
			})
			deps := admin.Deps{Store: st, Moderator: bot, Media: media, Auth: auth, Videos: videos, Publish: pub,
				MediaOrigin: cfg.S3.Endpoint, Log: logger}
			if uploaderOn { // a nil *youtube.Client in the interface would look like one that's there
				deps.YouTube, deps.Sealer = yt, sealer
			}
			adminPage = admin.New(deps)
		}
		go bot.Run(ctx)
	} else {
		logger.Info("moderation bot off: TELEGRAM_BOT_TOKEN is not set, releases stay 'submitted'")
	}

	// What the public costs page shows: a nil client leaves it with the pictures only.
	var runsSource costs.PolyMaths
	if cfg.Costs.APIKey != "" {
		runsSource = costs.NewClient(cfg.Costs.PolymathsURL, cfg.Costs.APIKey, cfg.Costs.ProjectID, cfg.Costs.NewsroomTeamID)
	}
	go costs.NewSyncer(runsSource, st, media, cfg.Costs.Interval, logger).Run(ctx)

	mcpHandler := mcpapi.NewHTTPHandler(svc)
	metrics := observability.NewMetrics()

	// requireMetricsAuth is false when HUB_METRICS_ADDR is set: /metrics is
	// then served unauthenticated on that separate address instead (below),
	// for deployments that scrape it from a private network interface.
	requireMetricsAuth := cfg.MetricsAddr == ""
	pages := publish.NewPages(st, pub, logger)
	router := api.NewRouter(svc, mcpHandler, pages, adminPage, metrics, logger, cfg.APIKeys, cfg.PublisherAPIKey, requireMetricsAuth)

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

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	authapp "github.com/avatarsik6699/smotryashchiy/internal/auth/application"
	authinfra "github.com/avatarsik6699/smotryashchiy/internal/auth/infrastructure"
	authhttp "github.com/avatarsik6699/smotryashchiy/internal/auth/interfaces/http"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/config"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/httpserver"
	telemetryapp "github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	telemetryinfra "github.com/avatarsik6699/smotryashchiy/internal/telemetry/infrastructure"
	telemetryhttp "github.com/avatarsik6699/smotryashchiy/internal/telemetry/interfaces/http"
)

const shutdownTimeout = 10 * time.Second

func runServer(stdout io.Writer) error {
	cfg, err := config.Load(release)
	if err != nil {
		return err
	}
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if err := db.Migrate(sqlDB); err != nil {
		return err
	}

	auth := authapp.NewService(authinfra.NewSettingsStore(sqlDB))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if cfg.Production {
		if err := auth.RequireAdminPassword(ctx); err != nil {
			return fmt.Errorf("%w; run `smotryashchiy admin set-password` first", err)
		}
	} else if password, err := auth.EnsureAdminPassword(ctx); err != nil {
		return err
	} else if password != "" {
		fmt.Fprintf(stdout, "development admin password (shown once): %s\n", password)
	}

	srv := httpserver.New(cfg.Addr)
	httpserver.RegisterHealth(srv.Mux, cfg.Release, sqlDB.PingContext)
	authhttp.NewHandlers(auth, authhttp.Options{
		SecureCookies:     cfg.SecureCookies,
		TrustedProxyCIDRs: cfg.TrustedProxyCIDRs,
	}).Register(srv.Mux)
	telemetryhttp.NewHandlers(telemetryapp.NewService(telemetryinfra.NewStore(sqlDB))).Register(srv.Mux)
	srv.Use(authhttp.RequireSession(auth))

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve() }()
	slog.Info("server started", "addr", cfg.Addr, "release", cfg.Release)

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return <-serveErr
}

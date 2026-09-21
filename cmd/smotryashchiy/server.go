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
	"github.com/avatarsik6699/smotryashchiy/internal/transport"
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
	store := telemetryinfra.NewStore(sqlDB)
	hub := telemetryapp.NewHub()
	telemetrySvc := telemetryapp.NewService(store).WithPublisher(hub)
	telemetryhttp.NewHandlers(telemetrySvc).Register(srv.Mux)
	telemetryhttp.NewStreamHandlers(hub).Register(srv.Mux)

	tunnel, enrollment, err := startTunnel(ctx, cfg, store)
	if err != nil {
		return err
	}
	defer tunnel.Close()
	telemetryhttp.NewEnrollHandlers(enrollment).Register(srv.Mux)
	tunnelLn, err := tunnel.Listen(transport.IngestPort)
	if err != nil {
		return err
	}
	go func() {
		if err := telemetryhttp.ServeTunnel(ctx, tunnelLn, telemetryhttp.NewIngestHandlers(telemetrySvc, enrollment)); err != nil {
			slog.Error("tunnel ingest listener stopped", "err", err)
		}
	}()
	maintenance := telemetryapp.NewMaintenance(store, cfg.RawRetentionDays, cfg.RollupRetentionDays, time.Now)
	go maintenance.Run(ctx)
	srv.Use(authhttp.RequireSession(auth))

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve() }()
	slog.Info("server started", "addr", cfg.Addr, "release", cfg.Release)

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	hub.Close() // hijacked WebSocket connections are not tracked by http.Server.Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return <-serveErr
}

// startTunnel brings up the in-process WireGuard endpoint and re-adds every enrolled peer.
func startTunnel(ctx context.Context, cfg config.Config, store *telemetryinfra.Store) (*transport.Server, *telemetryapp.EnrollmentService, error) {
	key, err := store.ServerKey(ctx)
	if err != nil {
		return nil, nil, err
	}
	serverIP := cfg.TunnelCIDR.Masked().Addr().Next()
	tunnel, err := transport.NewServer(key, cfg.WGPort, serverIP)
	if err != nil {
		return nil, nil, err
	}
	endpoint := cfg.PublicEndpoint
	if endpoint == "" { // development only; production config requires it
		endpoint = fmt.Sprintf("127.0.0.1:%d", tunnel.UDPPort())
	}
	enrollment := telemetryapp.NewEnrollmentService(store, tunnel, cfg.TunnelCIDR,
		telemetryapp.ServerInfo{PublicKey: tunnel.PublicKey(), Endpoint: endpoint, TunnelIP: serverIP}, time.Now)
	restored, err := enrollment.RestorePeers(ctx)
	if err != nil {
		tunnel.Close()
		return nil, nil, err
	}
	slog.Info("tunnel started", "udp_port", tunnel.UDPPort(), "tunnel_ip", serverIP, "peers", restored)
	return tunnel, enrollment, nil
}

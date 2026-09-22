package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/caddyserver/certmagic"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/config"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/httpserver"
)

// setupACME starts the ACME HTTP-01 listener (health checks plus a redirect of everything else to
// https://) and obtains (or loads) a certificate for cfg.TLSDomain, returning the TLS config for the
// app listener. The HTTP-01 listener must already be accepting connections before the certificate is
// requested — an ACME server validates the challenge by connecting to it — so this binds the
// listener synchronously and only then calls ManageSync; it keeps running afterward for renewals and
// keeps serving redirects and health checks (docs/SPEC.md §4g). Certificates are stored under
// <DB dir>/acme, which is writable next to the database even on a read-only root filesystem.
func setupACME(ctx context.Context, cfg config.Config, sqlDB *sql.DB) (*tls.Config, error) {
	acmeMux := http.NewServeMux()
	httpserver.RegisterHealth(acmeMux, cfg.Release, sqlDB.PingContext)
	acmeMux.Handle("/", redirectToHTTPS())

	_, portStr, err := net.SplitHostPort(cfg.ACMEHTTPAddr)
	if err != nil {
		return nil, fmt.Errorf("acme: invalid %s %q: %w", "ACMEHTTPAddr", cfg.ACMEHTTPAddr, err)
	}
	altHTTPPort, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("acme: invalid port in %q: %w", cfg.ACMEHTTPAddr, err)
	}

	acmeCfg := certmagic.NewDefault()
	acmeCfg.Storage = &certmagic.FileStorage{Path: filepath.Join(filepath.Dir(cfg.DBPath), "acme")}
	issuerCfg := certmagic.ACMEIssuer{
		CA:                      acmeCA(cfg.ACMEStaging),
		Email:                   cfg.ACMEEmail,
		Agreed:                  true,
		DisableTLSALPNChallenge: true, // only the HTTP-01 listener is exposed
		// certmagic's embedded HTTP-01 solver tries to bind this port itself; matching it to our own
		// already-running ACME listener makes that bind fail with "already in use", which is exactly
		// what tells certmagic to fall back to the external listener (HTTPChallengeHandler below) —
		// the standard library pattern from certmagic's own docs — instead of trying port 80 directly
		// (which an unprivileged, capability-less container could never bind).
		AltHTTPPort: altHTTPPort,
	}
	issuer := certmagic.NewACMEIssuer(acmeCfg, issuerCfg)
	acmeCfg.Issuers = []certmagic.Issuer{issuer}

	ln, err := net.Listen("tcp", cfg.ACMEHTTPAddr)
	if err != nil {
		return nil, fmt.Errorf("acme: listen on %s: %w", cfg.ACMEHTTPAddr, err)
	}
	acmeSrv := &http.Server{Handler: issuer.HTTPChallengeHandler(acmeMux), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := acmeSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("acme http listener stopped", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = acmeSrv.Shutdown(shutdownCtx)
	}()

	if err := acmeCfg.ManageSync(ctx, []string{cfg.TLSDomain}); err != nil {
		_ = acmeSrv.Close()
		return nil, fmt.Errorf("acme: obtain a certificate for %s: %w", cfg.TLSDomain, err)
	}
	return acmeCfg.TLSConfig(), nil
}

func acmeCA(staging bool) string {
	if staging {
		return certmagic.LetsEncryptStagingCA
	}
	return certmagic.LetsEncryptProductionCA
}

// redirectToHTTPS sends everything that is not an ACME challenge to the https:// origin. The port
// is not repeated: operators map the container's HTTPS listener to the host's standard 443, so a
// bare hostname resolves correctly for a browser.
func redirectToHTTPS() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		target := "https://" + host + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	}
}

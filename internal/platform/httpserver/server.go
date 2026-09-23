// Package httpserver owns HTTP server bootstrap, the shared middleware chain and the health
// endpoints. Bounded contexts mount their handlers on Mux.
package httpserver

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"time"
)

// Server wraps the shared mux and the net/http server that serves it.
type Server struct {
	Mux        *http.ServeMux
	addr       string
	httpServer *http.Server
	middleware []func(http.Handler) http.Handler
}

// New creates a Server listening on addr. Routes are mounted on Mux before Serve/ServeTLS.
func New(addr string) *Server {
	return &Server{Mux: http.NewServeMux(), addr: addr}
}

// Use registers middleware around Mux, inside the request-id/logging/recover chain, so even a
// rejected request is logged. Middleware runs in registration order: the first added is outermost
// and sees a request first, the last added runs closest to Mux.
func (s *Server) Use(mw func(http.Handler) http.Handler) {
	s.middleware = append(s.middleware, mw)
}

// Handler is the complete handler Serve and ServeHTTPS use: Mux inside the registered middleware
// and the platform chain. Tests serve it to exercise the real order.
func (s *Server) Handler() http.Handler { return s.chained() }

func (s *Server) chained() http.Handler {
	var handler http.Handler = s.Mux
	for i := len(s.middleware) - 1; i >= 0; i-- {
		handler = s.middleware[i](handler)
	}
	return Chain(handler)
}

// Serve blocks serving plain HTTP until Shutdown is called (returns nil) or a fatal error occurs.
// This is the Change 09 path: a trusted reverse proxy terminates TLS in front of it.
func (s *Server) Serve() error {
	s.httpServer = &http.Server{
		Addr:              s.addr,
		Handler:           s.chained(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ServeHTTPS blocks serving the app over TLS on addr (with tlsConfig, typically certmagic's) until
// Shutdown is called (returns nil) or a fatal error occurs. This is the Change 10 built-in-ACME
// path (docs/SPEC.md §4g): callers must obtain tlsConfig — and keep the separate ACME HTTP-01
// listener running throughout, both before and after this call — themselves; see
// cmd/smotryashchiy/acme.go. It ignores the addr given to New.
func (s *Server) ServeHTTPS(addr string, tlsConfig *tls.Config) error {
	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           withHSTS(s.chained()),
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
	}
	// The cert/key args are empty: certificates come from tlsConfig.GetCertificate (certmagic).
	if err := s.httpServer.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// hsts is sent on every response of the TLS listener only (docs/SPEC.md §4g). No includeSubDomains:
// the UI's domain may be a subdomain of a site whose other subdomains the operator does not control.
const hsts = "max-age=31536000"

func withHSTS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", hsts)
		next.ServeHTTP(w, r)
	})
}

// Shutdown gracefully stops serving.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

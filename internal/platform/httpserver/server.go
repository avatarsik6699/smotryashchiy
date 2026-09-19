// Package httpserver owns HTTP server bootstrap, the shared middleware chain and the health
// endpoints. Bounded contexts mount their handlers on Mux.
package httpserver

import (
	"context"
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

// New creates a Server listening on addr. Routes are mounted on Mux before Serve.
func New(addr string) *Server {
	return &Server{Mux: http.NewServeMux(), addr: addr}
}

// Use registers middleware around Mux, inside the request-id/logging/recover chain, so even a
// rejected request is logged. Middleware added first wraps closest to Mux.
func (s *Server) Use(mw func(http.Handler) http.Handler) {
	s.middleware = append(s.middleware, mw)
}

// Serve blocks serving HTTP until Shutdown is called (returns nil) or a fatal error occurs.
func (s *Server) Serve() error {
	var handler http.Handler = s.Mux
	for i := len(s.middleware) - 1; i >= 0; i-- {
		handler = s.middleware[i](handler)
	}
	s.httpServer = &http.Server{
		Addr:              s.addr,
		Handler:           Chain(handler),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully stops serving.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

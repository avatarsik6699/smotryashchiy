package http

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// ServeTunnel serves the tunnel-only routes (POST /api/ingest) on ln until ctx is done. The mux
// has no session middleware on purpose: WireGuard is the authentication layer.
func ServeTunnel(ctx context.Context, ln net.Listener, ingest *IngestHandlers) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	mux := http.NewServeMux()
	ingest.Register(mux)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	err := srv.Serve(ln)
	stop() // Serve may have failed on its own; release the shutdown goroutine either way
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

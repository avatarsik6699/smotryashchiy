package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/config"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
)

const healthTimeout = 3 * time.Second

// runVersion prints the release SHA and the number of migrations embedded in this binary.
func runVersion(stdout io.Writer) error {
	_, err := fmt.Fprintf(stdout, "smotryashchiy release=%s migrations=%d\n", release, db.MigrationCount())
	return err
}

// healthURL turns a listen address such as ":8080" or "0.0.0.0:8080" into the loopback URL of the
// readiness probe of the server running in this container/host.
func healthURL(listenAddr string) (string, error) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", listenAddr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/health/ready", nil
}

// runHealthcheck is the container HEALTHCHECK: exit 0 only when /health/ready answers 200 in time.
// The image has no shell or curl, so the binary probes itself.
func runHealthcheck(stdout io.Writer) error {
	url, err := healthURL(config.ListenAddr())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("not ready: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return errors.New("not ready: " + resp.Status)
	}
	_, err = fmt.Fprintln(stdout, "ok")
	return err
}

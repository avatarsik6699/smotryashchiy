// Package application holds the uptime prober: the checker that probes one target and the service that
// schedules probes and manages targets (docs/SPEC.md §4e).
package application

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/uptime/domain"
)

const (
	maxRedirects = 5
	maxErrorLen  = 300
	userAgent    = "smotryashchiy-uptime/1"
)

// Checker probes one target. It never returns an error: a failed probe is a Result with OK=false.
type Checker struct {
	Timeout time.Duration
	// RootCAs replaces the system roots (tests). Certificates are always verified.
	RootCAs *x509.CertPool
	Now     func() time.Time
	// Dial replaces the network dialer (tests).
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
}

// NewChecker returns a Checker with the SPEC's 10 s timeout and the system clock.
func NewChecker() *Checker { return &Checker{Timeout: domain.CheckTimeout, Now: time.Now} }

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) dial(ctx context.Context, network, address string) (net.Conn, error) {
	if c.Dial != nil {
		return c.Dial(ctx, network, address)
	}
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

// Check probes t once.
func (c *Checker) Check(ctx context.Context, t domain.Target) domain.Result {
	res := domain.Result{TargetID: t.ID, TS: c.now().UTC().Truncate(time.Millisecond)}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	switch t.Kind {
	case domain.KindHTTP:
		c.checkHTTP(ctx, t, &res)
	case domain.KindTCP:
		c.checkTCP(ctx, t, &res)
	case domain.KindTLS:
		c.checkTLS(ctx, t, &res)
	default:
		res.Error = "unknown target kind " + string(t.Kind)
	}
	res.Error = trimError(res.Error)
	return res
}

func (c *Checker) tlsConfig(serverName string) *tls.Config {
	return &tls.Config{RootCAs: c.RootCAs, ServerName: serverName, MinVersion: tls.VersionTLS12}
}

func (c *Checker) checkHTTP(ctx context.Context, t domain.Target, res *domain.Result) {
	client := &http.Client{
		Timeout: c.Timeout,
		Transport: &http.Transport{
			Proxy:             nil, // probe the target directly, never through an environment proxy
			DialContext:       c.dial,
			TLSClientConfig:   c.tlsConfig(""),
			DisableKeepAlives: true,
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.Address, nil)
	if err != nil {
		res.Error = err.Error()
		return
	}
	req.Header.Set("User-Agent", userAgent)
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		res.Error = describe(err, c.Timeout)
		return
	}
	defer resp.Body.Close() // the body is never read: only the answer matters
	latency := time.Since(start).Milliseconds()
	res.LatencyMS = &latency // the target answered, so the latency is a real measurement even for a bad status
	code := resp.StatusCode
	res.StatusCode = &code
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		notAfter := resp.TLS.PeerCertificates[0].NotAfter.UTC()
		res.CertExpiresAt = &notAfter
	}
	if code < 200 || code > 399 {
		res.Error = fmt.Sprintf("unexpected status %d", code)
		return
	}
	res.OK = true
}

func (c *Checker) checkTCP(ctx context.Context, t domain.Target, res *domain.Result) {
	start := time.Now()
	conn, err := c.dial(ctx, "tcp", t.Address)
	if err != nil {
		res.Error = describe(err, c.Timeout)
		return
	}
	_ = conn.Close()
	latency := time.Since(start).Milliseconds()
	res.LatencyMS = &latency
	res.OK = true
}

func (c *Checker) checkTLS(ctx context.Context, t domain.Target, res *domain.Result) {
	host, _, err := net.SplitHostPort(t.Address)
	if err != nil {
		res.Error = err.Error()
		return
	}
	start := time.Now()
	raw, err := c.dial(ctx, "tcp", t.Address)
	if err != nil {
		res.Error = describe(err, c.Timeout)
		return
	}
	defer raw.Close()
	conn := tls.Client(raw, c.tlsConfig(host))
	if err := conn.HandshakeContext(ctx); err != nil {
		res.Error = describe(err, c.Timeout)
		return
	}
	latency := time.Since(start).Milliseconds()
	res.LatencyMS = &latency
	if certs := conn.ConnectionState().PeerCertificates; len(certs) > 0 {
		notAfter := certs[0].NotAfter.UTC()
		res.CertExpiresAt = &notAfter
	}
	res.OK = true
}

// describe turns a probe error into short operator text, dropping the URL wrapper and naming timeouts.
func describe(err error, timeout time.Duration) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return fmt.Sprintf("timeout after %s", timeout)
	}
	return err.Error()
}

func trimError(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxErrorLen {
		return s[:maxErrorLen] + "…"
	}
	return s
}

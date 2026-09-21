// Package agent is the host-side client: it enrolls with the server and sends batches through the
// WireGuard tunnel (docs/SPEC.md §4b). Collectors and the run loop arrive in a later change.
package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/transport"
)

const httpTimeout = 15 * time.Second

// Config is the agent's persisted identity and tunnel description (agent.json, mode 0600).
type Config struct {
	HostID          string `json:"host_id"`
	PrivateKey      string `json:"private_key"`
	TunnelIP        string `json:"tunnel_ip"`
	ServerPublicKey string `json:"server_public_key"`
	ServerEndpoint  string `json:"server_endpoint"`
	ServerTunnelIP  string `json:"server_tunnel_ip"`
}

type enrollRequest struct {
	Secret    string `json:"secret"`
	PublicKey string `json:"public_key"`
}

// Enroll generates a key pair, exchanges secret for a tunnel address at serverURL and writes the
// resulting Config to path with mode 0600. It refuses to overwrite an existing config, because
// that would destroy the private key of an already enrolled host.
func Enroll(ctx context.Context, serverURL, secret, path string) (Config, error) {
	if _, err := os.Stat(path); err == nil {
		return Config{}, fmt.Errorf("agent: %s already exists; delete it to enroll again", path)
	}
	private, public, err := transport.GenerateKey()
	if err != nil {
		return Config{}, err
	}
	body, _ := json.Marshal(enrollRequest{Secret: secret, PublicKey: public})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(serverURL, "/")+"/api/enroll", bytes.NewReader(body))
	if err != nil {
		return Config{}, fmt.Errorf("agent: build enrollment request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return Config{}, fmt.Errorf("agent: enrollment request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return Config{}, fmt.Errorf("agent: enrollment rejected (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		HostID          string `json:"host_id"`
		TunnelIP        string `json:"tunnel_ip"`
		ServerPublicKey string `json:"server_public_key"`
		ServerEndpoint  string `json:"server_endpoint"`
		ServerTunnelIP  string `json:"server_tunnel_ip"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Config{}, fmt.Errorf("agent: decode enrollment response: %w", err)
	}
	cfg := Config{HostID: out.HostID, PrivateKey: private, TunnelIP: out.TunnelIP,
		ServerPublicKey: out.ServerPublicKey, ServerEndpoint: out.ServerEndpoint, ServerTunnelIP: out.ServerTunnelIP}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	if err := writeConfig(path, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// LoadConfig reads and validates the agent config at path.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("agent: read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("agent: parse config %s: %w", path, err)
	}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if c.HostID == "" || c.ServerEndpoint == "" {
		return errors.New("agent: config is incomplete (host_id/server_endpoint)")
	}
	if _, err := transport.PublicKey(c.PrivateKey); err != nil {
		return fmt.Errorf("agent: config private_key: %w", err)
	}
	if err := transport.ValidatePublicKey(c.ServerPublicKey); err != nil {
		return fmt.Errorf("agent: config server_public_key: %w", err)
	}
	for name, v := range map[string]string{"tunnel_ip": c.TunnelIP, "server_tunnel_ip": c.ServerTunnelIP} {
		if _, err := netip.ParseAddr(v); err != nil {
			return fmt.Errorf("agent: config %s: %w", name, err)
		}
	}
	return nil
}

// writeConfig creates path exclusively with mode 0600 (the file holds the private key).
func writeConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("agent: create config dir: %w", err)
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("agent: create config: %w", err)
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("agent: write config: %w", err)
	}
	return f.Close()
}

// Result is the ingest response for one pushed batch.
type Result struct {
	Accepted   Counts `json:"accepted"`
	Duplicates Counts `json:"duplicates"`
	Replayed   bool   `json:"replayed"`
}

// Counts is a per-kind record tally.
type Counts struct {
	Metrics int `json:"metrics"`
	Checks  int `json:"checks"`
	Events  int `json:"events"`
}

// StatusError is a non-200 answer from the ingest endpoint.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("agent: ingest rejected (%d): %s", e.Code, e.Body)
}

// Session is one tunnel to the server that can carry many batches.
type Session struct {
	tunnel *transport.Client
	url    string
	client *http.Client
}

// Dial brings up the tunnel described by cfg and waits for its handshake.
func Dial(ctx context.Context, cfg Config) (*Session, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	tunnel, err := transport.NewClient(ctx, transport.ClientConfig{
		PrivateKey:      cfg.PrivateKey,
		ServerPublicKey: cfg.ServerPublicKey,
		Endpoint:        cfg.ServerEndpoint,
		TunnelIP:        netip.MustParseAddr(cfg.TunnelIP),
		ServerTunnelIP:  netip.MustParseAddr(cfg.ServerTunnelIP),
	})
	if err != nil {
		return nil, err
	}
	return &Session{
		tunnel: tunnel,
		url:    fmt.Sprintf("http://%s/api/ingest", netip.AddrPortFrom(tunnel.ServerIP(), transport.IngestPort)),
		client: tunnel.HTTPClient(httpTimeout),
	}, nil
}

// Send posts batch with its Idempotency-Key. A non-200 answer is returned as *StatusError.
func (s *Session) Send(ctx context.Context, key string, batch []byte) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(batch))
	if err != nil {
		return Result{}, fmt.Errorf("agent: build ingest request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	resp, err := s.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("agent: ingest request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return Result{}, &StatusError{Code: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	var out Result
	if err := json.Unmarshal(raw, &out); err != nil {
		return Result{}, fmt.Errorf("agent: decode ingest response: %w", err)
	}
	return out, nil
}

// Close shuts the tunnel down.
func (s *Session) Close() { s.tunnel.Close() }

// Push sends one batch through a short-lived tunnel. An empty key gets a random Idempotency-Key.
func Push(ctx context.Context, cfg Config, batch []byte, key string) (Result, error) {
	if key == "" {
		key = NewKey()
	}
	sess, err := Dial(ctx, cfg)
	if err != nil {
		return Result{}, err
	}
	defer sess.Close()
	return sess.Send(ctx, key, batch)
}

// NewKey returns a random Idempotency-Key.
func NewKey() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("agent: crypto/rand failed: %v", err)) // no entropy: nothing sensible to do
	}
	return hex.EncodeToString(buf)
}

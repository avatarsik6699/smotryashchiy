// Package config loads typed settings from environment variables. It is the one place in the
// codebase allowed to call os.Getenv directly — every other package receives a Config.
package config

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Config holds every environment-derived setting the composition root needs.
type Config struct {
	// Addr is the host:port the HTTP server listens on.
	Addr string
	// DBPath is the filesystem path of the SQLite database file.
	DBPath string
	// Production enables the fail-closed public deployment contract.
	Production bool
	// Release is the exact 40-character Git SHA reported by readiness in production.
	Release string
	// SecureCookies forces the Secure attribute on session cookies (behind a TLS proxy).
	SecureCookies bool
	// TrustedProxyCIDRs are the only peers allowed to supply a forwarded client address.
	TrustedProxyCIDRs []netip.Prefix
	// RawRetentionDays is the TTL of raw metrics, checks and events (docs/SPEC.md §4.5).
	RawRetentionDays int
	// RollupRetentionDays is the TTL of hourly metric rollups.
	RollupRetentionDays int
	// WGPort is the UDP port of the in-process WireGuard endpoint (docs/SPEC.md §4b).
	WGPort int
	// TunnelCIDR is the tunnel subnet; the server takes its first host address.
	TunnelCIDR netip.Prefix
	// PublicEndpoint is the host:port (UDP) agents dial for WireGuard; empty until configured.
	PublicEndpoint string
}

const (
	envAddr       = "SMOTRYASHCHIY_ADDR"
	envDBPath     = "SMOTRYASHCHIY_DB_PATH"
	envProduction = "SMOTRYASHCHIY_PRODUCTION"
	envRelease    = "SMOTRYASHCHIY_RELEASE"
	envSecure     = "SMOTRYASHCHIY_SECURE_COOKIES"
	envProxies    = "SMOTRYASHCHIY_TRUSTED_PROXY_CIDRS"
	envRawDays    = "SMOTRYASHCHIY_RAW_RETENTION_DAYS"
	envRollupDays = "SMOTRYASHCHIY_ROLLUP_RETENTION_DAYS"
	envWGPort     = "SMOTRYASHCHIY_WG_PORT"
	envTunnelCIDR = "SMOTRYASHCHIY_TUNNEL_CIDR"
	envEndpoint   = "SMOTRYASHCHIY_PUBLIC_ENDPOINT"

	defaultWGPort     = 51820
	defaultTunnelCIDR = "10.99.0.0/16"

	defaultRawRetentionDays    = 30
	defaultRollupRetentionDays = 396 // 13 months
)

var releasePattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Load reads Config from the process environment. defaultRelease is the build-time release
// (set with -ldflags) used when SMOTRYASHCHIY_RELEASE is unset.
func Load(defaultRelease string) (Config, error) {
	production, err := getBool(envProduction, false)
	if err != nil {
		return Config{}, err
	}
	secureCookies, err := getBool(envSecure, production)
	if err != nil {
		return Config{}, err
	}
	proxies, err := parsePrefixes(os.Getenv(envProxies))
	if err != nil {
		return Config{}, err
	}
	rawDays, err := getPositiveInt(envRawDays, defaultRawRetentionDays)
	if err != nil {
		return Config{}, err
	}
	rollupDays, err := getPositiveInt(envRollupDays, defaultRollupRetentionDays)
	if err != nil {
		return Config{}, err
	}
	wgPort, err := getPort(envWGPort, defaultWGPort)
	if err != nil {
		return Config{}, err
	}
	tunnelCIDR, err := parseTunnelCIDR(getOr(envTunnelCIDR, defaultTunnelCIDR))
	if err != nil {
		return Config{}, err
	}
	endpoint := strings.TrimSpace(os.Getenv(envEndpoint))
	if endpoint != "" {
		if _, err := netip.ParseAddrPort(endpoint); err != nil {
			if host, port, splitErr := net.SplitHostPort(endpoint); splitErr != nil || host == "" || !validPort(port) {
				return Config{}, fmt.Errorf("config: %s must be host:port, got %q", envEndpoint, endpoint)
			}
		}
	}
	cfg := Config{
		Addr:              getOr(envAddr, ":8080"),
		DBPath:            getOr(envDBPath, "./data/smotryashchiy.db"),
		Production:        production,
		Release:           getOr(envRelease, defaultRelease),
		SecureCookies:     secureCookies,
		TrustedProxyCIDRs: proxies,

		RawRetentionDays:    rawDays,
		RollupRetentionDays: rollupDays,
		WGPort:              wgPort,
		TunnelCIDR:          tunnelCIDR,
		PublicEndpoint:      endpoint,
	}
	if cfg.Production {
		if !releasePattern.MatchString(cfg.Release) {
			return Config{}, fmt.Errorf("config: %s must be a 40-character lowercase Git SHA in production", envRelease)
		}
		if !cfg.SecureCookies {
			return Config{}, fmt.Errorf("config: %s must be true in production", envSecure)
		}
		if cfg.PublicEndpoint == "" {
			return Config{}, fmt.Errorf("config: %s (agent-facing WireGuard host:port) is required in production", envEndpoint)
		}
		if len(cfg.TrustedProxyCIDRs) == 0 {
			return Config{}, fmt.Errorf("config: %s requires at least one CIDR in production", envProxies)
		}
	}
	return cfg, nil
}

func getOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getBool(key string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("config: %s must be a boolean: %w", key, err)
	}
	return parsed, nil
}

func getPositiveInt(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("config: %s must be a positive integer number of days, got %q", key, value)
	}
	return parsed, nil
}

func validPort(raw string) bool {
	n, err := strconv.Atoi(raw)
	return err == nil && n >= 1 && n <= 65535
}

func getPort(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	if !validPort(strings.TrimSpace(value)) {
		return 0, fmt.Errorf("config: %s must be a port between 1 and 65535, got %q", key, value)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n, nil
}

// parseTunnelCIDR requires an IPv4 prefix no longer than /24 (room for hosts) and no shorter than
// /8, masked to its network address.
func parseTunnelCIDR(value string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() < 8 || prefix.Bits() > 24 {
		return netip.Prefix{}, fmt.Errorf("config: %s must be an IPv4 CIDR between /8 and /24, got %q", envTunnelCIDR, value)
	}
	return prefix.Masked(), nil
}

func parsePrefixes(value string) ([]netip.Prefix, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("config: %s contains invalid CIDR %q: %w", envProxies, part, err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

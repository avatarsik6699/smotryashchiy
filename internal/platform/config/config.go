// Package config loads typed settings from environment variables. It is the one place in the
// codebase allowed to call os.Getenv directly — every other package receives a Config.
package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
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
	// PublicURL is the http(s) base URL agents use to enroll and the UI shows in enrollment
	// commands; empty means "derive it from the request" (docs/SPEC.md §4d).
	PublicURL string
	// TLSDomain, when set, switches the server into self-terminated TLS via certmagic: it serves the
	// app on HTTPSAddr and the ACME HTTP-01 challenge (plus a redirect) on ACMEHTTPAddr
	// (docs/SPEC.md §4g). Empty means "no built-in TLS" (Change 09's reverse-proxy path).
	TLSDomain string
	// HTTPSAddr is the app's TLS listen address when TLSDomain is set.
	HTTPSAddr string
	// ACMEHTTPAddr is the ACME HTTP-01 challenge and redirect listen address when TLSDomain is set.
	ACMEHTTPAddr string
	// ACMEEmail is optional: the CA sends renewal/expiry notices to it.
	ACMEEmail string
	// ACMEStaging selects Let's Encrypt's staging CA (untrusted certs, no rate limit) for testing.
	ACMEStaging bool
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
	envPublicURL  = "SMOTRYASHCHIY_PUBLIC_URL"
	envTLSDomain  = "SMOTRYASHCHIY_TLS_DOMAIN"
	envHTTPSAddr  = "SMOTRYASHCHIY_HTTPS_ADDR"
	envACMEHTTP   = "SMOTRYASHCHIY_ACME_HTTP_ADDR"
	envACMEEmail  = "SMOTRYASHCHIY_ACME_EMAIL"
	envACMECA     = "SMOTRYASHCHIY_ACME_CA"

	defaultWGPort     = 51820
	defaultTunnelCIDR = "10.99.0.0/16"
	defaultHTTPSAddr  = ":8443"
	defaultACMEHTTP   = ":8080"

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
	publicURL, err := parsePublicURL(os.Getenv(envPublicURL))
	if err != nil {
		return Config{}, err
	}
	tlsDomain := strings.TrimSpace(os.Getenv(envTLSDomain))
	acmeCA := strings.TrimSpace(os.Getenv(envACMECA))
	var acmeStaging bool
	switch acmeCA {
	case "", "production":
	case "staging":
		acmeStaging = true
	default:
		return Config{}, fmt.Errorf("config: %s must be production or staging, got %q", envACMECA, acmeCA)
	}
	if publicURL == "" && tlsDomain != "" {
		publicURL = "https://" + tlsDomain
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
		PublicURL:           publicURL,

		TLSDomain:    tlsDomain,
		HTTPSAddr:    getOr(envHTTPSAddr, defaultHTTPSAddr),
		ACMEHTTPAddr: getOr(envACMEHTTP, defaultACMEHTTP),
		ACMEEmail:    strings.TrimSpace(os.Getenv(envACMEEmail)),
		ACMEStaging:  acmeStaging,
	}
	if cfg.Production {
		if !releasePattern.MatchString(cfg.Release) {
			return Config{}, fmt.Errorf("config: %s must be a 40-character lowercase Git SHA in production", envRelease)
		}
		if !cfg.SecureCookies {
			return Config{}, fmt.Errorf("config: %s must be true in production", envSecure)
		}
		if cfg.PublicURL == "" {
			return Config{}, fmt.Errorf("config: %s (the URL agents use to enroll) is required in production", envPublicURL)
		}
		if cfg.PublicEndpoint == "" {
			return Config{}, fmt.Errorf("config: %s (agent-facing WireGuard host:port) is required in production", envEndpoint)
		}
		// Traffic must be encrypted end to end: either the server terminates TLS itself (ACME) or a
		// trusted reverse proxy does, never neither.
		if cfg.TLSDomain == "" && len(cfg.TrustedProxyCIDRs) == 0 {
			return Config{}, fmt.Errorf("config: production requires either %s (built-in TLS) or %s (a trusted reverse proxy)", envTLSDomain, envProxies)
		}
	}
	return cfg, nil
}

// HealthProbeAddr is the address healthcheck should ask for /health/ready. It reads only the two
// envs it needs and must not fail on an otherwise-incomplete production configuration: with a TLS
// domain set, that is the unencrypted ACME listener (mounted with the app's routes, docs/SPEC.md
// §4g), so the probe never depends on a certificate being ready yet.
func HealthProbeAddr() string {
	if strings.TrimSpace(os.Getenv(envTLSDomain)) != "" {
		return getOr(envACMEHTTP, defaultACMEHTTP)
	}
	return getOr(envAddr, ":8080")
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

// parsePublicURL accepts an empty value or an absolute http(s) URL without credentials, query or
// fragment; a trailing slash is removed so it composes with "/api/...".
func parsePublicURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("config: %s must be an absolute http(s) URL without credentials, query or fragment, got %q", envPublicURL, raw)
	}
	return strings.TrimRight(raw, "/"), nil
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

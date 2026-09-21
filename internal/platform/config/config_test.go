package config

import (
	"strings"
	"testing"
)

const ep = "monitor.example.com:51820"

const sha = "0123456789abcdef0123456789abcdef01234567"

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{envAddr, envDBPath, envProduction, envRelease, envSecure, envProxies, envRawDays, envRollupDays, envWGPort, envTunnelCIDR, envEndpoint} {
		t.Setenv(k, "")
	}
}

func TestLoadDevelopmentDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load("development")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.Release != "development" || cfg.Production || cfg.SecureCookies {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadEnvOverridesBuildRelease(t *testing.T) {
	clearEnv(t)
	t.Setenv(envRelease, "from-env")
	cfg, err := Load("from-build")
	if err != nil || cfg.Release != "from-env" {
		t.Fatalf("release = %q, err = %v", cfg.Release, err)
	}
}

func TestLoadProductionContract(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		release string
		wantErr string
	}{
		{"valid", map[string]string{envProxies: "10.0.0.2/32", envEndpoint: ep}, sha, ""},
		{"no endpoint", map[string]string{envProxies: "10.0.0.2/32"}, sha, envEndpoint},
		{"development release", map[string]string{envProxies: "10.0.0.2/32", envEndpoint: ep}, "development", envRelease},
		{"short sha", map[string]string{envProxies: "10.0.0.2/32", envEndpoint: ep}, "abc123", envRelease},
		{"insecure cookies", map[string]string{envProxies: "10.0.0.2/32", envEndpoint: ep, envSecure: "false"}, sha, envSecure},
		{"no proxies", map[string]string{envEndpoint: ep}, sha, envProxies},
		{"bad cidr", map[string]string{envProxies: "nope", envEndpoint: ep}, sha, "invalid CIDR"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(envProduction, "true")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load(tc.release)
			if tc.wantErr == "" {
				if err != nil || !cfg.SecureCookies {
					t.Fatalf("err = %v, secure = %v", err, cfg.SecureCookies)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadRejectsInvalidBoolean(t *testing.T) {
	clearEnv(t)
	t.Setenv(envProduction, "maybe")
	if _, err := Load("development"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadRetentionDefaultsAndOverrides(t *testing.T) {
	clearEnv(t)
	cfg, err := Load("development")
	if err != nil || cfg.RawRetentionDays != 30 || cfg.RollupRetentionDays != 396 {
		t.Fatalf("defaults = %d/%d, err = %v", cfg.RawRetentionDays, cfg.RollupRetentionDays, err)
	}
	t.Setenv(envRawDays, "7")
	t.Setenv(envRollupDays, "90")
	cfg, err = Load("development")
	if err != nil || cfg.RawRetentionDays != 7 || cfg.RollupRetentionDays != 90 {
		t.Fatalf("overrides = %d/%d, err = %v", cfg.RawRetentionDays, cfg.RollupRetentionDays, err)
	}
}

func TestLoadRejectsInvalidRetention(t *testing.T) {
	for _, key := range []string{envRawDays, envRollupDays} {
		for _, bad := range []string{"0", "-3", "abc", "1.5"} {
			clearEnv(t)
			t.Setenv(key, bad)
			if _, err := Load("development"); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("%s=%q: err = %v, want error naming the variable", key, bad, err)
			}
		}
	}
}

func TestLoadTransportDefaultsAndOverrides(t *testing.T) {
	clearEnv(t)
	cfg, err := Load("development")
	if err != nil || cfg.WGPort != 51820 || cfg.TunnelCIDR.String() != "10.99.0.0/16" || cfg.PublicEndpoint != "" {
		t.Fatalf("defaults = %d %v %q, err = %v", cfg.WGPort, cfg.TunnelCIDR, cfg.PublicEndpoint, err)
	}
	t.Setenv(envWGPort, "40000")
	t.Setenv(envTunnelCIDR, "10.7.3.9/24")
	t.Setenv(envEndpoint, "monitor.example.com:40000")
	cfg, err = Load("development")
	if err != nil || cfg.WGPort != 40000 || cfg.TunnelCIDR.String() != "10.7.3.0/24" || cfg.PublicEndpoint != "monitor.example.com:40000" {
		t.Fatalf("overrides = %d %v %q, err = %v", cfg.WGPort, cfg.TunnelCIDR, cfg.PublicEndpoint, err)
	}
}

func TestLoadRejectsInvalidTransportSettings(t *testing.T) {
	for key, bad := range map[string][]string{
		envWGPort:     {"0", "70000", "abc"},
		envTunnelCIDR: {"10.0.0.1", "fd00::/64", "10.0.0.0/30", "10.0.0.0/4"},
		envEndpoint:   {"no-port", ":51820", "host:99999"},
	} {
		for _, v := range bad {
			clearEnv(t)
			t.Setenv(key, v)
			if _, err := Load("development"); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("%s=%q: err = %v, want error naming the variable", key, v, err)
			}
		}
	}
}

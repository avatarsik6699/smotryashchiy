package config

import (
	"strings"
	"testing"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{envAddr, envDBPath, envProduction, envRelease, envSecure, envProxies} {
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
		{"valid", map[string]string{envProxies: "10.0.0.2/32"}, sha, ""},
		{"development release", map[string]string{envProxies: "10.0.0.2/32"}, "development", envRelease},
		{"short sha", map[string]string{envProxies: "10.0.0.2/32"}, "abc123", envRelease},
		{"insecure cookies", map[string]string{envProxies: "10.0.0.2/32", envSecure: "false"}, sha, envSecure},
		{"no proxies", nil, sha, envProxies},
		{"bad cidr", map[string]string{envProxies: "nope"}, sha, "invalid CIDR"},
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

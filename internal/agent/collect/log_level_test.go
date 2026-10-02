package collect

import "testing"

func TestLogLevelRecognizedFormatsOverrideDockerStream(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"json level", `{"level":"debug","msg":"starting"}`, "info"},
		{"json severity", `{"severity":"WARNING","msg":"disk is nearly full"}`, "warn"},
		{"slog", `time=2026-10-01T17:07:15.123Z level=ERROR msg="database unavailable"`, "error"},
		{"zap console", "2026-10-01T17:07:15.123Z\tinfo\tmaintenance\tcertificate renewed", "info"},
		{"zap epoch", "1.790912345e+09\tinfo\tmaintenance\tcertificate renewed", "info"},
		{"zap dpanic", "1.790912345e+09\tDPANIC\tmaintenance\tinvariant violated", "error"},
		{"zap json dpanic", `{"level":"dpanic","msg":"invariant violated"}`, "error"},
		{"postgres", "2026-10-01 17:07:15.123 UTC [42] LOG: checkpoint complete", "info"},
		{"postgres critical", "2026-10-01 17:07:15.123 UTC [42] PANIC: could not flush data", "critical"},
		{"uvicorn", `INFO: 127.0.0.1:1 - "GET / HTTP/1.1" 200 OK`, "info"},
		{"uvicorn warning", "WARNING: worker count is low", "warn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, stderr := range []bool{false, true} {
				if got := dockerLogLevel(tc.line, stderr); got != tc.want {
					t.Errorf("dockerLogLevel(%q, stderr=%t) = %q, want %q", tc.line, stderr, got, tc.want)
				}
			}
		})
	}
}

func TestLogLevelFallbackDoesNotReadEmbeddedText(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		stderr bool
		want   string
	}{
		{"embedded slog-like text on stdout", `request completed message="level=error"`, false, "info"},
		{"embedded level text on stderr", `request completed message="level=info"`, true, "warn"},
		{"malformed json stdout", `{"level":"error"`, false, "info"},
		{"malformed json stderr", `{"severity":"critical"`, true, "warn"},
		{"invalid json severity stdout", `{"level":"trace","msg":"starting"}`, false, "info"},
		{"invalid json severity stderr", `{"severity":"trace","msg":"starting"}`, true, "warn"},
		{"invalid slog severity", `time=2026-10-01T17:07:15.123Z level=trace msg="starting"`, false, "info"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dockerLogLevel(tc.line, tc.stderr); got != tc.want {
				t.Errorf("dockerLogLevel(%q, stderr=%t) = %q, want %q", tc.line, tc.stderr, got, tc.want)
			}
		})
	}
}

func TestLogLevelMapping(t *testing.T) {
	cases := map[string]string{
		"debug": "info", "info": "info", "notice": "info", "log": "info",
		"warn": "warn", "warning": "warn", "error": "error", "fatal": "error", "dpanic": "error",
		"panic": "critical", "critical": "critical",
	}
	for input, want := range cases {
		if got, ok := normalizeLogLevel(input); !ok || got != want {
			t.Errorf("normalizeLogLevel(%q) = (%q, %t), want (%q, true)", input, got, ok, want)
		}
	}
	if _, ok := normalizeLogLevel("trace"); ok {
		t.Fatal("normalizeLogLevel(trace) unexpectedly recognized an unsupported severity")
	}
}

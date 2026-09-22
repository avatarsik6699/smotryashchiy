package collect

import "testing"

func TestIsRoutineHealthCheck(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{
			"uvicorn-style 200 on /health/ready",
			`INFO: 127.0.0.1:60538 - "GET /health/ready HTTP/1.1" 200 OK`,
			true,
		},
		{
			"json access log 200 on /health/ready",
			`{"method": "GET", "path": "/health/ready", "status_code": 200, "duration_ms": 80.47}`,
			true,
		},
		{
			"healthz path",
			`INFO: 10.0.0.1:1234 - "GET /healthz HTTP/1.1" 200 OK`,
			true,
		},
		{
			"failing healthcheck must not be filtered",
			`INFO: 127.0.0.1:60538 - "GET /health/ready HTTP/1.1" 503 Service Unavailable`,
			false,
		},
		{
			"unrelated path must not be filtered",
			`91.92.47.235 - - [22/Sep/2026:13:17:54 +0000] "GET / HTTP/1.1" 308 171 "-" "-"`,
			false,
		},
		{
			"lookalike path must not be filtered",
			`INFO: 127.0.0.1:1 - "GET /healthcare HTTP/1.1" 200 OK`,
			false,
		},
		{
			"unrelated line must not be filtered",
			`Failed password for invalid user jeff from 196.188.93.169 port 2150 ssh2`,
			false,
		},
		{
			"UFW block must not be filtered",
			`[UFW BLOCK] IN=ens3 OUT= SRC=85.217.140.11 DST=2.26.8.245 PROTO=TCP DPT=19482`,
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isRoutineHealthCheck(c.line); got != c.want {
				t.Errorf("isRoutineHealthCheck(%q) = %v, want %v", c.line, got, c.want)
			}
		})
	}
}

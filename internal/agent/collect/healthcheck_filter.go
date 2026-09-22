package collect

import "regexp"

// accessLogRe matches a quoted HTTP access-log record, e.g. `"GET /health/ready HTTP/1.1" 200` —
// the shape produced by uvicorn/nginx-style loggers. Group 1 is the path, group 2 the status.
var accessLogRe = regexp.MustCompile(`"(?:GET|HEAD)\s+(\S+)\s+HTTP/\d\.\d"\s+(\d{3})`)

// jsonPathRe and jsonStatusRe match a structured JSON access log, e.g.
// `{"method":"GET","path":"/health/ready","status_code":200,...}`.
var (
	jsonPathRe   = regexp.MustCompile(`"path"\s*:\s*"([^"]*)"`)
	jsonStatusRe = regexp.MustCompile(`"status(?:_code)?"\s*:\s*(\d{3})`)
)

// healthPathRe matches a path segment naming a health/readiness endpoint, bounded so
// "/healthcare" or "/api/health-check" do not false-positive.
var healthPathRe = regexp.MustCompile(`(?i)/(health|healthz|ready)(/|$|\?)`)

// isRoutineHealthCheck reports whether line is an access-log-style record of a *successful* (2xx)
// request to a health/readiness endpoint — the kind of line a monitored process emits every few
// seconds and that otherwise drowns out real events in the live dashboard (docs/SPEC.md §4h,
// Change 13). A failing healthcheck (non-2xx), a different path, or a line that is not an
// access-log record at all is never filtered — only this one narrow, routine pattern is.
func isRoutineHealthCheck(line string) bool {
	if m := accessLogRe.FindStringSubmatch(line); m != nil {
		return healthPathRe.MatchString(m[1]) && isSuccessStatus(m[2])
	}
	path := jsonPathRe.FindStringSubmatch(line)
	status := jsonStatusRe.FindStringSubmatch(line)
	if path != nil && status != nil {
		return healthPathRe.MatchString(path[1]) && isSuccessStatus(status[1])
	}
	return false
}

func isSuccessStatus(status string) bool {
	return len(status) == 3 && status[0] == '2'
}

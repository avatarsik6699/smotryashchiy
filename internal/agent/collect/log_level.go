package collect

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	slogLogLevel     = regexp.MustCompile(`^time=\S+\s+level=([^\s]+)\s+msg=`)
	zapLogLevel      = regexp.MustCompile(`^\S+\t([A-Za-z]+)\t[^\t]+\t`)
	postgresLogLevel = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)? [^ ]+ \[\d+\] ([A-Za-z]+):`)
	uvicornLogLevel  = regexp.MustCompile(`^([A-Za-z]+):`)
)

// dockerLogLevel recognizes severity only in known, anchored log formats. Unrecognized lines
// retain Docker's stdout/info and stderr/warn fallback so message text cannot change severity.
func dockerLogLevel(line string, stderr bool) string {
	if level, ok := jsonLogLevel(line); ok {
		return level
	}
	for _, pattern := range []*regexp.Regexp{slogLogLevel, zapLogLevel, postgresLogLevel, uvicornLogLevel} {
		if match := pattern.FindStringSubmatch(line); len(match) == 2 {
			if level, ok := normalizeLogLevel(match[1]); ok {
				return level
			}
		}
	}
	if stderr {
		return "warn"
	}
	return "info"
}

func jsonLogLevel(line string) (string, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &fields); err != nil || fields == nil {
		return "", false
	}
	for _, key := range []string{"level", "severity"} {
		raw, exists := fields[key]
		if !exists {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) == nil {
			if level, ok := normalizeLogLevel(value); ok {
				return level, true
			}
		}
	}
	return "", false
}

func normalizeLogLevel(level string) (string, bool) {
	switch strings.ToLower(level) {
	case "debug", "info", "notice", "log":
		return "info", true
	case "warn", "warning":
		return "warn", true
	case "error", "fatal", "dpanic":
		return "error", true
	case "panic", "critical":
		return "critical", true
	default:
		return "", false
	}
}

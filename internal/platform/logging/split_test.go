package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestSplitRoutesByLevel(t *testing.T) {
	var out, errOut bytes.Buffer
	log := NewSplit(&out, &errOut).With("component", "test")
	log.Debug("hidden")
	log.Info("request", "status", 200)
	log.Warn("slow")
	log.Error("failed")

	if got := out.String(); !strings.Contains(got, "msg=request") || !strings.Contains(got, "component=test") ||
		strings.Contains(got, "slow") || strings.Contains(got, "failed") || strings.Contains(got, "hidden") {
		t.Fatalf("stdout = %q; want only the info record, with the shared attribute", got)
	}
	if got := errOut.String(); !strings.Contains(got, "msg=slow") || !strings.Contains(got, "msg=failed") ||
		strings.Contains(got, "request") {
		t.Fatalf("stderr = %q; want only the warn and error records", got)
	}
}

package httpserver

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/logging"
)

// captureLogs routes the default logger into two buffers for the test and restores it afterwards.
func captureLogs(t *testing.T) (out, errOut *bytes.Buffer) {
	t.Helper()
	out, errOut = &bytes.Buffer{}, &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(logging.NewSplit(out, errOut))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return out, errOut
}

func TestAccessLineIsInfoOnStdout(t *testing.T) {
	out, errOut := captureLogs(t)
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/hosts", nil))

	if got := out.String(); !strings.Contains(got, "level=INFO msg=request") || !strings.Contains(got, "path=/api/hosts") ||
		!strings.Contains(got, "status=204") || !strings.Contains(got, "request_id=") {
		t.Fatalf("stdout = %q; want the access line", got)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q; want nothing for a routine request", errOut.String())
	}
}

func TestRecoveredPanicIsErrorOnStderr(t *testing.T) {
	out, errOut := captureLogs(t)
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500", rec.Code)
	}
	if got := errOut.String(); !strings.Contains(got, "level=ERROR msg=\"request panicked\"") || !strings.Contains(got, "panic=boom") {
		t.Fatalf("stderr = %q; want the panic record", got)
	}
	if strings.Contains(out.String(), "panic") {
		t.Fatalf("stdout = %q; the panic must not be logged as info", out.String())
	}
}

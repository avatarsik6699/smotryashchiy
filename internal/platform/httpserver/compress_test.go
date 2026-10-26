package httpserver

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveRec(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func gzipRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	return req
}

func TestLargeJSONIsGzipped(t *testing.T) {
	body := `{"rows":"` + strings.Repeat("x", 4000) + `"}`
	h := withCompression(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "4011")
		_, _ = io.WriteString(w, body[:100])
		_, _ = io.WriteString(w, body[100:])
	}))
	rec := serveRec(h, gzipRequest("/api/x"))
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Vary") != "Accept-Encoding" || rec.Header().Get("Content-Length") != "" {
		t.Fatalf("headers = %v", rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != body || rec.Body.Len() > 200 {
		t.Fatalf("round trip %d bytes, compressed %d", len(got), rec.Body.Len())
	}
}

func TestCompressionLeavesTheRestAlone(t *testing.T) {
	small := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	big := func(contentType, encoding string, status int) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentType)
			if encoding != "" {
				w.Header().Set("Content-Encoding", encoding)
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, strings.Repeat("y", 5000))
		})
	}
	noAccept := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	upgrade := gzipRequest("/api/stream")
	upgrade.Header.Set("Upgrade", "websocket")
	ranged := gzipRequest("/file")
	ranged.Header.Set("Range", "bytes=0-10")
	for name, c := range map[string]struct {
		h   http.Handler
		req *http.Request
	}{
		"small":              {small, gzipRequest("/api/x")},
		"no accept-encoding": {big("application/json", "", 200), noAccept},
		"websocket":          {big("application/json", "", 200), upgrade},
		"range":              {big("text/plain", "", 200), ranged},
		"image":              {big("image/png", "", 200), gzipRequest("/x.png")},
		"already brotli":     {big("application/javascript", "br", 200), gzipRequest("/a.js")},
		"error status":       {big("text/plain", "", 500), gzipRequest("/x")},
	} {
		rec := serveRec(compressed(c.h), c.req)
		if enc := rec.Header().Get("Content-Encoding"); enc == "gzip" {
			t.Errorf("%s: gzipped, want untouched", name)
		}
	}
}

func compressed(next http.Handler) http.Handler { return withCompression(next) }

func TestRecoveredPanicStillAnswers500ThroughCompression(t *testing.T) {
	_, _ = captureLogs(t)
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	if rec := serveRec(handler, gzipRequest("/api/x")); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestAcceptsEncoding(t *testing.T) {
	for header, want := range map[string]bool{
		"gzip": true, "deflate, gzip;q=0.5": true, "gzip;q=0": false, "br": false, "*": true, "*;q=0": false, "": false,
		"GZIP": true, "br, *;q=0.1": true,
	} {
		if got := AcceptsEncoding(header, "gzip"); got != want {
			t.Errorf("AcceptsEncoding(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestAPrecompressedAssetVariesOnce(t *testing.T) {
	h := withCompression(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Content-Encoding", "br")
		w.Header().Add("Vary", "Accept-Encoding")
		_, _ = io.WriteString(w, strings.Repeat("z", 3000))
	}))
	rec := serveRec(h, gzipRequest("/assets/a.js"))
	if got := rec.Header().Values("Vary"); len(got) != 1 || rec.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("Vary %v, encoding %q", got, rec.Header().Get("Content-Encoding"))
	}
}

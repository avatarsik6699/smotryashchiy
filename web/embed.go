// Package web embeds the built single-page UI (web/dist) and serves it with the security headers
// and SPA fallback of docs/SPEC.md §4d. A placeholder page is served when the UI was not built, so
// `go build` never needs node.
package web

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// zeroTime disables Last-Modified/If-Modified-Since: embedded files carry no meaningful mtime, and
// index.html is revalidated with no-cache while assets are content-hashed.
var zeroTime time.Time

//go:embed all:dist
var distFS embed.FS

//go:embed placeholder.html
var placeholder []byte

const (
	// csp: the SPA is same-origin only; styles come from files (JS sets them through the CSSOM).
	csp = "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; frame-ancestors 'none'"

	cacheImmutable  = "public, max-age=31536000, immutable" // content-hashed /assets/*
	cacheRevalidate = "no-cache"
)

// Handler serves the embedded UI.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("web: embedded dist is missing: " + err.Error()) // impossible: go:embed guarantees it
	}
	return newHandler(sub, placeholder)
}

type handler struct {
	fsys     fs.FS
	fallback []byte
}

func newHandler(fsys fs.FS, fallbackIndex []byte) http.Handler {
	return &handler{fsys: fsys, fallback: fallbackIndex}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	clean := path.Clean("/" + r.URL.Path)
	if clean == "/api" || strings.HasPrefix(clean, "/api/") {
		// An API path nothing claimed: never answer with the SPA shell.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not found"}`+"\n")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(clean, "/")
	if name != "" {
		if h.serveFile(w, r, name) {
			return
		}
		if path.Ext(name) != "" || name == "assets" || strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r) // a missing asset must fail loudly, not turn into HTML
			return
		}
	}
	h.serveIndex(w, r)
}

func (h *handler) serveFile(w http.ResponseWriter, r *http.Request, name string) bool {
	f, err := h.fsys.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	cache := cacheRevalidate
	if strings.HasPrefix(name, "assets/") {
		cache = cacheImmutable
	}
	w.Header().Set("Cache-Control", cache)
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	serveSeekable(w, r, name, f)
	return true
}

func (h *handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", cacheRevalidate)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if f, err := h.fsys.Open("index.html"); err == nil {
		defer f.Close()
		serveSeekable(w, r, "index.html", f)
		return
	}
	http.ServeContent(w, r, "index.html", zeroTime, bytes.NewReader(h.fallback))
}

func serveSeekable(w http.ResponseWriter, r *http.Request, name string, f fs.File) {
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, zeroTime, rs)
		return
	}
	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, name, zeroTime, bytes.NewReader(data))
}

func setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
}

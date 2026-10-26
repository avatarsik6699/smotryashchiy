package httpserver

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Change 38 (docs/SPEC.md §4d): responses are compressed. Static assets ship precompressed
// (web/embed.go picks the .br or .gz file); everything else that is text — API JSON, track.js —
// is gzipped here on the fly when it is large enough to be worth it.

// minCompressSize: below this a gzip frame costs about what it saves.
const minCompressSize = 1024

var gzipWriters = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
	return w
}}

// AcceptsEncoding reports whether an Accept-Encoding header admits coding (q > 0, or `*`).
func AcceptsEncoding(header, coding string) bool {
	star := false
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))
		if name != coding && name != "*" {
			continue
		}
		q := 1.0
		if v, ok := strings.CutPrefix(strings.ReplaceAll(params, " ", ""), "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		if name == coding {
			return q > 0
		}
		star = q > 0
	}
	return star
}

// compressible lists the media types worth gzipping; images other than SVG are already compressed.
func compressible(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return strings.HasPrefix(mt, "text/") || mt == "application/json" || mt == "application/javascript" ||
		mt == "image/svg+xml" || mt == "application/manifest+json"
}

// withCompression gzips eligible responses. A WebSocket upgrade, a range request and a HEAD pass
// through untouched, as does a response that already carries a Content-Encoding.
func withCompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "" || r.Header.Get("Range") != "" || r.Method == http.MethodHead ||
			!AcceptsEncoding(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		cw := &compressWriter{ResponseWriter: w, status: http.StatusOK}
		defer cw.finish()
		next.ServeHTTP(cw, r)
	})
}

// compressWriter holds the head of a response until it knows whether to gzip it: the first
// minCompressSize bytes decide, or the end of a shorter body.
type compressWriter struct {
	http.ResponseWriter
	status  int
	buf     []byte
	decided bool
	gz      *gzip.Writer
}

func (c *compressWriter) WriteHeader(status int) {
	if !c.decided {
		c.status = status
	}
}

func (c *compressWriter) Write(p []byte) (int, error) {
	if c.decided {
		if c.gz != nil {
			return c.gz.Write(p)
		}
		return c.ResponseWriter.Write(p)
	}
	c.buf = append(c.buf, p...)
	if len(c.buf) >= minCompressSize {
		if err := c.decide(); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (c *compressWriter) decide() error {
	c.decided = true
	h := c.ResponseWriter.Header()
	typed := compressible(h.Get("Content-Type"))
	if typed && !varies(h) {
		h.Add("Vary", "Accept-Encoding")
	}
	ok := typed && len(c.buf) >= minCompressSize && h.Get("Content-Encoding") == "" &&
		c.status >= 200 && c.status < 300 && c.status != http.StatusNoContent && c.status != http.StatusPartialContent
	if ok {
		h.Set("Content-Encoding", "gzip")
		h.Del("Content-Length")
		c.gz = gzipWriters.Get().(*gzip.Writer)
		c.gz.Reset(c.ResponseWriter)
	}
	c.ResponseWriter.WriteHeader(c.status)
	var err error
	if c.gz != nil {
		_, err = c.gz.Write(c.buf)
	} else if len(c.buf) > 0 {
		_, err = c.ResponseWriter.Write(c.buf)
	}
	c.buf = nil
	return err
}

// varies reports whether the response already varies on Accept-Encoding (a precompressed asset).
func varies(h http.Header) bool {
	for _, v := range h.Values("Vary") {
		for _, field := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(field), "Accept-Encoding") {
				return true
			}
		}
	}
	return false
}

func (c *compressWriter) finish() {
	if !c.decided {
		_ = c.decide()
	}
	if c.gz != nil {
		_ = c.gz.Close()
		gzipWriters.Put(c.gz)
		c.gz = nil
	}
}

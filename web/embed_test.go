package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testHandler() http.Handler {
	return newHandler(fstest.MapFS{
		"index.html":        {Data: []byte("<html>app</html>")},
		"assets/app-1a.js":  {Data: []byte("console.log(1)")},
		"assets/app-1a.css": {Data: []byte("body{}")},
		"favicon.svg":       {Data: []byte("<svg/>")},
	}, []byte("<html>placeholder</html>"))
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestEveryResponseCarriesSecurityHeaders(t *testing.T) {
	for _, target := range []string{"/", "/assets/app-1a.js", "/nope.js", "/api/x", "/deep/link"} {
		rec := do(testHandler(), http.MethodGet, target)
		csp := rec.Header().Get("Content-Security-Policy")
		for _, want := range []string{"default-src 'self'", "connect-src 'self'", "frame-ancestors 'none'"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: CSP %q lacks %q", target, csp, want)
			}
		}
		if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
			t.Errorf("%s: CSP must not allow unsafe-inline/unsafe-eval: %q", target, csp)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: missing nosniff/referrer headers", target)
		}
	}
}

func TestIndexIsServedAtRootAndForClientRoutesWithRevalidation(t *testing.T) {
	for _, target := range []string{"/", "/hosts/abc", "/index.html"} {
		rec := do(testHandler(), http.MethodGet, target)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "app") {
			t.Fatalf("%s = %d %q", target, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-cache" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s: Cache-Control %q, Content-Type %q", target, rec.Header().Get("Cache-Control"), rec.Header().Get("Content-Type"))
		}
	}
}

func TestHashedAssetsAreImmutableAndTyped(t *testing.T) {
	js := do(testHandler(), http.MethodGet, "/assets/app-1a.js")
	if js.Code != 200 || js.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || !strings.Contains(js.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("js = %d, cache %q, type %q", js.Code, js.Header().Get("Cache-Control"), js.Header().Get("Content-Type"))
	}
	css := do(testHandler(), http.MethodGet, "/assets/app-1a.css")
	if !strings.HasPrefix(css.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("css type = %q", css.Header().Get("Content-Type"))
	}
	if fav := do(testHandler(), http.MethodGet, "/favicon.svg"); fav.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("non-hashed files must revalidate, got %q", fav.Header().Get("Cache-Control"))
	}
}

func TestMissingAssetIsA404NotTheAppShell(t *testing.T) {
	for _, target := range []string{"/assets/missing.js", "/nope.css", "/assets/"} {
		rec := do(testHandler(), http.MethodGet, target)
		if rec.Code != 404 || strings.Contains(rec.Body.String(), "<html>") {
			t.Errorf("%s = %d %q; a missing file must not fall back to HTML", target, rec.Code, rec.Body.String())
		}
	}
}

func TestUnclaimedAPIPathsAre404JSONNeverHTML(t *testing.T) {
	for _, target := range []string{"/api/nope", "/api", "/api/hosts/x/y", "/api/../api/zzz"} {
		rec := do(testHandler(), http.MethodGet, target)
		if rec.Code != 404 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || strings.Contains(rec.Body.String(), "<html>") {
			t.Errorf("%s = %d (%s) %q", target, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
	}
	if rec := do(testHandler(), http.MethodPost, "/api/anything"); rec.Code != 404 {
		t.Errorf("POST to an unclaimed API path = %d, want 404", rec.Code)
	}
}

func TestOnlyReadMethodsAreServed(t *testing.T) {
	if rec := do(testHandler(), http.MethodPost, "/"); rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST / = %d, Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
	if rec := do(testHandler(), http.MethodHead, "/"); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD / = %d with %d body bytes", rec.Code, rec.Body.Len())
	}
}

func TestPathTraversalCannotEscapeTheEmbeddedRoot(t *testing.T) {
	rec := do(testHandler(), http.MethodGet, "/assets/../../etc/passwd")
	if strings.Contains(rec.Body.String(), "root:") {
		t.Fatal("traversal leaked a file")
	}
}

func TestPlaceholderIsServedWhenTheUIWasNotBuilt(t *testing.T) {
	h := newHandler(fstest.MapFS{".gitkeep": {}}, []byte("<html>placeholder</html>"))
	rec := do(h, http.MethodGet, "/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "placeholder") {
		t.Fatalf("/ = %d %q", rec.Code, rec.Body.String())
	}
}

func TestEmbeddedHandlerBuildsAndServesSomething(t *testing.T) {
	rec := do(Handler(), http.MethodGet, "/")
	if rec.Code != 200 || rec.Body.Len() == 0 {
		t.Fatalf("embedded / = %d, %d bytes", rec.Code, rec.Body.Len())
	}
}

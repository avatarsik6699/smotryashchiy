package http

import (
	_ "embed"
	"net/http"
)

// trackJS is the tracking snippet a tracked site embeds:
//
//	<script defer src="https://<this server>/track.js" data-site="<site id>"></script>
//
// It counts the initial load and every later change of pathname+search made through
// pushState/replaceState/popstate, so a client-side-routed SPA's in-app navigation is its own
// pageview (docs/SPEC.md §4i). It derives both the site id and the collector's own origin from the
// script tag itself (document.currentScript), so the tracked site needs no build step or config.
//
//go:embed track.js
var trackJS []byte

// TrackHandlers serves the public tracking snippet.
type TrackHandlers struct{}

// NewTrackHandlers returns TrackHandlers.
func NewTrackHandlers() *TrackHandlers { return &TrackHandlers{} }

// Register mounts GET /track.js on mux.
func (TrackHandlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /track.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(trackJS)
	})
}

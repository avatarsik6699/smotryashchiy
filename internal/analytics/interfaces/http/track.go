package http

import "net/http"

// trackJS is the tracking snippet a tracked site embeds:
//
//	<script defer src="https://<this server>/track.js" data-site="<site id>"></script>
//
// It listens for pushState/replaceState/popstate in addition to the initial load, so a
// client-side-routed SPA's in-app navigation is its own pageview — the entire reason this project
// chose a JS snippet over passive access-log parsing (docs/SPEC.md §4i). It derives both the site
// id and the collector's own origin from the script tag itself (document.currentScript), so no
// build step or configuration is needed on the tracked site.
const trackJS = `(function(){
"use strict";
var s = document.currentScript;
if (!s) return;
var site = s.getAttribute("data-site");
if (!site) return;
var origin = s.src.replace(/\/track\.js.*$/, "");
function send(){
  var payload = JSON.stringify({
    site: site,
    url: location.pathname + location.search,
    referrer: document.referrer,
    title: document.title,
    screen: (screen.width || 0) + "x" + (screen.height || 0),
    language: navigator.language || ""
  });
  try {
    if (navigator.sendBeacon) {
      navigator.sendBeacon(origin + "/api/collect", new Blob([payload], {type: "text/plain"}));
      return;
    }
  } catch (e) {}
  fetch(origin + "/api/collect", {method: "POST", body: payload, keepalive: true}).catch(function(){});
}
send();
var push = history.pushState, replace = history.replaceState;
history.pushState = function(){ push.apply(history, arguments); send(); };
history.replaceState = function(){ replace.apply(history, arguments); send(); };
window.addEventListener("popstate", send);
})();
`

// TrackHandlers serves the public tracking snippet.
type TrackHandlers struct{}

// NewTrackHandlers returns TrackHandlers.
func NewTrackHandlers() *TrackHandlers { return &TrackHandlers{} }

// Register mounts GET /track.js on mux.
func (TrackHandlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /track.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write([]byte(trackJS))
	})
}

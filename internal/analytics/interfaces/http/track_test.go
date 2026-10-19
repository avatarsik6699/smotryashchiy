package http

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// trackHarness runs the snippet in a node vm with a minimal browser shim, drives a fixed sequence
// of history operations and prints, per step, the URLs of the beacons that step produced.
const trackHarness = `
const vm = require("vm");
const fs = require("fs");
const code = fs.readFileSync(process.argv[2], "utf8");
const beacons = [];
const listeners = {};
const loc = { pathname: "/", search: "", hash: "" };
function setURL(u) {
  const x = new URL(u, "https://site.test" + loc.pathname + loc.search);
  loc.pathname = x.pathname; loc.search = x.search; loc.hash = x.hash;
}
const history = {
  pushState(s, t, u) { if (u != null) setURL(u); },
  replaceState(s, t, u) { if (u != null) setURL(u); },
};
const ctx = vm.createContext({
  document: {
    currentScript: { src: "https://collector.test/track.js", getAttribute: (n) => (n === "data-site" ? "site-1" : null) },
    referrer: "https://ref.test/from?q=1", title: "T",
  },
  location: loc, history, screen: { width: 1, height: 1 },
  navigator: { language: "ru", sendBeacon: (url, blob) => { beacons.push({ url, blob }); return true; } },
  window: { addEventListener: (ev, fn) => { (listeners[ev] = listeners[ev] || []).push(fn); } },
  Blob, JSON, fetch: () => { throw new Error("fetch fallback must not be used when sendBeacon exists"); },
});
const steps = [
  ["load", () => vm.runInContext(code, ctx)],
  ["same-url replaceState (hydration)", () => ctx.history.replaceState(null, "", "/")],
  ["replaceState without url (state only)", () => ctx.history.replaceState({ k: 1 }, "")],
  ["pushState new path", () => ctx.history.pushState(null, "", "/ege")],
  ["replaceState new search", () => ctx.history.replaceState(null, "", "/ege?topic=5")],
  ["hash-only pushState", () => ctx.history.pushState(null, "", "/ege?topic=5#part")],
  ["popstate back", () => { setURL("/"); (listeners.popstate || []).forEach((f) => f()); }],
];
(async () => {
  const out = [];
  for (const [name, run] of steps) {
    const before = beacons.length;
    run();
    const sent = [];
    for (const b of beacons.slice(before)) {
      if (b.url !== "https://collector.test/api/collect") throw new Error("beacon to " + b.url);
      const body = JSON.parse(await b.blob.text());
      if (body.site !== "site-1") throw new Error("beacon for site " + body.site);
      const keys = Object.keys(body).sort().join(",");
      if (keys !== "referrer,site,url") throw new Error("beacon keys " + keys + ", want referrer,site,url");
      sent.push(body.url + " ref=" + body.referrer);
    }
    out.push({ step: name, sent });
  }
  process.stdout.write(JSON.stringify(out));
})().catch((e) => { console.error(e); process.exit(1); });
`

// Each sent entry is "<url> ref=<referrer>": the pathname only, and the page load's referrer only on
// its first pageview (docs/SPEC.md §4i).
func TestTrackJSCountsPathChangesNotHistoryCalls(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed: the tracking snippet's behavior test needs a JS runtime")
	}

	mux := http.NewServeMux()
	NewTrackHandlers().Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/track.js")
	if err != nil {
		t.Fatal(err)
	}
	served, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	snippet := filepath.Join(dir, "track.js")
	harness := filepath.Join(dir, "harness.js")
	if err := os.WriteFile(snippet, served, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(harness, []byte(trackHarness), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, harness, snippet).Output()
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("harness failed: %v\n%s", err, stderr)
	}

	var got []struct {
		Step string   `json:"step"`
		Sent []string `json:"sent"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("harness output %q: %v", out, err)
	}
	want := map[string][]string{
		"load":                                  {"/ ref=https://ref.test/from?q=1"},
		"same-url replaceState (hydration)":     {},
		"replaceState without url (state only)": {},
		"pushState new path":                    {"/ege ref="},
		"replaceState new search":               {},
		"hash-only pushState":                   {},
		"popstate back":                         {"/ ref="},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d: %+v", len(got), len(want), got)
	}
	for _, g := range got {
		if !reflect.DeepEqual(g.Sent, want[g.Step]) {
			t.Errorf("%s: sent %q, want %q", g.Step, g.Sent, want[g.Step])
		}
	}
}

// deliveryHarness runs the snippet with a configurable sendBeacon (argv[3]: "true", "false",
// "throw" or "absent") and data-exclude (argv[4]), and prints every delivery as
// "<channel> <url>" in order.
const deliveryHarness = `
const vm = require("vm");
const fs = require("fs");
const [code, beacon, exclude] = [fs.readFileSync(process.argv[2], "utf8"), process.argv[3], process.argv[4]];
const sent = [];
const loc = { pathname: "/", search: "", hash: "" };
const history = { pushState(s, t, u) { loc.pathname = u; }, replaceState(s, t, u) { if (u) loc.pathname = u; } };
const listeners = {};
const navigator = {};
if (beacon !== "absent") navigator.sendBeacon = (url, blob) => {
  if (beacon === "throw") throw new Error("refused");
  sent.push({ via: "beacon", blob });
  return beacon === "true";
};
const ctx = vm.createContext({
  document: { currentScript: { src: "https://collector.test/track.js",
    getAttribute: (n) => (n === "data-site" ? "site-1" : n === "data-exclude" ? exclude : null) }, referrer: "" },
  location: loc, history, navigator, Blob, JSON,
  window: { addEventListener: (ev, fn) => { (listeners[ev] = listeners[ev] || []).push(fn); } },
  fetch: (url, init) => { sent.push({ via: "fetch", body: init.body }); return Promise.resolve(); },
});
vm.runInContext(code, ctx);
ctx.history.pushState(null, "", "/account");
ctx.history.pushState(null, "", "/account/settings");
ctx.history.pushState(null, "", "/accounting");
ctx.history.pushState(null, "", "/sign-in");
ctx.history.pushState(null, "", "/ege");
(async () => {
  const out = [];
  for (const s of sent) out.push(s.via + " " + JSON.parse(s.body ?? (await s.blob.text())).url);
  process.stdout.write(JSON.stringify(out));
})().catch((e) => { console.error(e); process.exit(1); });
`

// Change 26: excluded paths are never sent, a public page after them counts again, and a refused
// or failing sendBeacon falls back to fetch exactly once (docs/SPEC.md §4i).
func TestTrackJSExcludesPathsAndFallsBackToFetch(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed: the tracking snippet's behavior test needs a JS runtime")
	}
	mux := http.NewServeMux()
	NewTrackHandlers().Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/track.js")
	if err != nil {
		t.Fatal(err)
	}
	served, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	snippet := filepath.Join(dir, "track.js")
	harness := filepath.Join(dir, "harness.js")
	if err := os.WriteFile(snippet, served, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(harness, []byte(deliveryHarness), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := []string{"/", "/accounting", "/ege"}
	for beacon, channel := range map[string]string{"true": "beacon", "false": "fetch", "throw": "fetch", "absent": "fetch"} {
		out, err := exec.Command(node, harness, snippet, beacon, " /account/ , /sign-in").Output()
		if err != nil {
			var stderr []byte
			if ee, ok := err.(*exec.ExitError); ok {
				stderr = ee.Stderr
			}
			t.Fatalf("%s: harness failed: %v\n%s", beacon, err, stderr)
		}
		var got []string
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("%s: harness output %q: %v", beacon, out, err)
		}
		var want []string
		for _, p := range paths {
			if beacon == "false" {
				want = append(want, "beacon "+p)
			}
			want = append(want, channel+" "+p)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("sendBeacon %s: delivered %q, want %q", beacon, got, want)
		}
	}
}

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
    referrer: "", title: "T",
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
      sent.push(body.url);
    }
    out.push({ step: name, sent });
  }
  process.stdout.write(JSON.stringify(out));
})().catch((e) => { console.error(e); process.exit(1); });
`

func TestTrackJSCountsURLChangesNotHistoryCalls(t *testing.T) {
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
		"load":                                  {"/"},
		"same-url replaceState (hydration)":     {},
		"replaceState without url (state only)": {},
		"pushState new path":                    {"/ege"},
		"replaceState new search":               {"/ege?topic=5"},
		"hash-only pushState":                   {},
		"popstate back":                         {"/"},
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

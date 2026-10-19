(function () {
  "use strict";
  var s = document.currentScript;
  if (!s) return;
  var site = s.getAttribute("data-site");
  if (!site) return;
  var origin = s.src.replace(/\/track\.js.*$/, "");
  // Paths the site never wants counted, such as account pages reached by in-app navigation
  // (docs/SPEC.md §4i, Change 26): a listed path itself or anything under it.
  var exclude = (s.getAttribute("data-exclude") || "")
    .split(",")
    .map(function (p) {
      return p.trim().replace(/\/+$/, "");
    })
    .filter(Boolean);
  function excluded(path) {
    for (var i = 0; i < exclude.length; i++) {
      if (path === exclude[i] || path.indexOf(exclude[i] + "/") === 0) return true;
    }
    return false;
  }
  var last = null;
  function send() {
    // A pageview is a pathname change, not a history call: routers call replaceState with the same
    // URL on hydration and for scroll/state bookkeeping. The query string is neither counted nor
    // sent (it can carry tokens), and document.referrer, which client-side navigation never
    // changes, goes only with the first pageview of a page load (docs/SPEC.md §4i).
    var url = location.pathname;
    if (url === last) return;
    var referrer = last === null ? document.referrer : "";
    last = url;
    // An excluded page still becomes `last`, so returning to a public page counts again.
    if (excluded(url)) return;
    // Only what the server stores (docs/SPEC.md §4i): no title, screen or language.
    var payload = JSON.stringify({ site: site, url: url, referrer: referrer });
    // sendBeacon returns false when the browser refuses to queue the data (size or quota):
    // the pageview then goes by fetch instead of being lost (Change 26, audit A1).
    try {
      if (
        navigator.sendBeacon &&
        navigator.sendBeacon(
          origin + "/api/collect",
          new Blob([payload], { type: "text/plain" }),
        )
      )
        return;
    } catch (e) {}
    if (typeof fetch !== "function") return;
    fetch(origin + "/api/collect", {
      method: "POST",
      body: payload,
      keepalive: true,
    }).catch(function () {});
  }
  send();
  var push = history.pushState,
    replace = history.replaceState;
  history.pushState = function () {
    push.apply(history, arguments);
    send();
  };
  history.replaceState = function () {
    replace.apply(history, arguments);
    send();
  };
  window.addEventListener("popstate", send);
})();

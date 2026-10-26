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
  // Change 35: the landing URL's campaign: the five UTM parameters and whether an ad click id is
  // there (its value is never sent). Read once, sent with the first pageview of the load only.
  var landing = (function () {
    var utm = {},
      paid = false;
    try {
      var q = new URLSearchParams(location.search);
      ["source", "medium", "campaign", "content", "term"].forEach(function (k) {
        var v = q.get("utm_" + k);
        if (v) utm[k] = v.slice(0, 100);
      });
      paid = q.has("yclid") || q.has("gclid");
    } catch (e) {}
    return { utm: utm, paid: paid };
  })();
  // Only the referrer's origin leaves the page: a full referrer URL can carry search terms or
  // tokens (Change 35, audit A5).
  function referrerOrigin() {
    try {
      return document.referrer ? new URL(document.referrer).origin : "";
    } catch (e) {
      return "";
    }
  }
  function send() {
    // A pageview is a pathname change, not a history call: routers call replaceState with the same
    // URL on hydration and for scroll/state bookkeeping. The query string is neither counted nor
    // sent (it can carry tokens), and document.referrer, which client-side navigation never
    // changes, goes only with the first pageview of a page load (docs/SPEC.md §4i).
    var url = location.pathname;
    if (url === last) return;
    var first = last === null;
    var referrer = first ? referrerOrigin() : "";
    last = url;
    // An excluded page still becomes `last`, so returning to a public page counts again.
    if (excluded(url)) return;
    // Only what the server stores (docs/SPEC.md §4i): the window width becomes a screen group and
    // the hostname separates www and other hosts of the site; no title or language.
    var body = {
      v: 2,
      site: site,
      url: url,
      referrer: referrer,
      w: window.innerWidth || 0,
      h: location.hostname,
    };
    if (first) {
      body.utm = landing.utm;
      body.paid = landing.paid;
    }
    var payload = JSON.stringify(body);
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

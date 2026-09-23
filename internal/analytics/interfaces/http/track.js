(function () {
  "use strict";
  var s = document.currentScript;
  if (!s) return;
  var site = s.getAttribute("data-site");
  if (!site) return;
  var origin = s.src.replace(/\/track\.js.*$/, "");
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
    // Only what the server stores (docs/SPEC.md §4i): no title, screen or language.
    var payload = JSON.stringify({ site: site, url: url, referrer: referrer });
    try {
      if (navigator.sendBeacon) {
        navigator.sendBeacon(
          origin + "/api/collect",
          new Blob([payload], { type: "text/plain" }),
        );
        return;
      }
    } catch (e) {}
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

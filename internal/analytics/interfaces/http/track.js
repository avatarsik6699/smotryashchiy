(function () {
  "use strict";
  var s = document.currentScript;
  if (!s) return;
  var site = s.getAttribute("data-site");
  if (!site) return;
  var origin = s.src.replace(/\/track\.js.*$/, "");
  var last = null;
  function send() {
    var url = location.pathname + location.search;
    // A pageview is a URL change, not a history call: routers call replaceState with the same URL
    // on hydration and for scroll/state bookkeeping (docs/SPEC.md §4i).
    if (url === last) return;
    last = url;
    var payload = JSON.stringify({
      site: site,
      url: url,
      referrer: document.referrer,
      title: document.title,
      screen: (screen.width || 0) + "x" + (screen.height || 0),
      language: navigator.language || "",
    });
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

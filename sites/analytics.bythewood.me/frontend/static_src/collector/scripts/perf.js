// Real user performance and page health, sent as one perf event when the page
// is first hidden and again only if something new went wrong after that.

(function () {
  var PO = window.PerformanceObserver;
  var nav = performance.getEntriesByType && performance.getEntriesByType("navigation")[0];
  var act = (nav && nav.activationStart) || 0;
  var since = function (t) { return Math.max(0, Math.round(t - act)); };
  // Paint metrics from a tab that opened in the background measure nothing
  // the visitor saw.
  var hiddenAt = document.visibilityState === "hidden" ? 0 : Infinity;

  var v = {}, errs = [], seen = {}, lcpEl = "", sent = false;
  var cls = 0, win = 0, winFirst = 0, winLast = 0, clsEl = "";
  var inps = {};

  function sel(n) {
    if (!n || !n.tagName) return "";
    var s = n.tagName.toLowerCase();
    if (n.id) return s + "#" + n.id;
    if (typeof n.className === "string" && n.className.trim()) s += "." + n.className.trim().split(/\s+/)[0];
    return s;
  }

  function observe(type, fn, opts) {
    try {
      if (PO && PO.supportedEntryTypes.indexOf(type) >= 0) {
        var o = new PO(function (l) { l.getEntries().forEach(fn); });
        o.observe(Object.assign({ type: type, buffered: true }, opts));
        return o;
      }
    } catch (e) {}
  }

  observe("paint", function (e) {
    if (e.name === "first-contentful-paint" && e.startTime < hiddenAt) v.fcp = since(e.startTime);
  });

  function onLCP(e) {
    if (e.startTime < hiddenAt) {
      v.lcp = since(e.startTime);
      lcpEl = sel(e.element) || (e.url ? "img" : "");
    }
  }
  var lcp = observe("largest-contentful-paint", onLCP);
  // LCP stops at the first input, the same as the browser's own definition.
  ["keydown", "pointerdown"].forEach(function (t) {
    addEventListener(t, function () {
      if (lcp) lcp.takeRecords().forEach(onLCP), lcp.disconnect(), lcp = null;
    }, { once: true, capture: true });
  });

  observe("layout-shift", function (e) {
    if (e.hadRecentInput) return;
    // Session windows: shifts under a second apart and within five seconds add up.
    if (win && e.startTime - winLast < 1000 && e.startTime - winFirst < 5000) {
      win += e.value;
    } else {
      win = e.value;
      winFirst = e.startTime;
    }
    winLast = e.startTime;
    if (win > cls) {
      cls = win;
      (e.sources || []).some(function (src) {
        var n = src.node;
        return n && n !== document.body && n !== document.documentElement && (clsEl = sel(n));
      });
    }
  });

  observe("event", function (e) {
    if (e.interactionId) inps[e.interactionId] = Math.max(inps[e.interactionId] || 0, e.duration);
  }, { durationThreshold: 40 });
  observe("first-input", function (e) {
    inps[e.interactionId || 0] = Math.max(inps[e.interactionId || 0] || 0, e.duration);
  });

  function err(k, m, s) {
    m = String(m || "").slice(0, 200);
    var key = k + m + s;
    if (!m || seen[key] || errs.length >= 10) return;
    seen[key] = 1;
    errs.push({ k: k, m: m, s: s || "" });
  }

  function path(u) {
    try {
      var url = new URL(u, location.href);
      return (url.origin === location.origin ? "" : url.host) + url.pathname;
    } catch (e) { return ""; }
  }

  // Errors from extensions or another origin's script aren't this site's to fix.
  addEventListener("error", function (e) {
    var t = e.target;
    if (t && t !== window && (t.src || t.href)) {
      err("resource", t.tagName.toLowerCase() + " " + path(t.src || t.href), path(location.href));
      return;
    }
    var f = e.filename || "";
    if (!f || f.indexOf(location.origin) !== 0) return;
    err("js", (e.error && e.error.message) || e.message, path(f) + ":" + e.lineno + ":" + e.colno);
  }, true);

  addEventListener("unhandledrejection", function (e) {
    var r = e.reason || {}, st = String(r.stack || "");
    if (/-extension:\/\//.test(st)) return;
    var m = st.match(/https?:\/\/[^\s)]+:\d+:\d+/);
    if (m && m[0].indexOf(location.origin) !== 0) return;
    err("promise", r.message || r, m ? path(m[0]) : "");
  });

  document.addEventListener("securitypolicyviolation", function (e) {
    var b = e.blockedURI || "";
    err("csp", e.effectiveDirective + " " + (/^https?:/.test(b) ? new URL(b).host : b), path(e.sourceFile || location.href));
  });

  try {
    new ReportingObserver(function (rs) {
      rs.forEach(function (r) {
        err(r.type, (r.body && (r.body.id || r.body.message)) || "", path((r.body && r.body.sourceFile) || r.url));
      });
    }, { types: ["deprecation", "intervention"], buffered: true }).observe();
  } catch (e) {}

  function inp() {
    var d = Object.keys(inps).map(function (k) { return inps[k]; }).sort(function (a, b) { return b - a; });
    // The 98th percentile, which for fewer than fifty interactions is the worst.
    return d.length ? Math.round(d[Math.min(d.length - 1, Math.floor(d.length / 50))]) : undefined;
  }

  function biggestImage() {
    var worst = null;
    Array.prototype.forEach.call(document.images, function (i) {
      var shown = i.clientWidth * (window.devicePixelRatio || 1);
      if (i.complete && shown > 0 && i.naturalWidth > shown * 2 && i.naturalWidth > 600) {
        if (!worst || i.naturalWidth / shown > worst.r) worst = { r: i.naturalWidth / shown, s: path(i.currentSrc || i.src) };
      }
    });
    return worst && worst.s;
  }

  // Loads that failed before this script ran never reached the listener above.
  function failedLoads() {
    (performance.getEntriesByType("resource") || []).forEach(function (r) {
      if (r.responseStatus >= 400) err("resource", r.initiatorType + " " + path(r.name), path(location.href));
    });
  }

  function send() {
    if (!window.collectorServer || !window.collectorId) return;
    failedLoads();
    var data = { url: location.pathname };
    if (!sent) {
      if (nav) {
        if (nav.responseStart > 0) data.ttfb = since(nav.responseStart);
        if (nav.loadEventEnd > 0) data.load = since(nav.loadEventEnd);
        data.nav = act ? "prerender" : nav.type;
        var nr = nav.notRestoredReasons;
        if (nr && nr.reasons && nr.reasons.length) data.bf = nr.reasons.map(function (r) { return r.reason; }).join(",").slice(0, 200);
      }
      if (v.fcp !== undefined) data.fcp = v.fcp;
      if (v.lcp !== undefined) { data.lcp = v.lcp; data.lcp_el = lcpEl; }
      if (PO && PO.supportedEntryTypes.indexOf("layout-shift") >= 0) { data.cls = Math.round(cls * 1000) / 1000; data.cls_el = clsEl; }
      data.inp = inp();
      if (document.documentElement.scrollWidth > innerWidth + 1) data.overflow = 1;
      data.big_img = biggestImage();
    } else if (!errs.length) {
      return;
    }
    data.errors = errs;
    errs = [];
    sent = true;
    var body = JSON.stringify({ collectorId: window.collectorId, event: "perf", data: data });
    // A string body goes as text/plain, so the beacon needs no CORS preflight.
    if (!(navigator.sendBeacon && navigator.sendBeacon(window.collectorServer + "/collect", body))) {
      fetch(window.collectorServer + "/collect", { method: "POST", body: body, keepalive: true });
    }
  }

  document.addEventListener("visibilitychange", function () {
    if (document.visibilityState === "hidden") {
      hiddenAt = Math.min(hiddenAt, performance.now());
      send();
    }
  });
  addEventListener("pagehide", send);
})();

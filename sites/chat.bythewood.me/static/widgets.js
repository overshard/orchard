// Widgets are the structured half of an answer. The model still writes the
// prose underneath, and these draw the numbers it would otherwise have to
// recite, which it is bad at and which read better as a chart anyway.
//
// A widget is handed only its subject. The readings come from /api/widget/...
// here, so changing a chart's range costs no turn and reopening an old
// conversation draws today's price rather than replaying the one that was on
// screen when it was asked.
(function () {
  "use strict";

  const esc = (s) =>
    String(s == null ? "" : s).replace(/[&<>"']/g, (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

  // A widget endpoint that fails answers with plain text, not json, and
  // r.json() on that throws a SyntaxError which then shows up as the widget's
  // own message. This reads the body once and turns anything unparseable into
  // something worth reading.
  async function readJSON(r) {
    const text = await r.text();
    let d = null;
    try { d = JSON.parse(text); } catch {}
    if (!r.ok || !d || d.error) {
      throw new Error((d && d.error) || (r.ok ? "no data" : "no data (" + r.status + ")"));
    }
    return d;
  }

  const RANGES = [
    { key: "1d", label: "1D" },
    { key: "1w", label: "1W" },
    { key: "1m", label: "1M" },
    { key: "1y", label: "1Y" },
  ];

  // The chart is drawn in a fixed box and stretched to whatever width the
  // message column gives it, which is what dash does. A stretched box turns a
  // circle into an ellipse, so the cursor is a vertical rule and the dot on the
  // line is an HTML element positioned over the top.
  const VW = 1000, VH = 220, PAD = 3;

  const num = (v) => Math.round(v * 100) / 100;

  function money(v, cur) {
    if (v == null || !isFinite(v)) return "";
    const digits = Math.abs(v) >= 1000 ? 2 : Math.abs(v) < 1 ? 4 : 2;
    const s = v.toLocaleString("en-US", { minimumFractionDigits: digits, maximumFractionDigits: digits });
    return cur === "USD" || !cur ? "$" + s : s + " " + cur;
  }

  const signedPct = (v) => (v > 0 ? "+" : "") + v.toFixed(2) + "%";
  const dirOf = (v) => (v > 0.0001 ? "up" : v < -0.0001 ? "down" : "flat");

  function el(tag, cls, html) {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    if (html != null) n.innerHTML = html;
    return n;
  }

  // ---------------------------------------------------------------- ticker

  function tickerWidget(spec) {
    const root = el("section", "wdg wdg-ticker");
    root.innerHTML = `
      <header class="wdg-head">
        <div class="wdg-id">
          <b class="wdg-sym">${esc(spec.symbol || "")}</b>
          <span class="wdg-name"></span>
        </div>
        <div class="wdg-quote">
          <span class="wdg-price"></span>
          <span class="wdg-move"><span class="wdg-chg"></span><span class="wdg-pct"></span></span>
        </div>
      </header>
      <nav class="wdg-ranges">${RANGES.map(
        (r) => `<button type="button" data-range="${r.key}">${r.label}</button>`
      ).join("")}</nav>
      <div class="wdg-plot">
        <svg viewBox="0 0 ${VW} ${VH}" preserveAspectRatio="none" aria-hidden="true" focusable="false">
          <path class="wdg-area"></path>
          <line class="wdg-base" x1="0" x2="${VW}"></line>
          <path class="wdg-line"></path>
          <line class="wdg-cursor" y1="0" y2="${VH}" hidden></line>
          <rect class="wdg-band" y="0" height="${VH}" hidden></rect>
          <line class="wdg-anchor" y1="0" y2="${VH}" hidden></line>
        </svg>
        <i class="wdg-dot" hidden></i>
        <i class="wdg-dot wdg-dot-a" hidden></i>
        <div class="wdg-read" hidden></div>
        <p class="wdg-msg"></p>
      </div>
      <footer class="wdg-foot"><span class="wdg-span"></span><span class="wdg-hint"></span></footer>`;

    const svg = root.querySelector("svg");
    const plot = root.querySelector(".wdg-plot");
    const area = root.querySelector(".wdg-area");
    const line = root.querySelector(".wdg-line");
    const base = root.querySelector(".wdg-base");
    const cursor = root.querySelector(".wdg-cursor");
    const anchor = root.querySelector(".wdg-anchor");
    const band = root.querySelector(".wdg-band");
    const dot = root.querySelector(".wdg-dot");
    const dotA = root.querySelector(".wdg-dot-a");
    const read = root.querySelector(".wdg-read");
    const msg = root.querySelector(".wdg-msg");

    let series = null, xs = [], ys = [], dragFrom = -1, dragging = false;
    let range = "1d";
    const cache = new Map();

    function setRange(key) {
      range = key;
      root.querySelectorAll("[data-range]").forEach((b) =>
        b.classList.toggle("on", b.dataset.range === key));
      load();
    }

    async function load() {
      if (cache.has(range)) return paint(cache.get(range));
      msg.textContent = "";
      root.classList.add("loading");
      try {
        const r = await fetch(
          `/api/widget/ticker?symbol=${encodeURIComponent(spec.symbol)}&range=${range}`);
        const d = await readJSON(r);
        cache.set(range, d);
        paint(d);
      } catch (e) {
        series = null;
        clearCursor();
        line.removeAttribute("d");
        area.removeAttribute("d");
        base.setAttribute("hidden", "");
        msg.textContent = e.message || String(e);
      } finally {
        root.classList.remove("loading");
      }
    }

    function paint(d) {
      series = d;
      msg.textContent = "";
      root.querySelector(".wdg-name").textContent = d.name || "";
      root.querySelector(".wdg-price").textContent = money(d.price, d.currency);
      root.querySelector(".wdg-chg").textContent =
        (d.change > 0 ? "+" : "") + num(d.change).toLocaleString("en-US");
      root.querySelector(".wdg-pct").textContent = signedPct(d.percent || 0);
      root.dataset.dir = dirOf(d.percent || 0);

      const pts = d.points || [];
      const cs = pts.map((p) => p.c);
      let lo = Math.min(...cs), hi = Math.max(...cs);
      // The baseline has to be inside the box or the dotted rule that gives the
      // shape its meaning is drawn off the top of a chart that only went up.
      const hasBase = d.intraday && d.previous > 0;
      if (hasBase) { lo = Math.min(lo, d.previous); hi = Math.max(hi, d.previous); }
      if (hi - lo < 1e-9) hi = lo + 1;

      const sy = (v) => PAD + (1 - (v - lo) / (hi - lo)) * (VH - 2 * PAD);
      const step = pts.length > 1 ? VW / (pts.length - 1) : 0;
      xs = pts.map((_, i) => (pts.length > 1 ? i * step : VW / 2));
      ys = cs.map(sy);

      const path = xs.map((x, i) => `${i ? "L" : "M"}${num(x)},${num(ys[i])}`).join(" ");
      line.setAttribute("d", path);
      area.setAttribute("d", `${path} L${num(xs[xs.length - 1])},${VH} L${num(xs[0])},${VH} Z`);
      if (hasBase) {
        base.setAttribute("y1", num(sy(d.previous)));
        base.setAttribute("y2", num(sy(d.previous)));
        base.removeAttribute("hidden");
      } else {
        base.setAttribute("hidden", "");
      }

      root.querySelector(".wdg-span").textContent = spanLabel(d);
      root.querySelector(".wdg-hint").textContent = pts.length > 1 ? "drag to compare" : "";
      clearCursor();
    }

    function spanLabel(d) {
      const pts = d.points || [];
      if (!pts.length) return "";
      const f = new Date(pts[0].t * 1000), l = new Date(pts[pts.length - 1].t * 1000);
      if (d.range === "1d") {
        const day = f.toLocaleDateString("en-US", { month: "short", day: "numeric" });
        const t = (x) => x.toLocaleString("en-US", { hour: "numeric", minute: "2-digit" });
        return `${day}, ${t(f)} to ${t(l)}`;
      }
      // A year of daily bars starts and ends in the same week of two different
      // years, so without one "Sep 5 to Sep 4" reads as a day.
      const opts = { month: "short", day: "numeric" };
      if (f.getFullYear() !== l.getFullYear()) opts.year = "numeric";
      const stamp = (x) => x.toLocaleDateString("en-US", opts);
      return `${stamp(f)} to ${stamp(l)}`;
    }

    // The index under the pointer. The box is stretched to the column width, so
    // the fraction across the element is the only honest way back to a point.
    function indexAt(clientX) {
      const r = svg.getBoundingClientRect();
      if (!r.width || !xs.length) return -1;
      const frac = Math.min(1, Math.max(0, (clientX - r.left) / r.width));
      return Math.min(xs.length - 1, Math.round(frac * (xs.length - 1)));
    }

    function place(node, i) {
      const r = svg.getBoundingClientRect(), p = plot.getBoundingClientRect();
      node.style.left = (r.left - p.left + (xs[i] / VW) * r.width) + "px";
      node.style.top = (r.top - p.top + (ys[i] / VH) * r.height) + "px";
      node.removeAttribute("hidden");
    }

    function stampAt(i) {
      const t = new Date(series.points[i].t * 1000);
      return series.range === "1d"
        ? t.toLocaleString("en-US", { hour: "numeric", minute: "2-digit" })
        : t.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "2-digit" });
    }

    function show(i) {
      if (!series || i < 0 || i >= xs.length) return;
      cursor.setAttribute("x1", num(xs[i]));
      cursor.setAttribute("x2", num(xs[i]));
      cursor.removeAttribute("hidden");
      place(dot, i);

      const here = series.points[i].c;
      // Dragging measures between the two points held, which is the question a
      // drag is asking. Otherwise it measures from whatever this range's
      // baseline is, which is the previous close intraday and the first bar on
      // every longer span.
      let from, label;
      if (dragFrom >= 0 && dragFrom !== i) {
        from = series.points[dragFrom].c;
        label = `${stampAt(Math.min(dragFrom, i))} to ${stampAt(Math.max(dragFrom, i))}`;
        const a = Math.min(xs[dragFrom], xs[i]), b = Math.max(xs[dragFrom], xs[i]);
        band.setAttribute("x", num(a));
        band.setAttribute("width", num(b - a));
        band.removeAttribute("hidden");
        anchor.setAttribute("x1", num(xs[dragFrom]));
        anchor.setAttribute("x2", num(xs[dragFrom]));
        anchor.removeAttribute("hidden");
        place(dotA, dragFrom);
      } else {
        from = series.intraday && series.previous > 0 ? series.previous : series.points[0].c;
        label = stampAt(i);
      }
      const pct = from > 0 ? ((here - from) / from) * 100 : 0;
      read.innerHTML =
        `<b>${esc(money(here, series.currency))}</b>` +
        `<span class="wdg-read-pct" data-dir="${dirOf(pct)}">${esc(signedPct(pct))}</span>` +
        `<span class="wdg-read-at">${esc(label)}</span>`;
      read.removeAttribute("hidden");

      // Keep the readout inside the plot rather than letting it run off the
      // edge on the first or last bar.
      const r = svg.getBoundingClientRect(), p = plot.getBoundingClientRect();
      const x = r.left - p.left + (xs[i] / VW) * r.width;
      read.style.left = Math.min(Math.max(x, 4), p.width - 4) + "px";
      read.dataset.side = x > p.width / 2 ? "left" : "right";
    }

    function clearCursor() {
      dragFrom = -1; dragging = false;
      for (const n of [cursor, band, anchor, dot, dotA, read]) n.setAttribute("hidden", "");
    }

    plot.addEventListener("pointerdown", (e) => {
      if (!series) return;
      const i = indexAt(e.clientX);
      if (i < 0) return;
      dragging = true;
      dragFrom = i;
      plot.setPointerCapture(e.pointerId);
      show(i);
    });
    plot.addEventListener("pointermove", (e) => {
      if (!series) return;
      const i = indexAt(e.clientX);
      if (i < 0) return;
      // A hover with no button down is a reading, so it must not keep an old
      // drag anchor alive and go on reporting a span nobody is holding.
      if (!dragging) dragFrom = -1;
      show(i);
    });
    const end = (e) => {
      dragging = false;
      if (plot.hasPointerCapture && e.pointerId != null && plot.hasPointerCapture(e.pointerId)) {
        plot.releasePointerCapture(e.pointerId);
      }
    };
    plot.addEventListener("pointerup", end);
    plot.addEventListener("pointercancel", end);
    plot.addEventListener("pointerleave", () => { if (!dragging) clearCursor(); });

    root.querySelector(".wdg-ranges").addEventListener("click", (e) => {
      const b = e.target.closest("[data-range]");
      if (b) setRange(b.dataset.range);
    });

    setRange(spec.range || "1d");
    return root;
  }

  // ---------------------------------------------------------------- weather

  function tile(label, value, note, dir) {
    return `<div class="wdg-tile"${dir ? ` data-dir="${dir}"` : ""}>
      <span class="wdg-tile-k">${esc(label)}</span>
      <b class="wdg-tile-v">${esc(value)}</b>
      <span class="wdg-tile-n">${esc(note || "")}</span>
    </div>`;
  }

  function weatherWidget(spec) {
    const root = el("section", "wdg wdg-weather");
    root.innerHTML = `<p class="wdg-msg">loading</p>`;

    const q = new URLSearchParams({
      lat: spec.lat, lon: spec.lon, place: spec.place || "",
      zip: spec.zip || "", country: spec.country || "", days: "7",
    });

    fetch("/api/widget/weather?" + q)
      .then(readJSON)
      .then((d) => paint(root, d))
      .catch((e) => { root.innerHTML = `<p class="wdg-msg">${esc(e.message || String(e))}</p>`; });

    function paint(root, d) {
      const days = d.days || [];
      // The tiles are the readings that are not a temperature, and each one is
      // dropped rather than shown empty when its source had nothing. Pollen is
      // the one that is often missing, since it is a US only source.
      const tiles = [
        tile("rain", (days[0] ? Math.round(days[0].precip_pct) : 0) + "%", "today"),
        tile("uv", days[0] ? days[0].uv.toFixed(1) : "0", uvBand(days[0] ? days[0].uv : 0)),
      ];
      if (d.has_air) tiles.push(tile("aqi", Math.round(d.aqi), d.aqi_band, aqiDir(d.aqi)));
      if (d.has_pollen) tiles.push(tile("pollen", d.pollen.toFixed(1), d.pollen_band, pollenDir(d.pollen)));
      if (d.humidity) tiles.push(tile("humidity", Math.round(d.humidity) + "%", ""));
      if (d.wind_mph) tiles.push(tile("wind", Math.round(d.wind_mph), "mph"));

      root.innerHTML = `
        <header class="wdg-head">
          <div class="wdg-id">
            <b class="wdg-sym">${esc(d.place || spec.place || "")}</b>
            <span class="wdg-name">${esc(d.summary || "")}</span>
          </div>
          <div class="wdg-quote">
            <span class="wdg-price">${Math.round(d.now_f)}&deg;</span>
            <span class="wdg-move"><span class="wdg-chg">feels ${Math.round(d.feels_f)}&deg;</span></span>
          </div>
        </header>
        <div class="wdg-tiles">${tiles.join("")}</div>
        <ol class="wdg-days">${days.map(dayCell).join("")}</ol>`;
    }

    function dayCell(x, i) {
      // The bar is the day's spread placed on the week's, so a cool day is
      // short and low and a hot one is long and high. Reading seven pairs of
      // numbers is what this replaces.
      return `<li class="wdg-day" title="${esc(x.summary || "")}">
        <span class="wdg-day-k">${i === 0 ? "Today" : esc(x.weekday)}</span>
        <span class="wdg-day-rain" data-wet="${x.precip_pct >= 30 ? "yes" : "no"}">${Math.round(x.precip_pct)}%</span>
        <span class="wdg-day-t"><b>${Math.round(x.high_f)}&deg;</b><i>${Math.round(x.low_f)}&deg;</i></span>
        <span class="wdg-day-feels">feels ${Math.round(x.feels_high_f)}&deg;</span>
      </li>`;
    }

    return root;
  }

  const uvBand = (v) => (v < 3 ? "low" : v < 6 ? "moderate" : v < 8 ? "high" : v < 11 ? "very high" : "extreme");
  const aqiDir = (v) => (v <= 50 ? "up" : v <= 100 ? "flat" : "down");
  const pollenDir = (v) => (v < 4.8 ? "up" : v < 7.2 ? "flat" : "down");

  // ---------------------------------------------------------------- registry

  // Adding a kind is one entry here and one branch on the server. Nothing else
  // in the page knows what a widget is.
  const KINDS = { ticker: tickerWidget, weather: weatherWidget };

  window.Widgets = {
    // render fills a message's widget box. It is called both while a turn is
    // streaming and when a stored conversation is reopened, and it is the same
    // code either way because a widget only ever carries its subject.
    render(box, list) {
      if (!box) return;
      if (!list || !list.length) { box.hidden = true; return; }
      box.hidden = false;
      box.replaceChildren(...list.map(one).filter(Boolean));
    },
    add(box, spec) {
      if (!box) return;
      const node = one(spec);
      if (!node) return;
      box.hidden = false;
      box.appendChild(node);
    },
  };

  function one(spec) {
    const make = KINDS[spec && spec.kind];
    return make ? make(spec) : null;
  }
})();

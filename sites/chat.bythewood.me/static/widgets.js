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
  // The one kind that carries its own content. A report holds eleven sections
  // and an answer shows one, so the rest are invisible to anybody who does not
  // already know they are there. These are buttons rather than a list the model
  // writes out, because the model mangles a list and cannot be clicked.
  function promptsWidget(spec) {
    const asks = (spec && spec.asks) || [];
    if (!asks.length) return null;
    const box = el("div", "wdg asks");
    box.appendChild(el("div", "askhead", "More about " + esc(spec.label || "this")));
    const row = el("div", "askrow");
    asks.forEach((p) => {
      if (!p || !p.ask) return;
      const b = el("button", "askchip");
      b.type = "button";
      b.textContent = p.label || p.ask;
      b.title = p.ask;
      b.addEventListener("click", () => window.Widgets.onAsk && window.Widgets.onAsk(p.ask));
      row.appendChild(b);
    });
    box.appendChild(row);
    return box;
  }

  // ---------------------------------------------------------------- image
  //
  // A picture is half a minute of waiting, spent on the chat model writing a
  // prompt, that model coming off the card and the picture model going on, and
  // then the drawing. The wait is drawn as those stages with a count and a
  // guess on each, so a slow one reads as slow rather than as broken.

  const secs = (ms) => (ms < 10000 ? (ms / 1000).toFixed(1) : String(Math.round(ms / 1000))) + "s";
  const whole = (ms) => Math.max(0, Math.round(ms / 1000)) + "s";

  function frame(w, h, id) {
    const root = el("figure", "wdg wdg-image");
    root.style.setProperty("--w", w || 1024);
    root.style.setProperty("--h", h || 1024);
    root.dataset.image = id || "";
    return root;
  }

  function imageWidget(spec) {
    if (!spec.image) return null;
    const root = frame(spec.width, spec.height, spec.image);
    const src = "/api/image/" + encodeURIComponent(spec.image);
    root.innerHTML = `
      <a class="img-open" href="${src}" target="_blank" rel="noopener">
        <img alt="" decoding="async" loading="lazy" width="${spec.width || 1024}" height="${spec.height || 1024}">
      </a>
      <figcaption class="img-cap">
        <span class="img-model">${esc(spec.model || "picture")}</span>
        <span class="img-meta">${spec.width || 1024} &times; ${spec.height || 1024}<span class="img-took"></span></span>
        <a href="${src}" target="_blank" rel="noopener">full size</a>
      </figcaption>`;
    const img = root.querySelector("img");
    img.alt = spec.label || "";
    img.src = src;
    return root;
  }

  // The panel a picture is drawn behind. It keeps the last progress it was
  // sent and counts the seconds itself between them.
  function imageWait(p) {
    const root = frame(p.width, p.height, p.id);
    root.classList.add("img-wait");
    root.innerHTML = `
      <div class="img-open"><div class="img-panel">
        <ol class="img-stages">
          <li data-stage="prompt"><i></i><span class="k">Writing the prompt<span class="n">the chat model turns what you asked into a description</span></span><span class="t"></span></li>
          <li data-stage="loading"><i></i><span class="k">Loading ${esc(p.model)}<span class="n">the chat model comes off the card to make room</span></span><span class="t"></span></li>
          <li data-stage="drawing"><i></i><span class="k">Drawing<span class="n">four passes over the whole picture</span></span><span class="t"></span></li>
        </ol>
        <div class="img-bar"><b></b></div>
        <p class="img-note"></p>
      </div></div>
      <figcaption class="img-cap">
        <span class="img-model">${esc(p.model)}</span>
        <span class="img-meta"></span>
      </figcaption>`;
    const rows = {
      prompt: root.querySelector('[data-stage="prompt"]'),
      loading: root.querySelector('[data-stage="loading"]'),
      drawing: root.querySelector('[data-stage="drawing"]'),
    };
    const bar = root.querySelector(".img-bar b");
    const note = root.querySelector(".img-note");
    const meta = root.querySelector(".img-meta");
    // A picture the chat model decided on by itself had no prompt stage.
    if (p.stage !== "prompt") rows.prompt.remove();
    let last = p, timer = null;

    function row(name, state, text) {
      rows[name].dataset.state = state;
      rows[name].querySelector(".t").textContent = text;
    }

    // What each stage took, or how long it has been going, or what it
    // usually takes when it has not started yet.
    const ORDER = ["prompt", "loading", "drawing"];
    const took = { prompt: "prompt_ms", loading: "load_ms", drawing: "draw_ms" };
    const usual = { prompt: "prompt_guess", loading: "load_guess", drawing: "draw_guess" };

    function paint() {
      const q = last;
      if (q.stage !== "prompt") meta.innerHTML = `${q.width} &times; ${q.height}`;
      const since = Math.max(0, Date.now() - q.stage_at);
      const at = q.stage === "failed" ? (q.load_ms ? "drawing" : "loading")
        : q.stage === "stopped" ? q.was : q.stage;
      const live = ORDER.indexOf(at);
      let spent = 0, guess = 0, over = false;
      ORDER.forEach((name, i) => {
        if (!rows[name].parentNode) return;
        const g = q[usual[name]] || 0;
        guess += g;
        if (q.stage === "done" || i < live) {
          row(name, "done", secs(q[took[name]] || 0));
          spent += q[took[name]] || 0;
        } else if (i === live && (q.stage === "failed" || q.stage === "stopped")) {
          row(name, "bad", q.stage);
        } else if (i === live) {
          row(name, "on", whole(since) + (g ? " of about " + whole(g) : ""));
          spent += since;
          over = g > 0 && since > g * 1.5 + 5000;
        } else {
          row(name, "wait", g ? "about " + whole(g) : "");
        }
      });
      if (q.stage === "done") {
        note.dataset.tone = "";
        note.textContent = "Done in " + secs(spent) + ", bringing the picture over.";
        bar.style.transform = "scaleX(1)";
        return;
      }
      if (q.stage === "failed" || q.stage === "stopped") {
        note.dataset.tone = "bad";
        note.textContent = q.stage === "stopped" ? "The turn ended before the picture was drawn."
          : "It didn't come out, " + (q.error || "for no reason the server gave") + ".";
        bar.style.transform = "scaleX(0)";
        return;
      }
      // Never full until it is done, since a bar at the end with nothing to
      // show is the thing that looks broken.
      bar.style.transform = "scaleX(" + (guess ? Math.min(0.96, spent / guess) : 0.5) + ")";
      note.dataset.tone = over ? "slow" : "";
      const left = guess - spent;
      note.textContent = over
        ? "Taking longer than it usually does. It gives up on its own after four minutes, and will say so."
        : !guess ? ""
        : left >= 1000 ? "About " + whole(left) + " left, going by the last few."
        : "A little past the usual time, still going.";
    }

    root._update = (q) => {
      last = q;
      paint();
      const live = q.stage === "prompt" || q.stage === "loading" || q.stage === "drawing";
      if (live && !timer) {
        timer = setInterval(() => {
          if (!root.isConnected) { clearInterval(timer); timer = null; return; }
          paint();
        }, 500);
      } else if (!live && timer) {
        clearInterval(timer);
        timer = null;
      }
    };

    // The picture replaces the panel once it has loaded, not once it was
    // named, so there is never an empty frame between the two.
    root._land = (pic) => {
      const took = pic.querySelector(".img-took");
      if (took && last.stage === "done") {
        took.textContent = " \u00b7 loaded in " + secs(last.load_ms) + ", drawn in " + secs(last.draw_ms);
      }
      const img = pic.querySelector("img");
      img.loading = "eager";
      const src = img.src;
      let tries = 0;
      img.addEventListener("load", () => root.replaceWith(pic), { once: true });
      img.addEventListener("error", () => {
        // One more go, since the picture is being filed away at the moment
        // the turn ends and a request that lands in between can miss it.
        if (tries++ < 2) { setTimeout(() => { img.src = src + "?try=" + tries; }, 700 * tries); return; }
        note.dataset.tone = "bad";
        note.innerHTML = `It was drawn but would not load here. <a href="${src}" target="_blank" rel="noopener">Open it on its own</a>.`;
      });
    };

    // The turn ended or failed with this panel still counting, which happens
    // when the chat model errors before it ever calls for the picture.
    root._settle = () => {
      if (last.stage === "done" || last.stage === "failed" || last.stage === "stopped") return;
      root._update({ ...last, stage: "stopped", was: last.stage });
    };

    root._update(p);
    return root;
  }

  const KINDS = { ticker: tickerWidget, weather: weatherWidget, prompts: promptsWidget, image: imageWidget };

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
      const wait = spec.kind === "image" && waitFor(box, spec.image);
      if (wait && wait._land) { wait._land(node); return; }
      box.appendChild(node);
    },
    // settle stops any panel in the box that is still counting.
    settle(box) {
      if (!box) return;
      box.querySelectorAll(".img-wait").forEach((n) => n._settle && n._settle());
    },
    // progress draws or moves on the panel a picture is being made behind.
    progress(box, p) {
      if (!box || !p || !p.id) return;
      let node = waitFor(box, p.id);
      if (!node) {
        node = imageWait(p);
        box.hidden = false;
        box.appendChild(node);
        return;
      }
      if (node._update) node._update(p);
    },
  };

  function waitFor(box, id) {
    return id ? box.querySelector(`.img-wait[data-image="${CSS.escape(id)}"]`) : null;
  }

  function one(spec) {
    const make = KINDS[spec && spec.kind];
    return make ? make(spec) : null;
  }
})();

// The property page's own behaviour, the 90 day bar's drill in, the response
// chart stacked by phase and copying the Markdown report.

const SVG = "http://www.w3.org/2000/svg";
const PHASES = [
  ["dns", "dns", "#63a9c9"],
  ["tcp", "tcp", "#57b378"],
  ["tls", "tls", "#d8a83e"],
  ["ttfb", "first byte", "#7d7469"],
];
const FAILED = "#dc6a4b";

const read = (id) => {
  const el = document.getElementById(id);
  if (!el) return null;
  try {
    return JSON.parse(el.textContent);
  } catch {
    return null;
  }
};

const h = (tag, cls, text) => {
  const el = document.createElement(tag);
  if (cls) el.className = cls;
  if (text != null) el.textContent = text;
  return el;
};

const clock = (iso) => new Date(iso).toISOString().slice(11, 16);
const toast = (text, kind) => document.dispatchEvent(new CustomEvent("o-toast", { detail: { text, kind } }));

// ------------------------------------------------------------------ 90 days

function days() {
  const bar = document.getElementById("days");
  const det = document.getElementById("day-detail");
  const data = read("days-data");
  if (!bar || !det || !data) return;
  const byDay = Object.fromEntries(data.days.map((d) => [d.day, d]));
  const range = document.getElementById("day-range");
  range?.classList.remove("d-none");
  document.querySelector(".drill-hint")?.classList.remove("d-none");

  range?.addEventListener("change", (e) => bar.classList.toggle("r30", e.target.value === "30"));

  const overlaps = (inc, day) => {
    const start = Date.parse(day + "T00:00:00Z");
    const end = start + 86400000;
    const stop = inc.end ? Date.parse(inc.end) : Date.now();
    return Date.parse(inc.start) < end && stop > start;
  };

  const row = (t, kind, text) => {
    const r = h("div");
    r.append(h("span", "t", t), h("span", `p ${kind}`), h("span", "", text));
    return r;
  };

  const timeline = (d) => {
    const tl = h("div", "tl");
    const incs = data.incidents.filter((i) => overlaps(i, d.day)).reverse();
    for (const i of incs) {
      tl.append(row(clock(i.start), "bad", `First check failed, ${i.code}, ${i.cause}`));
      tl.append(row(clock(i.down_at), "bad", "Second failed check, counted as down"));
      if (i.pushed_down != null) tl.append(row(clock(i.down_at), i.pushed_down ? "warn" : "bad", i.pushed_down ? "Pushed the down alert to ntfy" : "The down alert didn't reach ntfy"));
      if (i.end) {
        tl.append(row(clock(i.end), "ok", `Answered 200 again after ${i.fails} failed checks, down for ${i.length}`));
        if (i.pushed_up != null) tl.append(row(clock(i.end), i.pushed_up ? "ok" : "bad", i.pushed_up ? "Pushed the recovery" : "The recovery didn't reach ntfy"));
      } else {
        tl.append(row("now", "bad", `Still failing, ${i.length} so far`));
      }
      if (i.backfilled) tl.append(row("", "", "Read back out of stored checks, so the pushes aren't known"));
    }
    if (!incs.length && d.failed > 0) tl.append(row("all day", "warn", `${d.failed} failed ${d.failed === 1 ? "check" : "checks"}, never two in a row, so no incident and no push`));
    if (!incs.length && d.failed === 0) tl.append(row("all day", "ok", "Every check answered 200. Nothing to show."));
    return tl;
  };

  const hours = (d) => {
    const wrap = h("div", "hrs");
    const top = Math.max(1, ...d.hours.map((x) => x[2] || 0));
    d.hours.forEach((x, i) => {
      const [n, failed, p50] = x;
      const t = h("i");
      const hh = String(i).padStart(2, "0") + ":00";
      if (!n) {
        t.className = "none";
        t.dataset.tip = `${hh}\nno checks`;
      } else {
        if (failed) t.className = failed * 2 >= n ? "bad" : "warn";
        t.style.height = failed ? "100%" : `${Math.max(12, ((p50 || 0) / top) * 100)}%`;
        t.dataset.tip = `${hh}\n${n - failed} of ${n} answered${p50 != null ? `\np50 ${p50}ms` : ""}`;
      }
      wrap.append(t);
    });
    return wrap;
  };

  const show = (key) => {
    const d = byDay[key];
    for (const t of bar.querySelectorAll("i.pick")) t.classList.remove("pick");
    bar.querySelector(`i[data-day="${key}"]`)?.classList.add("pick");
    det.replaceChildren();
    if (!d) {
      det.hidden = true;
      return;
    }
    const a = h("div", "o-panel");
    const head = h("div", "o-ph2");
    head.append(h("h2", "", d.label), h("span", `o-chip ${d.kind}`, d.kind === "bad" ? "outage" : d.kind === "warn" ? "under target" : "clean"));
    const kv = h("dl", "o-kv");
    kv.append(h("dt", "", "uptime"), h("dd", "o-mono", d.uptime.toFixed(2) + "%"));
    kv.append(h("dt", "", "checks"), h("dd", "o-mono", `${d.checks}, ${d.failed} failed`));
    kv.append(h("dt", "", "p50 / p95"), h("dd", "o-mono", d.p50 != null ? `${d.p50}ms / ${d.p95}ms` : "none answered"));
    a.append(head, kv, h("span", "o-lab", "by hour, height is p50"), hours(d));
    const b = h("div", "o-panel");
    const bh = h("div", "o-ph2");
    bh.append(h("h2", "", "Timeline"), h("span", "o-lab", "utc"));
    b.append(bh, timeline(d));
    det.append(a, b);
    det.hidden = false;
  };

  bar.addEventListener("click", (e) => {
    const t = e.target.closest("i[data-day]");
    if (!t) return;
    if (t.classList.contains("pick")) {
      t.classList.remove("pick");
      det.hidden = true;
      return;
    }
    show(t.dataset.day);
  });
}

// ------------------------------------------------------------------ phases

function phases() {
  const host = document.getElementById("ph");
  const leg = document.getElementById("ph-leg");
  const data = read("phase-data");
  if (!host || !leg || !data || !data.length) return;
  const on = Object.fromEntries(PHASES.map((p) => [p[0], true]));
  const W = 900;
  const H = 150;

  // A failed check has no phases, so it stands as one red bar of its total.
  const parts = (c) => {
    if (c.code !== 200 || c.ttfb == null) return [["failed", c.total, FAILED]];
    return PHASES.filter((p) => on[p[0]]).map((p) => [p[0], c[p[0]] || 0, p[2]]);
  };

  const node = (tag, attrs) => {
    const n = document.createElementNS(SVG, tag);
    for (const k in attrs) n.setAttribute(k, attrs[k]);
    return n;
  };

  const render = () => {
    // Scaled to the checks that answered, so one 10s timeout doesn't flatten
    // the rest, and a failed bar taller than that is clipped at the top.
    const sums = data.map((c) => parts(c).reduce((a, p) => a + p[1], 0));
    const answered = sums.filter((_, i) => data[i].code === 200);
    const top = Math.max(1, ...(answered.length ? answered : sums));
    const bw = W / data.length;
    const svg = node("svg", { class: "ch", viewBox: `0 0 ${W} ${H}`, preserveAspectRatio: "none", role: "img", "aria-label": "Response time by phase" });
    for (let k = 1; k <= 3; k++) svg.append(node("line", { x1: 0, x2: W, y1: H - (H * k) / 4, y2: H - (H * k) / 4, class: "grid" }));
    data.forEach((c, i) => {
      let y = H;
      for (const [, v, fill] of parts(c)) {
        const hgt = Math.min(y, (v / top) * (H - 4));
        y -= hgt;
        svg.append(node("rect", { x: (i * bw + 0.5).toFixed(1), y: y.toFixed(1), width: Math.max(0.5, bw - 1).toFixed(1), height: hgt.toFixed(1), fill }));
      }
      const hit = node("rect", { x: (i * bw).toFixed(1), y: 0, width: bw.toFixed(1), height: H, class: "hit" });
      const lines = [`${clock(c.label)} · ${c.code} · ${c.total}ms`];
      if (c.code === 200 && c.ttfb != null) for (const p of PHASES) lines.push(`${p[1]} ${c[p[0]] ?? "-"}ms`);
      hit.dataset.tip = lines.join("\n");
      hit.dataset.i = i;
      svg.append(hit);
    });
    const axis = h("div", "axis");
    for (const i of [0, data.length >> 1, data.length - 1]) axis.append(h("span", "", clock(data[i].label)));
    const vl = h("i", "vl");
    host.replaceChildren(svg, vl, axis);

    leg.replaceChildren();
    for (const p of PHASES) {
      const b = h("button", on[p[0]] ? "" : "off");
      b.type = "button";
      b.dataset.p = p[0];
      b.setAttribute("aria-pressed", String(on[p[0]]));
      const sw = h("i");
      sw.style.background = p[2];
      b.append(sw, p[1]);
      leg.append(b);
    }
    const f = h("span");
    const sw = h("i");
    sw.style.background = FAILED;
    f.append(sw, "failed");
    leg.append(f);
  };

  host.addEventListener("pointermove", (e) => {
    const t = e.target.closest?.("rect.hit");
    const vl = host.querySelector(".vl");
    if (!t || !vl) return;
    const r = host.getBoundingClientRect();
    vl.style.display = "block";
    vl.style.transform = `translateX(${((+t.dataset.i + 0.5) / data.length) * r.width}px)`;
  });
  host.addEventListener("pointerleave", () => {
    const vl = host.querySelector(".vl");
    if (vl) vl.style.display = "none";
  });
  leg.addEventListener("click", (e) => {
    const b = e.target.closest("[data-p]");
    if (!b) return;
    const k = b.dataset.p;
    if (on[k] && Object.values(on).filter(Boolean).length === 1) return;
    on[k] = !on[k];
    render();
  });
  render();
}

// ------------------------------------------------------------------ markdown

function copyMarkdown() {
  const btn = document.getElementById("copy-md");
  if (!btn) return;
  btn.addEventListener("click", async () => {
    // A promise inside ClipboardItem keeps the click's permission through the
    // fetch, which Safari drops for a writeText after an await.
    const text = fetch(btn.dataset.md, { credentials: "same-origin" }).then((res) => {
      if (!res.ok) throw new Error(res.status);
      return res.text();
    });
    try {
      if (window.ClipboardItem) {
        const blob = text.then((t) => new Blob([t], { type: "text/plain" }));
        await navigator.clipboard.write([new ClipboardItem({ "text/plain": blob })]);
      } else {
        await navigator.clipboard.writeText(await text);
      }
      toast("Copied the status report as Markdown");
    } catch {
      toast("Couldn't copy the report", "bad");
    }
  });
}

document.addEventListener("DOMContentLoaded", () => {
  days();
  phases();
  copyMarkdown();
});

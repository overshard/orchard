// The visitors chart is drawn on the server. This adds the hover, a rule at
// the bucket under the pointer and both periods' numbers beside it, and lets a
// tile above swap the chart to its own series.
document.addEventListener("DOMContentLoaded", () => {
  const box = document.getElementById("visitors-chart");
  const raw = document.getElementById("chart-visitors-data");
  if (!box || !raw) return;
  const { points, marks } = JSON.parse(raw.textContent);
  if (!points || !points.length) return;
  const markAt = new Map((marks || []).map((m) => [m.i, m.label]));
  const rule = box.querySelector(".vl");
  const tip = document.createElement("div");
  tip.className = "o-tip vtip";
  tip.hidden = true;
  box.append(tip);

  const cl = box.querySelector(".cl");
  const ca = box.querySelector(".ca");
  const pv = box.querySelector(".pv");
  const visitors = { line: cl.getAttribute("d"), area: ca.getAttribute("d") };
  const title = document.getElementById("chart-title");
  const pm = document.getElementById("chart-pm");
  const leg = document.getElementById("chart-leg");
  const legPrev = document.querySelector(".leg .leg-prev");
  // null is the visitors chart the server drew, with the period before.
  let metric = null;

  const line = (cls, text) => {
    const s = document.createElement("span");
    if (cls) s.className = cls;
    s.textContent = text;
    return s;
  };

  const pick = (tile, i) => {
    document.querySelectorAll(".o-tile.pick").forEach((t) => t.classList.toggle("on", t === tile));
    const name = tile.dataset.metric;
    title.textContent = `${name[0].toUpperCase()}${name.slice(1)} over time`;
    leg.textContent = name;
    const isVisitors = i === 0;
    pv.style.display = isVisitors ? "" : "none";
    if (pm) pm.hidden = !isVisitors;
    if (legPrev) legPrev.hidden = !isVisitors;
    if (isVisitors) {
      metric = null;
      cl.setAttribute("d", visitors.line);
      ca.setAttribute("d", visitors.area);
      return;
    }
    const values = tile.querySelector(".o-spark").dataset.v.split(",").map(Number);
    const unit = (tile.querySelector("b").textContent.trim().match(/[%s]$/) || [""])[0];
    const top = Math.max(...values, 1) * 1.1;
    const n = values.length;
    const x = (j) => (n === 1 ? 500 : (j / (n - 1)) * 1000);
    const d = values.map((v, j) => `${j ? "L" : "M"}${x(j).toFixed(1)},${(150 - (v / top) * 146).toFixed(1)}`).join(" ");
    cl.setAttribute("d", d);
    ca.setAttribute("d", `${d} L${x(n - 1).toFixed(1)},150 L${x(0).toFixed(1)},150 Z`);
    metric = { name, values, unit };
  };
  document.querySelectorAll(".o-tile.pick").forEach((t, i) => t.addEventListener("click", () => pick(t, i)));

  box.addEventListener("pointermove", (e) => {
    const r = box.getBoundingClientRect();
    const n = points.length;
    const i = n === 1 ? 0 : Math.max(0, Math.min(n - 1, Math.round(((e.clientX - r.left) / r.width) * (n - 1))));
    const p = points[i];
    const x = n === 1 ? r.width / 2 : (i / (n - 1)) * r.width;
    rule.style.transform = `translateX(${x}px)`;
    rule.hidden = false;
    let rows;
    if (metric) {
      rows = [line("t", p.label), line("", `${metric.name} ${(metric.values[i] ?? 0).toLocaleString()}${metric.unit}`)];
    } else {
      rows = [line("t", p.label), line("", `visitors ${p.count.toLocaleString()}`), line("k", `${p.prev_label} ${p.prev.toLocaleString()}`)];
      if (p.prev) {
        const pct = Math.round(((p.count - p.prev) / p.prev) * 100);
        rows.push(line(pct >= 0 ? "ok" : "warn", `${pct > 0 ? "+" : pct < 0 ? "−" : ""}${Math.abs(pct)}% on before`));
      }
    }
    if (markAt.has(i)) rows.push(line("k", markAt.get(i)));
    tip.replaceChildren(...rows);
    tip.hidden = false;
    const w = tip.offsetWidth;
    const left = x + 12 + w > r.width ? x - 12 - w : x + 12;
    tip.style.transform = `translate(${Math.round(left)}px, 8px)`;
  });
  box.addEventListener("pointerleave", () => {
    tip.hidden = true;
    rule.hidden = true;
  });
});

// The visitors chart is drawn on the server. This adds the hover, a rule at
// the bucket under the pointer and both periods' numbers beside it.
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

  const line = (cls, text) => {
    const s = document.createElement("span");
    if (cls) s.className = cls;
    s.textContent = text;
    return s;
  };

  box.addEventListener("pointermove", (e) => {
    const r = box.getBoundingClientRect();
    const n = points.length;
    const i = n === 1 ? 0 : Math.max(0, Math.min(n - 1, Math.round(((e.clientX - r.left) / r.width) * (n - 1))));
    const p = points[i];
    const x = n === 1 ? r.width / 2 : (i / (n - 1)) * r.width;
    rule.style.transform = `translateX(${x}px)`;
    rule.hidden = false;
    const rows = [line("t", p.label), line("", `visitors ${p.count.toLocaleString()}`), line("k", `${p.prev_label} ${p.prev.toLocaleString()}`)];
    if (p.prev) {
      const pct = Math.round(((p.count - p.prev) / p.prev) * 100);
      rows.push(line(pct >= 0 ? "ok" : "warn", `${pct > 0 ? "+" : pct < 0 ? "−" : ""}${Math.abs(pct)}% on before`));
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

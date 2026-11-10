import { $, watch, TIERS } from "./api.js";
import { drawIcon } from "./icons.js";

function tile(kind, found) {
  const cv = document.createElement("canvas"), ctx = cv.getContext("2d");
  cv.width = 76; cv.height = 48;
  drawIcon(ctx, kind, 22, 8, 4);
  if (!found) {
    ctx.globalCompositeOperation = "source-atop";
    ctx.fillStyle = "#2f2820"; ctx.fillRect(0, 0, 76, 48);
  }
  return cv;
}

function stamp(day) {
  const d = new Date(day + "T12:00:00");
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" }).toUpperCase();
}

function render(p) {
  const all = p.keepsakes || [];
  $("keep-count").textContent = all.filter((k) => k.days).length + " OF " + all.length;
  const grid = $("keep-grid");
  grid.textContent = "";
  all.forEach((k) => {
    const d = document.createElement("div"), n = document.createElement("span"), w = document.createElement("span"), t = document.createElement("span");
    d.className = "hattile";
    if (k.days) {
      n.textContent = k.name + (k.days > 1 ? " x" + k.days : "");
      w.textContent = stamp(k.first);
      d.setAttribute("aria-label", k.name + ", " + TIERS[k.tier] + ", first kept " + stamp(k.first) + (k.days > 1 ? ", " + k.days + " days" : ""));
    } else {
      n.textContent = "?";
      w.textContent = k.hint;
      d.setAttribute("aria-label", "A " + TIERS[k.tier] + " keepsake not kept yet. " + k.hint);
    }
    w.className = "dim";
    t.className = "tier t" + k.tier; t.textContent = TIERS[k.tier];
    d.append(tile(k.kind, k.days > 0), n, w, t);
    grid.append(d);
  });
}

export function initKeepsakes() { watch(render); }

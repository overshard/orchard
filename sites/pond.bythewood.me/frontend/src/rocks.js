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

function render(p) {
  const rocks = p.rocks || [];
  $("rock-count").textContent = rocks.filter((r) => r.found).length + " OF " + rocks.length;
  const grid = $("rock-grid");
  grid.textContent = "";
  rocks.forEach((r) => {
    const d = document.createElement("div"), n = document.createElement("span"), t = document.createElement("span");
    d.className = "hattile";
    d.setAttribute("aria-label", r.found ? r.name + ", " + TIERS[r.tier] + (r.count ? ", " + r.count + " held" : ", none left")
      : "A " + TIERS[r.tier] + " rock not found yet");
    n.textContent = r.found ? r.name + (r.count > 1 ? " x" + r.count : "") + (r.count ? "" : ", none left") : "?";
    t.className = "tier t" + r.tier; t.textContent = TIERS[r.tier];
    d.append(tile(r.kind, r.found), n, t);
    grid.append(d);
  });
}

export function initRocks() { watch(render); }

import qrcode from "qrcode-generator";
import { genome, bred, cells, colours, draw, croak, W, H } from "./frog.js";
import { drawIcon } from "./icons.js";
import { hatTile } from "./hats.js";
import { fishArt } from "./fishes.js";
import { stampArt } from "./company.js";

const scene = document.getElementById("scene"), sx = scene.getContext("2d"), d = scene.dataset;
const g = bred(genome(+d.seed), d.mods ? JSON.parse(d.mods) : null), stage = d.stage, hat = { shape: +d.hat, colour: +d.colour };
const spots = (d.spots || "").split(",");
const grown = stage === "froglet" || stage === "frog" || stage === "elder";
const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
let croakUntil = 0;

function frame(now) {
  const t = now / 1000;
  ["#123a3d", "#103336", "#0e2c30", "#0c262a"].forEach((c, i) => { sx.fillStyle = c; sx.fillRect(0, i * 68, 352, 68); });
  if (spots[2]) drawIcon(sx, spots[2], 160, 12 + Math.round(Math.sin(t * 0.9 + 2)) * 4, 4);
  if (grown) {
    for (let y = 222; y < 268; y += 4) for (let x = 20; x < 332; x += 4) {
      const a = (x + 2 - 176) / 150, b = (y + 2 - 246) / 20, dd = a * a + b * b;
      if (dd > 1 || (x > 286 && Math.abs(y - 240) < (x - 286) * 0.3)) continue;
      sx.fillStyle = dd > 0.82 ? "#1f5a2c" : (y < 236 ? "#56b45f" : "#3f9a4d");
      sx.fillRect(x, y, 4, 4);
    }
  }
  const o = { t: reduced ? 0 : t, hat: hat.shape, legs: +d.day >= 6 };
  if (grown && !reduced) {
    o.blink = (t % 4.3) < 0.16;
    o.breathe = Math.sin(t * 2.2) > 0.4 ? 0.5 : 0;
    if (now < croakUntil) o.croak = 0.55 + 0.45 * Math.abs(Math.sin(t * g.rate * 0.5));
  }
  draw(sx, cells(g, stage, o), W, H, colours(g, hat), 0, 0, 8);
  if (grown && spots[0]) drawIcon(sx, spots[0], 40, 236, 4);
  if (grown && spots[1]) drawIcon(sx, spots[1], 280, 236, 4);
}

let last = 0;
function loop(now) {
  if (now - last > 110) { last = now; frame(now); }
  requestAnimationFrame(loop);
}
frame(0);
if (!reduced) requestAnimationFrame(loop);

document.getElementById("scene-btn").addEventListener("click", () => {
  croakUntil = performance.now() + croak(g) * 1000;
});

const qr = document.getElementById("qr"), q = qrcode(0, "M");
q.addData(qr.dataset.url); q.make();
const n = q.getModuleCount(), m = Math.floor(qr.width / n), off = Math.floor((qr.width - n * m) / 2), qx = qr.getContext("2d");
qx.fillStyle = "#fff6dc"; qx.fillRect(0, 0, qr.width, qr.height);
qx.fillStyle = "#0a0908";
for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) if (q.isDark(r, c)) qx.fillRect(off + c * m, off + r * m, m, m);

const copy = document.getElementById("share-copy");
copy.addEventListener("click", () => {
  navigator.clipboard.writeText(qr.dataset.url).then(() => { copy.textContent = "Copied"; }, () => { copy.textContent = "Select it above"; });
});

// Best finds are drawn the way the bag draws them.
document.querySelectorAll("canvas.find").forEach((cv) => {
  const f = cv.dataset;
  if (f.shape !== undefined) { const t = hatTile({ shape: +f.shape, colour: +f.colour }, true); t.setAttribute("aria-hidden", "true"); cv.replaceWith(t); }
  else if (f.kind.startsWith("fish_")) { const t = fishArt(f.kind.slice(5)); t.setAttribute("aria-hidden", "true"); cv.replaceWith(t); }
  else drawIcon(cv.getContext("2d"), f.kind, 0, 0, 4);
});

// The page is the same cached copy for everybody, so what this visitor can
// stamp is asked for once it's open.
const keys = document.getElementById("stamps"), stampLine = document.getElementById("stamp-line");
const SAYS = {
  none: "Stamps are left by frogs. Get one of your own and come back to press one.",
  own: "Your frog's page. Stamps left here turn up when you next open the pond.",
  open: "Press one to leave it, once a day. The owner sees your frog's name and look.",
  done: "Stamped. Come back tomorrow for another.",
  spent: "That's all the stamping for today.",
};

function showStamps(st) {
  keys.querySelectorAll("button").forEach((b) => {
    b.disabled = st.state !== "open";
    b.setAttribute("aria-pressed", st.stamp === b.dataset.stamp ? "true" : "false");
  });
  stampLine.textContent = SAYS[st.state] || "";
}

keys.querySelectorAll("button").forEach((b) => {
  b.querySelector("canvas").replaceWith(stampArt(b.dataset.stamp, 4));
  b.addEventListener("click", () => {
    keys.querySelectorAll("button").forEach((x) => { x.disabled = true; });
    fetch("/api/stamp", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ page: keys.dataset.page, stamp: b.dataset.stamp }) })
      .then((r) => r.json().then((d) => (r.ok ? showStamps(d) : (stampLine.textContent = d.error || "That didn't work."))));
  });
});
fetch("/api/stamps?f=" + encodeURIComponent(keys.dataset.page), { credentials: "same-origin" })
  .then((r) => (r.ok ? r.json() : null)).then((st) => { if (st) showStamps(st); });

// The visitor's frog from the pond, asleep on a lily pad while everything on the
// page is up and hiding under a log while anything is down. pond is same site,
// so its cookie rides along, and a visitor with no frog gets the house frog.
import { W, H, genome, bred, cells, colours, draw } from "./frog.js";

const POND = "https://pond.bythewood.me";
const SW = 64, SH = 38, PX = 2, FX = 10, FY = 0;
const WATER = "#1d3a3a", WATER_HI = "#2d5552", PAD = "#4f7d4a", PAD_HI = "#6b9e78", PAD_DARK = "#355a35";
const BARK = "#5a4330", BARK_HI = "#7a5c40", BARK_DARK = "#3a2a1e", RING = "#a07c55", Z = "#ddd7cd";

function ell(x, y, cx, cy, rx, ry) { const a = (x - cx) / rx, b = (y - cy) / ry; return a * a + b * b <= 1; }

function cell(ctx, x, y, col) { ctx.fillStyle = col; ctx.fillRect(x * PX, y * PX, PX, PX); }

function water(ctx, t, from = 32) {
  for (let y = from; y < SH; y++) for (let x = 0; x < SW; x++) {
    const glint = (x + Math.floor(t * 3) + y * 7) % 23 === 0;
    cell(ctx, x, y, glint ? WATER_HI : WATER);
  }
}

// A pad with its notch, floating at the water line under the frog.
function pad(ctx, bob) {
  const cy = 32 + bob;
  for (let y = 27; y < SH; y++) for (let x = 0; x < SW; x++) {
    const px = x + 0.5, py = y + 0.5;
    if (!ell(px, py, 32, cy, 25, 3.6)) continue;
    if (px > 32 && px < 36 && py > cy) continue;
    cell(ctx, x, y, py < cy - 2.2 ? PAD_HI : ell(px, py, 32, cy, 21, 2.6) ? PAD : PAD_DARK);
  }
}

// A log lying across the water, in front of the frog so only its eyes show.
function log(ctx) {
  for (let y = 18; y < SH; y++) for (let x = 2; x < SW - 2; x++) {
    const px = x + 0.5, py = y + 0.5;
    const end = ell(px, py, SW - 6, 27, 4, 7);
    if (!(px < SW - 6 && py > 20 && py < 34) && !end) continue;
    let col = py < 22 ? BARK_HI : py > 32 ? BARK_DARK : (x * 7 + y * 3) % 11 === 0 ? BARK_DARK : BARK;
    if (end) col = ell(px, py, SW - 6, 27, 2.4, 4.4) ? (ell(px, py, SW - 6, 27, 1, 2) ? BARK : RING) : BARK_DARK;
    cell(ctx, x, y, col);
  }
}

// The top row of the frog itself, hat left out, so an egg, a tadpole and a
// grown frog all peek over the log by the same amount.
function head(c) {
  for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) { const v = c[y * W + x]; if (v && v < 10) return y; }
  return 0;
}

// A z drifts up from a sleeping frog every couple of seconds.
function zees(ctx, t) {
  for (let i = 0; i < 2; i++) {
    const p = (t / 2.4 + i / 2) % 1, x = Math.round(44 + p * 8), y = Math.round(14 - p * 12);
    if (p > 0.85) continue;
    for (let k = 0; k < 3; k++) { cell(ctx, x + k, y, Z); cell(ctx, x + k, y + 2, Z); }
    cell(ctx, x + 1, y + 1, Z);
  }
}

function show(el, d) {
  const f = d.frog, g = bred(genome(f.seed), f.mods), hat = f.hat && f.hat.shape ? f.hat : null;
  const col = colours(g, hat), down = el.dataset.down === "true" ? 1 : Number(el.dataset.down) || 0;
  const cv = el.querySelector("canvas"), ctx = cv.getContext("2d");
  cv.width = SW * PX; cv.height = SH * PX;
  const name = f.name.charAt(0) + f.name.slice(1).toLowerCase();
  const line = down
    ? name + " is under the log until " + (down === 1 ? "it's" : "they're") + " back up"
    : name + " is asleep, everything's up";
  el.querySelector("[data-pond-line]").textContent = line;
  cv.title = line;
  el.hidden = false;

  const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
  let blinkAt = 0;
  function frame(ms) {
    const t = still ? 0 : ms / 1000;
    ctx.clearRect(0, 0, cv.width, cv.height);
    water(ctx, t);
    if (down) {
      if (!blinkAt) blinkAt = t + 2 + Math.random() * 3;
      const blink = t > blinkAt;
      if (t > blinkAt + 0.14) blinkAt = t + 2 + Math.random() * 4;
      const peek = Math.round(Math.sin(t * 0.9) * 0.6);
      const c = cells(g, f.stage, { t, hat: hat ? hat.shape : 0, blink, legs: f.day >= 6 });
      draw(ctx, c, W, H, col, FX * PX, (FY + 16 - head(c) + peek) * PX, PX);
      log(ctx);
      water(ctx, t, 34);
    } else {
      const bob = Math.sin(t * 1.1) * 0.4;
      pad(ctx, bob);
      const c = cells(g, f.stage, { t, hat: hat ? hat.shape : 0, blink: f.stage !== "egg", breathe: Math.sin(t * 1.2) * 0.5, legs: f.day >= 6 });
      draw(ctx, c, W, H, col, FX * PX, Math.round(FY + bob) * PX, PX);
      if (!still) zees(ctx, t);
    }
    if (!still) requestAnimationFrame(frame);
  }
  requestAnimationFrame(frame);
  cv.addEventListener("click", () => { window.open(POND, "_blank", "noopener"); });
}

const el = document.querySelector("[data-pond]");
if (el) {
  fetch(POND + "/api/panel", { credentials: "include" })
    .then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
    .then((d) => show(el, d))
    .catch(() => {});
}

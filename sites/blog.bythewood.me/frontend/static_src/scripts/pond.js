// The visitor's frog from the pond, reading along at the foot of a post. pond is
// same site, so its cookie rides along, and a visitor with no frog gets the
// house frog. It closes its book once the reader reaches it.
import { W, H, genome, bred, cells, colours, draw } from "./frog.js";

const POND = "https://pond.bythewood.me";
const SW = 64, SH = 38, PX = 2, FX = 6;
const WATER = "#1d3a3a", WATER_HI = "#2d5552", PAD = "#4f7d4a", PAD_HI = "#6b9e78", PAD_DARK = "#355a35";
const PAGE = "#f3e4c2", PAGE_DIM = "#cdbb94", INK = "#8a7d66", COVER = "#b3222f", COVER_DARK = "#6e1720", SPINE = "#3a2412";

function ell(x, y, cx, cy, rx, ry) { const a = (x - cx) / rx, b = (y - cy) / ry; return a * a + b * b <= 1; }

function cell(ctx, x, y, col) { ctx.fillStyle = col; ctx.fillRect(x * PX, y * PX, PX, PX); }

function water(ctx, t) {
  for (let y = 32; y < SH; y++) for (let x = 0; x < SW; x++) {
    const glint = (x + Math.floor(t * 3) + y * 7) % 23 === 0;
    cell(ctx, x, y, glint ? WATER_HI : WATER);
  }
}

function pad(ctx, bob) {
  const cy = 32 + bob;
  for (let y = 27; y < SH; y++) for (let x = 0; x < SW; x++) {
    const px = x + 0.5, py = y + 0.5;
    if (!ell(px, py, 30, cy, 27, 3.6)) continue;
    if (px > 30 && px < 34 && py > cy) continue;
    cell(ctx, x, y, py < cy - 2.2 ? PAD_HI : ell(px, py, 30, cy, 23, 2.6) ? PAD : PAD_DARK);
  }
}

// An open book with a page turning now and then, or shut once the reader is done.
// x and y are its top left cell, and it's 14 cells wide and 8 tall when open.
function book(ctx, x, y, open, turn) {
  if (!open) {
    for (let j = 0; j < 7; j++) for (let i = 0; i < 9; i++) {
      const edge = j === 0 || j === 6 || i === 8;
      cell(ctx, x + 3 + i, y + 1 + j, i === 0 ? SPINE : edge ? COVER_DARK : COVER);
    }
    for (let i = 3; i < 7; i++) cell(ctx, x + 3 + i, y + 3, PAGE);
    return;
  }
  for (let i = 0; i < 14; i++) { cell(ctx, x + i, y + 7, COVER_DARK); cell(ctx, x + i, y + 8, COVER); }
  for (let j = 0; j < 7; j++) for (let i = 0; i < 14; i++) {
    const mid = i === 6 || i === 7;
    let col = mid ? PAGE_DIM : PAGE;
    if (!mid && j > 0 && j % 2 === 0 && i > 0 && i < 13 && (i * 5 + j * 3) % 7 !== 0) col = INK;
    cell(ctx, x + i, y + j + (i === 0 || i === 13 ? 1 : 0), col);
  }
  if (turn > 0) {
    // The lifted page shrinks toward the spine and lands on the left.
    const w = Math.max(1, Math.round(6 * Math.abs(Math.cos(turn * Math.PI))));
    const left = turn > 0.5;
    for (let j = 0; j < 6; j++) for (let i = 0; i < w; i++) {
      cell(ctx, left ? x + 6 - i : x + 7 + i, y - 1 + j - (w < 3 ? 1 : 0), PAGE);
    }
  }
}

function show(el, d) {
  const f = d.frog, g = bred(genome(f.seed), f.mods), hat = f.hat && f.hat.shape ? f.hat : null;
  const col = colours(g, hat), young = f.stage === "egg" || f.stage === "tadpole";
  const cv = el.querySelector("canvas"), ctx = cv.getContext("2d"), line = el.querySelector("[data-pond-line]");
  cv.width = SW * PX; cv.height = SH * PX;
  const name = f.name.charAt(0) + f.name.slice(1).toLowerCase();
  const mins = Number(el.dataset.minutes) || 0;
  let done = false, hopAt = 0;
  line.textContent = name + " is reading along.";
  el.hidden = false;

  function finish() {
    if (done) return;
    done = true;
    hopAt = performance.now() / 1000;
    let text = mins > 1 ? name + " read along, all " + mins + " minutes of it." : name + " read along.";
    if (d.house) text += " Get a frog of your own at the pond.";
    line.textContent = text;
  }
  new IntersectionObserver((es, ob) => {
    if (es.some((e) => e.isIntersecting)) { finish(); ob.disconnect(); }
  }, { threshold: 1 }).observe(cv);

  const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
  let blinkAt = 0;
  function frame(ms) {
    const t = still ? 0 : ms / 1000;
    ctx.clearRect(0, 0, cv.width, cv.height);
    water(ctx, t);
    const bob = Math.sin(t * 1.1) * 0.4;
    pad(ctx, bob);
    if (!blinkAt) blinkAt = t + 2 + Math.random() * 3;
    const blink = f.stage !== "egg" && t > blinkAt;
    if (t > blinkAt + 0.14) blinkAt = t + 2 + Math.random() * 4;
    const since = ms / 1000 - hopAt, hop = done && !still && since < 0.5 ? Math.round(Math.sin(since / 0.5 * Math.PI) * 4) : 0;
    const c = cells(g, f.stage, { t, hat: hat ? hat.shape : 0, blink, breathe: Math.sin(t * 1.2) * 0.5, legs: f.day >= 6 });
    const oy = Math.round(bob);
    draw(ctx, c, W, H, col, (young ? FX + 14 : FX) * PX, (oy - hop) * PX, PX);
    const turn = done || still ? 0 : (t % 5) / 0.8;
    if (young) book(ctx, 4, 21 + oy, !done, turn < 1 ? turn : 0);
    else book(ctx, FX + 15, 20 + oy, !done, turn < 1 ? turn : 0);
    if (!still) requestAnimationFrame(frame);
  }
  requestAnimationFrame(frame);
}

const el = document.querySelector("[data-pond]");
if (el) {
  fetch(POND + "/api/panel", { credentials: "include" })
    .then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
    .then((d) => show(el, d))
    .catch(() => {});
}

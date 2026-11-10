import { genome, bred, cells, colours, draw, W, H } from "./frog.js";
import { drawIcon } from "./icons.js";

// The pictures that sit over the text screens, so the bank, the reeds and
// the kitchen each have something to look at above their words.

function px(g, col, x, y, w, h) { g.fillStyle = col; g.fillRect(x, y, w, h); }

function setup(cv, w, h, s) {
  if (cv.width !== w * s || cv.height !== h * s) { cv.width = w * s; cv.height = h * s; }
  const g = cv.getContext("2d");
  g.setTransform(s, 0, 0, s, 0, 0);
  g.imageSmoothingEnabled = false;
  return g;
}

const BKW = 176, BKH = 64;

// The bank's three builds side by side on the bank of the pond. One that
// isn't built yet is dimmed out, and each grows a little with its level.
export function drawBank(cv, p) {
  const g = setup(cv, BKW, BKH, 3), night = p.day && p.day.night, b = p.bank;
  px(g, night ? "#141c2a" : "#3b5a6a", 0, 0, BKW, 26);
  px(g, "#2f5a3a", 0, 22, BKW, 4);
  for (let x = 0; x < BKW; x += 7) px(g, "#2f5a3a", x, 18 + ((x * 13) % 4), 2, 4);
  px(g, "#5a4630", 0, 26, BKW, 24);
  for (let d = 3; d < BKW; d += 11) px(g, "#6c5538", d, 30 + (d % 3) * 6, 3, 1);
  px(g, "#123a3d", 0, 50, BKW, 14);
  for (let w = 0; w < BKW; w += 16) px(g, "#7fd6c2", w + 3, 51, 7, 1);

  // The gravel bar, a heap of pebbles running into the water.
  const gl = b.gravel.level, heap = 8 + gl * 3;
  px(g, "#8a5a2b", 8, 30, 3, 20); px(g, "#8a5a2b", 8, 30, 22, 2);
  for (let r = 0; r < heap; r++) {
    const w = Math.round(Math.sqrt(r + 1) * 10), y = 52 - heap + r;
    for (let x = 0; x < w; x++) {
      const n = (x * 7 + r * 13) % 5;
      px(g, n === 0 ? "#b8bcc2" : n < 3 ? "#8f949c" : "#6b7078", 30 - Math.floor(w / 2) + x, y, 1, 1);
    }
  }

  // The dye works, a hut with a pot that steams in its colour while busy.
  const d = b.dye, hx = 66;
  px(g, "#6b4a2a", hx, 22, 26, 18); px(g, "#8a6230", hx + 2, 24, 22, 1);
  for (let r = 0; r < 8; r++) px(g, "#b8456a", hx - 2 + r, 14 + 8 - r, 30 - r * 2, 1);
  px(g, "#1a120b", hx + 10, 30, 6, 10);
  px(g, "#2b2b30", hx + 30, 34, 14, 8); px(g, "#3a3a42", hx + 29, 33, 16, 2);
  px(g, d.busy ? "#5aa9e6" : "#1a1a1e", hx + 31, 34, 12, 2);
  px(g, "#e2412d", hx + 33, 43, 2, 2); px(g, "#ffb000", hx + 36, 43, 3, 2); px(g, "#e2412d", hx + 40, 43, 2, 2);
  if (d.busy) for (let s = 0; s < 3; s++) px(g, "rgba(255,246,220,0.5)", hx + 33 + s * 4, 26 - s * 3, 2, 3);
  for (let l = 0; l < d.level; l++) px(g, "#cf9bff", hx + 2 + l * 4, 42, 2, 4);

  // The burrows, a mound with a doorway for each level.
  const bl = b.burrows.level, mx = 120;
  for (let y = 0; y < 22; y++) {
    const w = Math.min(54, Math.round(Math.sqrt(y * 140)));
    px(g, "#6c5538", mx + 27 - Math.floor(w / 2), 28 + y, w, 1);
  }
  for (let x = mx + 6; x < mx + 48; x += 5) px(g, "#2f5a3a", x, 28 + Math.abs(x - mx - 27) / 3, 2, 2);
  for (let i = 0; i < Math.max(bl, 1); i++) {
    const cx = mx + 8 + i * 9 + (bl < 3 ? 9 : 0);
    px(g, "#1a120b", cx, 40, 6, 9); px(g, "#1a120b", cx + 1, 39, 4, 1);
  }

  [[0, 60, gl], [60, 116, d.level], [116, BKW, bl]].forEach(([x, end, lv]) => {
    if (!lv) px(g, "rgba(14,12,10,0.72)", x, 0, end - x, BKH);
  });
}

// A strip of lily pads with every frog on one, the one out front first.
export function drawReeds(cv, p) {
  const n = Math.max(4, Math.min(8, p.pads)), sw = W + 4, sh = H + 8;
  const g = setup(cv, n * sw, sh, 2);
  px(g, "#123a3d", 0, 0, n * sw, sh);
  for (let w = 0; w < n * sw; w += 14) px(g, "#0e2c30", w + (w % 3), 6 + (w % 5) * 4, 6, 1);
  const frogs = [p.frogs.find((f) => f.front)].concat(p.frogs.filter((f) => !f.front));
  for (let i = 0; i < n; i++) {
    const x = i * sw, owned = i < p.pads;
    px(g, owned ? "#2f7a3f" : "#1b4a4a", x + 6, sh - 8, sw - 12, 5);
    px(g, owned ? "#3f9a4d" : "#21524f", x + 8, sh - 9, sw - 16, 2);
    if (owned) px(g, "#123a3d", x + sw / 2, sh - 8, 2, 3);
    const f = frogs[i];
    if (!f) continue;
    const ge = bred(genome(f.seed), f.mods);
    draw(g, cells(ge, f.stage, { hat: f.hat.shape, legs: f.day >= 6 }), W, H, colours(ge, f.hat), x + 2, sh - H - 6, 1);
    if (f.front) px(g, "#ffb000", x + sw / 2 - 1, 1, 3, 3);
  }
}

// The kitchen pot over a fire, with the two picked things over it.
export function drawPot(cv, picked, night) {
  const PW = 120, PH = 44, g = setup(cv, PW, PH, 3);
  px(g, night ? "#141c2a" : "#3b5a6a", 0, 0, PW, PH);
  px(g, "#5a4630", 0, 34, PW, 10);
  px(g, "#6b4a2a", 34, 14, 2, 22); px(g, "#6b4a2a", 84, 14, 2, 22); px(g, "#6b4a2a", 34, 14, 52, 2);
  px(g, "#2b2b30", 46, 20, 28, 12); px(g, "#3a3a42", 44, 19, 32, 2); px(g, "#1f1f24", 48, 31, 24, 2);
  px(g, picked.length === 2 ? "#c9974f" : "#1a1a1e", 46, 21, 28, 2);
  px(g, "#e2412d", 50, 34, 4, 3); px(g, "#ffb000", 55, 33, 6, 4); px(g, "#ffe27a", 57, 34, 2, 2); px(g, "#e2412d", 62, 34, 5, 3); px(g, "#8a5a2b", 48, 37, 22, 2);
  picked.forEach((k, i) => drawIcon(g, k, 44 + i * 18, 2, 2));
  if (picked.length === 2) for (let s = 0; s < 3; s++) px(g, "rgba(255,246,220,0.5)", 52 + s * 7, 14 - (s % 2) * 3, 2, 3);
}

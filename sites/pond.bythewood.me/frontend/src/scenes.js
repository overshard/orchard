import { genome, bred, cells, colours, draw, W, H } from "./frog.js";
import { drawIcon } from "./icons.js";

// The pictures that sit over the text screens, so Works, the reeds and the
// kitchen each have something to look at above their words.

function px(g, col, x, y, w, h) { g.fillStyle = col; g.fillRect(x, y, w, h); }

function setup(cv, w, h, s) {
  if (cv.width !== w * s || cv.height !== h * s) { cv.width = w * s; cv.height = h * s; }
  const g = cv.getContext("2d");
  g.setTransform(s, 0, 0, s, 0, 0);
  g.imageSmoothingEnabled = false;
  return g;
}

const WKW = 184, WKH = 60;

// A window or a lamp, lit when the thing behind it is ready.
function lamp(g, x, y, w, h, lit) {
  if (lit) px(g, "rgba(255,226,122,0.3)", x - 1, y - 1, w + 2, h + 2);
  px(g, lit ? "#ffe27a" : "#1a120b", x, y, w, h);
}

function hut(g, x, wall, roof) {
  px(g, wall, x, 26, 18, 14); px(g, "rgba(255,255,255,0.12)", x + 1, 27, 16, 1);
  for (let r = 0; r < 8; r++) px(g, roof, x - 2 + r, 25 - r, 22 - r * 2, 1);
  px(g, "#1a120b", x + 3, 32, 5, 8);
}

// The whole bank side of the pond in one window: the reeds, the garden, the
// gravel bar, the dye works and the burrows. ready is the kinds of thing
// waiting, and each one lights a window. One not built yet is dimmed out.
export function drawWorks(cv, p, ready) {
  const g = setup(cv, WKW, WKH, 3), night = p.day && p.day.night, b = p.bank;
  px(g, night ? "#141c2a" : "#3b5a6a", 0, 0, WKW, 22);
  px(g, "#2f5a3a", 0, 18, WKW, 4);
  for (let x = 0; x < WKW; x += 7) px(g, "#2f5a3a", x, 14 + ((x * 13) % 4), 2, 4);
  px(g, "#5a4630", 0, 22, WKW, 24);
  for (let d = 3; d < WKW; d += 11) px(g, "#6c5538", d, 26 + (d % 3) * 6, 3, 1);
  px(g, "#123a3d", 0, 46, WKW, 14);
  for (let w = 0; w < WKW; w += 16) px(g, "#7fd6c2", w + 3, 47, 7, 1);

  // The reeds, a thatched lodge with the frog out front on a pad before it.
  [[1, 12], [4, 8], [30, 10], [33, 14]].forEach(([x, top]) => {
    px(g, "#2f6b33", x, top, 1, 46 - top); px(g, "#6b4a2a", x, top - 4, 1, 5);
  });
  hut(g, 8, "#8a6230", "#c9974f");
  lamp(g, 19, 30, 5, 5, ready.has("shift"));
  px(g, "#2f7a3f", 9, 52, 16, 3); px(g, "#3f9a4d", 10, 51, 14, 2); px(g, "#123a3d", 17, 51, 1, 2);
  const f = p.frogs.find((x) => x.front);
  if (f && f.stage === "egg") {
    px(g, "#fff6dc", 15, 47, 4, 4); px(g, "#0a0908", 16, 48, 2, 2);
  } else if (f) {
    const ge = bred(genome(f.seed), f.mods);
    px(g, colours(ge, null)[1], 13, 47, 8, 5);
    px(g, "#fff6dc", 13, 45, 3, 3); px(g, "#fff6dc", 18, 45, 3, 3);
    px(g, "#0a0908", 14, 46, 1, 1); px(g, "#0a0908", 19, 46, 1, 1);
  }

  // The garden, a shed and a bed for each row of plots with what's coming up.
  hut(g, 40, "#7a5a34", "#3f7a45");
  lamp(g, 51, 30, 5, 5, ready.has("garden"));
  [28, 34, 40].forEach((y) => px(g, "#3a2a1c", 61, y, 13, 3));
  b.garden.plots.forEach((pl, i) => {
    const x = 62 + (i % 4) * 3, y = 28 + (Math.floor(i / 4) % 3) * 6, h = 1 + Math.round(Math.min(1, pl.grown) * 2);
    px(g, "#56b45f", x, y - h, 1, h);
    if (pl.ready_in === 0) px(g, "#e2412d", x, y - h - 1, 1, 1);
  });

  // The gravel bar, a heap of pebbles under a lamp on a post.
  const gl = b.gravel.level, heap = 6 + gl * 2;
  px(g, "#8a5a2b", 79, 26, 2, 22); px(g, "#8a5a2b", 79, 26, 10, 2);
  lamp(g, 86, 28, 3, 4, ready.has("gravel"));
  for (let r = 0; r < heap; r++) {
    const w = Math.round(Math.sqrt(r + 1) * 6), y = 48 - heap + r;
    for (let x = 0; x < w; x++) {
      const n = (x * 7 + r * 13) % 5;
      px(g, n === 0 ? "#b8bcc2" : n < 3 ? "#8f949c" : "#6b7078", 96 - Math.floor(w / 2) + x, y, 1, 1);
    }
  }

  // The dye works, a hut with a pot that steams in its colour while busy.
  const d = b.dye, hx = 114;
  hut(g, hx, "#6b4a2a", "#b8456a");
  lamp(g, hx + 11, 30, 5, 5, ready.has("dye"));
  px(g, "#2b2b30", hx + 22, 34, 11, 7); px(g, "#3a3a42", hx + 21, 33, 13, 2);
  px(g, d.busy ? "#5aa9e6" : "#1a1a1e", hx + 23, 34, 9, 2);
  px(g, "#e2412d", hx + 23, 42, 2, 2); px(g, "#ffb000", hx + 26, 42, 3, 2); px(g, "#e2412d", hx + 30, 42, 2, 2);
  if (d.busy) for (let s = 0; s < 3; s++) px(g, "rgba(255,246,220,0.5)", hx + 24 + s * 3, 27 - s * 3, 2, 3);
  for (let l = 0; l < d.level; l++) px(g, "#cf9bff", hx + 1 + l * 4, 42, 2, 4);

  // The burrows, a mound with a doorway for each level, lit when rent's due.
  const bl = b.burrows.level, mx = 149;
  for (let y = 0; y < 22; y++) {
    const w = Math.min(34, Math.round(Math.sqrt(y * 60)));
    px(g, "#6c5538", mx + 17 - Math.floor(w / 2), 24 + y, w, 1);
  }
  for (let x = mx + 5; x < mx + 30; x += 5) px(g, "#2f5a3a", x, 25 + Math.abs(x - mx - 17) / 3, 2, 2);
  const doors = Math.max(bl, 1);
  for (let i = 0; i < doors; i++) lamp(g, mx + 17 - doors * 3 + i * 6 + 1, 38, 4, 7, ready.has("rent"));

  [[36, 76, b.garden.level], [76, 110, gl], [110, 148, d.level], [148, WKW, bl]].forEach(([x, end, lv]) => {
    if (!lv) px(g, "rgba(14,12,10,0.72)", x, 0, end - x, WKH);
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

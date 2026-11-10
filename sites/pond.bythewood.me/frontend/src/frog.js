// Everything that turns a seed into a frog and a frog into pixels. It touches
// no page, so another site can take this one file to draw the same frog.
// genome.go makes the same draws in the same order and a fixture holds them
// together.

export const W = 44, H = 34, SW = 28, SH = 18;
const GROUND = 30;

export function rng(a) {
  return function () {
    a |= 0; a = a + 0x6D2B79F5 | 0;
    let t = Math.imul(a ^ a >>> 15, 1 | a);
    t = t + Math.imul(t ^ t >>> 7, 61 | t) ^ t;
    return ((t ^ t >>> 14) >>> 0) / 4294967296;
  };
}

function hash2(x, y, s) {
  let n = Math.imul(x * 374761393 + y * 668265263 + s * 974711 | 0, 1274126177);
  n ^= n >>> 15;
  return ((n >>> 0) % 1000) / 1000;
}

// Weights are out of 1000.
export const MORPHS = [
  { name: "GREEN", w: 330, h: [95, 135], s: [42, 58], l: [38, 46], eye: "#e8c552" },
  { name: "OLIVE", w: 220, h: [58, 82], s: [34, 48], l: [36, 44], eye: "#d9a441" },
  { name: "WOOD BROWN", w: 180, h: [20, 34], s: [34, 48], l: [34, 42], eye: "#d6a34a", pat: "mask" },
  { name: "GREY", w: 100, h: [70, 110], s: [5, 12], l: [46, 54], eye: "#cfc7a8" },
  { name: "GOLDEN", w: 60, h: [42, 48], s: [80, 90], l: [50, 56], eye: "#fff3d0" },
  { name: "RED EYE", w: 50, h: [108, 128], s: [58, 68], l: [42, 48], eye: "#e2412d", limb: 24 },
  { name: "STRAWBERRY", w: 35, h: [2, 10], s: [70, 80], l: [46, 52], eye: "#f3e4c2", limb: 222 },
  { name: "BLUE", w: 20, h: [198, 212], s: [55, 68], l: [46, 54], eye: "#e8c552" },
  { name: "ALBINO", w: 5, h: [44, 50], s: [55, 65], l: [84, 88], eye: "#e87f86" },
];
const PATTERNS = ["PLAIN", "SPOTS", "SPOTS", "BANDS", "SPECKLED", "MASK", "BACK STRIPE"];
const PUPILS = ["BAR", "BAR", "ROUND", "SLIT"];
const NA = ["", "", "", "OLD ", "LITTLE ", "BIG "];
const NB = ["PIP", "BRINDLE", "MOSS", "DOT", "MARSH", "TULLY", "BEAN", "SEDGE", "WREN", "PUDDLE", "FENN", "CLOVER", "RUSH", "NEWT", "BRAMBLE", "OTIS"];
const TEMPERS = ["BOLD", "SHY", "NIGHT OWL", "GLUTTON", "SINGER", "DRIFTER"];

export function genome(seed) {
  const r = rng(seed), roll = r() * 1000;
  let acc = 0, m = MORPHS[0];
  for (let i = 0; i < MORPHS.length; i++) { acc += MORPHS[i].w; if (roll < acc) { m = MORPHS[i]; break; } }
  const span = (a) => a[0] + r() * (a[1] - a[0]);
  const g = {
    seed: seed, morph: m,
    hue: span(m.h), sat: span(m.s), lit: span(m.l),
    bw: 11 + r() * 3.5, bh: 7.2 + r() * 2.6,
    eye: 2.6 + r() * 1.5, gap: 0.5 + r() * 0.2,
    pupil: PUPILS[Math.floor(r() * PUPILS.length)],
    pattern: m.pat && r() < 0.6 ? "MASK" : PATTERNS[Math.floor(r() * PATTERNS.length)],
    freq: 0.55 + r() * 0.5, phase: r() * 6.28,
    spots: [], temper: TEMPERS[Math.floor(r() * TEMPERS.length)],
    pitch: Math.round((260 + r() * r() * 2400) / 10) * 10,
    rate: Math.round(9 + r() * 34), notes: 1 + Math.floor(r() * 4),
    pseed: Math.floor(r() * 9999),
  };
  g.name = NA[Math.floor(r() * NA.length)] + NB[Math.floor(r() * NB.length)];
  const n = 4 + Math.floor(r() * 6);
  for (let k = 0; k < n; k++) g.spots.push({ x: (r() * 2 - 1) * 0.85, y: (r() * 2 - 1) * 0.9 - 0.2, r: 1 + r() * 1.6 });
  return g;
}

function ell(x, y, cx, cy, rx, ry) { const a = (x - cx) / rx, b = (y - cy) / ry; return a * a + b * b <= 1; }

// A hat is drawn from the top of whatever head it sits on, hx across from the
// middle and hy up from the crown, so one drawing fits an egg, a tadpole and
// every size of frog. 10 is the hat's main colour and 11 its trim.
function hatCell(shape, hx, hy) {
  const ax = Math.abs(hx);
  let v = 0;
  switch (shape) {
    case 1: if (hy > 0 && hy < 1.2 && ax < 5) v = 10; else if (hy >= 1.2 && hy < 6.5 && ax < 3) v = hy < 2.4 ? 11 : 10; break;
    case 2: if (hy > 0 && hy < 7 && ax < (7 - hy) * 0.5) v = (Math.floor(hy) % 3 === 1) ? 11 : 10; break;
    case 3: if (hy > 0 && hy < 2 && ax < 4) v = 10; else if (hy >= 2 && hy < 4 && ax < 4 && (Math.floor(ax) % 3 !== 1)) v = 10; if (v && hy < 1 && ax < 0.6) v = 11; break;
    case 4: if (ell(hx, hy, 0.5, 1.6, 6, 1.9)) v = Math.abs(hy - 1.6) < 0.5 ? 11 : 10; break;
    case 5: if (hy > 0 && ell(hx, hy, 0, 0, 4.7, 4.6)) v = hy < 1.3 ? 11 : 10; if (ell(hx, hy, 0, 5.4, 1.5, 1.5)) v = 11; break;
    case 6: if (ell(hx, hy, 2.3, 2.8, 1.4, 1.4) || ell(hx, hy, -2.3, 2.8, 1.4, 1.4) || ell(hx, hy, 0, 5.1, 1.4, 1.4) || ell(hx, hy, 0, 0.6, 1.4, 1.4)) v = 10; if (ell(hx, hy, 0, 2.8, 1.3, 1.3)) v = 11; break;
    case 7: if (hy > 0 && hy < 1.2 && ax < 7.6) v = 10; else if (hy >= 1.2 && hy < 2.3 && ax > 6 && ax < 7.6) v = 10; else if (hy >= 1.2 && hy < 5.2 && ax < 3.4 && !(hy > 4.3 && ax < 0.9)) v = hy < 2.3 ? 11 : 10; break;
    case 8: if (hy > 0 && hy < 1.2 && ax < 6) v = 10; else if (hy >= 1.2 && hy < 8.6 && ax < (8.8 - hy) * 0.48) v = hy < 2.4 || (Math.abs(hx + 1 - (hy % 3)) < 0.6 && hy > 3.2) ? 11 : 10; break;
    case 9: if (hy > 0 && hy < 1 && ax < 2) v = 11; if (hy >= 0.9 && ell(hx, hy, 0, 1, 6.6, 4.4)) v = (ell(hx, hy, -3, 2.8, 1.2, 1.1) || ell(hx, hy, 2.6, 3.4, 1.2, 1.1) || ell(hx, hy, 0, 4.4, 0.9, 0.8)) ? 11 : 10; break;
    case 10: if (hy > 0 && ell(hx, hy, 0, 0, 4.7, 4.1)) v = hy < 1.3 ? 11 : 10; if (ell(ax, hy, 5.5, 2.4, 1.3, 1.5) || ell(ax, hy, 6.5, 4.5, 1, 1.5)) v = 11; break;
    case 11: if (hy > 0 && hy < 2.6 && ax < 3.6) v = hy > 2 ? 11 : 10; if (ell(hx, hy, 0, 4.7, 5.2, 2.6) || ell(ax, hy, 3, 5.6, 2.4, 2)) v = 10; break;
    case 12: if (hy > 0 && ell(hx, hy, 0, 0, 4.5, 3.7)) v = Math.floor((hx + 6.6) / 2.2) % 2 ? 11 : 10; if (hy >= 3.6 && hy < 5 && ax < 0.6) v = 11; if (hy >= 5 && hy < 6 && ax < 4.2) v = 10; break;
    case 13: if (ell(hx, hy, 0, 4.4, 5.2, 1.6) && !ell(hx, hy, 0, 4.4, 3.4, 0.6)) v = hy > 4.6 ? 11 : 10; break;
    case 14: if (hy > 0.4 && hy < 4.8 && ax < 5.6 && Math.abs(hy - 2.6) < ax * 0.5 + 0.3) v = 10; if (ax < 1.1 && Math.abs(hy - 2.6) < 1.1) v = 11; break;
    case 15: if (hy > 0 && hy < 1.1 && ax < 8.2) v = 10; if (hy >= 1 && ell(hx, hy, 0, 1, 4.2, 3.4)) v = hy < 2.1 ? 11 : 10; break;
  }
  return v;
}

// Cell classes: 1 body, 2 belly, 3 pattern, 4 eye, 5 pupil, 6 dark, 7 sac,
// 8 limb, 9 outline, 10 and 11 hat.
function frogCells(g, o) {
  const c = new Uint8Array(W * H), cx = W / 2, s = o.scale || 1;
  const bw = g.bw * s, bh = g.bh * s, by = GROUND - bh - 0.6 + (o.breathe || 0);
  const er = Math.max(1.6, g.eye * s), eg = bw * g.gap, ey = by - bh * 0.98;
  const sr = (o.croak || 0) * bw * 0.58;
  for (let py = 0; py < H; py++) for (let px = 0; px < W; px++) {
    const x = px + 0.5, y = py + 0.5, dx = Math.abs(x - cx);
    let v = 0;
    if (ell(dx, y, bw * 0.92, by + bh * 0.42, bw * 0.42, bh * 0.66)) v = 8;
    if (ell(dx, y, bw * 1.12, GROUND - 0.7, bw * 0.4, 1.3)) v = 8;
    if (o.tail && ell(x, y, cx + bw + 3.5, GROUND - 1.4, 4.2, 1.2)) v = 1;
    let onBody = ell(x, y, cx, by, bw, bh) || ell(x, y, cx, by - bh * 0.5, bw * 0.82, bh * 0.72);
    if (onBody) v = 1;
    if (ell(dx, y, eg, ey, er + 1.2, er + 1.2)) { v = 1; onBody = true; }
    if (onBody) {
      let p = false;
      if (g.pattern === "SPOTS") {
        for (let i = 0; i < g.spots.length; i++) {
          const sp = g.spots[i], ddx = x - (cx + sp.x * bw), ddy = y - (by + sp.y * bh * 0.7);
          if (ddx * ddx + ddy * ddy < sp.r * sp.r * s * s) { p = true; break; }
        }
      } else if (g.pattern === "BANDS") p = Math.sin((x - cx) * g.freq + g.phase) > 0.5 && y < by + bh * 0.3;
      else if (g.pattern === "SPECKLED") p = hash2(px, py, g.pseed) < 0.17;
      else if (g.pattern === "MASK") p = y > ey + er * 0.3 && y < ey + er * 0.3 + 2.2 * s && dx > eg * 0.35;
      else if (g.pattern === "BACK STRIPE") p = dx < 1.1 * s && y < by + bh * 0.1;
      if (p) v = 3;
    }
    if (ell(x, y, cx, by + bh * 0.48, bw * 0.6, bh * 0.5)) v = 2;
    if (ell(dx, y, bw * 0.44, by + bh * 0.62, 1.3 * s + 0.3, bh * 0.42)) v = 8;
    if (ell(dx, y, bw * 0.46, GROUND - 0.7, 2.7 * s, 1.3)) v = 8;
    if (sr > 0.8 && ell(x, y, cx, by + bh * 0.12, sr, sr * 0.82)) v = 7;
    if (dx < bw * 0.6) {
      const my = by - bh * 0.34 - 1.3 * s * Math.pow(dx / (bw * 0.6), 2);
      if (Math.abs(y - my) < 0.55 && !(sr > 0.8 && v === 7)) v = 6;
    }
    if (s > 0.8 && Math.abs(dx - 1.5) < 0.6 && Math.abs(y - (by - bh * 0.72)) < 0.6) v = 6;
    if (ell(dx, y, eg, ey, er, er)) {
      if (o.blink) v = Math.abs(y - ey) < 0.6 ? 6 : 1;
      else {
        v = 4;
        const rx = g.pupil === "BAR" ? er * 0.82 : g.pupil === "SLIT" ? er * 0.34 : er * 0.52;
        const ry = g.pupil === "BAR" ? er * 0.36 : g.pupil === "SLIT" ? er * 0.82 : er * 0.52;
        if (ell(dx, y, eg, ey, rx, ry)) v = 5;
      }
    }
    if (o.hat) v = hatCell(o.hat, x - cx, by - bh * 1.2 - y) || v;
    c[py * W + px] = v;
  }
  return c;
}

function tadCells(g, o) {
  const c = new Uint8Array(W * H), cx = 16, cy = 20, t = o.t || 0;
  for (let py = 0; py < H; py++) for (let px = 0; px < W; px++) {
    const x = px + 0.5, y = py + 0.5;
    let v = 0;
    if (x > cx + 3 && x < cx + 24) {
      const u = (x - cx - 3) / 21, ty = cy + Math.sin(x * 0.55 - t * 5) * 2.2 * u;
      if (Math.abs(y - ty) < 3.2 * (1 - u) + 0.5) v = 8;
    }
    if (ell(x, y, cx, cy, 7, 5)) v = 1;
    if (ell(x, y, cx, cy + 2, 4.5, 2.4)) v = 2;
    if (o.legs && ell(x, y, cx + 4, cy + 5.2, 2.2, 1.1)) v = 8;
    if (ell(x, y, cx - 3.5, cy - 1.5, 1.8, 1.8)) v = 4;
    if (ell(x, y, cx - 3.8, cy - 1.5, 0.9, 0.9)) v = 5;
    if (o.hat) v = hatCell(o.hat, x - cx + 1, cy - 4.4 - y) || v;
    c[py * W + px] = v;
  }
  return c;
}

function eggCells(g, o) {
  const c = new Uint8Array(W * H), cx = W / 2, cy = 19, t = o.t || 0;
  const wob = Math.sin(t * 2) * 0.6;
  for (let py = 0; py < H; py++) for (let px = 0; px < W; px++) {
    const x = px + 0.5, y = py + 0.5;
    let v = 0;
    const d = Math.sqrt((x - cx) * (x - cx) + (y - cy) * (y - cy));
    if (d < 10) v = 7;
    if (d < 10 && d > 8.8) v = 8;
    if (ell(x, y, cx + wob, cy + 0.5, 4.2, 3.6)) v = 1;
    if (ell(x, y, cx + wob - 1.2, cy - 0.6, 1, 1)) v = 5;
    if (o.hat) v = hatCell(o.hat, x - cx, cy - 9.4 - y) || v;
    c[py * W + px] = v;
  }
  return c;
}

export function cells(g, stage, o) {
  o = Object.assign({}, o);
  if (stage === "egg") return eggCells(g, o);
  if (stage === "tadpole") return tadCells(g, o);
  if (stage === "froglet") { o.scale = 0.62; o.tail = true; return frogCells(g, o); }
  if (stage === "elder") { o.scale = 1.06; return frogCells(g, o); }
  o.scale = 0.94; return frogCells(g, o);
}

// The swimmer for Dive, seen from the side. A frog too young to have legs
// dives as a tadpole.
export function sideCells(g, stage, kick) {
  const c = new Uint8Array(SW * SH), young = stage === "egg" || stage === "tadpole";
  for (let py = 0; py < SH; py++) for (let px = 0; px < SW; px++) {
    const x = px + 0.5, y = py + 0.5;
    let v = 0;
    if (young) {
      if (x < 15) {
        const u = (15 - x) / 14, ty = 9.5 + Math.sin(x * 0.7 + (kick ? 2 : 0)) * 2.4 * u;
        if (Math.abs(y - ty) < 2.8 * (1 - u) + 0.5) v = 8;
      }
      if (ell(x, y, 19, 9.5, 6.5, 4.8)) v = 1;
      if (ell(x, y, 19, 11.5, 4.2, 2.2)) v = 2;
      if (ell(x, y, 22.4, 8, 1.8, 1.8)) v = 4;
      if (ell(x, y, 22.8, 8, 0.9, 0.9)) v = 5;
    } else {
      if (kick) { if (ell(x, y, 6, 11.5, 6, 1.7) || ell(x, y, 1.6, 13, 1.8, 2.6)) v = 8; }
      else if (ell(x, y, 10, 13.2, 4.2, 2.6) || ell(x, y, 7.5, 15.5, 3.2, 1.2)) v = 8;
      if (ell(x, y, 15, 10, 7.5, 5.4) || ell(x, y, 21, 9.4, 5, 4.6)) v = 1;
      if (v === 1 && g.pattern !== "PLAIN" && y < 8.5 && hash2(px, py, g.pseed) < 0.3) v = 3;
      if (ell(x, y, 16, 13, 6, 2.2)) v = 2;
      if (ell(x, y, 19.5, 14.6, 1.3, 2.2)) v = 8;
      if (ell(x, y, 21.5, 5.2, 3.3, 3.3)) v = 1;
      if (ell(x, y, 21.8, 5.2, 2.4, 2.4)) v = 4;
      if (ell(x, y, 22.6, 5.3, 1.1, 1.3)) v = 5;
      if (x > 21 && x < 25.6 && Math.abs(y - (11.4 - (x - 21) * 0.18)) < 0.55) v = 6;
    }
    c[py * SW + px] = v;
  }
  return c;
}

// Each shape's own two colours, then the three colourways any shape comes in.
const HATC = [null, ["#1b1a22", "#b3222f"], ["#e8559a", "#ffe27a"], ["#f2c230", "#d8402f"], ["#4f9a4a", "#2f6b33"], ["#d8402f", "#f3e4c2"], ["#f3e4c2", "#f2c230"], ["#8a5a2b", "#3a2412"], ["#4b3fa8", "#ffe27a"], ["#d8402f", "#fff6dc"], ["#9aa0a6", "#f3e4c2"], ["#fff6dc", "#c9ccd0"], ["#3d8fd6", "#f2c230"], ["#ffe27a", "#fff6dc"], ["#e8559a", "#b3306d"], ["#e3c57a", "#d8402f"]];
const CWC = [null, ["#4f9a4a", "#cfe8a0"], ["#b03a8c", "#ffd0ea"], ["#f2c230", "#fff6dc"]];

export function hatColours(hat) {
  if (!hat || !hat.shape) return null;
  return hat.colour ? CWC[hat.colour] : HATC[hat.shape];
}

function hsl(h, s, l) {
  return "hsl(" + h.toFixed(0) + " " + Math.max(0, Math.min(100, s)).toFixed(0) + "% " + Math.max(0, Math.min(100, l)).toFixed(0) + "%)";
}

// The colour of every cell class for this frog, hat included when it has one.
export function colours(g, hat) {
  const m = g.morph;
  const col = {
    1: hsl(g.hue, g.sat, g.lit), 2: hsl(g.hue + 18, g.sat * 0.45, Math.min(92, g.lit + 30)),
    3: hsl(g.hue - 8, g.sat, g.lit - 17), 4: m.eye, 5: "#0a0908", 6: hsl(g.hue, g.sat * 0.6, 12),
    7: hsl(g.hue + 24, g.sat * 0.4, Math.min(94, g.lit + 38)),
    8: m.limb != null ? hsl(m.limb, 70, 50) : hsl(g.hue, g.sat, g.lit - 8),
    9: hsl(g.hue, g.sat * 0.5, 9),
  };
  if (m.name === "ALBINO") col[5] = "#b3222f";
  const hc = hatColours(hat);
  if (hc) { col[10] = hc[0]; col[11] = hc[1]; }
  return col;
}

// draw paints a grid of cells at px pixels a cell, with a one cell dark
// outline round whatever is filled.
export function draw(ctx, c, w, h, col, ox, oy, px) {
  for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) {
    let v = c[y * w + x];
    if (!v) {
      const near = (x > 0 && c[y * w + x - 1]) || (x < w - 1 && c[y * w + x + 1]) || (y > 0 && c[(y - 1) * w + x]) || (y < h - 1 && c[(y + 1) * w + x]);
      if (!near) continue;
      v = 9;
    }
    ctx.fillStyle = col[v];
    ctx.fillRect(ox + x * px, oy + y * px, px, px);
  }
}

// The box a hat fills on a grown frog, for drawing the hat by itself.
export function hatCells(shape) {
  const g = genome(1207), c = cells(g, "frog", { hat: shape });
  let x0 = W, x1 = 0, y0 = H, y1 = 0;
  for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
    const v = c[y * W + x];
    if (v === 10 || v === 11) { x0 = Math.min(x0, x); x1 = Math.max(x1, x); y0 = Math.min(y0, y); y1 = Math.max(y1, y); }
  }
  return { c, x0, x1, y0, y1 };
}

let ac = null;

// croak plays the frog's own call, a sawtooth through a bandpass chopped at
// its pulse rate, and returns how long it lasts in seconds.
export function croak(g) {
  const len = 0.16 + 3 / g.rate, gap = 0.09;
  try {
    ac = ac || new (window.AudioContext || window.webkitAudioContext)();
    if (ac.state === "suspended") ac.resume();
    let t = ac.currentTime + 0.03;
    for (let i = 0; i < g.notes; i++) {
      const o = ac.createOscillator(), f = ac.createBiquadFilter(), am = ac.createGain(), env = ac.createGain(), lfo = ac.createOscillator(), lg = ac.createGain();
      o.type = "sawtooth";
      o.frequency.setValueAtTime(g.pitch * 0.92, t);
      o.frequency.linearRampToValueAtTime(g.pitch * 1.06, t + len);
      f.type = "bandpass"; f.frequency.value = g.pitch * 1.8; f.Q.value = 3;
      lfo.type = "square"; lfo.frequency.value = g.rate; lg.gain.value = 0.5; am.gain.value = 0.5;
      lfo.connect(lg); lg.connect(am.gain);
      env.gain.setValueAtTime(0, t);
      env.gain.linearRampToValueAtTime(0.22, t + 0.02);
      env.gain.setValueAtTime(0.22, t + len - 0.04);
      env.gain.linearRampToValueAtTime(0, t + len);
      o.connect(f); f.connect(am); am.connect(env); env.connect(ac.destination);
      o.start(t); lfo.start(t); o.stop(t + len + 0.02); lfo.stop(t + len + 0.02);
      t += len + gap;
    }
  } catch (e) {
    // No audio here, and the throat still moves.
  }
  return g.notes * (len + gap);
}

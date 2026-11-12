import { colours } from "./frog.js";
import { feel } from "./feel.js";
import { reduced } from "./api.js";
import { sfx } from "./sound.js";
import { W, H, WATER, SX, SY, PULL, SHORE, LEVELS, MATS, FROGS, round, begin, step, fling, power, pulled, arc, counts, bar } from "./slingworld.js";

// Cattail sling. A bent cattail flings the frog at acorns stacked on logs and
// rafts out on the pond, and every acorn knocked open or into the water
// clears. Five levels a round, a few frogs each, and a new frog most levels.
const FONT = "700 13px 'Space Grotesk', sans-serif", BIG = "700 18px 'Space Grotesk', sans-serif";
const SMALL = "500 8px 'JetBrains Mono', monospace";

let G = null, w = null;
const F = feel();

// Each sky is its top and bottom, the far bank and the light on the water.
const SKIES = [
  { top: "#9fd3e8", low: "#f3e6c0", bank: "#3e6b3a", far: "#5d8a4e", water: "#2f6f7e", deep: "#163f4a", sun: "#fff3c4" },
  { top: "#5fa8dc", low: "#bfe3f0", bank: "#2f5f30", far: "#4f8a45", water: "#2a6a86", deep: "#123a52", sun: "#fff6dc" },
  { top: "#4a3a6a", low: "#f0a060", bank: "#2a3a2a", far: "#3a4a34", water: "#3a4a62", deep: "#1a2234", sun: "#ffcf6a" },
  { top: "#b8c8d8", low: "#eef2f4", bank: "#5a6a62", far: "#7a8a84", water: "#5a7a8a", deep: "#2a3e4a", sun: "#ffffff" },
];

// The frogs are drawn from the front on a grid. X is the skin, B the belly,
// D the dark and E and P the eye, W a wart and K a spot.
const SPRITE = {
  pond: ["..XX..XX..", ".XEPXXPEX.", "XXXXXXXXXX", "XDXXXXXXDX", "XBDDDDDDBX", ".XBBBBBBX.", "F.F....F.F"],
  tree: ["..XX..XX..", ".XEPXXPEX.", "XXXXXXXXXX", "XDXXXXXXDX", "XBDDDDDDBX", ".XBBBBBBX.", "F.F....F.F"],
  dart: ["..XX..XX..", ".XEPXXPEX.", "XKXXKXXKXX", "XDXKXXKXDX", "XBDDDDDDBX", ".XBBBBBBX.", "K.K....K.K"],
  bull: ["...XX..XX...", "..XEPXXPEX..", ".XXXXXXXXXX.", "XXXXXXXXXXXX", "XDXXXXXXXXDX", "XBDDDDDDDDBX", ".XBBBBBBBBX.", "FF.F....F.FF"],
  toad: ["..XX..XX..", ".XEPXXPEX.", "XWXXWXXWXX", "XDXWXXWXDX", "XBDDDDDDBX", ".XBBWBBBX.", "F.F....F.F"],
};
const FROGPAL = {
  tree: { X: "#5fd04a", B: "#e8f0c0", D: "#2f6a2c", E: "#ff6a2a", P: "#0a0908", F: "#ff9a2a" },
  dart: { X: "#2b7fe0", B: "#ffd23a", D: "#14306a", E: "#fff6dc", P: "#0a0908", K: "#0a1a3a" },
  bull: { X: "#6b7a2a", B: "#e8d27a", D: "#3a4418", E: "#ffe27a", P: "#0a0908", F: "#4f5a1e" },
  toad: { X: "#9a6a3a", B: "#d9c09a", D: "#5a3a1e", E: "#ffb000", P: "#0a0908", W: "#6b4a2a", F: "#7a5230" },
};

function pal(k) {
  if (k !== "pond") return FROGPAL[k];
  return { X: G.col[1], B: G.col[2], D: G.col[3], E: "#fff6dc", P: "#0a0908", F: G.col[3] };
}

// frog draws a frog of kind k centred on cx, cy, a px a cell times s, with a
// dark outline round it.
function frog(x, k, cx, cy, s, blink) {
  const rows = SPRITE[k], p = pal(k), h = rows.length, wd = rows[0].length, c = s || 1;
  const ox = Math.round(cx - wd * c / 2), oy = Math.round(cy - h * c / 2);
  x.fillStyle = "#0a0908";
  for (let ry = 0; ry < h; ry++) for (let rx = 0; rx < wd; rx++) if (rows[ry][rx] !== ".") x.fillRect(ox + rx * c - c / 2, oy + ry * c - c / 2, c * 2, c * 2);
  for (let ry = 0; ry < h; ry++) for (let rx = 0; rx < wd; rx++) {
    let ch = rows[ry][rx];
    if (ch === ".") continue;
    if (blink && (ch === "E" || ch === "P")) ch = "D";
    x.fillStyle = p[ch] || p.X;
    x.fillRect(ox + rx * c, oy + ry * c, c, c);
  }
}

// ---------------------------------------------------------------- pieces

const LOOK = {
  reed: { base: "#a8b84a", line: "#6f8a2a", band: "#8a5a2b", bits: ["#c8d86a", "#6f8a2a", "#d9c9a8"] },
  wood: { base: "#9a7a52", line: "#6b4a2a", hi: "#c0a070", bits: ["#c0a070", "#6b4a2a", "#9a7a52"] },
  mud: { base: "#7a5230", line: "#4a3018", hi: "#9a6a40", fleck: "#d9c070", bits: ["#7a5230", "#4a3018", "#d9c070"] },
  ice: { base: "rgba(191,230,242,0.88)", line: "#7fb8cc", hi: "#f4fcff", bits: ["#f4fcff", "#bfe6f2", "#7fb8cc"] },
  stone: { base: "#8a8f96", line: "#5b6470", hi: "#b6bac0", bits: ["#b6bac0", "#5b6470", "#8a8f96"] },
  acorn: { bits: ["#b0702a", "#6b4a2a", "#e0a050"] },
};

function box(x, b) {
  const L = LOOK[b.mat], w2 = b.hx * 2, h2 = b.hy * 2, long = b.hx >= b.hy;
  x.save();
  x.translate(b.x, b.y); x.rotate(b.a);
  x.fillStyle = "#0a0908"; x.fillRect(-b.hx - 0.5, -b.hy - 0.5, w2 + 1, h2 + 1);
  x.fillStyle = L.base; x.fillRect(-b.hx, -b.hy, w2, h2);
  if (b.mat === "reed") {
    x.fillStyle = L.line;
    if (long) for (let y = -b.hy + 1; y < b.hy; y += 2) x.fillRect(-b.hx, y, w2, 0.5);
    else for (let v = -b.hx + 1; v < b.hx; v += 2) x.fillRect(v, -b.hy, 0.5, h2);
    x.fillStyle = L.band;
    for (const f of [-0.5, 0.5]) { if (long) x.fillRect(f * b.hx - 0.5, -b.hy, 1, h2); else x.fillRect(-b.hx, f * b.hy - 0.5, w2, 1); }
  } else if (b.mat === "wood") {
    x.fillStyle = L.hi;
    if (long) x.fillRect(-b.hx, -b.hy, w2, 1); else x.fillRect(-b.hx, -b.hy, 1, h2);
    x.fillStyle = L.line;
    if (long) { x.fillRect(-b.hx + 2, 0, w2 * 0.4, 0.5); x.fillRect(b.hx * 0.1, b.hy * 0.5, w2 * 0.35, 0.5); }
    else { x.fillRect(0, -b.hy + 2, 0.5, h2 * 0.4); x.fillRect(b.hx * 0.5, b.hy * 0.1, 0.5, h2 * 0.35); }
  } else if (b.mat === "mud") {
    x.fillStyle = L.hi; x.fillRect(-b.hx, -b.hy, w2, 1);
    x.fillStyle = L.line; x.fillRect(-b.hx, b.hy - 1, w2, 1);
    x.fillStyle = L.fleck; x.fillRect(-b.hx * 0.5, -b.hy * 0.2, 1.5, 0.5); x.fillRect(b.hx * 0.3, b.hy * 0.3, 1.5, 0.5);
  } else if (b.mat === "ice") {
    x.fillStyle = L.hi; x.fillRect(-b.hx + 1, -b.hy + 1, w2 - 2, 1); x.fillRect(-b.hx + 1, -b.hy + 1, 1, h2 - 2);
    x.fillStyle = L.line; x.fillRect(-b.hx, b.hy - 1, w2, 1);
  } else if (b.mat === "stone") {
    x.fillStyle = L.hi; x.fillRect(-b.hx, -b.hy, w2, 1.5);
    x.fillStyle = L.line; x.fillRect(-b.hx * 0.4, 0, 1, 1); x.fillRect(b.hx * 0.5, b.hy * 0.4, 1, 1); x.fillRect(-b.hx * 0.7, b.hy * 0.5, 1, 1);
  }
  cracks(x, b, b.hx, b.hy);
  x.restore();
}

// A knocked piece shows a crack, and a second once it's nearly through.
function cracks(x, b, hx, hy) {
  const k = b.hp / b.hp0;
  if (k > 0.7) return;
  x.fillStyle = "#0a0908";
  const n = k > 0.35 ? 1 : 2, s = (b.id * 7) % 5 / 5 - 0.5;
  for (let i = 0; i < n; i++) {
    const cx = (s + i * 0.4 - 0.2) * hx, len = Math.min(hy * 1.4, 6);
    for (let j = 0; j < len; j++) x.fillRect(cx + ((j * 3 + i) % 3 - 1) * 0.5, -hy + j * 1.2, 0.8, 0.8);
  }
}

function pebble(x, b) {
  x.save(); x.translate(b.x, b.y);
  x.fillStyle = "#0a0908"; disc(x, 0, 0, b.r + 0.5);
  x.fillStyle = LOOK.stone.base; disc(x, 0, 0, b.r);
  x.rotate(b.a);
  x.fillStyle = LOOK.stone.hi; x.fillRect(-b.r * 0.5, -b.r * 0.6, b.r * 0.6, 1);
  x.fillStyle = LOOK.stone.line; x.fillRect(b.r * 0.2, b.r * 0.2, 1, 1);
  cracks(x, b, b.r * 0.7, b.r * 0.7);
  x.restore();
}

// An acorn is drawn turned with its body, cap and stalk.
function acorn(x, cx, cy, a, gold, t) {
  x.save(); x.translate(cx, cy); x.rotate(a);
  x.fillStyle = "#0a0908"; x.fillRect(-3.5, -4.5, 7, 9);
  x.fillStyle = gold ? "#ffb000" : "#b0702a"; x.fillRect(-3, -1, 6, 4); x.fillRect(-2, 3, 4, 1);
  x.fillStyle = gold ? "#ffe27a" : "#e0a050"; x.fillRect(-2, -0.5, 1, 2);
  x.fillStyle = gold ? "#c07a00" : "#6b4a2a"; x.fillRect(-3.5, -3.5, 7, 3);
  x.fillStyle = gold ? "#ffe27a" : "#8a6a3a"; x.fillRect(-2.5, -3, 1, 1); x.fillRect(0.5, -3, 1, 1); x.fillRect(-1, -2, 1, 1); x.fillRect(2, -2, 1, 1);
  x.fillStyle = gold ? "#c07a00" : "#4a3018"; x.fillRect(-0.5, -5, 1, 1.5);
  x.restore();
  if (gold && w.st !== "done") twinkle(x, cx + 4, cy - 5, t);
}

function twinkle(x, cx, cy, t) {
  const on = reduced || Math.floor(t * 4) % 2;
  x.fillStyle = on ? "#fff6dc" : "#ffb000";
  const r = on ? 2 : 1;
  x.fillRect(Math.round(cx) - r, Math.round(cy), r * 2 + 1, 1); x.fillRect(Math.round(cx), Math.round(cy) - r, 1, r * 2 + 1);
}

function disc(x, cx, cy, r) {
  for (let y = -r; y < r; y += 1) {
    const hw = Math.sqrt(Math.max(0, r * r - (y + 0.5) * (y + 0.5)));
    x.fillRect(cx - hw, cy + y, hw * 2, 1);
  }
}

function plat(x, b) {
  const p = b.plat;
  if (p.k === "shore") return;
  if (p.k === "rock") {
    x.fillStyle = "#0a0908"; x.fillRect(b.x - p.hx - 1, p.top - 1, p.hx * 2 + 2, H);
    x.fillStyle = "#6f747b"; x.fillRect(b.x - p.hx, p.top, p.hx * 2, H);
    x.fillStyle = "#8a8f96"; x.fillRect(b.x - p.hx, p.top, p.hx * 2, 3);
    x.fillStyle = "#4f8a45"; x.fillRect(b.x - p.hx, p.top, p.hx * 2, 1); x.fillRect(b.x - p.hx + 3, p.top + 1, 6, 1);
    x.fillStyle = "#5b6470"; x.fillRect(b.x - p.hx * 0.4, p.top + 8, 3, 1); x.fillRect(b.x + p.hx * 0.3, p.top + 14, 4, 1);
    return;
  }
  if (p.k === "bank") {
    x.fillStyle = "#0a0908"; x.fillRect(b.x - p.hx - 1, p.top - 1, p.hx * 2 + 2, H);
    x.fillStyle = "#5a3e24"; x.fillRect(b.x - p.hx, p.top, p.hx * 2, H);
    x.fillStyle = "#4fa043"; x.fillRect(b.x - p.hx, p.top, p.hx * 2, 3);
    x.fillStyle = "#3f8a38"; for (let i = 0; i < p.hx * 2; i += 5) x.fillRect(b.x - p.hx + i, p.top - 2, 1, 2);
    return;
  }
  x.save(); x.translate(b.x, b.y); x.rotate(b.a);
  x.fillStyle = "#0a0908"; x.fillRect(-p.hx - 1, -p.hy - 1, p.hx * 2 + 2, p.hy * 2 + 2);
  if (p.k === "log") {
    x.fillStyle = "#6b4a2a"; x.fillRect(-p.hx, -p.hy, p.hx * 2, p.hy * 2);
    x.fillStyle = "#8a6a3a"; x.fillRect(-p.hx, -p.hy, p.hx * 2, 2);
    x.fillStyle = "#4a3018";
    for (let i = -p.hx + 4; i < p.hx - 4; i += 9) { x.fillRect(i, -p.hy + 3, 5, 1); x.fillRect(i + 4, -p.hy + 6, 4, 1); }
    x.fillStyle = "#c0a070"; x.fillRect(p.hx - 3, -p.hy, 3, p.hy * 2); x.fillRect(-p.hx, -p.hy, 3, p.hy * 2);
    x.fillStyle = "#8a6a3a"; x.fillRect(p.hx - 2, -2, 1, 4); x.fillRect(-p.hx + 1, -2, 1, 4);
    x.fillStyle = "#4fa043"; x.fillRect(-p.hx * 0.3, -p.hy - 1, 4, 1); x.fillRect(p.hx * 0.4, -p.hy - 1, 3, 1);
  } else {
    for (let i = -p.hx; i < p.hx; i += 8) {
      x.fillStyle = (i / 8) % 2 ? "#a88a5a" : "#b89a6a"; x.fillRect(i, -p.hy, Math.min(7.5, p.hx - i), p.hy * 2);
    }
    x.fillStyle = "#d9c9a8"; x.fillRect(-p.hx, -p.hy, p.hx * 2, 1);
    x.fillStyle = "#6b4a2a"; x.fillRect(-p.hx * 0.6, -p.hy, 2, p.hy * 2); x.fillRect(p.hx * 0.6 - 2, -p.hy, 2, p.hy * 2);
  }
  x.restore();
}

// ---------------------------------------------------------------- the scene

function sky(x, S, t) {
  const bands = 14;
  for (let i = 0; i < bands; i++) {
    x.fillStyle = mix(S.top, S.low, i / (bands - 1));
    x.fillRect(0, Math.floor(i * WATER / bands), W, Math.ceil(WATER / bands) + 1);
  }
  x.fillStyle = S.sun; disc(x, 160, 46, 10);
  x.globalAlpha = 0.25; disc(x, 160, 46, 14); x.globalAlpha = 1;
  x.fillStyle = "rgba(255,255,255,0.75)";
  for (let i = 0; i < 3; i++) {
    const cx = ((i * 83 + t * (reduced ? 0 : 3 + i)) % (W + 60)) - 30, cy = 30 + i * 22;
    x.fillRect(cx, cy, 22, 4); x.fillRect(cx + 4, cy - 3, 12, 3); x.fillRect(cx + 14, cy - 1, 10, 3);
  }
  // The far bank, a line of willows and bushes over the water.
  x.fillStyle = S.far;
  for (let i = 0; i < 12; i++) { const bx = i * 19 - 6, bh = 10 + ((i * 37) % 9); x.fillRect(bx, WATER - 14 - bh, 22, bh + 14); x.fillRect(bx + 4, WATER - 18 - bh, 14, 4); }
  x.fillStyle = S.bank; x.fillRect(0, WATER - 10, W, 10);
  for (let i = 0; i < W; i += 4) x.fillRect(i, WATER - 10 - ((i * 7) % 5), 1, 4);
}

function mix(a, b, k) {
  const pa = [1, 3, 5].map((i) => parseInt(a.slice(i, i + 2), 16)), pb = [1, 3, 5].map((i) => parseInt(b.slice(i, i + 2), 16));
  return "rgb(" + pa.map((v, i) => Math.round(v + (pb[i] - v) * k)).join(",") + ")";
}

function water(x, S, t) {
  x.fillStyle = S.water; x.fillRect(0, WATER, W, H - WATER);
  x.fillStyle = S.deep; x.fillRect(0, WATER + 26, W, H - WATER - 26);
  x.fillStyle = mix(S.water, S.deep, 0.5); x.fillRect(0, WATER + 18, W, 8);
  x.fillStyle = "rgba(255,255,255,0.18)";
  for (let i = 0; i < 26; i++) {
    const k = ((reduced ? 0 : t * 0.2) + i * 0.137) % 1, wx = (i * 61) % W, wy = WATER + 4 + (i * 23) % (H - WATER - 8), len = 3 + Math.round(Math.sin(k * Math.PI) * 5);
    x.fillRect(wx - len + Math.sin(t + i) * 2, wy, len * 2, 1);
  }
}

// The pond covers whatever has sunk past the surface.
function surface(x, S, t) {
  x.fillStyle = S.water; x.globalAlpha = 0.62; x.fillRect(0, WATER + 1, W, H - WATER); x.globalAlpha = 1;
  x.fillStyle = "rgba(255,255,255,0.35)";
  for (let i = 0; i < W; i += 6) x.fillRect(i + Math.sin(t * 1.5 + i) * 1.5, WATER + (i % 12 ? 0 : 1), 3, 1);
}

function bank(x) {
  x.fillStyle = "#0a0908"; x.fillRect(-1, 213, SHORE + 2, H);
  x.fillStyle = "#5a3e24"; x.fillRect(0, 214, SHORE, H);
  x.fillStyle = "#4a3018"; x.fillRect(0, 226, SHORE, 2); x.fillRect(4, 238, SHORE - 6, 1);
  x.fillStyle = "#4fa043"; x.fillRect(0, 214, SHORE, 3);
  x.fillStyle = "#3f8a38"; for (let i = 1; i < SHORE; i += 3) x.fillRect(i, 211 + (i * 5) % 3, 1, 3);
}

// The cattail is two stalks with brown heads, bent toward the pull and
// shivering after a fling.
function tips() {
  let bx = 0, by = 0;
  if (G.pull) { const p = pulled(G.pull.dx, G.pull.dy); bx = (p.x - SX) * 0.18; by = (p.y - SY) * 0.12; }
  else if (G.twang < 1.2 && !reduced) { const k = Math.exp(-5 * G.twang) * Math.cos(32 * G.twang); bx = -G.tw.x * 0.22 * k; by = -G.tw.y * 0.16 * k; }
  return [{ x: SX - 7 + bx, y: SY - 4 + by }, { x: SX + 7 + bx, y: SY - 4 + by }];
}

function cattail(x, front) {
  const T = tips(), bases = [{ x: SX - 6, y: 214 }, { x: SX + 6, y: 214 }];
  [0, 1].forEach((i) => {
    if ((i === 1) !== front) return;
    const b = bases[i], t = T[i];
    for (let s = 0; s <= 1; s += 0.04) {
      const u = s * s, xx = b.x + (t.x - b.x) * u, yy = b.y + (t.y - b.y) * s;
      x.fillStyle = "#0a0908"; x.fillRect(xx - 1.5, yy, 3, 1.5);
      x.fillStyle = i ? "#4f8a3a" : "#3f7a30"; x.fillRect(xx - 0.5, yy, 1.5, 1.5);
    }
    x.fillStyle = "#0a0908"; x.fillRect(t.x - 2.5, t.y - 10, 5, 11);
    x.fillStyle = "#6b4a2a"; x.fillRect(t.x - 1.5, t.y - 9, 3, 9);
    x.fillStyle = "#8a6a3a"; x.fillRect(t.x - 1.5, t.y - 9, 1, 8);
    x.fillStyle = "#3f7a30"; x.fillRect(t.x - 0.5, t.y - 13, 1, 4);
  });
}

function band(x, fx, fy, k) {
  const T = tips();
  x.strokeStyle = k > 0.9 ? "#ffb000" : k > 0.6 ? "#e6d27a" : "#d9c9a8"; x.lineWidth = 1.5;
  x.beginPath(); x.moveTo(T[0].x + 1, T[0].y - 2); x.lineTo(fx - 3, fy); x.lineTo(fx + 3, fy); x.lineTo(T[1].x - 1, T[1].y - 2); x.stroke();
}

// ---------------------------------------------------------------- events

let noiseAt = {};
function noise(kind, gap) {
  if (G.real - (noiseAt[kind] || -9) < (gap || 0.06)) return;
  noiseAt[kind] = G.real;
  sfx(kind);
}

function events() {
  for (const e of w.events) {
    if (e.e === "level") {
      G.banner = { t: 0, name: e.name, lv: e.lv, fresh: e.fresh || e.lv === 0, frog: e.frog };
      G.float = []; G.swim = []; G.hops = []; G.rings = []; G.spares = [];
    } else if (e.e === "fling") {
      G.tw = { x: SX - e.x, y: SY - e.y }; G.twang = 0;
      sfx("twang"); G.flown = true;
    } else if (e.e === "power") {
      if (e.k === "tree") { sfx("pip"); F.burst(e.x, e.y, "#5fd04a", 8); }
      else if (e.k === "dart") { sfx("whip"); F.burst(e.x, e.y, "#2b7fe0", 6); G.zipAt = G.real; }
      else if (e.k === "bull") { sfx("thump"); F.burst(e.x, e.y, "#6b7a2a", 6); }
      else if (e.k === "toad") { sfx("puff"); G.rings.push({ x: e.x, y: e.y, t: 0, r: 44 }); F.hit(0.45); F.freeze(3); }
    } else if (e.e === "knock") {
      if (e.j > 3) noise(e.mat === "acorn" ? "kick" : e.mat, 0.07);
      if (e.j > 8) F.hit(Math.min(0.5, e.j / 40));
      F.burst(e.x, e.y, LOOK[e.mat] && LOOK[e.mat].bits ? LOOK[e.mat].bits[1] : "#d9c9a8", 2);
    } else if (e.e === "break") {
      broke(e);
    } else if (e.e === "splash") {
      splash(e);
    } else if (e.e === "hop") {
      G.hops.push({ x: e.x, y: e.y, t: 0, k: e.frog, kid: e.kid, to: Math.max(SHORE + 6, Math.min(W - 6, e.x + 12)) });
    } else if (e.e === "collapse") {
      if (reduced) w.L.slow = 0;
      F.hit(0.5);
      F.pop(150, 120, "TIMBER", "#ffe27a");
    } else if (e.e === "tally") {
      if (e.clear) { F.pop(W / 2, 96, "CLEARED", "#ffe27a"); sfx("band"); }
      else { F.pop(W / 2, 96, "ALL FLUNG", "#cfe3ea"); sfx("plop"); }
      G.spares = Array.from({ length: e.spare }, (_, i) => ({ t: -0.5 - i * 0.4, k: w.L.frogs[i] }));
    } else if (e.e === "card") {
      G.card = { t: 0, stars: e.stars, shown: 0 };
    } else if (e.e === "end") {
      sfx("perk");
    }
  }
  w.events.length = 0;
}

function broke(e) {
  const L = LOOK[e.mat], size = e.r ? e.r * 2 : Math.max(e.hx, e.hy) * 2;
  const n = Math.min(16, 4 + Math.round(size / 3));
  L.bits.forEach((c) => F.burst(e.x, e.y, c, Math.ceil(n / 3)));
  G.dust.push({ x: e.x, y: e.y, t: 0, r: 4 + size / 4, c: e.mat === "ice" ? "#f4fcff" : e.mat === "reed" ? "#e8e0b0" : "#d9c9a8" });
  if (e.kind === "acorn") {
    noise("pop", 0.03);
    F.got(e.x, e.y, "+" + (e.gold ? 150 : 50), { sound: "catch", big: !!e.gold, c: e.gold ? "#ffb000" : "#ffe27a" });
    G.caps.push({ x: e.x, y: e.y - 3, vx: (Math.random() - 0.5) * 60, vy: -90, t: 0, gold: e.gold });
  } else {
    noise(e.mat, 0.04);
    F.pop(e.x, e.y - 4, "+" + MATS[e.mat].pts, "#ece5d8");
    if (e.mat === "stone") F.hit(0.35);
  }
}

function splash(e) {
  const big = e.kind === "frog" ? 1 : e.r ? e.r / 5 : Math.max(e.hx, e.hy) / 10;
  if (!e.off) {
    G.rings.push({ x: e.x, y: WATER + 1, t: 0, r: 8 + big * 6, flat: true });
    F.burst(e.x, WATER - 2, "#cfe3ea", 4 + Math.round(big * 4));
    noise("splash", 0.05);
  }
  if (e.kind === "frog") { if (!e.off) G.swim.push({ x: e.x, t: 0, k: e.frog, kid: e.kid }); return; }
  if (e.kind === "acorn") F.pop(e.x, WATER - 12, "+" + (e.gold ? 150 : 50), e.gold ? "#ffb000" : "#ffe27a");
  else F.pop(e.x, WATER - 10, "+5", "#cfe3ea");
  if (!e.off) G.float.push({ x: e.x, y: WATER, vx: Math.max(-14, Math.min(14, e.vx * 0.1)) + 4, t: 0, mat: e.mat, kind: e.kind, hx: e.hx, hy: e.hy, r: e.r, a: e.a, gold: e.gold, sink: e.mat === "stone" || e.mat === "mud" });
}

// ---------------------------------------------------------------- the game

export const sling = {
  key: "sling", W, H, title: "CATTAIL SLING", feel: F,
  aria: "Pull back anywhere to aim the frog in the cattail and let go to fling it at acorns stacked on logs and rafts out on the pond. Tap while it flies for its trick. Knock every acorn open or into the water. Arrow keys aim and space flings.",
  help: ["PULL BACK ANYWHERE AND LET GO", "TAP IN THE AIR FOR THE FROG'S TRICK", "KNOCK EVERY ACORN OFF OR OPEN", "FIVE LEVELS, STARS FOR FROGS TO SPARE", "TAP TO START"],

  reset(g) {
    G = { g, col: colours(g, null), real: 0, pull: null, key: null, twang: 9, tw: { x: 0, y: 0 }, banner: null, float: [], swim: [], hops: [], rings: [], dust: [], caps: [], spares: [], card: null, flown: false, zipAt: -9, blinkAt: 2 };
    w = round();
    begin(w, 0);
    w.events = w.events.filter((e) => e.e === "level");
    events();
  },

  demo() { return w && w.L && !G.flown ? { x: 90, y: 190, to: [66, 206] } : null; },

  press(x, y) {
    if (!w || !w.L) return;
    if (w.L.st === "fly") { if (!power(w)) noise("pip", 0.2); return; }
    if (w.L.st === "ready") { G.pull = { x0: x, y0: y, dx: 0, dy: 0, step: 0 }; G.key = null; }
  },

  drag(x, y) {
    if (!G || !G.pull) return;
    G.pull.dx = x - G.pull.x0; G.pull.dy = y - G.pull.y0;
    const s = Math.floor(pulled(G.pull.dx, G.pull.dy).l / 7);
    if (s !== G.pull.step) { G.pull.step = s; if (s) noise("creak", 0.05); }
  },

  release() {
    if (!G || !G.pull) return;
    const p = G.pull;
    G.pull = null;
    if (!G.cancel) fling(w, p.dx, p.dy);
    G.cancel = false;
  },

  keys(code, down) {
    if (!G || !down || !w.L) return;
    const st = w.L.st;
    if (code === "Space" || code === "Enter") {
      if (st === "fly") power(w);
      else if (st === "ready" && G.key) { const k = G.key; G.key = null; fling(w, -Math.cos(k.a) * k.l, Math.sin(k.a) * k.l); }
      else if (st === "ready") G.key = { a: 0.5, l: 24 };
      return;
    }
    if (st !== "ready") return;
    G.key = G.key || { a: 0.5, l: 24 };
    if (code === "ArrowUp" || code === "KeyW") G.key.a = Math.min(1.3, G.key.a + 0.04);
    if (code === "ArrowDown" || code === "KeyS") G.key.a = Math.max(-0.3, G.key.a - 0.04);
    if (code === "ArrowRight" || code === "KeyD") G.key.l = Math.min(PULL, G.key.l + 1);
    if (code === "ArrowLeft" || code === "KeyA") G.key.l = Math.max(8, G.key.l - 1);
  },

  step(dt) {
    G.real += dt;
    if (w.st !== "done") step(w, dt);
    else w.done_t += dt;
    G.twang += dt;
    const ad = (o) => { o.t += dt; };
    [G.float, G.swim, G.hops, G.rings, G.dust, G.caps, G.spares].forEach((l) => l.forEach(ad));
    if (G.banner) G.banner.t += dt;
    if (G.card) G.card.t += dt;
    G.float.forEach((f) => { f.x += f.vx * dt; });
    G.float = G.float.filter((f) => f.x < W + 20 && f.x > -20 && f.t < (f.sink ? 2 : 14));
    G.hops = G.hops.filter((h) => { if (h.t < 0.5) return true; splash({ x: h.to, kind: "frog", frog: h.k, kid: h.kid }); return false; });
    G.swim = G.swim.filter((s) => s.t < 9 && swimX(s) > SHORE);
    G.rings = G.rings.filter((r) => r.t < 0.6);
    G.dust = G.dust.filter((d) => d.t < 0.5);
    G.caps = G.caps.filter((c) => c.t < 1.2);
    G.spares.forEach((s) => { if (s.t >= 0 && !s.paid) { s.paid = true; F.pop(24, 196, "+100", "#ffe27a"); sfx("hop"); } });
    if (G.card) {
      const n = Math.min(G.card.stars, Math.floor((G.card.t - 0.2) / 0.3) + 1);
      if (G.card.t > 0.2 && n > G.card.shown) { G.card.shown = n; sfx("perk"); F.burst(W / 2 - 30 + (n - 1) * 30, 136, "#ffb000", 10); }
    }
    events();
  },

  pause() { if (G) { G.cancel = !!G.pull; G.key = null; } },
  over() { return w && w.st === "done" && w.done_t > 1.4; },
  score() { return w ? w.score : 0; },
  why() { if (!w) return ""; const s = w.results.reduce((a, r) => a + r.stars, 0); return s + " OF " + LEVELS * 3 + " STARS"; },
  report() { return { counts: counts(w) }; },

  // A row a level with its name, its stars and what it scored.
  card(quick) {
    if (!w || !w.results.length) return null;
    const box = document.createElement("div"), c = document.createElement("canvas"), x = c.getContext("2d");
    box.className = "slingcard";
    const rows = w.results, rh = 36, cw = 480;
    c.width = cw; c.height = rows.length * rh + 4;
    c.style.width = (cw / 2) + "px";
    x.imageSmoothingEnabled = false;
    rows.forEach((r, i) => {
      const t = i * rh + 2;
      x.fillStyle = "#0b1f17"; x.fillRect(2, t, cw - 4, rh - 4);
      x.font = "700 19px 'JetBrains Mono', monospace"; x.textAlign = "left"; x.textBaseline = "middle";
      x.fillStyle = r.clear ? "#ece5d8" : "#9c8f79"; x.fillText(r.name, 10, t + rh / 2 - 1);
      for (let s = 0; s < 3; s++) star(x, cw - 170 + s * 26, t + rh / 2 - 2, 10, s < r.stars);
      x.textAlign = "right"; x.fillStyle = "#ffe27a"; x.fillText(String(r.score), cw - 10, t + rh / 2 - 1);
    });
    if (!quick) {
      const p = document.createElement("p");
      p.className = "dim";
      const c2 = w.counts;
      p.textContent = c2.acorns + " acorns and " + c2.blocks + " pieces broken, " + c2.splashed + " in the pond";
      box.append(p);
    }
    box.append(c);
    return box;
  },

  draw(x) {
    if (!w) return;
    const L = w.L, S = SKIES[L.def.sky], t = G.real;
    sky(x, S, t);
    water(x, S, t);
    G.float.filter((f) => !f.sink).forEach((f) => floater(x, f));
    for (const b of L.bodies) if (b.kind === "plat") plat(x, b);
    for (const b of L.bodies) {
      if (b.kind === "plat" || b.kind === "frog" || b.gone) continue;
      if (b.kind === "acorn") acorn(x, b.x, b.y, b.a, b.gold, t);
      else if (b.s === "circle") pebble(x, b);
      else box(x, b);
    }
    G.caps.forEach((c) => { const cx = c.x + c.vx * c.t, cy = c.y + c.vy * c.t + 160 * c.t * c.t; x.fillStyle = c.gold ? "#c07a00" : "#6b4a2a"; x.fillRect(cx - 3, cy - 1, 6, 3); });
    G.float.filter((f) => f.sink).forEach((f) => floater(x, f));
    surface(x, S, t);
    G.swim.forEach((s) => swimmer(x, s));
    G.rings.forEach((r) => ring(x, r));
    G.dust.forEach((d) => { x.fillStyle = d.c; x.globalAlpha = 0.6 * (1 - d.t / 0.5); disc(x, d.x, d.y - d.t * 8, d.r + d.t * 14); x.globalAlpha = 1; });
    bank(x);
    queue(x, L);
    cattail(x, false);
    aim(x, L);
    // The frog in the sling, or each one in the air.
    if (L.st === "ready" || (L.st === "load" && L.st_t > 0.35)) {
      const p = G.pull ? pulled(G.pull.dx, G.pull.dy) : G.key ? pulled(-Math.cos(G.key.a) * G.key.l, Math.sin(G.key.a) * G.key.l) : { x: SX, y: SY, l: 0 };
      band(x, p.x, p.y + 2, p.l / PULL);
      if (L.frogs[0]) frog(x, L.frogs[0], p.x, p.y, 1, blink());
    } else {
      band(x, SX, SY + 2, 0);
      if (L.st === "load" || L.st === "intro") hopIn(x, L);
    }
    for (const b of L.flying) {
      if (b.gone) continue;
      const s = b.kid ? 0.8 : 1;
      if (b.zip > 0 && !reduced) { x.fillStyle = "rgba(255,255,255,0.5)"; for (let i = 1; i < 5; i++) x.fillRect(b.x - b.vx * i * 0.008 - 1, b.y - b.vy * i * 0.008, 2, 1); }
      frog(x, b.frog, b.x, b.y, s * (b.puff > 0 ? 1 + b.puff : 1));
    }
    cattail(x, true);
    G.hops.forEach((h) => { const k = h.t / 0.5, hx = h.x + (h.to - h.x) * k, hy = h.y + (WATER - h.y) * k - Math.sin(k * Math.PI) * 14; frog(x, h.k, hx, hy, h.kid ? 0.8 : 1); });
    F.draw(x);
    hud(x, L);
  },
};

function blink() { return (G.real % 3.3) < 0.12; }

function floater(x, f) {
  const bob = Math.sin(f.t * 2.4 + f.x) * 0.8, y = f.sink ? WATER + 2 + f.t * 12 : WATER + 1 + bob;
  if (f.sink) {
    x.globalAlpha = Math.max(0, 1 - f.t / 2);
    x.fillStyle = "rgba(207,240,246,0.7)"; x.fillRect(f.x + Math.sin(f.t * 6) * 2, WATER + 2 + f.t * 4, 1, 1);
  }
  if (f.kind === "acorn") acorn(x, f.x, y, 0.3 + Math.sin(f.t) * 0.3, f.gold, f.t);
  else if (f.r) pebble(x, { x: f.x, y, a: 0, r: f.r, hp: 1, hp0: 1, id: 0 });
  else box(x, { x: f.x, y, a: Math.abs(Math.cos(f.a)) > 0.7 ? 0 : Math.PI / 2, hx: f.hx, hy: f.hy, mat: f.mat, hp: 1, hp0: 1, id: 0 });
  x.globalAlpha = 1;
}

// A frog in the pond swims back to the bank with its eyes up and a V behind it.
function swimX(s) { return s.x - Math.max(0, s.t - 0.5) * 32; }

function swimmer(x, s) {
  if (s.t < 0.5) return;
  const sx = swimX(s), p = pal(s.k), y = WATER;
  x.fillStyle = "rgba(255,255,255,0.4)";
  for (let i = 1; i < 6; i++) { x.fillRect(sx + 3 + i * 2, y + 1 + i * 0.5, 1, 1); x.fillRect(sx + 3 + i * 2, y + 1 - i * 0.3, 1, 1); }
  x.fillStyle = "#0a0908"; x.fillRect(sx - 4, y - 3, 9, 4);
  x.fillStyle = p.X; x.fillRect(sx - 3, y - 2, 7, 3);
  x.fillStyle = p.E; x.fillRect(sx - 3, y - 3, 2, 2); x.fillRect(sx + 2, y - 3, 2, 2);
  x.fillStyle = "#0a0908"; x.fillRect(sx - 2, y - 3, 1, 1); x.fillRect(sx + 3, y - 3, 1, 1);
}

function ring(x, r) {
  const k = r.t / 0.6, rr = r.r * (0.3 + k), n = Math.round(10 + rr);
  x.fillStyle = r.flat ? "rgba(230,248,252," + (0.8 * (1 - k)).toFixed(2) + ")" : "rgba(255,246,220," + (0.7 * (1 - k)).toFixed(2) + ")";
  for (let i = 0; i < n; i++) { const a = i / n * Math.PI * 2; x.fillRect(r.x + Math.cos(a) * rr, r.y + Math.sin(a) * rr * (r.flat ? 0.25 : 1), 1, 1); }
}

// The frogs still to fling wait on the bank, the next one hopping into the
// cattail once the last is back in the pond.
function queue(x, L) {
  const wait = L.st === "ready" || (L.st === "load" && L.st_t > 0.35) ? L.frogs.slice(1) : L.frogs.slice(L.st === "load" || L.st === "intro" ? 1 : 0);
  wait.forEach((k, i) => frog(x, k, 24 - i * 11, 210 - (k === "bull" ? 1 : 0), 1, (G.real + i) % 4.1 < 0.12));
  G.spares.forEach((s) => {
    if (s.t < 0) return;
    const k = Math.min(1, s.t / 0.5), hx = 24 + k * 40, hy = 210 + k * (WATER - 210) - Math.sin(k * Math.PI) * 16;
    if (k < 1) frog(x, s.k, hx, hy, 1);
  });
}

function hopIn(x, L) {
  if (!L.frogs[0]) return;
  const k = L.st === "intro" ? 0 : Math.min(1, L.st_t / 0.35), hx = 24 + (SX - 24) * k, hy = 210 + (SY - 210) * k - Math.sin(k * Math.PI) * 18;
  frog(x, L.frogs[0], hx, hy, 1);
}

// While pulling the dots show the start of the path, and the last fling's
// whole path stays behind faintly to aim the next one off.
function aim(x, L) {
  x.fillStyle = "rgba(255,246,220,0.4)";
  for (let i = 0; i < L.last.length; i += 2) x.fillRect(L.last[i].x - 0.5, L.last[i].y - 0.5, 1, 1);
  if (L.st !== "ready") return;
  const d = G.pull ? G.pull : G.key ? { dx: -Math.cos(G.key.a) * G.key.l, dy: Math.sin(G.key.a) * G.key.l } : null;
  if (!d) return;
  const p = pulled(d.dx, d.dy);
  if (p.l < 6) return;
  arc(p, 0.42, 0.035).forEach((q, i) => {
    x.globalAlpha = 1 - i / 14;
    x.fillStyle = "#0a0908"; x.fillRect(q.x - 1.5, q.y - 1.5, 3, 3);
    x.fillStyle = "#fff6dc"; x.fillRect(q.x - 1, q.y - 1, 2, 2);
  });
  x.globalAlpha = 1;
}

function star(x, cx, cy, r, on) {
  const pts = [];
  for (let i = 0; i < 10; i++) { const a = -Math.PI / 2 + i * Math.PI / 5, rr = i % 2 ? r * 0.45 : r; pts.push([cx + Math.cos(a) * rr, cy + Math.sin(a) * rr]); }
  x.beginPath(); pts.forEach(([px, py], i) => (i ? x.lineTo(px, py) : x.moveTo(px, py))); x.closePath();
  x.fillStyle = on ? "#ffb000" : "#2a2620"; x.fill();
  x.lineWidth = 1.5; x.strokeStyle = "#0a0908"; x.stroke();
}

function text(x, s, cx, cy, font, c, align) {
  x.font = font; x.textAlign = align || "center"; x.lineWidth = 3; x.lineJoin = "round"; x.miterLimit = 2; x.strokeStyle = "#0a0908";
  x.strokeText(s, cx, cy); x.fillStyle = c; x.fillText(s, cx, cy);
}

function hud(x, L) {
  text(x, String(w.score), 6, 18, FONT, "#ece5d8", "left");
  text(x, (w.lv + 1) + "/" + LEVELS, W - 6, 18, FONT, "#ece5d8", "right");
  text(x, L.def.name, W - 6, 28, SMALL, "#fff6dc", "right");
  if (L.score) text(x, "+" + L.score, 6, 28, SMALL, "#ffe27a", "left");
  // A pip a star over the level's score, lit once it's reached.
  const b = bar(w.lv, L.def);
  for (let i = 0; i < 3; i++) star(x, 9 + i * 10, 38, 3.5, i ? L.score >= b[i] : L.acorns <= 0);
  const B = G.banner;
  if (B && B.t < 2.4 && L.st !== "card") {
    const a = Math.min(1, B.t / 0.2, (2.4 - B.t) / 0.4);
    x.globalAlpha = a;
    x.fillStyle = "rgba(10,9,8,0.6)"; x.fillRect(0, 68, W, B.fresh ? 78 : 36);
    text(x, "LEVEL " + (B.lv + 1), W / 2, 84, SMALL, "#9fdcf2");
    text(x, B.name, W / 2, 98, BIG, "#ffe27a");
    if (B.fresh) {
      const f = FROGS[B.frog];
      frog(x, B.frog, W / 2, 114, 1.4, false);
      text(x, f.name, W / 2, 128, SMALL, "#ece5d8");
      text(x, f.line, W / 2, 139, SMALL, "#9fdcf2");
    }
    x.globalAlpha = 1;
  }
  if (L.st === "card" && G.card) {
    const C = G.card;
    x.fillStyle = "rgba(10,9,8,0.7)"; x.fillRect(0, 96, W, 72);
    text(x, L.def.name, W / 2, 112, FONT, "#ece5d8");
    for (let i = 0; i < 3; i++) {
      const on = i < C.shown, k = on && !reduced ? 1 + 0.4 * Math.max(0, 1 - (C.t - 0.2 - i * 0.3) / 0.15) : 1;
      star(x, W / 2 - 30 + i * 30, 136, 10 * k, on);
    }
    text(x, String(L.score), W / 2, 162, FONT, "#ffe27a");
  }
}

// The label on the Play disk.
export function slingArt(x, g) {
  const S = SKIES[0], was = w, wasG = G;
  G = { real: 0, col: colours(g, null) };
  w = { st: "done" };
  for (let i = 0; i < 8; i++) { x.fillStyle = mix(S.top, S.low, i / 7); x.fillRect(0, i * 9, 168, 9); }
  x.fillStyle = S.sun; disc(x, 140, 18, 7);
  x.fillStyle = S.far; for (let i = 0; i < 10; i++) x.fillRect(i * 18 - 4, 56 - (i * 37) % 7, 20, 20);
  x.fillStyle = S.water; x.fillRect(0, 72, 168, 24);
  x.fillStyle = "rgba(255,255,255,0.25)"; for (let i = 0; i < 8; i++) x.fillRect((i * 41) % 160, 78 + (i * 13) % 16, 6, 1);
  x.fillStyle = "#0a0908"; x.fillRect(91, 67, 74, 12);
  x.fillStyle = "#6b4a2a"; x.fillRect(92, 68, 72, 10); x.fillStyle = "#8a6a3a"; x.fillRect(92, 68, 72, 2); x.fillStyle = "#c0a070"; x.fillRect(92, 68, 3, 10); x.fillRect(161, 68, 3, 10);
  box(x, { x: 106, y: 56, a: 0, hx: 3, hy: 12, mat: "wood", hp: 1, hp0: 1, id: 0 });
  box(x, { x: 150, y: 56, a: 0, hx: 3, hy: 12, mat: "wood", hp: 1, hp0: 1, id: 0 });
  box(x, { x: 128, y: 41, a: 0, hx: 28, hy: 3, mat: "reed", hp: 1, hp0: 1, id: 0 });
  x.save(); x.translate(128, 61); x.scale(1.4, 1.4); acorn(x, 0, 0, 0, false, 0); x.restore();
  x.save(); x.translate(114, 31); x.scale(1.4, 1.4); acorn(x, 0, 0, 0.2, true, 0); x.restore();
  x.save(); x.translate(142, 31); x.scale(1.4, 1.4); acorn(x, 0, 0, -0.2, false, 0); x.restore();
  x.fillStyle = "#0a0908"; x.fillRect(0, 71, 40, 25);
  x.fillStyle = "#5a3e24"; x.fillRect(0, 72, 39, 24); x.fillStyle = "#4fa043"; x.fillRect(0, 72, 39, 3);
  for (const cx of [14, 32]) {
    x.fillStyle = "#0a0908"; x.fillRect(cx - 1.5, 30, 4, 43); x.fillRect(cx - 3, 18, 7, 15);
    x.fillStyle = "#3f7a30"; x.fillRect(cx - 0.5, 31, 2, 42);
    x.fillStyle = "#6b4a2a"; x.fillRect(cx - 2, 19, 5, 13); x.fillStyle = "#8a6a3a"; x.fillRect(cx - 2, 19, 1, 12);
  }
  x.strokeStyle = "#e6d27a"; x.lineWidth = 2; x.beginPath(); x.moveTo(15, 30); x.lineTo(10, 40); x.lineTo(33, 30); x.stroke();
  frog(x, "pond", 10, 40, 2, false);
  for (let i = 1; i < 10; i++) { const s = i * 0.1; x.fillStyle = "#0a0908"; x.fillRect(38 + 120 * s - 1.5, 30 - 70 * s + 60 * s * s - 1.5, 3, 3); x.fillStyle = "#fff6dc"; x.fillRect(38 + 120 * s - 1, 30 - 70 * s + 60 * s * s - 1, 2, 2); }
  G = wasG; w = was;
}

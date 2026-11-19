import { reduced } from "./api.js";

// The painted scenes at the top of each place, painted small once and laid on
// the canvas a whole number of device pixels to a pixel so they stay crisp.
// After that only fireflies, lamps and rain move, and only while on screen.

export function rng(a) {
  return () => {
    a |= 0; a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export function px(g, c, x, y, w = 1, h = 1) { g.fillStyle = c; g.fillRect(Math.round(x), Math.round(y), w, h); }

// The left edge of a picture laid out w wide, centred on a canvas of width cw.
export function mid(cw, w) { return Math.floor((cw - w) / 2); }

// What the sky and water look like by the real day. By day it's dusk, so
// the lamps still read, and rain greys both.
export function look(day) {
  const d = day || {}, dark = !!d.night, wet = !!d.rain;
  if (dark && wet) return { dark, wet, sky: ["#0a0e18", "#10151f", "#161c28", "#1c2330"], hill: "#131a26", tree: "#0a0f18", bank: "#0e1820", water: ["#0d1824", "#0a131c"], line: "#1a2a3a", line2: "#24384a" };
  if (dark) return { dark, wet, sky: ["#070b1e", "#0c1330", "#141d44", "#1f2a58"], hill: "#111a3c", tree: "#0a1128", bank: "#0e1d24", water: ["#0b1b30", "#081424"], line: "#16304a", line2: "#21456a" };
  if (wet) return { dark, wet, sky: ["#2e3346", "#3a3f52", "#4a4f62", "#5a5f70"], hill: "#2a2f3e", tree: "#1e2230", bank: "#26302c", water: ["#26334a", "#1e2a3e"], line: "#34445e", line2: "#40546e" };
  return { dark, wet, sky: ["#2a2350", "#5a3a6a", "#a8546a", "#e08a62", "#f2b878"], hill: "#4a3050", tree: "#2a1f3a", bank: "#2f3a2a", water: ["#2a3a58", "#1e2c48"], line: "#34507a", line2: "#40608a" };
}

// Bands of colour with a dither where each meets the next.
export function sky(g, w, h, bands) {
  const n = bands.length, bh = h / n;
  for (let y = 0; y < h; y++) {
    const b = Math.min(n - 1, Math.floor(y / bh)), t = (y - b * bh) / bh;
    px(g, bands[b], 0, y, w, 1);
    if (b < n - 1 && t > 0.6) {
      g.fillStyle = bands[b + 1];
      for (let x = (y % 2); x < w; x += 2) if (t > 0.8 || x % 4 < 2) g.fillRect(x, y, 1, 1);
    }
  }
}

export function stars(g, w, h, r, n = 40) { for (let i = 0; i < n; i++) px(g, r() < 0.2 ? "#ffffff" : "#8a96d0", r() * w, r() * h); }

export function glow(g, x, y, r, c, a) {
  g.save();
  g.fillStyle = c;
  for (let k = r; k > 0; k -= 2) { g.globalAlpha = a * (1 - k / r) * 0.6 + 0.02; g.beginPath(); g.arc(x, y, k, 0, 7); g.fill(); }
  g.restore();
}

export function water(g, w, y0, h, top, bot, r, line = "#16304a", line2 = "#21456a") {
  px(g, top, 0, y0, w, Math.ceil(h / 2));
  px(g, bot, 0, y0 + Math.ceil(h / 2), w, h - Math.ceil(h / 2));
  for (let i = 0; i < w / 3; i++) {
    const y = y0 + 2 + Math.floor(r() * (h - 2)), x = Math.floor(r() * w);
    px(g, i % 5 ? line : line2, x, y, 2 + Math.floor(r() * 7), 1);
  }
}

export function pad(g, cx, cy, rx, ry, flower, bare) {
  for (let y = -ry; y <= ry; y++) for (let x = -rx; x <= rx; x++) {
    if ((x * x) / (rx * rx) + (y * y) / (ry * ry) > 1) continue;
    if (x > rx * 0.25 && y < 0 && y > -x * 0.4) continue;
    const edge = (x * x) / ((rx - 1) ** 2) + (y * y) / ((ry - 1) ** 2) > 1;
    const c = bare ? (edge ? (y < 0 ? "#3a6a48" : "#12301e") : "#24503a") : edge ? (y < 0 ? "#5fa860" : "#163a22") : (y < -ry * 0.3 ? "#3f8a4a" : "#2d6a38");
    px(g, c, cx + x, cy + y);
  }
  if (flower) {
    const fx = cx - rx + 3, fy = cy - 2;
    px(g, "#f6b8cc", fx, fy - 2, 3, 2); px(g, "#e2687a", fx + 1, fy - 3); px(g, "#ffe39a", fx + 1, fy - 1);
    px(g, "#fff", fx - 1, fy - 1); px(g, "#fff", fx + 3, fy - 1);
  }
}

export function cattail(g, x, base, top, sway) {
  for (let y = base; y > top; y--) px(g, "#2d3a1e", x + Math.round(sway * (base - y) / (base - top)), y);
  const hx = x + sway;
  px(g, "#5a3a1e", hx - 1, top - 1, 3, 7); px(g, "#7a5230", hx - 1, top - 1, 1, 6); px(g, "#2d3a1e", hx, top - 4, 1, 3);
}

export function reeds(g, x0, x1, base, r, col = ["#16301e", "#2a4a26"]) {
  for (let x = x0; x < x1; x++) if (r() < 0.55) {
    const h = 4 + Math.floor(r() * 10);
    for (let y = 0; y < h; y++) px(g, y > h - 3 ? col[1] : col[0], x + (y > h / 2 && r() < 0.3 ? 1 : 0), base - y);
  }
}

export function trees(g, w, base, r, col = "#0a1128") {
  for (let x = -4; x < w; x += 5 + Math.floor(r() * 5)) {
    const th = 8 + Math.floor(r() * 12);
    if (r() < 0.6) for (let y = 0; y < th; y++) { const half = Math.floor((y / th) * 4); px(g, col, x + 2 - half, base - th + y, half * 2 + 1, 1); }
    else { g.fillStyle = col; g.beginPath(); g.arc(x + 3, base - th + 5, 5, 0, 7); g.fill(); px(g, col, x + 2, base - th + 5, 2, th - 5); }
  }
}

export function hills(g, w, base, amp, col, k = 19) {
  for (let x = 0; x < w; x++) px(g, col, x, base + Math.round(Math.sin(x / k) * amp + Math.sin(x / 7) * 1.2), 1, 200);
}

export const LAMP = "#ffcf6a";

export function lantern(g, x, y, h = 30, light = 0.5) {
  glow(g, x, y + 6, 24, LAMP, light);
  px(g, "#2a1d12", x - 1, y, 2, h); px(g, "#3a2a1a", x - 4, y - 2, 8, 2);
  px(g, "#1a120a", x - 4, y + 1, 8, 10); px(g, LAMP, x - 3, y + 2, 6, 8); px(g, "#fff2c0", x - 1, y + 4, 2, 4); px(g, "#1a120a", x - 1, y + 2, 2, 8);
}

export function hanging(g, x, y, c = LAMP) {
  glow(g, x, y + 3, 10, c, 0.55);
  px(g, "#1a120a", x - 2, y, 5, 6); px(g, c, x - 1, y + 1, 3, 4);
}

// A string of lights between two points, and where each light hangs so a
// scene can flicker them.
const BULBS = [LAMP, "#f29bb8", "#9fe6b8", LAMP, "#9fc8ff"];

export function string(g, x0, x1, y, sag, n) {
  for (let x = x0; x <= x1; x++) { const t = (x - x0) / (x1 - x0); px(g, "#2a2018", x, y + Math.sin(t * Math.PI) * sag); }
  const lit = [];
  for (let i = 1; i < n; i++) {
    const t = i / n, lx = Math.round(x0 + (x1 - x0) * t), ly = Math.round(y + Math.sin(t * Math.PI) * sag) + 1, c = BULBS[(i - 1) % BULBS.length];
    hanging(g, lx, ly, c);
    lit.push([lx, ly + 3, 10, c]);
  }
  return lit;
}

export function moon(g, x, y, rr = 8) {
  glow(g, x, y, rr * 2.7, "#9fb0ff", 0.25);
  g.fillStyle = "#f4ecd0"; g.beginPath(); g.arc(x, y, rr, 0, 7); g.fill();
  if (rr > 5) { px(g, "#d8cca8", x - 3, y - 3, 2, 2); px(g, "#d8cca8", x + 2, y + 2, 3, 2); }
}

// A low sun behind the hills, for a dusk that's dry.
export function sun(g, x, y) {
  glow(g, x, y, 30, "#ffd8a0", 0.35);
  g.fillStyle = "#ffe2a8"; g.beginPath(); g.arc(x, y, 9, 0, 7); g.fill();
}

// The sky, the far hills and trees, and the water, for the day. horizon is
// where the water starts. A moon or a sun is placed at opt.moon.
export function backdrop(g, w, h, seed, opt, day) {
  const L = look(day), r = rng(seed), hor = opt.horizon || Math.round(h * 0.48);
  sky(g, w, hor, L.sky);
  if (L.dark && !L.wet) stars(g, w, hor - 14, r, opt.stars ?? 40);
  if (opt.moon && !L.wet) {
    const [mx, my, mr] = opt.moon;
    if (L.dark) moon(g, mx, my, (day && day.moon) ? mr + 2 : mr);
    else sun(g, mx, Math.max(my, hor - 16));
  }
  hills(g, w, hor - 14, 3, L.hill);
  trees(g, w, hor - 2, r, L.tree);
  px(g, L.bank, 0, hor - 3, w, 4);
  water(g, w, hor, h - hor, L.water[0], L.water[1], r, L.line, L.line2);
  return r;
}


// ---------------------------------------------------------------- the live bits

function alive(g, L, t, w, h) {
  g.save();
  g.translate(L.off || 0, 0);
  (L.lamps || []).forEach(([x, y, r, c], i) => {
    const f = (Math.sin(t * 6.1 + i * 2.3) + Math.sin(t * 2.7 + i)) / 2;
    if (f > 0) glow(g, x, y, r, c || LAMP, 0.12 * f);
  });
  if (L.flies) {
    const [n, x0, x1, y0, y1] = L.flies, r = rng(L.seed || 5);
    for (let i = 0; i < n; i++) {
      const bx = x0 + r() * (x1 - x0), by = y0 + r() * (y1 - y0);
      if (Math.sin(t * 1.7 + i * 1.9) < -0.4) continue;
      const x = Math.round(bx + Math.sin(t * 0.5 + i) * 3), y = Math.round(by + Math.cos(t * 0.6 + i * 1.3) * 2);
      glow(g, x, y, 4, "#fff6a0", 0.5); px(g, "#fffbd0", x, y);
    }
  }
  g.restore();
  if (L.rain) {
    g.fillStyle = "rgba(190,210,235,0.45)";
    for (let i = 0; i < w / 3; i++) {
      const x = Math.floor((i * 37.7 + t * 18) % (w + 8)) - 4, y = Math.floor((i * 23.3 + t * 90) % (h + 6)) - 6;
      g.fillRect(x, y, 1, 3);
    }
  }
}

// ---------------------------------------------------------------- the canvas

const all = new Set();
let io = null, ticking = 0, lastTick = 0;

function buffer(cv, w, h) {
  cv = cv || document.createElement("canvas");
  if (cv.width !== w || cv.height !== h) { cv.width = w; cv.height = h; }
  return cv;
}

function blit(sc, t) {
  let src = sc.base;
  if (sc.live) {
    sc.work = buffer(sc.work, sc.w, sc.h);
    const g = sc.work.getContext("2d");
    g.drawImage(sc.base, 0, 0);
    alive(g, sc.live, reduced ? 0 : t, sc.w, sc.h);
    src = sc.work;
  }
  const x = sc.cv.getContext("2d");
  x.imageSmoothingEnabled = false;
  x.drawImage(src, sc.ox, sc.oy, sc.w * sc.s, sc.h * sc.s);
}

function repaint(sc) {
  const cv = sc.cv, r = window.devicePixelRatio || 1, cw = cv.clientWidth, ch = cv.clientHeight;
  if (!cw || !ch || !sc.paint) return;
  const W = Math.round(cw * r), H = Math.round(ch * r), s = Math.max(1, Math.round(sc.scale * r));
  if (cv.width !== W || cv.height !== H) { cv.width = W; cv.height = H; }
  sc.s = s; sc.w = Math.ceil(W / s); sc.h = Math.ceil(H / s);
  sc.ox = Math.floor((W - sc.w * s) / 2); sc.oy = H - sc.h * s;
  sc.base = buffer(sc.base, sc.w, sc.h);
  const g = sc.base.getContext("2d");
  g.setTransform(1, 0, 0, 1, 0, 0);
  g.imageSmoothingEnabled = false;
  g.clearRect(0, 0, sc.w, sc.h);
  sc.live = sc.paint(g, sc.w, sc.h) || null;
  g.setTransform(1, 0, 0, 1, 0, 0);
  sc.stale = false;
  blit(sc, performance.now() / 1000);
  if (sc.live) tick();
}

function tick() {
  if (ticking || reduced) return;
  const step = (now) => {
    let any = false;
    all.forEach((sc) => { if (sc.shown && sc.live && sc.base) any = true; });
    if (!any || document.hidden) { ticking = 0; return; }
    if (now - lastTick > 120) {
      lastTick = now;
      all.forEach((sc) => { if (sc.shown && sc.live && sc.base) blit(sc, now / 1000); });
    }
    ticking = requestAnimationFrame(step);
  };
  ticking = requestAnimationFrame(step);
}

document.addEventListener("visibilitychange", () => { if (!document.hidden) tick(); });

let resized = 0;
window.addEventListener("resize", () => {
  cancelAnimationFrame(resized);
  resized = requestAnimationFrame(() => all.forEach((sc) => { sc.stale = true; if (sc.shown) repaint(sc); }));
});

// vista makes a canvas a painted scene, scale CSS pixels to one of its own.
export function vista(cv, scale = 2) {
  if (cv.vista) return cv.vista;
  const sc = { cv, scale, paint: null, base: null, work: null, live: null, shown: false, stale: true, w: 0, h: 0, s: 1, ox: 0, oy: 0 };
  // draw sets what the scene paints and paints it if it's in view. paint
  // gets the context and the size and hands back what moves, if anything.
  sc.draw = (paint) => { sc.paint = paint; sc.stale = true; if (sc.shown) repaint(sc); };
  // at is where a pointer event falls in the scene's own pixels.
  sc.at = (e) => {
    const r = cv.getBoundingClientRect(), k = cv.width / r.width;
    return [((e.clientX - r.left) * k - sc.ox) / sc.s, ((e.clientY - r.top) * k - sc.oy) / sc.s];
  };
  cv.vista = sc;
  all.add(sc);
  io = io || new IntersectionObserver((es) => es.forEach((e) => {
    const s = e.target.vista;
    s.shown = e.isIntersecting;
    if (s.shown && (s.stale || s.cv.width !== Math.round(s.cv.clientWidth * (window.devicePixelRatio || 1)))) repaint(s);
    else if (s.shown) tick();
  }));
  io.observe(cv);
  return sc;
}

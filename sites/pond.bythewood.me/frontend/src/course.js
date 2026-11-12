// Pad putt's holes and how the ball rolls on them. Nothing in here
// touches the page, so a script can play every hole through without one.
export const R = 3, CUP = 5, CAP = 6, VMAX = 420, PULL = 56, DT = 1 / 240;
const K = 0.9, C = 30, STOP = 6, WALL = 0.7, LOG = 3, SINK = 170, PULLIN = 380, MUD = 4;

function box(x0, y0, x1, y1) { return [[x0, y0, x1, y0], [x1, y0, x1, y1], [x1, y1, x0, y1], [x0, y1, x0, y0]]; }

// The pool a round is dealt from, easiest first. Walls are logs from end to
// end, zones are [x0, y0, x1, y1] and a push is pixels a second squared.
export const HOLES = [
  { name: "BANK SHOT", tier: 1, par: 2, tee: [60, 282], cup: [60, 68], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300), [30, 150, 124, 150]] },
  { name: "MOSS SLOPE", tier: 1, par: 2, tee: [100, 284], cup: [70, 62], ground: [[40, 30, 160, 300]],
    walls: [...box(40, 30, 160, 300), [118, 96, 160, 96]],
    slopes: [{ at: [40, 116, 160, 226], push: [80, 0] }] },
  { name: "MUSHROOMS", tier: 1, par: 2, tee: [100, 284], cup: [100, 60], ground: [[25, 30, 175, 300]],
    walls: [...box(25, 30, 175, 300), [25, 250, 50, 276], [175, 250, 150, 276]],
    bumpers: [[100, 172, 9], [64, 136, 8], [136, 136, 8], [100, 104, 8]] },
  { name: "MUD FLAT", tier: 1, par: 2, tee: [100, 284], cup: [70, 64], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300), [30, 112, 96, 112]],
    mud: [[30, 140, 136, 200]],
    bumpers: [[154, 120, 8]] },
  { name: "WATERWHEEL", tier: 1, par: 2, tee: [100, 284], cup: [100, 66], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300), [30, 166, 86, 166], [114, 166, 170, 166]],
    wheel: { x: 100, y: 166, half: 14, n: 3, turn: 3.2 } },
  { name: "TWO WAYS", tier: 1, par: 2, tee: [100, 284], cup: [100, 62], ground: [[25, 30, 175, 300]],
    walls: [...box(25, 30, 175, 300), [100, 120, 100, 246]],
    mud: [[25, 160, 96, 206]],
    bumpers: [[140, 184, 8]] },
  { name: "DOGLEG", tier: 1, par: 3, tee: [60, 282], cup: [146, 70], ground: [[30, 40, 170, 120], [30, 120, 90, 300]],
    walls: [[30, 40, 170, 40], [170, 40, 170, 120], [170, 120, 90, 120], [90, 120, 90, 300], [90, 300, 30, 300], [30, 300, 30, 40]],
    slopes: [{ at: [90, 40, 170, 120], push: [-30, 0] }] },
  { name: "THE STREAM", tier: 2, par: 2, tee: [80, 284], cup: [122, 60], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300)],
    streams: [{ at: [30, 140, 170, 182], push: [230, 0] }],
    water: [[148, 140, 170, 182]],
    bumpers: [[70, 96, 8]] },
  { name: "HOLLOW LOGS", tier: 2, par: 2, tee: [100, 284], cup: [70, 62], ground: [[30, 30, 170, 168], [30, 202, 170, 300]],
    walls: [...box(30, 30, 170, 168), ...box(30, 202, 170, 300)],
    slopes: [{ at: [30, 84, 170, 126], push: [-50, 0] }],
    warp: { from: [150, 208, 8], to: [56, 160], dir: [0, -1] } },
  { name: "LOG BRIDGE", tier: 2, par: 2, tee: [128, 284], cup: [66, 64], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300)],
    water: [[30, 130, 170, 200]],
    bridges: [[118, 130, 138, 200]] },
  { name: "THE TURTLE", tier: 2, par: 2, tee: [100, 284], cup: [100, 64], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300), [30, 160, 70, 160], [130, 160, 170, 160]],
    turtle: { x: 100, y: 160, r: 10, dx: 24, period: 5 } },
  { name: "ONE WAY REEDS", tier: 2, par: 2, tee: [100, 284], cup: [100, 62], ground: [[25, 30, 175, 300]],
    walls: [...box(25, 30, 175, 300), [62, 30, 62, 110], [138, 30, 138, 110], [62, 110, 82, 110], [118, 110, 138, 110]],
    oneway: [[82, 110, 118, 110, 0, -1]],
    bumpers: [[100, 186, 9]] },
  { name: "THE POND", tier: 2, par: 3, tee: [50, 282], cup: [146, 60], ground: [[25, 30, 175, 300]],
    walls: [...box(25, 30, 175, 300), [150, 210, 175, 240]],
    water: [[25, 30, 115, 160]],
    drift: { x: 70, y: 160, r: 11, dx: 34, dy: 0, period: 7 } },
  { name: "LILY CROSSING", tier: 2, par: 2, tee: [100, 284], cup: [128, 62], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300)],
    water: [[30, 136, 170, 204]],
    lilies: [[110, 200, 16], [112, 180, 16], [115, 160, 16], [118, 140, 16],
      [60, 200, 14], [57, 180, 14], [60, 160, 14], [64, 140, 14]] },
  { name: "THE GUARD", tier: 2, par: 2, tee: [100, 284], cup: [100, 62], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300), [30, 150, 82, 150], [118, 150, 170, 150]],
    guard: { x: 136, y: 136, reach: 30, wake: 1.6, period: 4, back: [0, 1] } },
  { name: "REED GATE", tier: 2, par: 3, tee: [100, 284], cup: [100, 62], ground: [[25, 30, 175, 300]],
    walls: [...box(25, 30, 175, 300), [25, 106, 82, 106], [118, 106, 175, 106]],
    gate: [82, 106, 118, 106], button: [152, 152, 7],
    bumpers: [[58, 196, 8]] },
  { name: "TIGHT SQUEEZE", tier: 3, par: 3, tee: [80, 284], cup: [100, 52], ground: [[60, 30, 140, 300]],
    walls: [...box(60, 30, 140, 300), [60, 226, 110, 226], [90, 160, 140, 160]],
    turtle: { x: 100, y: 104, r: 9, dx: 24, period: 4.4 } },
  { name: "GUSTY GAP", tier: 3, par: 3, tee: [100, 284], cup: [100, 64], ground: [[30, 30, 170, 130], [30, 200, 170, 300]],
    walls: [[30, 30, 170, 30], [30, 30, 30, 300], [170, 30, 170, 300], [30, 300, 170, 300], [30, 130, 170, 130], [30, 200, 80, 200], [120, 200, 170, 200]],
    water: [[30, 130, 170, 200]],
    slopes: [{ at: [80, 200, 120, 236], push: [0, 60] }],
    ramp: { x0: 80, x1: 120, y: 200, reach: [60, 0.25], at: [80, 200, 120, 236] },
    gust: { at: [30, 130, 170, 200], push: [170, 0], on: 1.6, period: 4 } },
  { name: "WINDY BRIDGE", tier: 3, par: 3, tee: [100, 284], cup: [60, 60], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300)],
    water: [[30, 100, 170, 220]],
    bridges: [[52, 100, 72, 220], [128, 100, 148, 220]],
    gust: { at: [30, 100, 100, 220], push: [150, 0], on: 1.4, period: 3.6 },
    mud: [[30, 30, 170, 70]] },
  { name: "ISLAND GREEN", tier: 3, par: 3, tee: [100, 284], cup: [100, 74], ground: [[30, 236, 170, 300], [66, 44, 134, 116]],
    walls: [[30, 300, 170, 300], [30, 236, 30, 300], [170, 236, 170, 300], [30, 236, 90, 236], [110, 236, 170, 236],
      [66, 44, 134, 44], [66, 44, 66, 116], [134, 44, 134, 116]],
    water: [[0, 116, 90, 236], [110, 116, 200, 236], [0, 0, 66, 116], [134, 0, 200, 116], [66, 0, 134, 44]],
    bridges: [[90, 116, 110, 236]],
    mud: [[66, 44, 134, 62]],
    drift: { x: 160, y: 150, r: 11, dx: 0, dy: 40, period: 8 } },
  { name: "THE LONG ONE", tier: 3, par: 4, tee: [60, 284], cup: [146, 56], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300), [30, 216, 132, 216], [68, 132, 170, 132]],
    mud: [[30, 240, 100, 300]],
    slopes: [{ at: [68, 140, 170, 210], push: [-60, 0] }],
    bumpers: [[100, 82, 8], [130, 104, 7]] },
  { name: "FROG AND GATE", tier: 3, par: 3, tee: [100, 284], cup: [100, 62], ground: [[25, 30, 175, 300]],
    walls: [...box(25, 30, 175, 300), [25, 106, 82, 106], [118, 106, 175, 106]],
    gate: [82, 106, 118, 106], button: [52, 160, 7],
    guard: { x: 40, y: 196, reach: 26, wake: 1.5, period: 4.2, back: [0.7, 0.7] } },
  { name: "DRIFTING PAD", tier: 3, par: 3, tee: [100, 284], cup: [0, -6], ground: [[30, 190, 170, 300]],
    walls: [[30, 300, 170, 300], [30, 190, 30, 300], [170, 190, 170, 300], [30, 190, 85, 190], [115, 190, 170, 190]],
    water: [[30, 30, 170, 190]],
    slopes: [{ at: [85, 190, 115, 226], push: [0, 60] }],
    ramp: { x0: 85, x1: 115, y: 190, reach: [48, 0.2], at: [85, 190, 115, 226] },
    bumpers: [[58, 244, 8]],
    pad: { x: 100, y: 104, r: 28, dx: 40, period: 12 } },
];

// Three holes from each tier, kept in pool order, so a round still starts
// gentle. The same seed always deals the same nine.
export function deal(seed) {
  let n = seed >>> 0;
  const rnd = () => {
    n = (n + 0x6d2b79f5) >>> 0;
    let z = Math.imul(n ^ (n >>> 15), n | 1);
    z ^= z + Math.imul(z ^ (z >>> 7), z | 61);
    return ((z ^ (z >>> 14)) >>> 0) / 4294967296;
  };
  const out = [];
  for (const tier of [1, 2, 3]) {
    const left = HOLES.filter((h) => h.tier === tier);
    for (let i = 0; i < 3; i++) out.push(left.splice(Math.floor(rnd() * left.length), 1)[0]);
  }
  return out.sort((a, b) => HOLES.indexOf(a) - HOLES.indexOf(b));
}

const PI2 = Math.PI * 2;

// Where the moving things are at t seconds into a hole, and how fast they go.
export function driftAt(d, t) {
  const a = PI2 * t / d.period, s = Math.sin(a), c = Math.cos(a) * PI2 / d.period;
  return { x: d.x + (d.dx || 0) * s, y: d.y + (d.dy || 0) * s, r: d.r, vx: (d.dx || 0) * c, vy: (d.dy || 0) * c };
}

export function cupAt(h, t) {
  if (!h.pad) return { x: h.cup[0], y: h.cup[1] };
  const p = driftAt(h.pad, t);
  return { x: p.x + h.cup[0], y: p.y + h.cup[1] };
}

// The wheel's paddles block the gap while one of them is down in it.
export function paddles(w, t) {
  const out = [];
  for (let i = 0; i < w.n; i++) out.push(((t / w.turn) * PI2 + i * PI2 / w.n) % PI2);
  return out;
}
export function wheelShut(w, t) { return paddles(w, t).some((a) => Math.abs(a - Math.PI) < 0.42); }

export function gustOn(g, t) { return t % g.period < g.on; }
export function awake(g, t) { return t % g.period < g.wake; }

// Reeds that fold one way, so they only stop a ball going against them.
const against = (w, b) => b.vx * w[4] + b.vy * w[5] < 0;

// Every wall the ball can meet right now, the gate and the wheel among them.
export function wallsAt(h, s, t) {
  const out = h.walls.slice();
  if (h.gate && !s.open) out.push(h.gate);
  if (h.wheel && wheelShut(h.wheel, t)) out.push([h.wheel.x - h.wheel.half, h.wheel.y, h.wheel.x + h.wheel.half, h.wheel.y]);
  return out;
}

const inside = (z, x, y) => x >= z[0] && x <= z[2] && y >= z[1] && y <= z[3];

// How far a ball sent at v goes on flat moss, for the aim line.
export function rollFor(v) { return v / K - C / (K * K) * Math.log(1 + K * v / C); }

export function speedFor(k) { return VMAX * Math.pow(Math.max(0, Math.min(1, k)), 1.6); }

function seg(b, w, e) {
  const [x1, y1, x2, y2] = w, dx = x2 - x1, dy = y2 - y1, L = dx * dx + dy * dy;
  const u = L ? Math.max(0, Math.min(1, ((b.x - x1) * dx + (b.y - y1) * dy) / L)) : 0;
  const px = x1 + dx * u, py = y1 + dy * u, m = R + LOG;
  let nx = b.x - px, ny = b.y - py, d = Math.hypot(nx, ny);
  if (d >= m) return 0;
  if (d < 1e-6) { nx = -dy; ny = dx; d = Math.hypot(nx, ny) || 1; }
  nx /= d; ny /= d;
  b.x = px + nx * m; b.y = py + ny * m;
  const vn = b.vx * nx + b.vy * ny;
  if (vn >= 0) return 0;
  b.vx -= (1 + e) * vn * nx; b.vy -= (1 + e) * vn * ny;
  return -vn;
}

function round(b, cx, cy, r, e, mvx, mvy) {
  let nx = b.x - cx, ny = b.y - cy;
  const d = Math.hypot(nx, ny), m = r + R;
  if (d >= m || d < 1e-6) return 0;
  nx /= d; ny /= d;
  b.x = cx + nx * m; b.y = cy + ny * m;
  const rx = b.vx - (mvx || 0), ry = b.vy - (mvy || 0), vn = rx * nx + ry * ny;
  if (vn >= 0) return 0;
  b.vx -= (1 + e) * vn * nx; b.vy -= (1 + e) * vn * ny;
  return -vn;
}

export function newBall(h, t) {
  const b = { x: h.tee[0], y: h.tee[1], vx: 0, vy: 0, air: 0, onPad: false, lip: false, warped: 0, flicked: 0, moving: false, rolled: 0 };
  b.from = { x: b.x, y: b.y, onPad: false };
  return b;
}

export function hit(b, a, k, t, h) {
  const v = speedFor(k);
  b.from = { x: b.x, y: b.y, onPad: b.onPad, padAt: b.onPad && h.pad ? driftAt(h.pad, t) : null };
  b.vx = Math.cos(a) * v; b.vy = Math.sin(a) * v; b.moving = true; b.rolled = 0; b.lip = false;
}

// One small step of the roll. ev gets what happened, for the sounds and bits.
export function roll(h, s, b, t, ev) {
  const dt = DT;
  b.rolled += dt;
  b.warped = Math.max(0, b.warped - dt);
  b.flicked = Math.max(0, b.flicked - dt);
  if (h.pad && b.onPad) { const p = driftAt(h.pad, t); b.x += p.vx * dt; b.y += p.vy * dt; }
  if (!b.moving) return;
  const gust = h.gust && gustOn(h.gust, t) && inside(h.gust.at, b.x, b.y);
  if (b.air > 0) {
    if (gust) { b.vx += h.gust.push[0] * dt; b.vy += h.gust.push[1] * dt; }
    b.x += b.vx * dt; b.y += b.vy * dt; b.air -= dt;
    if (b.air <= 0) {
      const p = h.pad && driftAt(h.pad, t);
      if (p && Math.hypot(b.x - p.x, b.y - p.y) < p.r - R) { b.onPad = true; b.vx *= 0.35; b.vy *= 0.35; ev.push(["land", b.x, b.y]); }
      else { b.vx *= 0.5; b.vy *= 0.5; ev.push(["thud", b.x, b.y]); }
    }
    return;
  }
  let sp = Math.hypot(b.vx, b.vy);
  const flow = (h.streams || []).find((z) => inside(z.at, b.x, b.y));
  if (flow) { b.vx += flow.push[0] * dt; b.vy += flow.push[1] * dt; }
  if (sp > STOP) (h.slopes || []).forEach((z) => { if (inside(z.at, b.x, b.y)) { b.vx += z.push[0] * dt; b.vy += z.push[1] * dt; } });
  if (sp > STOP && gust) { b.vx += h.gust.push[0] * dt; b.vy += h.gust.push[1] * dt; }
  const cup = cupAt(h, t), cd = Math.hypot(cup.x - b.x, cup.y - b.y);
  if (cd < CUP + 4 && cd > 0.01 && sp < SINK * 1.3) {
    const f = PULLIN * (1 - cd / (CUP + 4)) * dt;
    b.vx += (cup.x - b.x) / cd * f; b.vy += (cup.y - b.y) / cd * f;
  }
  sp = Math.hypot(b.vx, b.vy);
  const mud = (h.mud || []).some((z) => inside(z, b.x, b.y));
  const slow = Math.max(0, sp - (K * sp + C) * (mud ? MUD : 1) * dt);
  if (sp > 0) { b.vx *= slow / sp; b.vy *= slow / sp; }
  const py = b.y;
  b.x += b.vx * dt; b.y += b.vy * dt;

  wallsAt(h, s, t).forEach((w) => { const n = seg(b, w, w === h.gate ? 0.4 : WALL); if (n > 20) ev.push(["wall", b.x, b.y, n]); });
  (h.oneway || []).forEach((w) => { if (!against(w, b)) return; const n = seg(b, w, 0.4); if (n > 20) ev.push(["wall", b.x, b.y, n]); });
  (h.bumpers || []).forEach(([cx, cy, r]) => {
    const n = round(b, cx, cy, r, 1, 0, 0);
    if (!n) return;
    const v = Math.hypot(b.vx, b.vy), want = Math.min(520, Math.max(160, v * 1.2));
    b.vx *= want / v; b.vy *= want / v;
    ev.push(["bump", cx, cy, r]);
  });
  if (h.drift) {
    const d = driftAt(h.drift, t), n = round(b, d.x, d.y, d.r, 0.8, d.vx, d.vy);
    if (n > 20) ev.push(["pad", b.x, b.y, n]);
  }
  if (h.turtle) {
    const d = driftAt(h.turtle, t), n = round(b, d.x, d.y, d.r, 0.6, d.vx, d.vy);
    if (n > 20) ev.push(["shell", b.x, b.y, n]);
  }
  const g = h.guard;
  if (g && !b.flicked && awake(g, t) && Math.hypot(b.x - g.x, b.y - g.y) < g.reach) {
    const v = Math.max(150, Math.hypot(b.vx, b.vy) * 0.8), m = Math.hypot(g.back[0], g.back[1]);
    b.vx = g.back[0] / m * v; b.vy = g.back[1] / m * v; b.flicked = 0.6;
    ev.push(["flick", b.x, b.y]);
  }
  if (h.pad && b.onPad) {
    const p = driftAt(h.pad, t), dx = b.x - p.x, dy = b.y - p.y, d = Math.hypot(dx, dy), m = p.r - R - 1;
    if (d > m) {
      const nx = dx / d, ny = dy / d;
      b.x = p.x + nx * m; b.y = p.y + ny * m;
      const vn = (b.vx - p.vx) * nx + (b.vy - p.vy) * ny;
      if (vn > 0) { b.vx -= 1.4 * vn * nx; b.vy -= 1.4 * vn * ny; if (vn > 20) ev.push(["rim", b.x, b.y, vn]); }
    }
  }
  if (h.ramp && !b.onPad && py >= h.ramp.y && b.y < h.ramp.y && b.vy < 0 && b.x > h.ramp.x0 && b.x < h.ramp.x1) {
    // A hard hit flies further but not by as much, so the pad is a fair target.
    const v = Math.hypot(b.vx, b.vy);
    b.air = b.flight = (h.ramp.reach[0] + h.ramp.reach[1] * v) / v; ev.push(["launch", b.x, b.y]); return;
  }
  if (h.warp && !b.warped && Math.hypot(b.x - h.warp.from[0], b.y - h.warp.from[1]) < h.warp.from[2]) {
    const v = Math.hypot(b.vx, b.vy) * 0.9;
    ev.push(["warp", b.x, b.y]);
    b.x = h.warp.to[0]; b.y = h.warp.to[1]; b.vx = h.warp.dir[0] * v; b.vy = h.warp.dir[1] * v; b.warped = 0.4;
    ev.push(["out", b.x, b.y]);
  }
  if (h.button && !s.open && Math.hypot(b.x - h.button[0], b.y - h.button[1]) < h.button[2]) { s.open = true; ev.push(["open", h.button[0], h.button[1]]); }

  sp = Math.hypot(b.vx, b.vy);
  const cd2 = Math.hypot(cup.x - b.x, cup.y - b.y);
  if (cd2 < CUP - 1) {
    if (sp < SINK) { b.moving = false; b.vx = b.vy = 0; ev.push(["sink", cup.x, cup.y]); return; }
    if (!b.lip) {
      // Too fast to drop, so it catches the far rim and kicks off it.
      const a = (Math.random() < 0.5 ? -1 : 1) * (0.35 + Math.random() * 0.3), c = Math.cos(a), sn = Math.sin(a);
      const vx = b.vx * 0.7, vy = b.vy * 0.7;
      b.vx = vx * c - vy * sn; b.vy = vx * sn + vy * c; b.lip = true;
      ev.push(["lip", cup.x, cup.y]);
    }
  } else if (cd2 > CUP + 2) b.lip = false;

  const over = !b.onPad && (h.water || []).some((z) => inside(z, b.x, b.y)) && !(h.bridges || []).some((z) => inside(z, b.x, b.y));
  const pad = over ? (h.lilies || []).findIndex(([x, y, r]) => Math.hypot(b.x - x, b.y - y) < r) : -1;
  if ((over && pad < 0) || b.x < 0 || b.x > 200 || b.y < 0 || b.y > 320) { b.moving = false; b.vx = b.vy = 0; ev.push(["water", b.x, b.y]); return; }
  // A lily holds a rolling ball up but sinks under one that stops on it.
  if (pad >= 0 && sp < STOP) { s.sunk = [pad, t]; b.moving = false; b.vx = b.vy = 0; ev.push(["water", b.x, b.y]); return; }
  if (sp < STOP && !flow) { b.vx = b.vy = 0; b.moving = false; ev.push(["rest", b.x, b.y]); return; }
  if (b.rolled > 12) { b.vx = b.vy = 0; b.moving = false; ev.push(pad >= 0 ? ["water", b.x, b.y] : ["rest", b.x, b.y]); }
}

// Back to where the stroke was played from, after a splash.
export function putBack(h, b, t) {
  const f = b.from;
  b.x = f.x; b.y = f.y; b.onPad = f.onPad; b.air = 0; b.moving = false; b.vx = b.vy = 0;
  if (f.onPad && f.padAt) { const p = driftAt(h.pad, t); b.x += p.x - f.padAt.x; b.y += p.y - f.padAt.y; }
}

// Where a straight line from the ball meets something first, for the aim.
export function firstHit(h, s, b, a, most, t) {
  const dx = Math.cos(a), dy = Math.sin(a), d = h.drift && driftAt(h.drift, t), tu = h.turtle && driftAt(h.turtle, t);
  const walls = wallsAt(h, s, t).concat((h.oneway || []).filter((w) => against(w, { vx: dx, vy: dy })));
  for (let l = 2; l < most; l += 2) {
    const x = b.x + dx * l, y = b.y + dy * l;
    for (const w of walls) {
      const ex = w[2] - w[0], ey = w[3] - w[1], L = ex * ex + ey * ey;
      const u = L ? Math.max(0, Math.min(1, ((x - w[0]) * ex + (y - w[1]) * ey) / L)) : 0;
      if (Math.hypot(x - w[0] - ex * u, y - w[1] - ey * u) < R + LOG) return l;
    }
    for (const [cx, cy, r] of h.bumpers || []) if (Math.hypot(x - cx, y - cy) < r + R) return l;
    if (d && Math.hypot(x - d.x, y - d.y) < d.r + R) return l;
    if (tu && Math.hypot(x - tu.x, y - tu.y) < tu.r + R) return l;
    if (h.pad && b.onPad) { const p = driftAt(h.pad, t); if (Math.hypot(x - p.x, y - p.y) > p.r - R - 1) return l; }
  }
  return most;
}

// Points for a hole, ten at par, three more a stroke under and two less a
// stroke over, never under two once it's in.
export function pointsFor(strokes, par) { return Math.max(2, 10 + (strokes < par ? 3 : 2) * (par - strokes)); }

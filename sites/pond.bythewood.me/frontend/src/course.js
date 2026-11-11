// Pad putt's nine holes and how the ball rolls on them. Nothing in here
// touches the page, so a script can play every hole through without one.
export const R = 3, CUP = 5, CAP = 6, VMAX = 420, PULL = 56, DT = 1 / 240;
const K = 0.9, C = 30, STOP = 6, WALL = 0.7, LOG = 3, SINK = 170, PULLIN = 380;

function box(x0, y0, x1, y1) { return [[x0, y0, x1, y0], [x1, y0, x1, y1], [x1, y1, x0, y1], [x0, y1, x0, y0]]; }

// Each hole adds one thing, and the last few mix them. Walls are logs from
// end to end, zones are [x0, y0, x1, y1] and a push is pixels a second squared.
export const HOLES = [
  { name: "BANK SHOT", par: 2, tee: [60, 282], cup: [60, 68], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300), [30, 150, 124, 150]] },
  { name: "MOSS SLOPE", par: 2, tee: [100, 284], cup: [70, 62], ground: [[40, 30, 160, 300]],
    walls: [...box(40, 30, 160, 300), [118, 96, 160, 96]],
    slopes: [{ at: [40, 116, 160, 226], push: [80, 0] }] },
  { name: "MUSHROOMS", par: 2, tee: [100, 284], cup: [100, 60], ground: [[25, 30, 175, 300]],
    walls: [...box(25, 30, 175, 300), [25, 250, 50, 276], [175, 250, 150, 276]],
    bumpers: [[100, 172, 9], [64, 136, 8], [136, 136, 8], [100, 104, 8]] },
  { name: "WATERWHEEL", par: 2, tee: [100, 284], cup: [100, 66], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300), [30, 166, 86, 166], [114, 166, 170, 166]],
    wheel: { x: 100, y: 166, half: 14, n: 3, turn: 3.2 } },
  { name: "THE POND", par: 3, tee: [50, 282], cup: [146, 60], ground: [[25, 30, 175, 300]],
    walls: [...box(25, 30, 175, 300), [150, 210, 175, 240]],
    water: [[25, 30, 115, 160]],
    drift: { x: 70, y: 160, r: 11, dx: 34, dy: 0, period: 7 } },
  { name: "THE STREAM", par: 2, tee: [80, 284], cup: [122, 60], ground: [[30, 30, 170, 300]],
    walls: [...box(30, 30, 170, 300)],
    streams: [{ at: [30, 140, 170, 182], push: [230, 0] }],
    water: [[148, 140, 170, 182]],
    bumpers: [[70, 96, 8]] },
  { name: "HOLLOW LOGS", par: 2, tee: [100, 284], cup: [70, 62], ground: [[30, 30, 170, 168], [30, 202, 170, 300]],
    walls: [...box(30, 30, 170, 168), ...box(30, 202, 170, 300)],
    slopes: [{ at: [30, 84, 170, 126], push: [-50, 0] }],
    warp: { from: [150, 208, 8], to: [56, 160], dir: [0, -1] } },
  { name: "REED GATE", par: 3, tee: [100, 284], cup: [100, 62], ground: [[25, 30, 175, 300]],
    walls: [...box(25, 30, 175, 300), [25, 106, 82, 106], [118, 106, 175, 106]],
    gate: [82, 106, 118, 106], button: [152, 152, 7],
    bumpers: [[58, 196, 8]] },
  { name: "DRIFTING PAD", par: 3, tee: [100, 284], cup: [0, -6], ground: [[30, 190, 170, 300]],
    walls: [[30, 300, 170, 300], [30, 190, 30, 300], [170, 190, 170, 300], [30, 190, 85, 190], [115, 190, 170, 190]],
    water: [[30, 30, 170, 190]],
    slopes: [{ at: [85, 190, 115, 226], push: [0, 60] }],
    ramp: { x0: 85, x1: 115, y: 190, reach: [48, 0.2] },
    bumpers: [[58, 244, 8]],
    pad: { x: 100, y: 104, r: 28, dx: 40, period: 12 } },
];

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
  const b = { x: h.tee[0], y: h.tee[1], vx: 0, vy: 0, air: 0, onPad: false, lip: false, warped: 0, moving: false, rolled: 0 };
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
  if (h.pad && b.onPad) { const p = driftAt(h.pad, t); b.x += p.vx * dt; b.y += p.vy * dt; }
  if (!b.moving) return;
  if (b.air > 0) {
    b.x += b.vx * dt; b.y += b.vy * dt; b.air -= dt;
    if (b.air <= 0) {
      const p = h.pad && driftAt(h.pad, t);
      if (p && Math.hypot(b.x - p.x, b.y - p.y) < p.r - R) { b.onPad = true; b.vx *= 0.35; b.vy *= 0.35; ev.push(["land", b.x, b.y]); }
    }
    return;
  }
  let sp = Math.hypot(b.vx, b.vy);
  const flow = (h.streams || []).find((z) => inside(z.at, b.x, b.y));
  if (flow) { b.vx += flow.push[0] * dt; b.vy += flow.push[1] * dt; }
  if (sp > STOP) (h.slopes || []).forEach((z) => { if (inside(z.at, b.x, b.y)) { b.vx += z.push[0] * dt; b.vy += z.push[1] * dt; } });
  const cup = cupAt(h, t), cd = Math.hypot(cup.x - b.x, cup.y - b.y);
  if (cd < CUP + 4 && cd > 0.01 && sp < SINK * 1.3) {
    const f = PULLIN * (1 - cd / (CUP + 4)) * dt;
    b.vx += (cup.x - b.x) / cd * f; b.vy += (cup.y - b.y) / cd * f;
  }
  sp = Math.hypot(b.vx, b.vy);
  const slow = Math.max(0, sp - (K * sp + C) * dt);
  if (sp > 0) { b.vx *= slow / sp; b.vy *= slow / sp; }
  const py = b.y;
  b.x += b.vx * dt; b.y += b.vy * dt;

  wallsAt(h, s, t).forEach((w) => { const n = seg(b, w, w === h.gate ? 0.4 : WALL); if (n > 20) ev.push(["wall", b.x, b.y, n]); });
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

  const wet = !b.onPad && (h.water || []).some((z) => inside(z, b.x, b.y));
  if (wet || b.x < 0 || b.x > 200 || b.y < 0 || b.y > 320) { b.moving = false; b.vx = b.vy = 0; ev.push(["water", b.x, b.y]); return; }
  if (sp < STOP && !flow) { b.vx = b.vy = 0; b.moving = false; ev.push(["rest", b.x, b.y]); return; }
  if (b.rolled > 12) { b.vx = b.vy = 0; b.moving = false; ev.push(["rest", b.x, b.y]); }
}

// Back to where the stroke was played from, after a splash.
export function putBack(h, b, t) {
  const f = b.from;
  b.x = f.x; b.y = f.y; b.onPad = f.onPad; b.air = 0; b.moving = false; b.vx = b.vy = 0;
  if (f.onPad && f.padAt) { const p = driftAt(h.pad, t); b.x += p.x - f.padAt.x; b.y += p.y - f.padAt.y; }
}

// Where a straight line from the ball meets something first, for the aim.
export function firstHit(h, s, b, a, most, t) {
  const walls = wallsAt(h, s, t), dx = Math.cos(a), dy = Math.sin(a), d = h.drift && driftAt(h.drift, t);
  for (let l = 2; l < most; l += 2) {
    const x = b.x + dx * l, y = b.y + dy * l;
    for (const w of walls) {
      const ex = w[2] - w[0], ey = w[3] - w[1], L = ex * ex + ey * ey;
      const u = L ? Math.max(0, Math.min(1, ((x - w[0]) * ex + (y - w[1]) * ey) / L)) : 0;
      if (Math.hypot(x - w[0] - ex * u, y - w[1] - ey * u) < R + LOG) return l;
    }
    for (const [cx, cy, r] of h.bumpers || []) if (Math.hypot(x - cx, y - cy) < r + R) return l;
    if (d && Math.hypot(x - d.x, y - d.y) < d.r + R) return l;
    if (h.pad && b.onPad) { const p = driftAt(h.pad, t); if (Math.hypot(x - p.x, y - p.y) > p.r - R - 1) return l; }
  }
  return most;
}

// Points for a hole, ten at par, three more a stroke under and two less a
// stroke over, never under two once it's in.
export function pointsFor(strokes, par) { return Math.max(2, 10 + (strokes < par ? 3 : 2) * (par - strokes)); }

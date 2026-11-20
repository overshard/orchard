// The pebble pusher's table, every pebble on it and the shelf that shoves
// them. Nothing in here touches the page, so a script can run it with bun.
// It's seen from above, so there's no gravity, only the floor dragging every
// pebble to a stop and the shelf pushing the pile toward the lip.
export const W = 200, H = 320, FX0 = 24, FX1 = 176, WALL = 80, LIP = 170, GUT = 134;
export const SHELF = 103, REACH = 8, CYCLE = 0.7, CHUTE = 64, DROPY = 100, TRAY = 206;
export const HOLE = { x: 100, y: 100, r: 3 }, JAR = { x: 172, y: 250 };
export const R = 5, BIG = 7, STEP = 1 / 240, FLOOR = 40, KEEP = 300;
const ITER = 10, PUSH = 4, SLOP = 0.3, FAST = 260, BOUNCE = 0.2, REST = 30, MU = 0.3, CREEP = 8, HUSH = 10;
// The floor's drag, a steady pull to a stop, so a pebble moves while it's
// pushed and not much after.
const DRAG = 260;
// The server takes 2.5 drops a second at most, so a hand is held under that.
export const GAP = 0.45;
// How many of a new table's pebbles ride the shelf.
const SHELVED = 20;
const FALL = 0.3, RAIN = 0.6, PERCH = 0.35, KICK = 36, CELL = 13;
export const SFMIN = SHELF - REACH, SFMAX = SHELF + REACH;

let ids = 0;

function body(x, y, kind, top) {
  const r = kind ? BIG : R;
  return { id: ++ids, x, y, vx: 0, vy: 0, r, m: kind ? r * r * 1.6 : r * r, kind, top: !!top, perch: 0, dx: Math.round(x), dy: Math.round(y), touch: false };
}

// The back wall and sides never move, and the shelf's face moves with the shelf.
const STILL = { vx: 0, vy: 0, wall: true }, FACE = { vx: 0, vy: 0, wall: true };

export function world(rand) {
  const w = {
    rand: rand || Math.random, t: 0, acc: 0, bodies: [], air: [], events: [], warm: new Map(),
    sf: SFMIN, vsf: 0, spill: 0, on: 0, wait: 0, ready: new Set(), floor: FLOOR, room: Infinity,
    counts: { dropped: 0, won: 0, spilled: 0, back: 0 }, claimed: [], shoved: 0, cycle: 0,
  };
  shelf(w);
  return w;
}

// The shelf starts all the way back, so nothing loaded onto the field is
// inside it.
function shelf(w) {
  const a = 2 * Math.PI * CYCLE * w.t - Math.PI / 2;
  w.sf = SHELF + REACH * Math.sin(a);
  w.vsf = REACH * 2 * Math.PI * CYCLE * Math.cos(a);
  FACE.vy = w.vsf;
  const c = Math.floor(w.t * CYCLE + 0.25);
  if (c !== w.cycle) {
    w.cycle = c;
    if (w.shoved > 40) w.events.push({ k: "shove", v: w.shoved });
    w.shoved = 0;
  }
}

// fresh lays a new table's pile as if it had been pushed for a while, a row
// and a half riding the shelf and the rest a bank toward the front that
// thins out toward the shelf.
export function fresh(w, n) {
  const dx = 2 * R + 0.6, dy = dx * 0.87, spots = [];
  let rode = 0;
  for (let row = 0, y = WALL + R + 0.5; row < 2 && rode < SHELVED && rode < n; row++, y += dy) {
    for (let x = FX0 + R + 1 + (row % 2) * dx / 2; x < FX1 - R && rode < SHELVED && rode < n; x += dx * (row ? 2 : 1)) {
      w.bodies.push(body(x + (w.rand() - 0.5) * 0.8, y, 0, true));
      rode++;
    }
  }
  for (let row = 0, y = LIP - R - 1; y > SFMAX + R; row++, y -= dy) {
    for (let x = FX0 + R + 1 + (row % 2) * dx / 2; x < FX1 - R; x += dx) {
      const front = (y - SFMAX) / (LIP - SFMAX);
      spots.push({ x: x + (w.rand() - 0.5) * 0.8, y: y + (w.rand() - 0.5) * 0.8, k: front * front + w.rand() * 0.55 });
    }
  }
  spots.sort((a, b) => b.k - a.k);
  for (let i = 0; i < n - rode && i < spots.length; i++) w.bodies.push(body(spots[i].x, spots[i].y, 0, false));
  w.on = w.bodies.length;
}

// A layout is three bytes a body, across and down the table and its kind,
// with the top bit set for one riding on the shelf.
export function pack(w) {
  const all = w.bodies.map((b) => [b.x, b.y, b.kind, b.top]).concat(w.air.map((a) => [a.x, a.y, a.kind, a.top]));
  const out = new Uint8Array(all.length * 3);
  all.forEach(([x, y, k, top], i) => {
    out[i * 3] = Math.max(0, Math.min(255, Math.round((x - FX0) / (FX1 - FX0) * 255)));
    out[i * 3 + 1] = Math.max(0, Math.min(255, Math.round((y - WALL) / (LIP - WALL) * 255)));
    out[i * 3 + 2] = (k & 0x7f) | (top ? 0x80 : 0);
  });
  return out;
}

export function unpack(w, bytes) {
  w.bodies = []; w.air = []; w.on = 0;
  for (let i = 0; i + 2 < bytes.length; i += 3) {
    const x = FX0 + bytes[i] / 255 * (FX1 - FX0), y = WALL + bytes[i + 1] / 255 * (LIP - WALL);
    const k = bytes[i + 2] & 0x7f, top = (bytes[i + 2] & 0x80) !== 0;
    const b = body(x, top ? Math.min(y, SFMIN - 1) : Math.max(y, SFMIN + (k ? BIG : R)), k, top);
    w.bodies.push(b);
    if (!k) w.on++;
  }
}

// prizes says which prizes are on the table, by id.
export function prizes(w) {
  return w.bodies.filter((b) => b.kind).map((b) => b.kind).concat(w.air.filter((a) => a.kind).map((a) => a.kind));
}

export function aimed(x) { return Math.max(FX0 + R + 1, Math.min(FX1 - R - 1, x)); }

// drop lets a pebble go down the chute at x, a little off where it was aimed.
export function drop(w, x) {
  if (w.wait > 0) return false;
  x = aimed(x);
  w.air.push({ x: aimed(x + (w.rand() - 0.5) * 2.4), y: DROPY + (w.rand() - 0.5) * 6, fx: x, fy: CHUTE, t: 0, dur: FALL, kind: 0, src: "drop", top: false });
  w.counts.dropped++; w.on++; w.wait = GAP;
  return true;
}

// place puts a prize down on the back of the pile from the chute.
export function place(w, id) {
  const x = FX0 + 40 + w.rand() * (FX1 - FX0 - 80);
  w.air.push({ x, y: SFMAX + BIG + 1, fx: x, fy: CHUTE, t: 0, dur: FALL * 1.4, kind: id, src: "prize", top: false });
}

// shower tips the spill jar back over the field, every pebble in it at once.
function shower(w) {
  const n = w.spill;
  w.spill = 0; w.counts.back += n; w.on += n;
  for (let i = 0; i < n; i++) {
    w.air.push({ x: FX0 + 10 + w.rand() * (FX1 - FX0 - 20), y: SFMAX + 8 + w.rand() * 50, fx: JAR.x, fy: JAR.y - 20, t: -i * 0.04, dur: RAIN, kind: 0, src: "shower", top: false });
  }
  w.events.push({ k: "shower", n });
}

// A pebble stays on the lip while the table is down to its floor or the page
// has won all a save can take, and a prize until it's sat long enough.
function canFall(w, b) { return b.kind ? w.ready.has(b.kind) : w.on > w.floor && w.room > 0; }

export function run(w, dt) {
  w.wait = Math.max(0, w.wait - dt);
  w.acc += dt;
  while (w.acc >= STEP) { w.acc -= STEP; tick(w); }
}

function tick(w) {
  const h = STEP;
  w.t += h;
  shelf(w);
  for (const a of w.air) { a.t += h; if (a.t >= a.dur) land(w, a); }
  w.air = w.air.filter((a) => !a.done);
  const bs = w.bodies;
  for (const b of bs) {
    // The floor drags a pebble toward standing still, and the shelf's top
    // toward riding along with it.
    const rv = b.top ? w.vsf : 0, ux = b.vx, uy = b.vy - rv, u = Math.hypot(ux, uy), d = DRAG * h;
    if (u <= d) { b.vx = 0; b.vy = rv; } else { b.vx -= ux / u * d; b.vy -= uy / u * d; }
    b.perch = Math.max(0, b.perch - h);
    b.touch = false;
  }
  const near = pairs(bs), cs = contacts(w, near), warm = new Map();
  for (const c of cs) push(c, c.pn, c.pt);
  for (let it = 0; it < ITER; it++) for (const c of cs) solve(c);
  for (const c of cs) warm.set(c.key, [c.pn, c.pt]);
  w.warm = warm;
  for (const b of bs) {
    const v = Math.hypot(b.vx, b.vy);
    if (v > FAST) { b.vx *= FAST / v; b.vy *= FAST / v; }
    // Something barely moving in a pile is let come to rest, so it can't creep.
    if (b.touch && !b.top && v < CREEP) { const k = 1 - HUSH * h; b.vx *= k; b.vy *= k; }
    b.x += b.vx * h; b.y += b.vy * h;
  }
  for (let it = 0; it < PUSH; it++) {
    for (const [a, b] of near) apart(a, b);
    for (const b of bs) walls(w, b);
  }
  edges(w);
}

// Bodies close enough to touch, found off a grid so a big pile stays cheap.
function pairs(bs) {
  const grid = new Map(), out = [];
  for (const b of bs) {
    const k = Math.floor(b.x / CELL) + Math.floor(b.y / CELL) * 64;
    let c = grid.get(k);
    if (!c) grid.set(k, c = []);
    c.push(b);
  }
  for (const b of bs) {
    const cx = Math.floor(b.x / CELL), cy = Math.floor(b.y / CELL);
    for (let oy = -1; oy <= 1; oy++) for (let ox = -1; ox <= 1; ox++) {
      const c = grid.get(cx + ox + (cy + oy) * 64);
      if (!c) continue;
      for (const o of c) {
        if (o.id <= b.id || o.top !== b.top) continue;
        const dx = o.x - b.x, dy = o.y - b.y, m = b.r + o.r + 0.5;
        if (dx * dx + dy * dy < m * m) out.push([b, o]);
      }
    }
  }
  return out;
}

function contacts(w, near) {
  const cs = [];
  for (const [a, b] of near) {
    let dx = b.x - a.x, dy = b.y - a.y, d = Math.hypot(dx, dy);
    if (a.r + b.r - d < -0.05) continue;
    if (d < 1e-6) { dx = 0; dy = 1; d = 1; }
    cs.push(touch(a, b, dx / d, dy / d, w, a.id * 65536 + b.id));
  }
  for (const b of w.bodies) {
    const walled = b.top || b.y < GUT, key = -b.id * 8;
    if (walled && b.x - b.r < FX0 + 0.05) cs.push(touch(STILL, b, 1, 0, w, key));
    if (walled && b.x + b.r > FX1 - 0.05) cs.push(touch(STILL, b, -1, 0, w, key - 1));
    if (b.top && b.y - b.r < WALL + 0.05) cs.push(touch(STILL, b, 0, 1, w, key - 2));
    if (!b.top && b.y - b.r < w.sf + 0.05) cs.push(touch(FACE, b, 0, 1, w, key - 3));
    if (!b.top && b.y > LIP - 1 && !canFall(w, b)) cs.push(touch(STILL, b, 0, -1, w, key - 4));
  }
  return cs;
}

function touch(a, b, nx, ny, w, key) {
  const vn = (b.vx - a.vx) * nx + (b.vy - a.vy) * ny;
  // Below REST a touch doesn't bounce, so a pile at rest stays at rest.
  const target = vn < -REST ? -BOUNCE * vn : 0;
  b.touch = true;
  if (!a.wall) a.touch = true;
  // A touch that was there last step starts from the push it ended on, so a
  // long row pushed from one end moves as one.
  const c = { a, b, nx, ny, target, key, pn: 0, pt: 0, face: a === FACE }, was = w.warm.get(key);
  if (was) { c.pn = was[0]; c.pt = was[1]; }
  c.w = w;
  return c;
}

function solve(c) {
  const { a, b, nx, ny } = c, tx = -ny, ty = nx, ia = a.wall ? 0 : 1 / a.m, ib = 1 / b.m;
  let rx = b.vx - a.vx, ry = b.vy - a.vy;
  const j = (c.target - (rx * nx + ry * ny)) / (ia + ib), p0 = c.pn;
  c.pn = Math.max(0, p0 + j);
  push(c, c.pn - p0, 0);
  if (c.face) c.w.shoved += (c.pn - p0) * ib;
  rx = b.vx - a.vx; ry = b.vy - a.vy;
  const vt = rx * tx + ry * ty, lim = MU * c.pn, t0 = c.pt;
  c.pt = Math.max(-lim, Math.min(lim, t0 - vt / (ia + ib)));
  push(c, 0, c.pt - t0);
}

function push(c, j, k) {
  const { a, b, nx, ny } = c, px = nx * j - ny * k, py = ny * j + nx * k;
  if (!a.wall) { a.vx -= px / a.m; a.vy -= py / a.m; }
  b.vx += px / b.m; b.vy += py / b.m;
}

// What overlap is left after the step is pushed out, the light one moving
// most. One just landed on another sits on it a moment and slides off.
function apart(a, b) {
  const dx = b.x - a.x, dy = b.y - a.y, d = Math.hypot(dx, dy);
  const pen = a.r + b.r - d - SLOP;
  if (pen <= 0 || d < 1e-6) return;
  const soft = a.perch > 0 || b.perch > 0 ? 0.12 : 0.5;
  const ia = 1 / a.m, ib = 1 / b.m, k = pen * soft / (ia + ib) / d;
  a.x -= dx * k * ia; a.y -= dy * k * ia; b.x += dx * k * ib; b.y += dy * k * ib;
}

function walls(w, b) {
  if (b.top || b.y < GUT) b.x = Math.max(FX0 + b.r, Math.min(FX1 - b.r, b.x));
  if (b.top) b.y = Math.max(WALL + b.r, b.y);
  else b.y = Math.max(w.sf + b.r, b.y);
  if (!b.top && b.y > LIP - 0.5 && !canFall(w, b)) b.y = LIP - 0.5;
}

// A pebble whose middle is past the shelf's front edge drops onto the field,
// past the lip falls into the tray and past a side into the gutter.
function edges(w) {
  let gone = false;
  for (const b of w.bodies) {
    if (b.top && b.y > w.sf) {
      b.top = false; b.y = w.sf + b.r; b.vy = Math.max(b.vy, w.vsf); b.perch = PERCH;
      w.events.push({ k: "scrape", x: b.x, y: b.y });
      continue;
    }
    if (b.top || !canFall(w, b)) continue;
    if (b.y > LIP) {
      b.gone = gone = true;
      if (b.kind) w.claimed.push(b.kind); else { w.counts.won++; w.on--; w.room--; }
      w.events.push({ k: "won", x: b.x, y: b.y, kind: b.kind, vx: b.vx });
    } else if (b.y >= GUT && (b.x < FX0 || b.x > FX1)) {
      b.gone = gone = true;
      if (b.kind) w.claimed.push(b.kind); else { w.counts.spilled++; w.spill++; w.on--; }
      w.events.push({ k: "gutter", x: b.x, y: b.y, kind: b.kind, right: b.x > FX1 });
    }
  }
  if (gone) w.bodies = w.bodies.filter((b) => !b.gone);
}

// Where a falling pebble comes down. A drop timed to the shelf being back
// can find the lily pad hole, which tips the spill jar out over the field.
function land(w, a) {
  a.done = true;
  if (a.src === "drop" && Math.hypot(a.x - HOLE.x, a.y - HOLE.y) < HOLE.r && w.sf < HOLE.y - 2) {
    w.on--; w.counts.spilled++; w.spill++;
    w.events.push({ k: "hole", x: a.x, y: a.y });
    shower(w);
    return;
  }
  const top = a.y < w.sf - 1;
  const b = body(a.x, top ? Math.max(a.y, WALL + R) : Math.max(a.y, w.sf + (a.kind ? BIG : R)), a.kind, top);
  b.perch = PERCH;
  if (top) b.vy = w.vsf;
  // What it lands on is knocked aside a little.
  for (const o of w.bodies) {
    if (o.top !== top) continue;
    const dx = o.x - b.x, dy = o.y - b.y, d = Math.hypot(dx, dy), reach = b.r + o.r + 3;
    if (d >= reach || d < 1e-6) continue;
    const s = KICK * (1 - d / reach) * (a.src === "shower" ? 1.5 : a.kind ? 1.3 : 1), turn = (w.rand() - 0.5) * 0.3;
    const c = Math.cos(turn), sn = Math.sin(turn), nx = dx / d, ny = dy / d;
    o.vx += (nx * c - ny * sn) * s; o.vy += (nx * sn + ny * c) * s;
  }
  w.bodies.push(b);
  w.events.push({ k: "land", x: b.x, y: b.y, src: a.src, top, kind: a.kind });
}

// Where a pebble in the air is drawn, and how high, from 0 on the table.
export function flight(a) {
  const u = Math.max(0, Math.min(1, a.t / a.dur)), e = u * u;
  const lift = a.src === "shower" ? 70 : 14;
  return { x: a.fx + (a.x - a.fx) * (a.src === "shower" ? u : e), y: a.fy + (a.y - a.fy) * (a.src === "shower" ? u : e), z: a.src === "shower" ? Math.sin(u * Math.PI) * lift : (1 - e) * lift, u };
}

// What's sitting still. A body is only redrawn once it's moved most of a
// pixel, so a pile at rest holds still on screen too.
export function settle(w) {
  let moving = 0;
  for (const b of w.bodies) {
    if (Math.hypot(b.vx, b.vy - (b.top ? w.vsf : 0)) > 6) moving++;
    if (Math.abs(b.x - b.dx) > 0.6) b.dx = Math.round(b.x);
    if (Math.abs(b.y - b.dy) > 0.6) b.dy = Math.round(b.y);
  }
  return moving;
}

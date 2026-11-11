// The spawn jar's world, every body in it and how they knock about. Nothing in
// here touches the page, so a script can drop a whole run through it with bun.
export const W = 200, H = 320, JL = 22, JR = 178, FLOOR = 300, LINE = 88, DROP = 58;
export const R = [7, 9, 12, 16, 21, 27, 36, 47], TOP = R.length - 1;
export const PAYS = [0, 2, 4, 8, 15, 25, 40, 60], POP = 300;
export const NAMES = ["EGG", "TADPOLE", "FROGLET", "FROG", "BULLFROG", "ELDER", "GREAT FROG", "POND KING"];
export const STEP = 1 / 240, WAIT = 0.5, FULL = 1.5;
const DEALT = 3, GRAV = 480, ITER = 10, PUSH = 4, SLOP = 0.3, FAST = 600;
const BOUNCE = 0.3, WALL = 0.15, REST = 30, MU = 0.4, WALLMU = 0.25, ROLL = 1.5, CREEP = 8, HUSH = 10;
const GROW = 0.5, START = 0.55, BACK = 2.5, SETTLE = 1, STILL = 60;

// A merge grows in from small and swells past its size before it settles,
// so it shoves whatever is round it.
function size(t) { const u = t - 1; return START + (1 - START) * (1 + (BACK + 1) * u * u * u + BACK * u * u); }

let ids = 0;

function body(x, y, s) {
  const r = R[s];
  return { id: ++ids, x, y, vx: 0, vy: 0, a: 0, w: 0, s, r, m: r * r, grow: 1, dr: 0, age: 0, dx: Math.round(x), dy: Math.round(y), da: 0 };
}

// Two of each of the smallest few in a bag, shuffled and dealt till it's empty.
function deal(w) {
  if (!w.bag.length) {
    for (let s = 0; s < DEALT; s++) w.bag.push(s, s);
    for (let i = w.bag.length - 1; i > 0; i--) { const j = Math.floor(w.rand() * (i + 1)); [w.bag[i], w.bag[j]] = [w.bag[j], w.bag[i]]; }
  }
  return w.bag.pop();
}

export function world(rand) {
  const w = { rand: rand || Math.random, bodies: [], bag: [], wait: 0, acc: 0, over: 0, full: false, score: 0, made: R.map(() => 0), pops: 0, best: 0, drops: 0, events: [], warm: new Map() };
  w.hand = deal(w); w.next = deal(w);
  return w;
}

export function aimed(s, x) { return Math.max(JL + R[s], Math.min(JR - R[s], x)); }

export function drop(w, x) {
  if (w.full || w.wait > 0) return false;
  w.bodies.push(body(aimed(w.hand, x), DROP, w.hand));
  w.hand = w.next; w.next = deal(w); w.wait = WAIT; w.drops++;
  return true;
}

// run moves the jar on by dt, and says the jar is full once something has sat
// over the line for FULL seconds.
export function run(w, dt) {
  w.wait = Math.max(0, w.wait - dt);
  if (w.full) return;
  w.acc += dt;
  while (w.acc >= STEP) { w.acc -= STEP; tick(w); }
  let high = false;
  for (const b of w.bodies) if (b.age > SETTLE && b.y - b.r < LINE && Math.hypot(b.vx, b.vy) < STILL) high = true;
  w.over = high ? w.over + dt : Math.max(0, w.over - dt * 2);
  if (w.over > FULL) w.full = true;
}

function tick(w) {
  const bs = w.bodies, h = STEP;
  for (const b of bs) {
    b.age += h;
    if (b.grow < 1) {
      const r0 = b.r;
      b.grow = Math.min(1, b.grow + h / GROW);
      b.r = R[b.s] * size(b.grow); b.m = b.r * b.r; b.dr = (b.r - r0) / h;
    } else b.dr = 0;
    b.vy += GRAV * h;
    b.touch = false;
  }
  const pairs = [], cs = contacts(w, pairs), warm = new Map();
  for (const c of cs) push(c, c.pn, c.pt);
  for (let it = 0; it < ITER; it++) for (const c of cs) solve(c);
  for (const c of cs) warm.set(c.key, [c.pn, c.pt]);
  w.warm = warm;
  for (const b of bs) {
    const v = Math.hypot(b.vx, b.vy);
    if (v > FAST) { b.vx *= FAST / v; b.vy *= FAST / v; }
    if (b.touch) b.w *= 1 - ROLL * h;
    // Something barely moving in a pile is let come to rest, so it can't creep.
    if (b.touch && v < CREEP) { const k = 1 - HUSH * h; b.vx *= k; b.vy *= k; b.w *= k; }
    b.x += b.vx * h; b.y += b.vy * h; b.a += b.w * h;
  }
  for (let it = 0; it < PUSH; it++) {
    for (let i = 0; i < bs.length; i++) for (let j = i + 1; j < bs.length; j++) apart(bs[i], bs[j]);
    for (const b of bs) walls(b);
  }
  merges(w, pairs);
}

// Every touch this step, with what each one should bounce back at. Two of a
// stage touching are noted to merge once the step is over.
function contacts(w, pairs) {
  const bs = w.bodies, cs = [];
  for (let i = 0; i < bs.length; i++) for (let j = i + 1; j < bs.length; j++) {
    const a = bs[i], b = bs[j];
    let dx = b.x - a.x, dy = b.y - a.y, d = Math.hypot(dx, dy);
    const pen = a.r + b.r - d;
    if (pen < -0.05) continue;
    if (a.s === b.s) pairs.push([a, b]);
    if (d < 1e-6) { dx = 0; dy = -1; d = 1; }
    cs.push(touch(a, b, dx / d, dy / d, BOUNCE, MU, w, a.id * 8192 + b.id));
  }
  for (const b of bs) {
    if (b.x - b.r < JL + 0.05) cs.push(touch(null, b, 1, 0, WALL, WALLMU, w, -b.id * 4));
    if (b.x + b.r > JR - 0.05) cs.push(touch(null, b, -1, 0, WALL, WALLMU, w, -b.id * 4 - 1));
    if (b.y + b.r > FLOOR - 0.05) cs.push(touch(null, b, 0, -1, WALL, WALLMU, w, -b.id * 4 - 2));
  }
  return cs;
}

function touch(a, b, nx, ny, e, mu, w, key) {
  const vn = (b.vx - (a ? a.vx : 0)) * nx + (b.vy - (a ? a.vy : 0)) * ny;
  // Below REST a touch doesn't bounce, so a pile at rest stays at rest.
  let target = vn < -REST ? -e * vn : 0;
  target = Math.max(target, (a ? a.dr : 0) + b.dr);
  if (vn < -110) w.events.push({ k: "knock", x: b.x - nx * b.r, y: b.y - ny * b.r, v: -vn });
  b.touch = true;
  if (a) a.touch = true;
  // A touch that was there last step starts from the push it ended on, so a
  // tall pile holds up without sinking and creeping.
  const c = { a, b, nx, ny, e, mu, target, key, pn: 0, pt: 0 }, was = w.warm.get(key);
  if (was) { c.pn = was[0]; c.pt = was[1]; }
  return c;
}

// One pass of a touch's push and its friction. Friction acts at the rim, so
// it spins a ball as well as slowing it, and that's what makes things roll.
function solve(c) {
  const { a, b, nx, ny } = c, tx = -ny, ty = nx, ia = a ? 1 / a.m : 0, ib = 1 / b.m;
  let rx = b.vx - (a ? a.vx : 0), ry = b.vy - (a ? a.vy : 0);
  let j = (c.target - (rx * nx + ry * ny)) / (ia + ib);
  const p0 = c.pn;
  c.pn = Math.max(0, p0 + j);
  push(c, c.pn - p0, 0);

  rx = b.vx - (a ? a.vx : 0); ry = b.vy - (a ? a.vy : 0);
  const vt = rx * tx + ry * ty - b.w * b.r - (a ? a.w * a.r : 0);
  const lim = c.mu * c.pn, t0 = c.pt;
  c.pt = Math.max(-lim, Math.min(lim, t0 - vt / (3 * ia + 3 * ib)));
  push(c, 0, c.pt - t0);
}

function push(c, j, k) {
  const { a, b, nx, ny } = c, ia = a ? 1 / a.m : 0, ib = 1 / b.m, px = nx * j - ny * k, py = ny * j + nx * k;
  if (a) { a.vx -= px * ia; a.vy -= py * ia; a.w -= 2 * k * ia / a.r; }
  b.vx += px * ib; b.vy += py * ib; b.w -= 2 * k * ib / b.r;
}

// What overlap is left after the step is pushed out, the light one moving most.
function apart(a, b) {
  const dx = b.x - a.x, dy = b.y - a.y, d = Math.hypot(dx, dy);
  const pen = a.r + b.r - d - SLOP;
  if (pen <= 0 || d < 1e-6) return;
  const ia = 1 / a.m, ib = 1 / b.m, k = pen * 0.5 / (ia + ib) / d;
  a.x -= dx * k * ia; a.y -= dy * k * ia; b.x += dx * k * ib; b.y += dy * k * ib;
}

function walls(b) {
  b.x = Math.max(JL + b.r, Math.min(JR - b.r, b.x));
  if (b.y + b.r > FLOOR) b.y = FLOOR - b.r;
}

// A body merges once at most in a step, so a row of three alike makes one
// of the next and leaves one over.
function merges(w, pairs) {
  let any = false;
  for (const [a, b] of pairs) {
    if (a.gone || b.gone) continue;
    a.gone = b.gone = true; any = true;
    const x = (a.x + b.x) / 2, y = (a.y + b.y) / 2;
    if (a.s === TOP) {
      w.score += POP; w.pops++;
      w.events.push({ k: "pop", x, y, s: TOP + 1 });
      continue;
    }
    const n = body(x, y, a.s + 1), m = a.m + b.m;
    n.vx = (a.vx * a.m + b.vx * b.m) / m; n.vy = (a.vy * a.m + b.vy * b.m) / m;
    n.grow = 0; n.r = R[n.s] * START; n.m = n.r * n.r;
    walls(n);
    w.bodies.push(n);
    w.score += PAYS[n.s]; w.made[n.s]++; w.best = Math.max(w.best, n.s);
    w.events.push({ k: "merge", x, y, s: n.s });
  }
  if (any) w.bodies = w.bodies.filter((b) => !b.gone);
}

// Where a dropped thing would first land, for the guide line.
export function landing(w, x) {
  let top = FLOOR;
  for (const b of w.bodies) { const dx = Math.abs(b.x - x); if (dx < b.r) top = Math.min(top, b.y - Math.sqrt(b.r * b.r - dx * dx)); }
  return top;
}

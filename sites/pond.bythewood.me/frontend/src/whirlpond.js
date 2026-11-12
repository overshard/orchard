// Whirlpool's pond, everything floating in it and the eddy going round it.
// Nothing in here touches the page, so a script can play a whole round with bun.
export const W = 200, H = 300, ROUND = 75, LAST = 10;
export const PW = 1300, PH = 1900, BANK = 70;
const R0 = 3, EAT = 0.8, GROW = 0.13, Z0 = 2, ZP = 0.62, SPEED = 130, LEASH = 34;
const SHED = 1, GRACE = 1.5, HARD = 0.8, NEAR = 1.25;

// Each band is a row of things about the same size, and the eddy reaches a
// band once it can take the smallest of them.
export const BANDS = [
  { name: "DUCKWEED", line: "SPECKS" },
  { name: "LEAVES", line: "LEAVES" },
  { name: "TWIGS", line: "TWIGS AND BUGS" },
  { name: "LILY PADS", line: "LILY PADS" },
  { name: "REEDS", line: "THE REEDS" },
  { name: "DUCKS", line: "DUCKS" },
  { name: "LOGS", line: "THE OLD LOG" },
  { name: "THE JETTY", line: "THE JETTY" },
];

// s is about half the thing's longest side. act is how it gets about, and a
// thing that's alive is only ever taken for a spin and paddles off after.
export const KINDS = [
  { k: "duckweed", name: "Duckweed", b: 0, s: 1.2, pts: 1, w: 6, act: "float" },
  { k: "petal", name: "A petal", b: 0, s: 1.5, pts: 1, w: 3, act: "float" },
  { k: "bubble", name: "A bubble", b: 0, s: 1.3, pts: 1, w: 3, act: "float" },
  { k: "fluff", name: "Seed fluff", b: 0, s: 1.7, pts: 1, w: 3, act: "drift" },
  { k: "flea", name: "A water flea", b: 0, s: 1.2, pts: 2, w: 2, act: "swim", alive: true },
  { k: "midge", name: "A midge", b: 0, s: 1.1, pts: 2, w: 2, act: "fly", alive: true },

  { k: "leaf", name: "An oak leaf", b: 1, s: 3.0, pts: 2, w: 5, act: "float" },
  { k: "willow", name: "A willow leaf", b: 1, s: 2.8, pts: 2, w: 4, act: "float" },
  { k: "feather", name: "A feather", b: 1, s: 3.4, pts: 2, w: 3, act: "drift" },
  { k: "acorn", name: "An acorn", b: 1, s: 2.6, pts: 2, w: 3, act: "float" },
  { k: "skater", name: "A pond skater", b: 1, s: 2.7, pts: 3, w: 2, act: "swim", alive: true },
  { k: "tadpole", name: "A tadpole", b: 1, s: 2.9, pts: 3, w: 2, act: "swim", alive: true },
  { k: "clover", name: "A four leaf clover", b: 1, s: 2.6, pts: 15, w: 0, act: "float", rare: true },

  { k: "twig", name: "A twig", b: 2, s: 5.6, pts: 3, w: 5, act: "float" },
  { k: "cone", name: "A pine cone", b: 2, s: 5.2, pts: 3, w: 3, act: "float" },
  { k: "cap", name: "A bottle cap", b: 2, s: 4.6, pts: 3, w: 2, act: "float" },
  { k: "snail", name: "A pond snail", b: 2, s: 4.8, pts: 4, w: 3, act: "drift", alive: true },
  { k: "dragonfly", name: "A dragonfly", b: 2, s: 5.0, pts: 5, w: 2, act: "fly", alive: true },
  { k: "beetle", name: "A diving beetle", b: 2, s: 4.9, pts: 4, w: 2, act: "swim", alive: true },
  { k: "minnow", name: "A minnow", b: 2, s: 5.4, pts: 4, w: 2, act: "swim", alive: true },
  { k: "marble", name: "A glass marble", b: 2, s: 4.6, pts: 25, w: 0, act: "float", rare: true },

  { k: "pad", name: "A lily pad", b: 3, s: 9.5, pts: 5, w: 6, act: "float" },
  { k: "lily", name: "A water lily", b: 3, s: 8.6, pts: 5, w: 3, act: "float" },
  { k: "boat", name: "A paper boat", b: 3, s: 9.0, pts: 5, w: 3, act: "drift" },
  { k: "ball", name: "A rubber ball", b: 3, s: 8.4, pts: 5, w: 2, act: "bob", bump: true },
  { k: "newt", name: "A newt", b: 3, s: 9.2, pts: 6, w: 2, act: "swim", alive: true },
  { k: "goldfish", name: "A goldfish", b: 3, s: 8.4, pts: 6, w: 2, act: "swim", alive: true },
  { k: "rubberduck", name: "A rubber duck", b: 3, s: 8.8, pts: 40, w: 0, act: "drift", rare: true },
  { k: "bottle", name: "A note in a bottle", b: 3, s: 9.6, pts: 40, w: 0, act: "drift", rare: true },

  { k: "reeds", name: "A clump of reeds", b: 4, s: 16, pts: 8, w: 4, act: "still" },
  { k: "bucket", name: "A bucket", b: 4, s: 14.5, pts: 8, w: 3, act: "drift" },
  { k: "welly", name: "A welly", b: 4, s: 14, pts: 8, w: 2, act: "drift" },
  { k: "sailboat", name: "A toy sailboat", b: 4, s: 15.5, pts: 9, w: 2, act: "drift" },
  { k: "beachball", name: "A beach ball", b: 4, s: 15, pts: 8, w: 2, act: "bob", bump: true },
  { k: "carp", name: "A carp", b: 4, s: 16, pts: 10, w: 2, act: "swim", alive: true },
  { k: "turtle", name: "A turtle", b: 4, s: 15, pts: 10, w: 2, act: "paddle", alive: true, bump: true },
  { k: "koi", name: "A golden koi", b: 4, s: 15, pts: 60, w: 0, act: "swim", alive: true, rare: true },

  { k: "duck", name: "A duck", b: 5, s: 25, pts: 13, w: 4, act: "paddle", alive: true, bump: true },
  { k: "goose", name: "A goose", b: 5, s: 28, pts: 14, w: 2, act: "paddle", alive: true, bump: true },
  { k: "swan", name: "A swan", b: 5, s: 30, pts: 15, w: 1, act: "paddle", alive: true },
  { k: "buoy", name: "A lifebuoy", b: 5, s: 24, pts: 13, w: 3, act: "bob" },
  { k: "crate", name: "A crate", b: 5, s: 26, pts: 13, w: 3, act: "drift" },
  { k: "driftwood", name: "Driftwood", b: 5, s: 27, pts: 13, w: 3, act: "drift" },
  { k: "kite", name: "A lost kite", b: 5, s: 25, pts: 80, w: 0, act: "drift", rare: true },

  { k: "heron", name: "A heron", b: 6, s: 42, pts: 21, w: 2, act: "stalk", alive: true },
  { k: "otter", name: "An otter", b: 6, s: 40, pts: 21, w: 2, act: "swim", alive: true },
  { k: "canoe", name: "A canoe", b: 6, s: 48, pts: 22, w: 2, act: "drift" },
  { k: "padisle", name: "A raft of pads", b: 6, s: 50, pts: 22, w: 2, act: "still" },
  { k: "pedalo", name: "A pedalo", b: 6, s: 46, pts: 22, w: 2, act: "drift" },
  { k: "branch", name: "A fallen branch", b: 6, s: 50, pts: 22, w: 2, act: "drift" },
  { k: "log", name: "The old log", b: 6, s: 52, pts: 30, w: 0, act: "still", fixed: true },
  { k: "chest", name: "A sunken chest", b: 6, s: 44, pts: 120, w: 0, act: "bob", rare: true },

  { k: "tyre", name: "A tractor tyre", b: 7, s: 84, pts: 35, w: 2, act: "bob" },
  { k: "houseboat", name: "A houseboat", b: 7, s: 92, pts: 45, w: 2, act: "drift" },
  { k: "rowboat", name: "The rowing boat", b: 7, s: 80, pts: 40, w: 0, act: "still", fixed: true },
  { k: "punt", name: "The punt", b: 7, s: 86, pts: 40, w: 0, act: "still", fixed: true },
  { k: "jetty", name: "The jetty", b: 7, s: 105, pts: 80, w: 0, act: "still", fixed: true },
  { k: "isle", name: "The willow island", b: 7, s: 120, pts: 100, w: 0, act: "still", fixed: true },
];
export const KIND = Object.fromEntries(KINDS.map((k) => [k.k, k]));
export const REACH = BANDS.map((_, b) => Math.min(...KINDS.filter((k) => k.b === b).map((k) => k.s)) / EAT);

// The big things are moored in the same place every round, so the old log is a
// wall the first time the eddy meets it and swallowed by the end.
const MOORED = [["log", 700, 1340], ["rowboat", 930, 230], ["punt", 1080, 880], ["jetty", 650, 70], ["isle", 260, 520], ["canoe", 300, 1230], ["padisle", 1020, 1540]];
const START = { x: 640, y: 1680 };
// How many of each band to keep near the eddy, by how far the band is off its own.
const KEEP = { "-2": 10, "-1": 24, "0": 60, "1": 12, "2": 4 };

export function eats(w, kind) { return kind.s <= w.r * EAT; }
export function zoomAt(r) { return Z0 * Math.pow(R0 / r, ZP); }
function bandOf(r) { let b = 0; while (b + 1 < BANDS.length && r >= REACH[b + 1]) b++; return b; }

let ids = 0;

function thing(w, kind, x, y, born) {
  const o = { id: ++ids, kind, x, y, vx: 0, vy: 0, a: w.rand() * Math.PI * 2, t: w.rand() * 9, born: born ? 0 : 1, flee: 0, hold: 0, step: 0, face: w.rand() < 0.5 ? -1 : 1 };
  w.things.push(o);
  return o;
}

export function world(rand) {
  const w = {
    rand: rand || Math.random, t: 0, st: "play", done_t: 0, x: START.x, y: START.y, tx: START.x, ty: START.y, vx: 0, vy: 0,
    r: R0, band: 0, z: Z0, cx: START.x, cy: START.y, spin: 0, things: [], spins: [], gut: [], events: [], fill: 0,
    took: {}, score: 0, pts: 0, things_n: 0, swirled: 0, rares: 0, bumps: 0, safe: 0, kick: 0, rareSeen: {},
  };
  MOORED.forEach(([k, x, y]) => thing(w, KIND[k], x, y, false));
  for (let i = 0; i < 4; i++) top(w, true);
  return w;
}

export function view(w) { return { w: W / w.z, h: H / w.z }; }

// top keeps the water near the eddy stocked, the band it's on most and the
// next band in a few, and in a fresh pond everything goes in at once.
function top(w, all) {
  const v = view(w), near = Math.hypot(v.w, v.h) * 0.75;
  const count = {};
  w.things.forEach((o) => { if (!o.kind.fixed && Math.hypot(o.x - w.x, o.y - w.y) < near * 1.3) count[o.kind.b] = (count[o.kind.b] || 0) + 1; });
  for (let rel = -2; rel <= 2; rel++) {
    const b = w.band + rel;
    if (b < 0 || b >= BANDS.length) continue;
    const pool = KINDS.filter((k) => k.b === b && k.w > 0);
    if (!pool.length) continue;
    let want = KEEP[rel] - (count[b] || 0);
    if (!all) want = Math.min(want, 2);
    for (let i = 0; i < want; i++) {
      const p = spot(w, v, b, all);
      if (p) thing(w, rareOr(w, b) || pick(w, pool), p.x, p.y, !all);
    }
  }
}

function pick(w, pool) {
  let n = w.rand() * pool.reduce((a, k) => a + k.w, 0);
  for (const k of pool) if ((n -= k.w) < 0) return k;
  return pool[0];
}

// Every rare thing turns up once a round at most, a few times in a hundred
// of its band, and a round never sees them all.
function rareOr(w, b) {
  const r = KINDS.find((k) => k.rare && k.b === b && !w.rareSeen[k.k]);
  if (!r || w.rand() > 0.035) return null;
  w.rareSeen[r.k] = true;
  return r;
}

function inPond(x, y, s) { return x > s && y > s && x < PW - s && y < PH - s; }

// A new thing comes up anywhere round the view clear of the eddy and of
// anything big, and in sight it blooms up out of the water.
function spot(w, v, b, all) {
  const s = KINDS.find((k) => k.b === b).s;
  for (let tries = 0; tries < 8; tries++) {
    const x = w.x + (w.rand() - 0.5) * v.w * 1.35, y = w.y + (w.rand() - 0.5) * v.h * 1.35;
    if (!inPond(x, y, s * 1.2)) continue;
    if (Math.hypot(x - w.x, y - w.y) < w.r * 3 + 12 / w.z) continue;
    if (!all && Math.abs(x - w.cx) < v.w / 2 && Math.abs(y - w.cy) < v.h / 2 && w.rand() < 0.5) continue;
    if (w.things.some((o) => o.kind.s >= s && Math.hypot(o.x - x, o.y - y) < o.kind.s + s)) continue;
    return { x, y };
  }
  return null;
}

function emit(w, e) { w.events.push(e); }

// fast is the eddy's top speed in pond units, the same on screen at any zoom.
function fast(w) { return SPEED / w.z; }

export function steer(w, dx, dy) {
  if (w.st !== "play") return;
  const l = LEASH / w.z;
  w.tx += dx; w.ty += dy;
  const ox = w.tx - w.x, oy = w.ty - w.y, d = Math.hypot(ox, oy);
  if (d > l) { w.tx = w.x + ox / d * l; w.ty = w.y + oy / d * l; }
}

function swallow(w, o) {
  o.gone = true;
  const k = o.kind;
  w.r = Math.sqrt(w.r * w.r + GROW * k.s * k.s);
  w.took[k.k] = (w.took[k.k] || 0) + 1;
  w.pts += k.pts; w.things_n++;
  if (k.alive) w.swirled++;
  else w.gut.push(k.k);
  if (k.rare) w.rares++;
  const a = Math.atan2(o.y - w.y, o.x - w.x);
  w.spins.push({ kind: k, a, d: Math.hypot(o.x - w.x, o.y - w.y), t: 0, rot: o.a, out: !!k.alive, face: o.face });
  emit(w, { e: "eat", k: k.k, s: k.s, x: o.x, y: o.y, alive: !!k.alive, rare: !!k.rare });
  const b = bandOf(w.r);
  if (b > w.band) {
    w.band = b; w.kick = 1;
    emit(w, { e: "band", b });
    top(w, false);
  }
}

// A hard knock on something too big spits a few things back out, though
// never enough to drop the eddy out of the band it's reached.
function bump(w, o, nx, ny) {
  w.bumps++; w.safe = GRACE;
  const push = fast(w) * (o.kind.bump ? 0.9 : 0.6);
  w.vx = nx * push; w.vy = ny * push;
  w.tx = w.x + nx * 6 / w.z; w.ty = w.y + ny * 6 / w.z;
  const lost = [];
  for (let i = 0; i < SHED && w.gut.length; i++) {
    const j = Math.floor(w.rand() * w.gut.length), k = KIND[w.gut[j]];
    const r2 = w.r * w.r - GROW * k.s * k.s;
    if (r2 < REACH[w.band] * REACH[w.band]) break;
    w.gut.splice(j, 1);
    w.r = Math.sqrt(r2);
    w.took[k.k]--; w.pts -= k.pts; w.things_n--;
    if (!w.took[k.k]) delete w.took[k.k];
    const a = Math.atan2(ny, nx) + (w.rand() - 0.5) * 2.4, d = w.r + k.s + 2;
    const t = thing(w, k, w.x + Math.cos(a) * d, w.y + Math.sin(a) * d, false);
    t.vx = Math.cos(a) * fast(w) * 0.5; t.vy = Math.sin(a) * fast(w) * 0.5; t.hold = 1.2;
    lost.push(k.k);
  }
  emit(w, { e: "bump", x: w.x - nx * w.r, y: w.y - ny * w.r, k: o.kind.k, lost });
}

// How a thing gets about. Creatures the eddy can take keep away from it.
function move(w, o, dt) {
  const k = o.kind, d = Math.hypot(o.x - w.x, o.y - w.y) || 1;
  o.t += dt;
  if (o.born < 1) o.born = Math.min(1, o.born + dt * 2.5);
  const scared = k.alive && eats(w, k) && d < Math.max(w.r * 5, 50 / w.z) && w.st === "play";
  let ax = 0, ay = 0, v = 0, turn = 2;
  if (k.act === "swim" || k.act === "paddle" || k.act === "stalk") {
    o.step -= dt;
    if (o.step <= 0) { o.step = 1 + w.rand() * 2; o.a += (w.rand() - 0.5) * 2.2; }
    v = k.s * (k.act === "swim" ? 1.6 : 0.9);
    if (k.act === "stalk") v *= Math.sin(o.t * 2.2) > 0.3 ? 1.6 : 0;
    ax = Math.cos(o.a); ay = Math.sin(o.a);
    if (scared) {
      // Something running away goes a little under the eddy's speed, so a chase pays.
      ax = (o.x - w.x) / d + Math.cos(o.a) * 0.4; ay = (o.y - w.y) / d + Math.sin(o.a) * 0.4;
      v = fast(w) * (k.rare ? 0.62 : k.act === "swim" ? 0.5 : 0.4); turn = 5;
      o.flee = 1;
    } else o.flee = Math.max(0, o.flee - dt);
  } else if (k.act === "fly") {
    o.step -= dt;
    if (o.step <= 0) { o.step = 0.3 + w.rand() * 0.6; o.a = w.rand() * Math.PI * 2; }
    v = k.s * 9; turn = 6;
    ax = Math.cos(o.a); ay = Math.sin(o.a);
  } else if (k.act === "drift") {
    v = k.s * 0.35; ax = Math.cos(o.t * 0.2 + o.id); ay = Math.sin(o.t * 0.17 + o.id * 2);
  } else if (k.act === "float" || k.act === "bob") {
    v = k.s * 0.12; ax = Math.cos(o.t * 0.3 + o.id); ay = Math.sin(o.t * 0.23 + o.id);
  }
  if (!k.fixed && k.act !== "still") {
    const kk = Math.min(1, turn * dt);
    o.vx += (ax * v - o.vx) * kk; o.vy += (ay * v - o.vy) * kk;
  } else { o.vx *= Math.exp(-3 * dt); o.vy *= Math.exp(-3 * dt); }
  if (Math.abs(o.vx) > 0.5) o.face = o.vx < 0 ? -1 : 1;
  // Anything the eddy could take near it is drawn in, faster the closer it gets.
  const pull = w.r * (w.t > ROUND - LAST ? 3.8 : 2.8) + 8 / w.z;
  if (eats(w, k) && d < pull && o.hold <= 0 && w.st === "play") {
    const f = (1 - d / pull) * fast(w) * 1.1 * (k.alive ? 0.7 : 1);
    o.x += (w.x - o.x) / d * f * dt; o.y += (w.y - o.y) / d * f * dt;
  }
  o.hold -= dt;
  o.x += o.vx * dt; o.y += o.vy * dt;
  const m = k.s * 0.6;
  if (o.x < m || o.x > PW - m) { o.x = Math.max(m, Math.min(PW - m, o.x)); o.vx *= -1; o.a = Math.PI - o.a; }
  if (o.y < m || o.y > PH - m) { o.y = Math.max(m, Math.min(PH - m, o.y)); o.vy *= -1; o.a = -o.a; }
}

export function step(w, dt) {
  w.t += dt;
  w.spin += dt * (4 + 6 / Math.sqrt(w.r)) * (w.st === "done" ? 2.5 : 1);
  w.kick = Math.max(0, w.kick - dt * 1.6);
  w.safe = Math.max(0, w.safe - dt);
  if (w.st === "done") {
    w.done_t += dt;
    // The camera backs off to the whole pond for the finish.
    const all = Math.min(W / (PW + 2 * BANK), H / (PH + 2 * BANK));
    w.z += (all - w.z) * Math.min(1, dt * 1.8);
    w.cx += (PW / 2 - w.cx) * Math.min(1, dt * 1.8); w.cy += (PH / 2 - w.cy) * Math.min(1, dt * 1.8);
  } else {
    const l = Math.hypot(w.tx - w.x, w.ty - w.y), v = fast(w);
    let mx = 0, my = 0;
    if (l > 0.01) { const m = Math.min(l, v * dt); mx = (w.tx - w.x) / l * m; my = (w.ty - w.y) / l * m; }
    w.x += mx + w.vx * dt; w.y += my + w.vy * dt;
    const f = Math.exp(-6 * dt);
    w.vx *= f; w.vy *= f;
    const sp = Math.hypot(mx, my) / dt / v;
    const e = w.r * 0.7;
    w.x = Math.max(e, Math.min(PW - e, w.x)); w.y = Math.max(e, Math.min(PH - e, w.y));
    for (const o of w.things) {
      if (o.gone) continue;
      const k = o.kind, dx = o.x - w.x, dy = o.y - w.y, d = Math.hypot(dx, dy) || 0.01;
      if (eats(w, k)) {
        if (d < w.r * 0.85 + k.s * 0.4 && o.hold <= 0 && o.born > 0.6) swallow(w, o);
        continue;
      }
      const reach = w.r * 0.85 + k.s * 0.72;
      if (d >= reach) continue;
      const nx = -dx / d, ny = -dy / d;
      w.x += nx * (reach - d); w.y += ny * (reach - d);
      // Something only a little too big just blocks the way, and only a hard knock
      // on something well over spits anything out.
      if (w.safe <= 0 && k.s > w.r * NEAR && sp > (k.bump ? HARD * 0.5 : HARD)) bump(w, o, nx, ny);
      else { w.tx = w.x; w.ty = w.y; }
    }
    const zt = zoomAt(w.r);
    w.z += (zt - w.z) * Math.min(1, dt * 1.5);
    const v2 = view(w);
    w.cx += (w.x - w.cx) * Math.min(1, dt * 6); w.cy += (w.y - w.cy) * Math.min(1, dt * 6);
    w.cx = v2.w >= PW + 2 * BANK ? PW / 2 : Math.max(v2.w / 2 - BANK, Math.min(PW + BANK - v2.w / 2, w.cx));
    w.cy = v2.h >= PH + 2 * BANK ? PH / 2 : Math.max(v2.h / 2 - BANK, Math.min(PH + BANK - v2.h / 2, w.cy));
    if (w.t >= ROUND) end(w);
  }
  w.things.forEach((o) => { if (!o.gone) move(w, o, dt); });
  const v = view(w), far = Math.hypot(v.w, v.h) * 1.1;
  w.things = w.things.filter((o) => !o.gone && (o.kind.fixed || Math.hypot(o.x - w.x, o.y - w.y) < (o.kind.b > w.band + 1 ? far * 2 : far)));
  w.spins.forEach((s) => { s.t += dt; });
  w.spins = w.spins.filter((s) => s.t < (s.out ? 1.8 : 0.7));
  w.fill -= dt;
  if (w.fill <= 0 && w.st === "play") { w.fill = 0.25; top(w, false); }
}

function end(w) {
  w.st = "done";
  w.size = Math.round(w.r * 2);
  w.score = w.pts + w.size;
  emit(w, { e: "end" });
}

export function score(w) { return w.st === "done" ? w.score : w.pts + Math.round(w.r * 2); }

// Every count a round's goals read.
export function counts(w) {
  return {
    band: w.band, things: w.things_n, kinds: Object.keys(w.took).length, rares: w.rares, swirled: w.swirled, bumps: w.bumps,
    log: w.took.log ? 1 : 0, jetty: w.took.jetty ? 1 : 0, isle: w.took.isle ? 1 : 0,
  };
}

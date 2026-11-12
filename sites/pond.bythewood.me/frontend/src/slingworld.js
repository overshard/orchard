// Cattail sling's pond, every block and acorn floating on it and how they
// knock about. Nothing in here touches the page, so a script can play a whole
// round through with bun.
export const W = 200, H = 300, WATER = 232, STEP = 1 / 240, LEVELS = 5;
export const SX = 38, SY = 181, PULL = 26, FLING = 12.7, SHORE = 54;
const GRAV = 520, ITER = 10, PITER = 4, SLOP = 0.4, BETA = 0.25, MAXC = 3, REST = 60;
const ROLL = 0.15, VMIN = 80, DMG = 1000, FALL = 0.8, SLEEPV = 5, SLEEPT = 0.5, FAST = 700;

// d is density, mu friction and hp how much knocking a piece takes. pts is
// what breaking one scores and dent how a hit on it is felt.
export const MATS = {
  reed: { d: 0.4, mu: 0.7, hp: 1.6, pts: 10, name: "REED" },
  wood: { d: 0.7, mu: 0.6, hp: 5, pts: 15, name: "DRIFTWOOD" },
  mud: { d: 1.2, mu: 0.8, hp: 4, pts: 15, name: "MUD" },
  ice: { d: 0.9, mu: 0.08, hp: 2.6, pts: 15, name: "ICE" },
  stone: { d: 2.2, mu: 0.7, hp: 14, pts: 25, name: "STONE" },
  acorn: { d: 0.9, mu: 0.5, hp: 0.8, pts: 50 },
  frog: { d: 1.6, mu: 0.5, hp: 1e9, pts: 0 },
  plat: { d: 0, mu: 0.8, hp: 1e9, pts: 0 },
};
export const ACORN = 50, GOLD = 150, SPLASH = 10, SPARE = 100;

// Each frog has one tap mid flight. hit is how hard it knocks each material,
// so a dart goes through reeds and a bullfrog through stone.
export const FROGS = {
  pond: { name: "POND FROG", r: 5, d: 1.6, line: "NO TRICKS, JUST A GOOD FLING", hit: {} },
  tree: { name: "TREE FROG", r: 4.5, d: 1.5, line: "TAP TO SPLIT INTO THREE", hit: { reed: 1.6, ice: 1.4, acorn: 1.3, stone: 0.6 } },
  dart: { name: "DART FROG", r: 4.5, d: 1.8, line: "TAP TO ZIP STRAIGHT", hit: { reed: 3, ice: 3, wood: 1.5, stone: 0.5 } },
  bull: { name: "BULLFROG", r: 6.5, d: 1.8, line: "TAP TO DROP LIKE A STONE", hit: { stone: 2.6, mud: 2, wood: 1.4, reed: 0.8 } },
  toad: { name: "PUFF TOAD", r: 5.5, d: 1.6, line: "TAP TO PUFF UP AND BLOW", hit: { mud: 1.3 } },
};

// ---------------------------------------------------------------- levels

// A level is its platforms and what's stacked on them, pieces placed by the
// bottom of each as a height over the platform it sits on.
function log(x, hx, ph) { return { k: "log", x, y: WATER, hx, hy: 6, bob: 1.6, tilt: 0.02, ph: ph || 0, top: WATER - 6 }; }
function raft(x, hx, drift, ph) { return { k: "raft", x, y: WATER - 1, hx, hy: 4, bob: 1.2, tilt: 0.012, drift, ph: ph || 0, top: WATER - 5 }; }
function rock(x, hx, top) { return { k: "rock", x, y: (top + H + 10) / 2, hx, hy: (H + 10 - top) / 2, top }; }
function bank(x0, x1, top) { return { k: "bank", x: (x0 + x1) / 2, y: (top + H + 10) / 2, hx: (x1 - x0) / 2, hy: (H + 10 - top) / 2, top }; }

const post = (p, dx, y, mat, h, w) => ({ s: "box", p, dx, y, mat, hx: (w || 5) / 2, hy: h / 2 });
const slab = (p, dx, y, mat, len, th) => ({ s: "box", p, dx, y, mat, hx: len / 2, hy: (th || 4) / 2 });
const cube = (p, dx, y, mat, s) => ({ s: "box", p, dx, y, mat, hx: s / 2, hy: s / 2 });
const ball = (p, dx, y, mat, r) => ({ s: "circle", p, dx, y, mat, r: r || 5 });
const nut = (p, dx, y) => ({ s: "circle", p, dx, y, mat: "acorn", r: 4 });

// Five slots in a round, each a few levels to deal one from, and the frogs a
// slot hands out bring a new one in each time.
export const POOL = [
  {
    frogs: ["pond", "pond", "pond"], levels: [
      { name: "THREE ON A LOG", sky: 0, plats: [log(140, 36)], parts: [
        post(0, -24, 0, "wood", 22), post(0, 24, 0, "wood", 22), slab(0, 0, 22, "wood", 58),
        nut(0, 0, 0), nut(0, -14, 26), nut(0, 14, 26)] },
      { name: "REED ROW", sky: 0, plats: [log(138, 42, 1)], parts: [
        post(0, -28, 0, "reed", 20, 6), post(0, 0, 0, "reed", 20, 6), post(0, 28, 0, "reed", 20, 6),
        nut(0, -28, 20), nut(0, 0, 20), nut(0, 28, 20), ball(0, -14, 0, "stone", 4), ball(0, 14, 0, "stone", 4)] },
      { name: "LITTLE CAIRN", sky: 0, plats: [rock(156, 18, 214), log(108, 22, 2)], parts: [
        cube(0, -8, 0, "wood", 12), cube(0, 8, 0, "wood", 12), slab(0, 0, 12, "stone", 32, 5), nut(0, 0, 17),
        ball(1, -10, 0, "stone", 4), ball(1, 10, 0, "stone", 4), nut(1, 0, 0)] },
    ],
  },
  {
    frogs: ["tree", "tree", "pond"], levels: [
      { name: "REED HUTS", sky: 1, plats: [raft(140, 42, 10)], parts: [
        post(0, -30, 0, "reed", 16), post(0, -12, 0, "reed", 16), slab(0, -21, 16, "reed", 26), nut(0, -21, 0), nut(0, -21, 20),
        post(0, 12, 0, "reed", 16), post(0, 30, 0, "reed", 16), slab(0, 21, 16, "reed", 26), nut(0, 21, 0), nut(0, 21, 20)] },
      { name: "TWO LOGS", sky: 1, plats: [log(116, 22, 0.5), log(172, 22, 2.2)], parts: [
        post(0, 0, 0, "reed", 18, 6), slab(0, 0, 18, "reed", 28), nut(0, -10, 22), nut(0, 10, 22),
        post(1, 0, 0, "reed", 18, 6), slab(1, 0, 18, "reed", 28), nut(1, -10, 22), nut(1, 10, 22)] },
      { name: "SHELVES", sky: 1, plats: [log(146, 44, 1.2)], parts: [
        nut(0, -20, 0), nut(0, 20, 0),
        post(0, -34, 0, "reed", 14), post(0, -6, 0, "reed", 14), post(0, 6, 0, "reed", 14), post(0, 34, 0, "reed", 14),
        slab(0, -20, 14, "reed", 34), slab(0, 20, 14, "reed", 34), nut(0, -20, 18), nut(0, 20, 18),
        post(0, -14, 18, "reed", 12), post(0, 14, 18, "reed", 12), slab(0, 0, 30, "wood", 40), nut(0, 0, 34)] },
    ],
  },
  {
    frogs: ["dart", "pond", "dart"], levels: [
      { name: "LONG TABLE", sky: 2, plats: [log(142, 46, 0.3)], parts: [
        post(0, -36, 0, "wood", 18), post(0, -12, 0, "wood", 18), post(0, 12, 0, "wood", 18), post(0, 36, 0, "wood", 18),
        slab(0, 0, 18, "wood", 82), nut(0, -24, 0), nut(0, 0, 0), nut(0, 24, 0), nut(0, -24, 22), nut(0, 24, 22),
        post(0, -43, 0, "reed", 26, 4)] },
      { name: "REED TOWER", sky: 2, plats: [rock(162, 16, 212), log(112, 20, 1.7)], parts: [
        post(0, -9, 0, "reed", 14), post(0, 9, 0, "reed", 14), slab(0, 0, 14, "reed", 26), nut(0, 0, 0),
        post(0, -9, 18, "reed", 14), post(0, 9, 18, "reed", 14), slab(0, 0, 32, "reed", 26), nut(0, 0, 18),
        post(0, -9, 36, "reed", 14), post(0, 9, 36, "reed", 14), slab(0, 0, 50, "reed", 26), nut(0, 0, 36), nut(0, 0, 54),
        cube(1, 0, 0, "wood", 10), nut(1, 0, 10)] },
      { name: "FROST SHELF", sky: 3, plats: [log(144, 42, 2.5)], parts: [
        post(0, -28, 0, "ice", 16, 6), post(0, 28, 0, "ice", 16, 6), slab(0, 0, 16, "ice", 66, 5),
        nut(0, -16, 21), nut(0, 0, 21), nut(0, 16, 21), cube(0, 0, 0, "ice", 10), nut(0, -14, 0), nut(0, 14, 0)] },
    ],
  },
  {
    frogs: ["bull", "tree", "dart"], levels: [
      { name: "STONE HUT", sky: 0, plats: [log(144, 44, 0.8)], parts: [
        post(0, -20, 0, "wood", 20, 6), post(0, 20, 0, "wood", 20, 6), slab(0, 0, 20, "stone", 50, 6),
        nut(0, -7, 0), nut(0, 7, 0), nut(0, 0, 26),
        cube(0, -36, 0, "stone", 10), nut(0, -36, 10), cube(0, 36, 0, "stone", 10), nut(0, 36, 10)] },
      { name: "MUD WALL", sky: 1, plats: [bank(172, 220, 208), log(132, 26, 1.1)], parts: [
        cube(1, -5, 0, "mud", 10), cube(1, 5, 0, "mud", 10), cube(1, -5, 10, "mud", 10), cube(1, 5, 10, "mud", 10),
        cube(1, 0, 20, "mud", 10), nut(1, 0, 30), nut(1, -18, 0),
        nut(0, -8, 0), nut(0, 6, 0), cube(0, 18, 0, "mud", 10)] },
      { name: "CAIRN", sky: 2, plats: [rock(152, 24, 214)], parts: [
        ball(0, -15, 0, "stone", 5), ball(0, -5, 0, "stone", 5), ball(0, 5, 0, "stone", 5), ball(0, 15, 0, "stone", 5),
        ball(0, -10, 8.66, "stone", 5), nut(0, 0, 9.4), ball(0, 10, 8.66, "stone", 5),
        ball(0, -5, 17.32, "stone", 5), ball(0, 5, 17.32, "stone", 5), nut(0, 0, 26.6),
        nut(0, -21, 0.1), nut(0, 21, 0.1)] },
    ],
  },
  {
    frogs: ["toad", "bull", "tree", "dart"], levels: [
      { name: "THE OLD DAM", sky: 2, plats: [rock(152, 30, 218)], parts: [
        cube(0, -24, 0, "mud", 10), cube(0, -14, 0, "mud", 10), cube(0, 14, 0, "mud", 10), cube(0, 24, 0, "mud", 10),
        nut(0, -1, 0), nut(0, 6, 0),
        slab(0, 0, 10, "wood", 60, 5), post(0, -20, 15, "wood", 16), post(0, 20, 15, "wood", 16), nut(0, -8, 15), nut(0, 8, 15),
        slab(0, 0, 31, "wood", 52, 5), cube(0, -12, 36, "mud", 9), cube(0, 12, 36, "mud", 9), nut(0, 0, 36), nut(0, -12, 45), nut(0, 12, 45)] },
      { name: "RAFT FORT", sky: 1, plats: [raft(144, 48, 14, 1)], parts: [
        post(0, -40, 0, "wood", 22), post(0, -28, 0, "wood", 22), slab(0, -34, 22, "stone", 18, 5), nut(0, -34, 27),
        post(0, 28, 0, "wood", 22), post(0, 40, 0, "wood", 22), slab(0, 34, 22, "stone", 18, 5), nut(0, 34, 27),
        post(0, -10, 0, "reed", 14), post(0, 10, 0, "reed", 14), slab(0, 0, 14, "reed", 26), nut(0, 0, 0), nut(0, 0, 18),
        nut(0, -19, 0), nut(0, 19, 0)] },
      { name: "THE OLD NEST", sky: 3, plats: [log(146, 34, 0.4)], parts: [
        post(0, -14, 0, "wood", 40, 5), post(0, 14, 0, "wood", 40, 5), slab(0, 0, 40, "reed", 40, 4),
        post(0, -17, 44, "reed", 8, 6), post(0, 17, 44, "reed", 8, 6), nut(0, -6, 44), nut(0, 3, 44), nut(0, 10, 44),
        nut(0, 0, 0), ball(0, -24, 0, "stone", 4), ball(0, 24, 0, "stone", 4)] },
    ],
  },
];

// ---------------------------------------------------------------- bodies

let ids = 0;

function body(o) {
  const m = MATS[o.mat], b = { id: ++ids, s: o.s, mat: o.mat, x: o.x, y: o.y, a: o.a || 0, vx: 0, vy: 0, w: 0, hx: o.hx || 0, hy: o.hy || 0, r: o.r || 0, mu: m.mu, hp: m.hp, hp0: m.hp, kind: o.kind || (o.mat === "acorn" ? "acorn" : "block"), sleep: false, still: 0, rest: null };
  setMass(b, o.d || m.d);
  return b;
}

function setMass(b, d) {
  if (!d) { b.m = b.im = b.I = b.iI = 0; return; }
  if (b.s === "circle") { b.m = d * Math.PI * b.r * b.r; b.I = b.m * b.r * b.r / 2; }
  else { b.m = d * 4 * b.hx * b.hy; b.I = b.m * (b.hx * b.hx + b.hy * b.hy) / 3; }
  b.im = 1 / b.m; b.iI = 1 / b.I;
}

function platBody(p) {
  const b = body({ s: "box", mat: "plat", x: p.x, y: p.y, hx: p.hx, hy: p.hy, kind: "plat" });
  b.plat = p;
  return b;
}

// A log bobs and rocks a little and a raft drifts as well, moved by the clock
// and never pushed, so whatever sits on one rides along.
function placePlat(b, t) {
  const p = b.plat;
  if (p.k !== "log" && p.k !== "raft") return;
  const w1 = 2 * Math.PI / 3.4, w2 = 2 * Math.PI / 4.6, w3 = 2 * Math.PI / 9;
  b.y = p.y + p.bob * Math.sin(w1 * t + p.ph); b.vy = p.bob * w1 * Math.cos(w1 * t + p.ph);
  b.a = p.tilt * Math.sin(w2 * t + p.ph * 1.7); b.w = p.tilt * w2 * Math.cos(w2 * t + p.ph * 1.7);
  if (p.drift) { b.x = p.x + p.drift * Math.sin(w3 * t + p.ph); b.vx = p.drift * w3 * Math.cos(w3 * t + p.ph); }
}

// ---------------------------------------------------------------- the world

export function round(rand) {
  rand = rand || Math.random;
  const deal = POOL.map((s) => Math.floor(rand() * s.levels.length));
  const gold = Math.floor(rand() * LEVELS);
  return { deal, gold, lv: -1, score: 0, results: [], events: [], counts: { acorns: 0, golden: 0, blocks: 0, stones: 0, splashed: 0, spare: 0, cleared: 0, stars: 0, three: 0, oneshot: 0 }, st: "next", t: 0, L: null, rand };
}

// The levels a round could deal, for the sim and for working out the most.
export function levelOf(slot, i) { return POOL[slot].levels[i]; }

export function begin(w, slot, pick, raw) {
  w.lv = slot;
  const def = POOL[slot].levels[pick === undefined ? w.deal[slot] : pick];
  const L = { def, t: 0, bodies: [], plats: [], arbs: new Map(), frogs: POOL[slot].frogs.slice(), flying: [], st: "settle", st_t: 0, score: 0, acorns: 0, flung: 0, trail: [], last: [], stars: 0, slow: 0, breaks: [], isl: 0 };
  def.plats.forEach((p) => { const b = platBody(p); L.plats.push(b); L.bodies.push(b); });
  const shore = platBody(bank(-30, SHORE, 214));
  shore.plat.k = "shore";
  L.bodies.push(shore);
  let gi = slot === w.gold ? Math.floor(w.rand() * def.parts.filter((o) => o.mat === "acorn").length) : -1;
  for (const p of L.plats) placePlat(p, 0);
  for (const o of def.parts) {
    // Each piece starts where it sits on its platform as the level opens, at
    // the platform's tilt and moving with it.
    const p = L.plats[o.p], lx = o.dx, ly = -p.hy - o.y - (o.s === "circle" ? o.r : o.hy), c = Math.cos(p.a), s = Math.sin(p.a);
    const b = body({ s: o.s, mat: o.mat, x: p.x + c * lx - s * ly, y: p.y + s * lx + c * ly, a: p.a, hx: o.hx, hy: o.hy, r: o.r });
    b.vx = p.vx - p.w * (b.y - p.y); b.vy = p.vy + p.w * (b.x - p.x); b.w = p.w;
    if (b.kind === "acorn") { if (L.acorns === gi) b.gold = true; L.acorns++; }
    L.bodies.push(b);
  }
  // Settle out of sight so a level opens on stacks already at rest.
  w.L = L;
  w.quietSettle = true;
  for (let i = 0; i < 480 && !raw && !allAsleep(L); i++) tick(w, L);
  w.quietSettle = false;
  L.t = 0; L.st = "intro"; L.st_t = 0;
  w.events.push({ e: "level", lv: slot, name: def.name, frog: POOL[slot].frogs[0], fresh: slot > 0 && POOL[slot].frogs[0] !== POOL[slot - 1].frogs[0] });
  return L;
}

function allAsleep(L) { return L.bodies.every((b) => b.kind === "plat" || b.sleep); }

// ---------------------------------------------------------------- flinging

// Where a pull leaves the frog, held inside the sling's reach and off the bank.
export function pulled(dx, dy) {
  const l = Math.hypot(dx, dy), k = l > PULL ? PULL / l : 1;
  return { x: SX + dx * k, y: Math.min(SY + 26, SY + dy * k), l: Math.min(l, PULL) };
}

export function launchVel(p) { return { vx: (SX - p.x) * FLING, vy: (SY - p.y) * FLING }; }

// The path a fling would take for t seconds, a point every step of it.
export function arc(p, t, step) {
  const v = launchVel(p), out = [];
  for (let s = step; s <= t; s += step) out.push({ x: p.x + v.vx * s, y: p.y + v.vy * s + GRAV * s * s / 2 });
  return out;
}

// Where a fling would go, a point a frame, up to the first thing it would
// touch or the water.
export function aimPath(w, p, t) {
  const L = w.L, r = FROGS[L.frogs[0] || "pond"].r, out = [];
  for (const q of arc(p, t || 1.2, 1 / 60)) {
    out.push(q);
    if (q.y > WATER || q.x > W + 10) break;
    if (L.bodies.some((b) => !b.gone && b.kind !== "frog" && !(b.plat && b.plat.k === "shore") && touches(b, q.x, q.y, r))) { out.hit = true; break; }
  }
  return out;
}

function touches(b, x, y, r) {
  if (b.s === "circle") return Math.hypot(x - b.x, y - b.y) < b.r + r;
  const c = Math.cos(b.a), s = Math.sin(b.a), dx = x - b.x, dy = y - b.y;
  return Math.abs(c * dx + s * dy) < b.hx + r && Math.abs(-s * dx + c * dy) < b.hy + r;
}

export function fling(w, dx, dy) {
  const L = w.L;
  if (!L || L.st !== "ready" || !L.frogs.length) return false;
  const p = pulled(dx, dy);
  if (p.l < 6) return false;
  const k = L.frogs.shift(), f = FROGS[k], v = launchVel(p);
  const b = body({ s: "circle", mat: "frog", x: p.x, y: p.y, r: f.r, kind: "frog", d: f.d });
  b.vx = v.vx; b.vy = v.vy; b.frog = k; b.age = 0; b.touched = false; b.used = false; b.zip = 0; b.puff = 0; b.restT = 0;
  L.bodies.push(b); L.flying.push(b);
  L.last = L.trail; L.trail = [{ x: b.x, y: b.y }];
  L.flung++; L.st = "fly"; L.st_t = 0;
  w.events.push({ e: "fling", k, l: p.l, x: p.x, y: p.y });
  return true;
}

// The frog in the air uses its one trick. A tap with nothing to do is a croak.
export function power(w) {
  const L = w.L;
  if (!L || L.st !== "fly") return false;
  const b = L.flying.find((f) => !f.gone && !f.done && !f.used);
  if (!b) return false;
  const k = b.frog;
  if (k === "pond" || (k !== "toad" && b.touched)) return false;
  b.used = true;
  const sp = Math.hypot(b.vx, b.vy) || 1;
  if (k === "tree") {
    const a0 = Math.atan2(b.vy, b.vx);
    for (const da of [-0.2, 0.2]) {
      const c = body({ s: "circle", mat: "frog", x: b.x, y: b.y, r: 4, kind: "frog", d: 1.5 });
      c.vx = Math.cos(a0 + da) * sp; c.vy = Math.sin(a0 + da) * sp;
      Object.assign(c, { frog: "tree", age: b.age, touched: false, used: true, zip: 0, puff: 0, restT: 0, kid: true });
      L.bodies.push(c); L.flying.push(c);
    }
    b.r = 4; setMass(b, 1.5);
  } else if (k === "dart") {
    const s = Math.max(sp, 540);
    b.vx = b.vx / sp * s; b.vy = b.vy / sp * s; b.zip = 0.35;
  } else if (k === "bull") {
    b.vx *= 0.15; b.vy = Math.max(b.vy, 0) + 480; setMass(b, 4.5);
  } else if (k === "toad") {
    puff(w, L, b);
  }
  w.events.push({ e: "power", k, x: b.x, y: b.y });
  return true;
}

// A toad puffing up blows everything near it away from it, harder the closer.
function puff(w, L, b) {
  const R = 44;
  b.puff = 0.6;
  for (const o of L.bodies) {
    if (o === b || o.kind === "plat" || o.kind === "frog" || o.gone) continue;
    const dx = o.x - b.x, dy = o.y - b.y, d = Math.hypot(dx, dy);
    if (d > R) continue;
    const k = 1 - d / R, dv = 300 * k / Math.sqrt(o.m / 60);
    o.vx += dx / (d || 1) * dv; o.vy += dy / (d || 1) * dv - 40 * k; o.w += (o.x > b.x ? 1 : -1) * 4 * k;
    hurt(w, L, o, 3.5 * k * (o.mat === "reed" || o.mat === "ice" ? 1.5 : o.mat === "stone" ? 0.4 : 1), b);
  }
  b.vx *= 0.3; b.vy = Math.min(b.vy, -60);
  wakeAll(L);
}

// ---------------------------------------------------------------- stepping

export function step(w, dt) {
  const L = w.L;
  if (!L) return;
  L.acc = (L.acc || 0) + dt * (L.slow > 0 ? 0.4 : 1);
  L.slow = Math.max(0, L.slow - dt);
  let n = 0;
  while (L.acc >= STEP && n < 12) { L.acc -= STEP; tick(w, L); n++; }
  if (n === 12) L.acc = 0;
  flow(w, L, dt);
}

function tick(w, L) {
  const h = STEP;
  L.t += h;
  for (const p of L.plats) placePlat(p, L.t);
  for (const b of L.bodies) {
    if (b.kind === "plat") continue;
    if (b.sleep) { ride(b); continue; }
    if (b.zip > 0) b.zip -= h;
    else b.vy += GRAV * h;
    b.vx *= 1 - 0.02 * h; b.vy *= 1 - 0.02 * h; b.w *= 1 - 0.6 * h;
  }
  let arbs = collide(w, L);
  if (L.woke) arbs = collide(w, L);
  impacts(w, L, arbs);
  const live = arbs.filter((a) => !a.a.broken && !a.b.broken);
  for (const a of live) prep(a);
  for (let i = 0; i < ITER; i++) for (const a of live) solve(a);
  for (const b of L.bodies) {
    if (b.kind === "plat" || b.sleep) continue;
    const v = Math.hypot(b.vx, b.vy);
    if (v > FAST) { b.vx *= FAST / v; b.vy *= FAST / v; }
    b.x += b.vx * h; b.y += b.vy * h; b.a += b.w * h;
  }
  for (let i = 0; i < PITER; i++) for (const a of live) correct(a);
  const next = new Map();
  for (const a of live) next.set(a.key, a);
  L.arbs = next;
  frogs(w, L, h);
  sleep(L, live, h);
  sweep(w, L);
}

// A sleeping piece keeps its place on whatever it rests on.
function ride(b) {
  const r = b.rest;
  if (!r.p) return;
  const p = r.p, c = Math.cos(p.a), s = Math.sin(p.a);
  b.x = p.x + c * r.x - s * r.y; b.y = p.y + s * r.x + c * r.y; b.a = p.a + r.a;
  b.vx = p.vx - p.w * (b.y - p.y); b.vy = p.vy + p.w * (b.x - p.x); b.w = p.w;
}

// ---------------------------------------------------------------- contacts

function aabb(b) {
  if (b.s === "circle") return [b.x - b.r, b.y - b.r, b.x + b.r, b.y + b.r];
  const c = Math.abs(Math.cos(b.a)), s = Math.abs(Math.sin(b.a)), ex = c * b.hx + s * b.hy, ey = s * b.hx + c * b.hy;
  return [b.x - ex, b.y - ey, b.x + ex, b.y + ey];
}

function collide(w, L) {
  L.woke = false;
  const bs = L.bodies, boxes = bs.map(aabb), out = [];
  for (let i = 0; i < bs.length; i++) for (let j = i + 1; j < bs.length; j++) {
    let a = bs[i], b = bs[j];
    if (a.gone || b.gone) continue;
    const fa = a.kind === "plat" || a.sleep, fb = b.kind === "plat" || b.sleep;
    if (fa && fb) continue;
    if (a.kind === "frog" && b.kind === "frog") continue;
    const A = boxes[i], B = boxes[j];
    if (A[0] > B[2] + 1 || B[0] > A[2] + 1 || A[1] > B[3] + 1 || B[1] > A[3] + 1) continue;
    if (b.s === "box" && a.s === "circle") [a, b] = [b, a];
    const pts = contact(a, b);
    if (!pts.length) continue;
    // Anything awake touching a sleeping stack wakes all of it.
    if (a.sleep) wake(L, a.isl);
    if (b.sleep) wake(L, b.isl);
    const key = a.id * 4096 + b.id, old = L.arbs.get(key);
    for (const p of pts) {
      p.pn = 0; p.pt = 0;
      if (old) for (const q of old.pts) if (Math.abs(q.x - p.x) + Math.abs(q.y - p.y) < 2) { p.pn = q.pn; p.pt = q.pt; break; }
    }
    const roll = a.s === "circle" ? a.r : b.s === "circle" ? b.r : 0;
    if (a.kind === "frog" && b.kind !== "plat") a.brushed = true;
    if (b.kind === "frog" && a.kind !== "plat") b.brushed = true;
    out.push({ key, a, b, pts, mu: Math.sqrt(a.mu * b.mu), roll, lr: 0 });
  }
  return out;
}

// contact is where a and b touch, each point with the normal from a to b and
// how far apart they are there, below nought when they overlap.
function contact(a, b) {
  if (a.s === "circle") {
    const dx = b.x - a.x, dy = b.y - a.y, d = Math.hypot(dx, dy), sep = d - a.r - b.r;
    if (sep > 0.5) return [];
    const nx = d > 1e-6 ? dx / d : 0, ny = d > 1e-6 ? dy / d : -1;
    return [{ nx, ny, sep, x: a.x + nx * (a.r + sep / 2), y: a.y + ny * (a.r + sep / 2) }];
  }
  if (b.s === "circle") return boxCircle(a, b);
  return boxBox(a, b);
}

function boxCircle(a, b) {
  const c = Math.cos(a.a), s = Math.sin(a.a), dx = b.x - a.x, dy = b.y - a.y;
  const lx = c * dx + s * dy, ly = -s * dx + c * dy;
  const qx = Math.max(-a.hx, Math.min(a.hx, lx)), qy = Math.max(-a.hy, Math.min(a.hy, ly));
  let nx, ny, sep, px = qx, py = qy;
  if (qx === lx && qy === ly) {
    const ex = a.hx - Math.abs(lx), ey = a.hy - Math.abs(ly);
    if (ex < ey) { nx = Math.sign(lx) || 1; ny = 0; sep = -ex - b.r; px = nx * a.hx; }
    else { nx = 0; ny = Math.sign(ly) || 1; sep = -ey - b.r; py = ny * a.hy; }
  } else {
    const ox = lx - qx, oy = ly - qy, d = Math.hypot(ox, oy);
    sep = d - b.r;
    if (sep > 0.5) return [];
    nx = ox / d; ny = oy / d;
  }
  return [{ nx: c * nx - s * ny, ny: s * nx + c * ny, sep, x: a.x + c * px - s * py, y: a.y + s * px + c * py }];
}

function axes(b) { const c = Math.cos(b.a), s = Math.sin(b.a); return [[c, s], [-s, c]]; }
function corners(b) {
  const [u, v] = axes(b), out = [];
  for (const [i, j] of [[1, 1], [-1, 1], [-1, -1], [1, -1]]) out.push([b.x + u[0] * b.hx * i + v[0] * b.hy * j, b.y + u[1] * b.hx * i + v[1] * b.hy * j]);
  return out;
}

// The face of each box that least overlaps the other picks which is the
// reference, and the other's nearest face is clipped to it for two points.
function faceSep(a, b) {
  const ax = axes(a), bx = axes(b), dx = b.x - a.x, dy = b.y - a.y;
  let best = { sep: -Infinity };
  for (let i = 0; i < 2; i++) {
    const u = ax[i], ha = i ? a.hy : a.hx, d = dx * u[0] + dy * u[1];
    const hb = b.hx * Math.abs(bx[0][0] * u[0] + bx[0][1] * u[1]) + b.hy * Math.abs(bx[1][0] * u[0] + bx[1][1] * u[1]);
    const sep = Math.abs(d) - ha - hb;
    if (sep > best.sep) best = { sep, nx: u[0] * (d >= 0 ? 1 : -1), ny: u[1] * (d >= 0 ? 1 : -1), i, h: ha, side: i ? a.hx : a.hy };
  }
  return best;
}

function boxBox(a, b) {
  const fa = faceSep(a, b);
  if (fa.sep > 0.5) return [];
  const fb = faceSep(b, a);
  if (fb.sep > 0.5) return [];
  let ref = a, inc = b, f = fa, flip = false;
  if (fb.sep > fa.sep * 0.95 + 0.05) { ref = b; inc = a; f = fb; flip = true; }
  const nx = f.nx, ny = f.ny, tx = -ny, ty = nx;
  const cx = ref.x + nx * f.h, cy = ref.y + ny * f.h, tc = cx * tx + cy * ty;
  // The incident face is the one on inc facing most against n.
  const cs = corners(inc);
  let bi = 0, bd = Infinity;
  for (let i = 0; i < 4; i++) {
    const p = cs[i], q = cs[(i + 1) % 4], ex = q[0] - p[0], ey = q[1] - p[1], l = Math.hypot(ex, ey);
    const d = (ey / l) * nx + (-ex / l) * ny;
    if (d < bd) { bd = d; bi = i; }
  }
  let v = [cs[bi], cs[(bi + 1) % 4]];
  v = clip(v, -tx, -ty, -tc + f.side);
  if (v.length < 2) return [];
  v = clip(v, tx, ty, tc + f.side);
  if (v.length < 2) return [];
  const out = [];
  for (const p of v) {
    const sep = (p[0] - cx) * nx + (p[1] - cy) * ny;
    if (sep > 0.5) continue;
    out.push({ nx: flip ? -nx : nx, ny: flip ? -ny : ny, sep, x: p[0] - nx * sep / 2, y: p[1] - ny * sep / 2 });
  }
  return out;
}

// clip keeps the part of a segment with p.n <= off.
function clip(v, nx, ny, off) {
  const d0 = v[0][0] * nx + v[0][1] * ny - off, d1 = v[1][0] * nx + v[1][1] * ny - off, out = [];
  if (d0 <= 0) out.push(v[0]);
  if (d1 <= 0) out.push(v[1]);
  if (d0 * d1 < 0) { const k = d0 / (d0 - d1); out.push([v[0][0] + (v[1][0] - v[0][0]) * k, v[0][1] + (v[1][1] - v[0][1]) * k]); }
  return out;
}

// ---------------------------------------------------------------- damage

// A knock is the push of the hardest point of a touch over a floor, so a
// piece at rest takes nothing and a frog at full fling breaks driftwood.
function impacts(w, L, arbs) {
  if (w.quietSettle) return;
  for (const ar of arbs) {
    const { a, b } = ar;
    let vn = 0;
    for (const p of ar.pts) {
      const rv = relVel(a, b, p.x, p.y);
      vn = Math.min(vn, rv[0] * p.nx + rv[1] * p.ny);
    }
    const speed = -vn - VMIN;
    if (speed <= 0) continue;
    const k = a.im + b.im;
    if (!k) continue;
    const frog = a.kind === "frog" ? a : b.kind === "frog" ? b : null;
    const J = speed / k / DMG * (frog ? 1 : FALL);
    if (frog) frog.touched = true;
    for (const [x, o] of [[a, b], [b, a]]) {
      if (x.kind === "plat" || x.kind === "frog") continue;
      const mult = o.kind === "frog" ? (FROGS[o.frog].hit[x.mat] || 1) : 1;
      hurt(w, L, x, J * mult, o);
    }
    if (J > 2) w.events.push({ e: "knock", x: ar.pts[0].x, y: ar.pts[0].y, j: J, mat: (a.kind === "plat" || a.kind === "frog" ? b : a).mat, frog: !!frog });
  }
}

function hurt(w, L, x, dmg, by) {
  if (x.broken || dmg <= 0) return;
  x.hp -= dmg;
  if (x.hp > 0) return;
  x.broken = true;
  // What broke it carries on through, a little slower.
  if (by && by.kind !== "plat" && !by.sleep) { by.vx *= 0.8; by.vy *= 0.8; }
  L.breaks.push(L.t);
  w.events.push({ e: "break", x: x.x, y: x.y, a: x.a, mat: x.mat, kind: x.kind, hx: x.hx, hy: x.hy, r: x.r, gold: x.gold });
  if (x.kind === "acorn") clearNut(w, L, x);
  else { add(w, L, MATS[x.mat].pts); w.counts.blocks++; if (x.mat === "stone") w.counts.stones++; }
}

function clearNut(w, L, x) {
  L.acorns--;
  add(w, L, x.gold ? GOLD : ACORN);
  w.counts.acorns++;
  if (x.gold) w.counts.golden++;
}

function add(w, L, n) { L.score += n; w.score += n; }

function relVel(a, b, x, y) {
  return [b.vx - b.w * (y - b.y) - a.vx + a.w * (y - a.y), b.vy + b.w * (x - b.x) - a.vy - a.w * (x - a.x)];
}

// ---------------------------------------------------------------- solver

function prep(ar) {
  const { a, b } = ar;
  for (const p of ar.pts) {
    const rax = p.x - a.x, ray = p.y - a.y, rbx = p.x - b.x, rby = p.y - b.y;
    const rna = rax * p.ny - ray * p.nx, rnb = rbx * p.ny - rby * p.nx;
    const tx = -p.ny, ty = p.nx, rta = rax * ty - ray * tx, rtb = rbx * ty - rby * tx;
    p.mn = 1 / (a.im + b.im + a.iI * rna * rna + b.iI * rnb * rnb);
    p.mt = 1 / (a.im + b.im + a.iI * rta * rta + b.iI * rtb * rtb);
    const rv = relVel(a, b, p.x, p.y), vn = rv[0] * p.nx + rv[1] * p.ny;
    // Only a real knock bounces, so a stack at rest stays at rest.
    p.bias = (vn < -REST ? -0.12 * vn : 0) - Math.max(0, p.sep) / STEP;
    apply(a, b, p, p.nx * p.pn + tx * p.pt, p.ny * p.pn + ty * p.pt);
  }
}

function apply(a, b, p, px, py) {
  a.vx -= px * a.im; a.vy -= py * a.im; a.w -= a.iI * ((p.x - a.x) * py - (p.y - a.y) * px);
  b.vx += px * b.im; b.vy += py * b.im; b.w += b.iI * ((p.x - b.x) * py - (p.y - b.y) * px);
}

function solve(ar) {
  const { a, b } = ar;
  for (const p of ar.pts) {
    const tx = -p.ny, ty = p.nx;
    let rv = relVel(a, b, p.x, p.y);
    const vt = rv[0] * tx + rv[1] * ty, lim = ar.mu * p.pn;
    let pt = Math.max(-lim, Math.min(lim, p.pt - vt * p.mt));
    const dt = pt - p.pt;
    p.pt = pt;
    apply(a, b, p, tx * dt, ty * dt);
    rv = relVel(a, b, p.x, p.y);
    const vn = rv[0] * p.nx + rv[1] * p.ny, pn = Math.max(0, p.pn + (p.bias - vn) * p.mn), dn = pn - p.pn;
    p.pn = pn;
    apply(a, b, p, p.nx * dn, p.ny * dn);
  }
  // An acorn or a pebble isn't a true ball, so it resists a roll like a nut
  // does and sits still on a rocking log.
  if (ar.roll) {
    const k = a.iI + b.iI, lim = ROLL * ar.roll * ar.pts[0].pn;
    const lr = ar.lr, l = Math.max(-lim, Math.min(lim, lr - (b.w - a.w) / k)), dl = l - lr;
    ar.lr = l;
    a.w -= a.iI * dl; b.w += b.iI * dl;
  }
}

// What overlap is left after the move is pushed out a part at a time, from
// a fresh look at the touch each pass.
function correct(ar) {
  const { a, b } = ar;
  if (a.sleep || b.sleep) return;
  const pts = contact(a, b);
  for (const p of pts) {
    const C = Math.max(-MAXC, Math.min(0, BETA * (p.sep + SLOP)));
    if (!C) continue;
    const rax = p.x - a.x, ray = p.y - a.y, rbx = p.x - b.x, rby = p.y - b.y;
    const rna = rax * p.ny - ray * p.nx, rnb = rbx * p.ny - rby * p.nx;
    const K = a.im + b.im + a.iI * rna * rna + b.iI * rnb * rnb;
    if (!K) continue;
    const j = -C / K, px = p.nx * j, py = p.ny * j;
    a.x -= px * a.im; a.y -= py * a.im; a.a -= a.iI * (rax * py - ray * px);
    b.x += px * b.im; b.y += py * b.im; b.a += b.iI * (rbx * py - rby * px);
  }
}

// ---------------------------------------------------------------- sleeping

// A heap that has sat still on one platform long enough sleeps as a piece of
// it, so nothing at rest can creep or jitter. A touch wakes the whole heap.
function sleep(L, arbs, h) {
  const par = new Map(), find = (b) => { while (par.get(b) !== b) { par.set(b, par.get(par.get(b))); b = par.get(b); } return b; };
  const on = new Map();
  for (const b of L.bodies) if (b.kind !== "plat" && !b.sleep && !b.gone) par.set(b, b);
  for (const ar of arbs) {
    const { a, b } = ar;
    if (par.has(a) && par.has(b)) par.set(find(a), find(b));
  }
  for (const ar of arbs) {
    for (const [x, p] of [[ar.a, ar.b], [ar.b, ar.a]]) {
      if (!par.has(x) || p.kind !== "plat") continue;
      const r = find(x), s = on.get(r) || new Set();
      s.add(p); on.set(r, s);
    }
  }
  const groups = new Map();
  for (const b of par.keys()) {
    const r = find(b);
    if (!groups.has(r)) groups.set(r, []);
    groups.get(r).push(b);
  }
  for (const [r, g] of groups) {
    const ps = on.get(r);
    let p = null, ok = !!ps && ps.size === 1;
    if (ok) p = [...ps][0];
    for (const b of g) {
      if (b.kind === "frog") { ok = false; b.still = 0; continue; }
      const rv = p ? relVel(p, b, b.x, b.y) : [b.vx, b.vy], dw = b.w - (p ? p.w : 0);
      if (Math.hypot(rv[0], rv[1]) < SLEEPV && Math.abs(dw) < 0.15) b.still += h; else b.still = 0;
      if (b.still < SLEEPT) ok = false;
    }
    if (!ok) continue;
    const isl = ++L.isl;
    for (const b of g) {
      const dx = b.x - p.x, dy = b.y - p.y, c = Math.cos(p.a), s = Math.sin(p.a);
      b.sleep = true; b.isl = isl; b.rest = { p, x: c * dx + s * dy, y: -s * dx + c * dy, a: b.a - p.a };
      ride(b);
    }
  }
}

function wake(L, isl) {
  L.woke = true;
  for (const b of L.bodies) if (b.sleep && b.isl === isl) { b.sleep = false; b.still = 0; b.rest = null; }
}

function wakeAll(L) {
  for (const b of L.bodies) if (b.sleep) { b.sleep = false; b.still = 0; b.rest = null; }
}

// ---------------------------------------------------------------- the water

// Anything below the waterline has gone in, and an acorn in the pond is as
// cleared as one knocked open.
function sweep(w, L) {
  let gone = false;
  for (const b of L.bodies) {
    if (b.kind === "plat" || b.gone) continue;
    if (b.broken) { b.gone = true; gone = true; continue; }
    const wet = b.y > WATER + 2, off = b.x < -30 || b.x > W + 30;
    if (!wet && !off) continue;
    b.gone = true; gone = true;
    if (b.kind === "frog") {
      b.done = true;
      w.events.push({ e: "splash", x: b.x, kind: "frog", frog: b.frog, kid: b.kid, off });
      if (!b.brushed && !b.kid) w.events.push({ e: "miss", x: Math.max(10, Math.min(W - 10, b.x)), y: WATER - 8 });
      continue;
    }
    if (!w.quietSettle) {
      w.events.push({ e: "splash", x: Math.max(4, Math.min(W - 4, b.x)), kind: b.kind, mat: b.mat, a: b.a, hx: b.hx, hy: b.hy, r: b.r, gold: b.gold, off, vx: b.vx });
      if (b.kind === "acorn") clearNut(w, L, b);
      else { add(w, L, SPLASH); w.counts.splashed++; }
    } else if (b.kind === "acorn") L.acorns--;
  }
  if (gone) {
    L.bodies = L.bodies.filter((b) => !b.gone || b.kind === "frog");
    wakeAll(L);
  }
}

// ---------------------------------------------------------------- frogs

// A frog is done once it's in the water, or once it has sat still a moment,
// when it hops off into the pond by itself and swims back.
function frogs(w, L, h) {
  for (const b of L.flying) {
    if (b.gone || b.done) continue;
    b.age += h;
    b.puff = Math.max(0, b.puff - h);
    if (L.trail && !b.kid && Math.floor(b.age / STEP) % 3 === 0) L.trail.push({ x: b.x, y: b.y });
    const v = Math.hypot(b.vx, b.vy);
    b.restT = v < 12 ? b.restT + h : 0;
    if (b.restT > 0.6 || b.age > 7) {
      b.done = true; b.gone = true;
      if (!b.brushed && !b.kid) w.events.push({ e: "miss", x: b.x, y: b.y });
      w.events.push({ e: "hop", x: b.x, y: b.y, frog: b.frog, kid: b.kid });
    }
  }
  if (L.flying.some((b) => b.gone)) L.bodies = L.bodies.filter((b) => !(b.kind === "frog" && b.gone));
}

// ---------------------------------------------------------------- the level's flow

function quiet(L) {
  return L.bodies.every((b) => b.kind === "plat" || b.sleep || b.gone || (Math.hypot(b.vx, b.vy) < 15 && Math.abs(b.w) < 1));
}

function flow(w, L, dt) {
  L.st_t += dt;
  const done = !L.flying.some((b) => !b.done);
  // Three pieces gone in a breath is a collapse, and it goes slow a moment.
  L.breaks = L.breaks.filter((t) => L.t - t < 0.35);
  if (L.breaks.length >= 3 && !L.slowed) { L.slowed = true; L.slow = 0.7; w.events.push({ e: "collapse" }); }
  if (!L.breaks.length) L.slowed = false;
  if (L.st === "intro" && L.st_t > 1.1) to(L, "ready");
  else if (L.st === "ready" && L.acorns <= 0) to(L, "clear");
  else if (L.st === "fly" && done) {
    if (L.acorns <= 0) to(L, "clear");
    else if (L.frogs.length) to(L, "load");
    else to(L, "settle");
  } else if (L.st === "load" && L.st_t > 0.5) to(L, L.acorns <= 0 ? "clear" : "ready");
  else if (L.st === "settle") {
    if (L.acorns <= 0) to(L, "clear");
    else if ((quiet(L) && L.st_t > 0.8) || L.st_t > 3.5) tally(w, L, false);
  } else if (L.st === "clear" && ((quiet(L) && L.st_t > 1) || L.st_t > 2.5)) tally(w, L, true);
  else if (L.st === "tally" && L.st_t > 1.4 + 0.4 * L.spare) {
    to(L, "card");
    w.events.push({ e: "card", stars: L.stars });
  } else if (L.st === "card" && L.st_t > 2) {
    if (w.lv + 1 < LEVELS) begin(w, w.lv + 1);
    else { w.st = "done"; w.done_t = 0; w.events.push({ e: "end" }); }
  }
  if (w.st === "done") w.done_t += dt;
}

function to(L, st) { L.st = st; L.st_t = 0; }

// The stars a level asks for, the acorns for one and spare frogs and a fair
// bit broken for the rest.
export function bar(slot, def) {
  const nuts = def.parts.filter((p) => p.mat === "acorn").length * ACORN;
  const blocks = def.parts.reduce((s, p) => s + (p.mat === "acorn" ? 0 : MATS[p.mat].pts), 0);
  const n = POOL[slot].frogs.length;
  return [nuts, nuts + Math.round(blocks * 0.2 / 5) * 5 + SPARE * Math.max(1, n - 2), nuts + Math.round(blocks * 0.3 / 5) * 5 + SPARE * (n - 1)];
}

function tally(w, L, clear) {
  L.spare = clear ? L.frogs.length : 0;
  add(w, L, SPARE * L.spare);
  const b = bar(w.lv, L.def);
  L.stars = clear ? 1 + (L.score >= b[1]) + (L.score >= b[2]) : 0;
  const c = w.counts;
  c.stars += L.stars; c.spare += L.spare;
  if (clear) c.cleared++;
  if (L.stars === 3) c.three++;
  if (clear && L.flung === 1) c.oneshot++;
  w.results.push({ name: L.def.name, score: L.score, stars: L.stars, clear, spare: L.spare });
  w.events.push({ e: "tally", clear, spare: L.spare, stars: L.stars });
  to(L, "tally");
}

export function counts(w) { return Object.assign({}, w.counts); }

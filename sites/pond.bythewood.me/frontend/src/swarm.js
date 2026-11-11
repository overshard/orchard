import { cells, colours, draw, W as FW, H as FH } from "./frog.js";
import { feel } from "./feel.js";
import { reduced } from "./api.js";
import { sfx } from "./sound.js";

// Swarm season. The frog is steered round a big lily pad and its tongue snaps
// the nearest bug by itself. Every fifteen seconds the round stops for a pick
// of one perk from three, and the perks stack.
const W = 200, H = 300, ROUND = 90, EVERY = 15, LILIES = 3, TOP = 3;
const PAD = { x: 100, y: 158, r: 88 }, ROAM = 72, BODY = 10, STEER = 110;
const REACH = 50, COOL = 0.55, OUT = 0.07, BACK = 0.1, SAFE = 1.4, SLOW = 0.9, CAP = 170;
const FONT = "700 13px 'Space Grotesk', sans-serif", NAME = "700 10px 'Space Grotesk', sans-serif";
const SMALL = "500 8px 'JetBrains Mono', monospace";
const KINDS = {
  gnat: { hp: 1, pts: 1, dew: 0.3, v: 16, r: 4 },
  fly: { hp: 1, pts: 1, dew: 1, v: 26, r: 6 },
  moth: { hp: 2, pts: 2, dew: 2, v: 14, r: 8 },
  wasp: { hp: 1, pts: 3, dew: 2, v: 30, r: 7, harm: true },
  beetle: { hp: 5, pts: 6, dew: 4, v: 12, r: 8, harm: true },
};
const PERKS = {
  tongue: { name: "Long tongue", line: "Snaps from further off" },
  quick: { name: "Quick snap", line: "Snaps come faster" },
  twin: { name: "Twin snap", line: "One more bug a snap" },
  flop: { name: "Belly flop", line: "A thump knocks bugs back" },
  bud: { name: "Lily bud", line: "Grows one more lily" },
  firefly: { name: "Firefly pal", line: "Circles you and zaps bugs" },
  dew: { name: "Dew magnet", line: "Pulls the dew drops in" },
  gulp: { name: "Big gulp", line: "Snaps catch bugs nearby" },
};
// Each beetle stops, sights the frog along a dotted line for AIM and then runs it.
const AIM = 0.8, CHARGE = 150;
const CARD = { x: 8, w: 184, h: 34, gap: 6 };

let G = null, spr = null, col = null, bg = null;
const F = feel();

function rnd(a, b) { return a + Math.random() * (b - a); }

function lv(k) { return G.perks[k] || 0; }
function reach() { return REACH + 16 * lv("tongue"); }
function cool() { return COOL * Math.pow(0.74, lv("quick")); }
function pickup() { return [12, 30, 46, 64][lv("dew")]; }
function mouth() { return { x: G.x, y: G.y - 5 }; }

function noise(kind, gap, streak) {
  if (G.real - (G.sounds[kind] || -9) < gap) return;
  G.sounds[kind] = G.real;
  sfx(kind, streak);
}

// edge is a spot just off the field on one side, 0 to 3 from the top round.
function edge(side) {
  if (side === 0) return { x: rnd(10, W - 10), y: -8 };
  if (side === 1) return { x: W + 8, y: rnd(30, H - 30) };
  if (side === 2) return { x: rnd(10, W - 10), y: H + 8 };
  return { x: -8, y: rnd(30, H - 30) };
}

function spawn(k, at) {
  if (G.bugs.length >= CAP) return null;
  const kind = KINDS[k], p = at || edge(Math.floor(Math.random() * 4));
  const b = { k, kind, x: p.x, y: p.y, vx: 0, vy: 0, kx: 0, ky: 0, hp: kind.hp, t: rnd(0, 6), ph: rnd(0, 6), stun: 0, zap: 0, hurt: 0, st: "walk", st_t: rnd(2, 3.5), aim: null };
  G.bugs.push(b);
  return b;
}

function trickle(p) {
  const r = Math.random(), wasp = G.t > 8 ? 0.04 + 0.1 * p : 0, moth = G.t > 12 ? 0.18 : 0;
  spawn(r < wasp ? "wasp" : r < wasp + moth ? "moth" : r < wasp + moth + 0.42 ? "fly" : "gnat");
}

// A wave comes off one side as a cloud, with its water stirring first.
function wave(w, p) {
  const at = edge(w.side), n = Math.round(6 + 10 * p);
  for (let i = 0; i < n; i++) spawn("gnat", { x: at.x + rnd(-14, 14), y: at.y + rnd(-14, 14) });
  for (let i = 0; i < 2 + Math.round(4 * p); i++) spawn("fly", { x: at.x + rnd(-18, 18), y: at.y + rnd(-18, 18) });
  for (let i = 0; i < Math.floor(2.6 * p); i++) spawn("wasp", { x: at.x + rnd(-20, 20), y: at.y + rnd(-20, 20) });
  if (p > 0.5) for (let i = 0; i < 2; i++) spawn("moth", { x: at.x + rnd(-16, 16), y: at.y + rnd(-16, 16) });
}

function offer() {
  const open = Object.keys(PERKS).filter((k) => lv(k) < TOP);
  for (let i = open.length - 1; i > 0; i--) { const j = Math.floor(Math.random() * (i + 1)); [open[i], open[j]] = [open[j], open[i]]; }
  return open.slice(0, 3);
}

function hit(b, n, from) {
  if (b.dead) return false;
  b.hp -= n; b.hurt = 0.12;
  if (b.hp > 0) {
    const d = Math.hypot(b.x - from.x, b.y - from.y) || 1;
    b.kx += (b.x - from.x) / d * 50; b.ky += (b.y - from.y) / d * 50;
    if (b.st === "aim") { b.st = "walk"; b.st_t = 1.2; b.aim = null; }
    return false;
  }
  kill(b);
  return true;
}

function kill(b) {
  b.dead = true;
  G.score += b.kind.pts; G.bugsN++;
  if (b.k === "wasp") G.wasps++;
  if (b.k === "beetle") G.beetles++;
  let n = Math.floor(b.kind.dew) + (Math.random() < b.kind.dew % 1 ? 1 : 0);
  while (n-- > 0) drop(b.x, b.y);
  F.burst(b.x, b.y, b.kind.harm ? "#ffb000" : "#ffe27a", b.kind.harm ? 8 : 3);
  if (b.kind.harm) F.got(b.x, b.y, "+" + b.kind.pts, { sound: "chomp", big: b.k === "beetle", c: "#ffb000" });
  else noise("pip", 0.05);
}

// Dew lands on the pad, never out on the water where the frog can't go.
function drop(x, y) {
  const a = rnd(0, Math.PI * 2), v = rnd(20, 50);
  G.drops.push({ x, y, vx: Math.cos(a) * v, vy: Math.sin(a) * v, t: 0 });
}

// A wasp or a beetle in reach counts as a little nearer than it is, so the
// tongue goes for it ahead of the gnats crowding the frog.
function near(b, m) { return Math.hypot(b.x - m.x, b.y - m.y) - (b.kind.harm ? 24 : 0); }

function snap() {
  const m = mouth(), r = reach(), taken = new Set(G.snaps.map((s) => s.b));
  const hits = G.bugs.filter((b) => !b.dead && !taken.has(b) && Math.hypot(b.x - m.x, b.y - m.y) < r)
    .sort((a, b) => near(a, m) - near(b, m))
    .slice(0, 1 + lv("twin"));
  if (!hits.length) return false;
  hits.forEach((b) => G.snaps.push({ b, t: 0, got: false, x: b.x, y: b.y }));
  noise("lick", 0.05);
  return true;
}

function snaps(dt) {
  const m = mouth(), dmg = lv("gulp") >= 2 ? 2 : 1, splash = [0, 12, 16, 22][lv("gulp")];
  G.snaps.forEach((s) => {
    s.t += dt;
    if (s.t < OUT && !s.b.dead) { s.x = s.b.x; s.y = s.b.y; }
    if (s.t >= OUT && !s.done) {
      s.done = true;
      if (!s.b.dead) {
        s.got = hit(s.b, dmg, m);
        if (splash) G.bugs.forEach((o) => { if (o !== s.b && !o.dead && Math.hypot(o.x - s.x, o.y - s.y) < splash) hit(o, 1, s); });
      }
    }
  });
  G.snaps = G.snaps.filter((s) => {
    if (s.t < OUT + BACK) return true;
    if (s.got) { F.squash(0.45); F.burst(m.x, m.y - 2, "#e2567a", 2); }
    return false;
  });
}

function flop(dt) {
  if (!lv("flop")) return;
  G.flopT -= dt;
  const r = [0, 46, 58, 70][lv("flop")];
  if (G.flopT <= 0 && G.bugs.some((b) => !b.dead && Math.hypot(b.x - G.x, b.y - G.y) < r * 0.7)) {
    G.flopT = [0, 6, 4.5, 3.5][lv("flop")];
    G.rings.push({ x: G.x, y: G.y, r: 0, max: r, t: 0, hit: new Set() });
    F.squash(1); noise("thump", 0.2);
  }
  G.rings.forEach((g) => {
    g.t += dt; g.r = g.max * Math.min(1, g.t / 0.35);
    G.bugs.forEach((b) => {
      if (b.dead || g.hit.has(b)) return;
      const d = Math.hypot(b.x - g.x, b.y - g.y);
      if (d > g.r) return;
      g.hit.add(b);
      const push = 70 + 130 * (1 - d / g.max);
      b.kx += (b.x - g.x) / (d || 1) * push; b.ky += (b.y - g.y) / (d || 1) * push;
      b.stun = 0.6;
      if (b.st !== "walk") { b.st = "walk"; b.st_t = 1.5; b.aim = null; }
      if (lv("flop") >= 2) hit(b, 1, g);
    });
  });
  G.rings = G.rings.filter((g) => g.t < 0.6);
}

function flies() {
  const n = lv("firefly"), r = [0, 24, 26, 30][n], w = n >= 3 ? 3.6 : 3;
  return Array.from({ length: n }, (_, i) => {
    const a = G.t * w + i * Math.PI * 2 / n;
    return { x: G.x + Math.cos(a) * r, y: G.y + Math.sin(a) * r * 0.8 };
  });
}

function zaps(dt) {
  G.sparks = G.sparks.filter((s) => (s.t -= dt) > 0);
  flies().forEach((f) => G.bugs.forEach((b) => {
    if (b.dead || b.zap > 0 || Math.hypot(b.x - f.x, b.y - f.y) > b.kind.r + 4) return;
    b.zap = 0.35;
    G.sparks.push({ a: f, b: { x: b.x, y: b.y }, t: 0.08 });
    noise("zap", 0.06);
    hit(b, 1, f);
  }));
}

function sting(b) {
  G.lives--; G.bumps++; G.safe = SAFE;
  F.crash(G.x, G.y - 6, { s: b.k === "wasp" ? "WASP" : "BUMP" });
  const d = Math.hypot(b.x - G.x, b.y - G.y) || 1;
  b.kx += (b.x - G.x) / d * 220; b.ky += (b.y - G.y) / d * 220; b.stun = 0.8;
  b.st = "walk"; b.st_t = 2; b.aim = null;
  if (G.lives <= 0) end("OUT OF LILIES");
}

// steer is how each kind goes after the frog. Gnats drift, flies jink, moths
// flutter, wasps come straight in and a beetle walks, stops to aim and runs.
function steer(b, dt) {
  const dx = G.x - b.x, dy = G.y - b.y, d = Math.hypot(dx, dy) || 1, fast = 1 + 0.25 * Math.min(1, G.t / ROUND);
  let tx = dx / d, ty = dy / d, v = b.kind.v * fast, turn = 3;
  if (G.st === "done") { tx = -tx; ty = -ty; v *= 2.5; }
  else if (b.k === "gnat") { tx += Math.cos(b.t * 1.3 + b.ph) * 0.9; ty += Math.sin(b.t * 1.7 + b.ph) * 0.9; turn = 1.5; }
  else if (b.k === "fly") { tx += Math.cos(b.t * 5 + b.ph) * 0.6; ty += Math.sin(b.t * 4 + b.ph) * 0.6; }
  else if (b.k === "moth") { tx += Math.sin(b.t * 3 + b.ph) * 1.1; turn = 2; }
  else if (b.k === "wasp") { tx += -ty * Math.sin(b.t * 7 + b.ph) * 0.5; ty += tx * Math.sin(b.t * 7 + b.ph) * 0.5; turn = 4; }
  else if (b.k === "beetle" && G.st === "play") {
    b.st_t -= dt;
    if (b.st === "walk" && b.st_t <= 0 && d < 150) {
      b.st = "aim"; b.st_t = AIM; b.aim = { x: G.x, y: G.y, dx: dx / d, dy: dy / d };
      noise("warn", 0.3);
    } else if (b.st === "aim") {
      v = 0;
      if (b.st_t <= 0) { b.st = "run"; b.st_t = 0.9; }
    } else if (b.st === "run") {
      tx = b.aim.dx; ty = b.aim.dy; v = CHARGE; turn = 30;
      if (b.st_t <= 0) { b.st = "walk"; b.st_t = rnd(2, 3.2); b.aim = null; }
    }
  }
  const tl = Math.hypot(tx, ty) || 1, k = Math.min(1, turn * dt);
  b.vx += (tx / tl * v - b.vx) * k; b.vy += (ty / tl * v - b.vy) * k;
}

// Bugs that pile up on each other are nudged apart, so a swarm reads as a
// crowd and not one blob.
function crowd() {
  const bs = G.bugs;
  for (let i = 0; i < bs.length; i++) for (let j = i + 1; j < bs.length; j++) {
    const a = bs[i], b = bs[j], dx = b.x - a.x, dy = b.y - a.y, r = a.kind.r + b.kind.r;
    if (Math.abs(dx) > r || Math.abs(dy) > r) continue;
    const d = Math.hypot(dx, dy) || 0.01;
    if (d >= r) continue;
    const s = (r - d) / d * 0.25;
    a.x -= dx * s; a.y -= dy * s; b.x += dx * s; b.y += dy * s;
  }
}

function move(dt) {
  if (G.st !== "play") return;
  const kx = (G.keys.right ? 1 : 0) - (G.keys.left ? 1 : 0), ky = (G.keys.down ? 1 : 0) - (G.keys.up ? 1 : 0);
  if (!kx && !ky) return;
  const l = Math.hypot(kx, ky);
  place(G.x + kx / l * STEER * dt, G.y + ky / l * STEER * dt);
}

function place(x, y) {
  const dx = x - PAD.x, dy = y - PAD.y, d = Math.hypot(dx, dy);
  const k = d > ROAM ? ROAM / d : 1, nx = PAD.x + dx * k, ny = PAD.y + dy * k;
  G.moved += Math.hypot(nx - G.x, ny - G.y);
  G.x = nx; G.y = ny;
  return k < 1;
}

function end(why) {
  if (G.st === "done") return;
  G.st = "done"; G.why = why; G.anchor = null;
}

function take(i) {
  const k = G.offer[i];
  if (!k) return;
  G.perks[k] = lv(k) + 1;
  if (k === "bud") { G.most++; G.lives = Math.min(G.most, G.lives + 1); }
  G.picks.push(k);
  G.st = "play"; G.slow = SLOW; G.safe = Math.max(G.safe, 1); G.offer = []; G.anchor = null;
  F.pop(G.x, G.y - 24, PERKS[k].name.toUpperCase(), "#ffb000");
  sfx("perk");
}

// The cards go on whichever half of the field the frog isn't on.
function cards() {
  const top = G.y >= 150, h = 3 * CARD.h + 2 * CARD.gap, y0 = top ? 16 : H - 16 - h;
  return G.offer.map((k, i) => ({ k, x: CARD.x, y: y0 + i * (CARD.h + CARD.gap), w: CARD.w, h: CARD.h }));
}

export const swarm = {
  key: "swarm", W, H, title: "SWARM SEASON", feel: F, timed: true, edge: ["#173a3a", "#173a3a"],
  aria: "Drag anywhere to steer the frog round the pad and its tongue snaps the nearest bug by itself. Every fifteen seconds tap one of three perks.",
  help: ["DRAG ANYWHERE TO STEER THE FROG", "THE TONGUE SNAPS BUGS BY ITSELF", "EVERY 15 SECONDS TAKE A PERK", "WASPS AND BEETLES COST A LILY", "TAP TO START"],

  reset(g, p) {
    const stage = ["egg", "tadpole"].includes(p.frog.stage) ? "froglet" : p.frog.stage;
    spr = cells(g, stage, { hat: p.frog.hat.shape });
    col = colours(g, p.frog.hat);
    G = {
      t: 0, real: 0, st: "play", why: "", done_t: 0, x: PAD.x, y: PAD.y + 10, anchor: null, keys: {}, moved: 0,
      bugs: [], snaps: [], drops: [], rings: [], sparks: [], sounds: {}, perks: {}, picks: [], offer: [], hover: 0,
      pick_t: 0, nextPick: EVERY, slow: 0, safe: 0, cool: 0.6, flopT: 0, next: 0.4, beetle: 20,
      waves: [6, 13, 20, 27, 34, 41, 48, 55, 62, 69, 76, 83].map((at) => ({ at, side: Math.floor(Math.random() * 4), warned: false })),
      lives: LILIES, most: LILIES, score: 0, bugsN: 0, wasps: 0, beetles: 0, dew: 0, dewRun: 0, dewAt: -9, bumps: 0,
    };
    for (let i = 0; i < 4; i++) spawn("gnat");
  },

  demo() {
    if (!G) return { x: 100, y: 230, to: [140, 200] };
    return { x: G.x, y: Math.min(H - 30, G.y + 50), to: [G.x + 40, Math.min(H - 30, G.y + 30)] };
  },

  press(x, y) {
    if (!G || G.st !== "pick" || G.pick_t < 0.3) return;
    const i = cards().findIndex((c) => x >= c.x && x <= c.x + c.w && y >= c.y && y <= c.y + c.h);
    if (i >= 0) take(i);
  },

  // A drag moves the frog by as much as the finger moved, so the thumb can be
  // anywhere and never sits on it.
  drag(x, y) {
    if (!G || G.st !== "play") return;
    const a = G.anchor;
    if (!a) { G.anchor = { fx: x, fy: y, x: G.x, y: G.y }; return; }
    if (place(a.x + x - a.fx, a.y + y - a.fy)) G.anchor = { fx: x, fy: y, x: G.x, y: G.y };
  },

  release() { if (G) G.anchor = null; },

  keys(code, down) {
    if (!G) return;
    const dir = { ArrowUp: "up", KeyW: "up", ArrowDown: "down", KeyS: "down", ArrowLeft: "left", KeyA: "left", ArrowRight: "right", KeyD: "right" }[code];
    if (G.st !== "pick") { if (dir) G.keys[dir] = down; return; }
    if (!down || G.pick_t < 0.3) return;
    const n = { Digit1: 0, Digit2: 1, Digit3: 2, Numpad1: 0, Numpad2: 1, Numpad3: 2 }[code];
    if (n !== undefined) take(n);
    else if (dir === "up" || dir === "left") G.hover = (G.hover + G.offer.length - 1) % G.offer.length;
    else if (dir === "down" || dir === "right") G.hover = (G.hover + 1) % G.offer.length;
    else if (code === "Space" || code === "Enter") take(G.hover);
  },

  step(dt) {
    G.real += dt;
    if (G.st === "pick") { G.pick_t += dt; return; }
    if (G.st === "done") G.done_t += dt;
    let k = 1;
    if (G.slow > 0) { k = 0.25 + 0.75 * (1 - G.slow / SLOW); G.slow -= dt; }
    dt *= k;
    G.safe = Math.max(0, G.safe - dt);
    move(dt);
    if (G.st === "play") {
      G.t += dt;
      const p = Math.min(1, G.t / ROUND);
      G.next -= dt;
      if (G.next <= 0) { trickle(p); G.next = 1.1 - 0.85 * p + rnd(-0.1, 0.1); }
      G.waves.forEach((w) => { if (!w.warned && G.t >= w.at - 1.2) { w.warned = true; noise("warn", 0.3); } });
      G.waves = G.waves.filter((w) => { if (G.t < w.at) return true; wave(w, p); return false; });
      if (G.t >= G.beetle) { spawn("beetle"); G.beetle += 13 - 7 * p; }
      G.cool -= dt;
      if (G.cool <= 0 && snap()) G.cool = cool();
    }
    snaps(dt);
    flop(dt);
    zaps(dt);
    G.bugs.forEach((b) => {
      b.t += dt; b.zap -= dt; b.hurt -= dt;
      if (b.stun > 0) b.stun -= dt; else steer(b, dt);
      const f = Math.exp(-4 * dt);
      b.x += (b.vx + b.kx) * dt; b.y += (b.vy + b.ky) * dt;
      b.kx *= f; b.ky *= f;
      if (b.stun > 0) { b.vx *= f; b.vy *= f; }
      if (b.dead || G.st !== "play") return;
      const d = Math.hypot(b.x - G.x, b.y - G.y), r = BODY + b.kind.r;
      if (d >= r) return;
      if (b.kind.harm && G.safe <= 0) { sting(b); return; }
      // Anything harmless the frog walks into is shouldered out of the way.
      b.x = G.x + (b.x - G.x) / (d || 1) * r; b.y = G.y + (b.y - G.y) / (d || 1) * r;
    });
    crowd();
    G.bugs = G.bugs.filter((b) => !b.dead && !(G.st === "done" && (b.x < -20 || b.x > W + 20 || b.y < -20 || b.y > H + 20)));
    const pull = pickup(), mag = lv("dew") > 0;
    G.drops.forEach((d) => {
      d.t += dt;
      const f = Math.exp(-5 * dt);
      d.x += d.vx * dt; d.y += d.vy * dt; d.vx *= f; d.vy *= f;
      const px = d.x - PAD.x, py = d.y - PAD.y, pd = Math.hypot(px, py);
      if (pd > PAD.r - 6) { d.x = PAD.x + px / pd * (PAD.r - 6); d.y = PAD.y + py / pd * (PAD.r - 6); }
      const dx = G.x - d.x, dy = G.y - d.y, dd = Math.hypot(dx, dy);
      if (mag && dd < pull && d.t > 0.3) { d.x += dx / dd * 160 * dt; d.y += dy / dd * 160 * dt; }
      if (dd < (mag ? 10 : pull) && G.st === "play" && d.t > 0.25) {
        d.got = true; G.score++; G.dew++;
        G.dewRun = G.real - G.dewAt < 0.6 ? G.dewRun + 1 : 1; G.dewAt = G.real;
        noise("dew", 0.03, Math.min(12, G.dewRun));
      }
    });
    G.drops = G.drops.filter((d) => !d.got && d.t < 9);
    if (G.st === "play" && G.t >= G.nextPick && G.t < ROUND - 2) {
      G.nextPick += EVERY;
      G.offer = offer();
      if (G.offer.length) { G.st = "pick"; G.pick_t = 0; G.hover = 0; G.anchor = null; G.keys = {}; noise("warn", 0); }
    }
    if (G.st === "play" && G.t >= ROUND) end("SEASON'S OVER");
  },

  pause() { if (G) { G.anchor = null; G.keys = {}; } },

  over() { return G && G.st === "done" && !G.snaps.length && G.done_t > 0.8; },
  score() { return G ? G.score : 0; },
  why() { return G ? G.why : ""; },
  report() {
    const maxed = Object.values(G.perks).filter((n) => n >= TOP).length;
    return { counts: { wasps: G.wasps, beetles: G.beetles, dew: G.dew, bumps: G.bumps, bugs: G.bugsN, maxed } };
  },
  tally() {
    if (!G) return [];
    const top = Object.entries(G.perks).sort((a, b) => b[1] - a[1]).slice(0, 2).map(([k, n]) => PERKS[k].name + " " + n);
    return [G.bugsN + " bugs snapped", G.dew + " dew drops", top.length ? top.join(", ") : "No perks"];
  },

  draw(x) {
    scene(x, G ? G.real : 0);
    if (!G) return;
    G.drops.forEach((d) => dewDrop(x, d.x, d.y, d.t));
    G.rings.forEach((g) => {
      x.strokeStyle = "rgba(207,227,234," + (0.7 * (1 - g.t / 0.6)).toFixed(2) + ")"; x.lineWidth = 2;
      x.beginPath(); x.ellipse(g.x, g.y, g.r, g.r * 0.8, 0, 0, Math.PI * 2); x.stroke();
    });
    warnings(x);
    const carried = new Set(G.snaps.filter((s) => s.got).map((s) => s.b));
    G.bugs.forEach((b) => { if (!carried.has(b)) bug(x, b, b.x, b.y); });
    frog(x);
    tongues(x);
    flies().forEach((f) => firefly(x, f.x, f.y, G.real));
    x.strokeStyle = "#fff6dc"; x.lineWidth = 1;
    G.sparks.forEach((s) => { x.beginPath(); x.moveTo(s.a.x, s.a.y); x.lineTo((s.a.x + s.b.x) / 2 + 2, (s.a.y + s.b.y) / 2 - 2); x.lineTo(s.b.x, s.b.y); x.stroke(); });
    F.draw(x);
    hud(x);
    if (G.st === "pick") picker(x);
  },
};

function frog(x) {
  if (G.safe > 0 && G.st === "play" && Math.floor(G.safe * 12) % 2) x.globalAlpha = 0.45;
  // The body sits low in the sprite's box, so the box goes up to centre it.
  const fx = Math.round(G.x - FW / 2), fy = Math.round(G.y - 23);
  x.fillStyle = "rgba(10,9,8,0.25)"; x.fillRect(fx + 10, fy + FH - 3, FW - 20, 3);
  F.squashed(x, G.x, fy + FH, () => {
    draw(x, spr, FW, FH, col, fx, fy, 1);
    if (G.snaps.length) { const m = mouth(); x.fillStyle = "#3a1020"; x.fillRect(Math.round(m.x) - 3, Math.round(m.y) - 1, 6, 2); }
  });
  x.globalAlpha = 1;
}

// A snap is a pink line out to the bug and back, with a kill riding it home.
function tongues(x) {
  const m = mouth();
  G.snaps.forEach((s) => {
    const k = s.t < OUT ? s.t / OUT : 1 - (s.t - OUT) / BACK, tx = m.x + (s.x - m.x) * k, ty = m.y + (s.y - m.y) * k;
    x.lineCap = "round";
    x.strokeStyle = "#9c2a52"; x.lineWidth = 3; x.beginPath(); x.moveTo(m.x, m.y); x.lineTo(tx, ty); x.stroke();
    x.strokeStyle = "#e2567a"; x.lineWidth = 1.5; x.beginPath(); x.moveTo(m.x, m.y); x.lineTo(tx, ty); x.stroke();
    x.fillStyle = "#e2567a"; x.fillRect(Math.round(tx) - 2, Math.round(ty) - 2, 4, 4);
    if (s.got) bug(x, s.b, tx, ty);
    else if (s.done && s.t < OUT + 0.05) { x.fillStyle = "#fff6dc"; x.fillRect(Math.round(s.x) - 1, Math.round(s.y) - 1, 2, 2); }
  });
}

// A wave's side of the water ripples and points in before it comes, and a
// beetle about to run draws a dotted line to where it's going.
function warnings(x) {
  const blink = reduced || Math.floor(G.real * 6) % 2;
  G.waves.forEach((w) => {
    if (!w.warned || !blink) return;
    x.fillStyle = "#7fd6c2";
    for (let i = 0; i < 5; i++) {
      const a = 30 + i * 35;
      if (w.side === 0) chevron(x, a, 6, 0, 1);
      else if (w.side === 2) chevron(x, a, H - 8, 0, -1);
      else if (w.side === 1) chevron(x, W - 6, 40 + i * 52, -1, 0);
      else chevron(x, 6, 40 + i * 52, 1, 0);
    }
  });
  G.bugs.forEach((b) => {
    if (b.st !== "aim" || !b.aim) return;
    x.fillStyle = blink ? "#ffb000" : "#e2412d";
    for (let s = 8; s < CHARGE * 0.9; s += 6) x.fillRect(Math.round(b.x + b.aim.dx * s) - 1, Math.round(b.y + b.aim.dy * s) - 1, 2, 2);
  });
}

function chevron(x, cx, cy, dx, dy) {
  for (let i = -2; i <= 2; i++) {
    const o = 2 - Math.abs(i);
    if (dy) x.fillRect(cx + i * 2, cy + dy * o * 2, 2, 2);
    else x.fillRect(cx + dx * o * 2, cy + i * 2, 2, 2);
  }
}

function hud(x) {
  x.font = FONT; x.textAlign = "left"; x.lineWidth = 3; x.strokeStyle = "#0a0908";
  x.strokeText(String(G.score), 6, 16); x.fillStyle = "#ece5d8"; x.fillText(String(G.score), 6, 16);
  for (let i = 0; i < G.most; i++) lily(x, W - 10 - i * 13, 9, i < G.lives);
  let px = 4;
  Object.keys(PERKS).forEach((k) => {
    if (!lv(k)) return;
    icon(x, k, px, H - 15, 1);
    x.font = SMALL; x.textAlign = "left"; x.fillStyle = "#ffb000"; x.fillText(String(lv(k)), px + 10, H - 8);
    px += 18;
  });
  x.fillStyle = "#0a0908"; x.fillRect(0, H - 3, W, 3);
  x.fillStyle = "#ffb000"; x.fillRect(0, H - 3, Math.round(W * Math.max(0, 1 - G.t / ROUND)), 3);
  x.fillStyle = "#0a0908";
  for (let t = EVERY; t < ROUND; t += EVERY) x.fillRect(Math.round(W * (1 - t / ROUND)), H - 3, 1, 3);
}

function picker(x) {
  x.fillStyle = "rgba(10,9,8,0.55)"; x.fillRect(0, 0, W, H);
  frog(x);
  const cs = cards(), ready = G.pick_t >= 0.3;
  x.font = SMALL; x.textAlign = "center"; x.fillStyle = "#ffb000";
  x.fillText("PICK A PERK", W / 2, cs[0].y - 4);
  cs.forEach((c, i) => {
    const n = lv(c.k), on = i === G.hover;
    x.fillStyle = "#0a0908"; x.fillRect(c.x - 1, c.y - 1, c.w + 2, c.h + 2);
    x.fillStyle = on ? "#2a2418" : "#1c1a16"; x.fillRect(c.x, c.y, c.w, c.h);
    x.fillStyle = ready ? (on ? "#ffb000" : "#7a6a48") : "#3a342a";
    x.fillRect(c.x, c.y, c.w, 1); x.fillRect(c.x, c.y + c.h - 1, c.w, 1); x.fillRect(c.x, c.y, 1, c.h); x.fillRect(c.x + c.w - 1, c.y, 1, c.h);
    x.fillStyle = "#0e1f1c"; x.fillRect(c.x + 5, c.y + 5, 24, 24);
    icon(x, c.k, c.x + 8, c.y + 10, 2);
    x.textAlign = "left"; x.font = NAME; x.fillStyle = "#ece5d8"; x.fillText(PERKS[c.k].name, c.x + 35, c.y + 14);
    x.font = SMALL; x.fillStyle = "#bcaf99"; x.fillText(PERKS[c.k].line, c.x + 35, c.y + 26);
    for (let j = 0; j < TOP; j++) { x.fillStyle = j < n ? "#ffb000" : j === n ? "#9fe08f" : "#3a342a"; x.fillRect(c.x + c.w - 26 + j * 7, c.y + 7, 5, 5); }
  });
}

// Perk icons are 9 by 7 and drawn s pixels a cell.
const ICONS = {
  tongue: ["........", "......PP.", "pppppPPPP", "pppppPPPP", "......PP.", "........."],
  quick: ["....yy...", "...yy....", "..yyyyy..", "....yy...", "...yy....", "..yy.....", ".y......."],
  twin: ["......PP.", "....pPPP.", "ppppp....", "ppppp....", "....pPPP.", "......PP."],
  flop: ["..ccccc..", ".c.....c.", "c..ggg..c", "c..ggg..c", ".c.....c.", "..ccccc.."],
  bud: ["...pp....", "..pPPp...", "...pp....", ".ggggggg.", "gggg.gggg", ".ggggggg."],
  firefly: ["y.......y", "...yyy...", "..yYYYy..", "...yyy...", "..k...k..", "y.......y"],
  dew: ["....b....", "...bbb...", "..bbWbb..", "..bbbbb..", "...bbb..."],
  gulp: ["ggggggggg", "g.......g", "gPPPPPPPg", "gPPPPPPPg", "ggggggggg"],
};
const ICOL = { p: "#9c2a52", P: "#e2567a", y: "#ffb000", Y: "#fff6dc", c: "#cfe3ea", g: "#4fa043", b: "#7fc8e8", W: "#fff6dc", k: "#3a3f46" };

function icon(x, k, ox, oy, s) {
  ICONS[k].forEach((row, ry) => [...row].forEach((ch, rx) => {
    if (ch === ".") return;
    x.fillStyle = ICOL[ch]; x.fillRect(ox + rx * s, oy + ry * s, s, s);
  }));
}

// Bugs are seen from above, so they read the same whichever way they're going,
// and drawn on a grid of 2 so they read on a phone.
const SPRITES = {
  gnat: [[".w.w.", "..k..", "..k.."], ["w...w", ".wkw.", "..k.."]],
  fly: [["..r.r..", "ww.k.ww", "wwkkkww", ".wkkkw.", "...k..."], ["..r.r..", ".w.k.w.", ".wkkkw.", "..kkk..", "...k..."]],
  moth: [["...a.a...", ".ttt.ttt.", "tttmbmttt", "ttt.b.ttt", ".tt.b.tt.", "..t...t.."], ["...a.a...", "..tt.tt..", ".ttmbmtt.", ".tt.b.tt.", "..t.b.t..", "........."]],
  wasp: [["...k.k...", "ww.kkk.ww", "wwwyyywww", ".w.kkk.w.", "...yyy...", "...kkk...", "....k...."], ["...k.k...", ".w.kkk.w.", "wwwyyywww", "...kkk...", "...yyy...", "...kkk...", "....k...."]],
  beetle: [["..l...l..", "...hhh...", "l.sssss.l", ".sSsSsss.", "lsSsSsssl", ".ssssSss.", "l.sssss.l", "...s.s..."], ["...l.l...", "...hhh...", ".lsssssl.", ".sSsSsss.", ".sSsSsss.", "lssssSssl", "..sssss..", "...s.s..."]],
};
const BCOL = {
  w: "rgba(207,227,234,0.85)", k: "#1a1a20", r: "#e2412d", t: "#d9c9a8", m: "#8a6a48", b: "#5a4630", a: "#5a4630",
  y: "#f2c230", l: "#0a0908", h: "#1a2a4a", s: "#2a3a6a", S: "#7a9ae8",
};

function bug(x, b, cx, cy) {
  const fr = SPRITES[b.k], f = fr[Math.floor(b.t * (b.k === "beetle" ? 6 : b.k === "moth" ? 7 : 18)) % 2];
  const ox = Math.round(cx - f[0].length), oy = Math.round(cy - f.length);
  const flash = b.hurt > 0, red = b.k === "beetle" && (b.st === "aim" || b.st === "run");
  f.forEach((row, ry) => [...row].forEach((ch, rx) => {
    if (ch === ".") return;
    x.fillStyle = flash ? "#fff6dc" : red && (ch === "s" || ch === "h") ? (ch === "s" ? "#8a2a1a" : "#5a1a10") : BCOL[ch];
    x.fillRect(ox + rx * 2, oy + ry * 2, 2, 2);
  }));
}

function dewDrop(x, cx, cy, t) {
  if (t > 7 && Math.floor(t * 8) % 2) return;
  const X = Math.round(cx), Y = Math.round(cy);
  x.fillStyle = "#3a7aa0"; x.fillRect(X - 1, Y - 2, 3, 5); x.fillRect(X - 2, Y - 1, 5, 3);
  x.fillStyle = "#9fdcf2"; x.fillRect(X - 1, Y - 1, 2, 2);
  if (Math.floor(t * 3 + cx) % 5 === 0) { x.fillStyle = "#fff6dc"; x.fillRect(X - 1, Y - 2, 1, 1); }
}

function firefly(x, cx, cy, t) {
  const X = Math.round(cx), Y = Math.round(cy), glow = Math.floor(t * 8) % 2;
  x.fillStyle = "rgba(255,226,122,0.25)"; x.fillRect(X - 4, Y - 4, 9, 9);
  x.fillStyle = "#3a3f46"; x.fillRect(X - 1, Y - 2, 3, 2);
  x.fillStyle = glow ? "#fff6dc" : "#ffe27a"; x.fillRect(X - 1, Y, 3, 2);
}

function lily(x, cx, cy, on) {
  x.fillStyle = on ? "#4fa043" : "#24402c";
  x.fillRect(cx - 5, cy - 2, 11, 5); x.fillRect(cx - 4, cy - 3, 9, 7);
  x.fillStyle = on ? "#173a3a" : "#122a2a"; x.fillRect(cx, cy - 3, 2, 3);
  if (on) { x.fillStyle = "#f2a0c0"; x.fillRect(cx - 2, cy - 1, 2, 2); }
}

// Midsummer on the pond seen from above, one big pad in the middle of dark
// water, painted once and kept since it never changes.
function scene(x, t) {
  if (!bg) {
    bg = document.createElement("canvas");
    bg.width = W * 2; bg.height = H * 2;
    const c = bg.getContext("2d");
    c.scale(2, 2);
    paint(c);
  }
  x.drawImage(bg, 0, 0, W, H);
  x.fillStyle = "rgba(207,227,234,0.16)";
  for (let i = 0; i < 6; i++) {
    const k = (t * 0.3 + i * 0.17) % 1, w = Math.round(3 + k * 10), wx = [24, 170, 30, 176, 100, 60][i], wy = [40, 52, 262, 250, 22, 284][i];
    x.fillRect(wx - w, wy, w * 2, 1);
  }
}

function paint(c) {
  c.fillStyle = "#173a3a"; c.fillRect(0, 0, W, H);
  c.fillStyle = "#1c4442";
  for (let i = 0; i < 40; i++) c.fillRect((i * 53) % W, (i * 97) % H, 6 + (i % 4) * 3, 1);
  pad(c, 18, 30, 14, "#2f6a2c", "#3f8a38");
  pad(c, 184, 272, 12, "#2f6a2c", "#3f8a38");
  pad(c, 176, 26, 9, "#2f6a2c", "#3f8a38");
  c.fillStyle = "rgba(10,9,8,0.35)";
  for (let y = -PAD.r; y < PAD.r; y += 2) { const w = Math.round(Math.sqrt(PAD.r * PAD.r - y * y) * 2 / 2) * 2; c.fillRect(PAD.x - w / 2 + 3, PAD.y + y + 4, w, 2); }
  pad(c, PAD.x, PAD.y, PAD.r, "#2f6a2c", "#4a9a3e");
  c.fillStyle = "#3f8a38";
  for (let a = 0; a < 12; a++) {
    const ang = a * Math.PI / 6 + 0.26;
    for (let s = 6; s < PAD.r - 6; s += 2) c.fillRect(Math.round(PAD.x + Math.cos(ang) * s), Math.round(PAD.y + Math.sin(ang) * s), 2, 2);
  }
  c.fillStyle = "#56a848"; c.fillRect(PAD.x - 3, PAD.y - 3, 6, 6);
  // A notch runs in from the rim like every lily pad's.
  c.fillStyle = "#173a3a";
  for (let s = PAD.r * 0.45; s < PAD.r + 2; s += 1) {
    const w = (s - PAD.r * 0.45) * 0.18 + 1;
    c.fillRect(Math.round(PAD.x + s * 0.7 - w / 2), Math.round(PAD.y - s * 0.71 - w / 2), Math.ceil(w), Math.ceil(w));
  }
  [[38, 210], [160, 120]].forEach(([fx, fy]) => {
    c.fillStyle = "#f2a0c0"; c.fillRect(fx - 4, fy - 2, 8, 4); c.fillRect(fx - 2, fy - 4, 4, 8);
    c.fillStyle = "#fff0f6"; c.fillRect(fx - 1, fy - 1, 2, 2);
  });
}

function pad(c, cx, cy, r, rim, face) {
  for (let y = -r; y < r; y += 2) {
    const w = Math.round(Math.sqrt(r * r - y * y)) * 2;
    c.fillStyle = rim; c.fillRect(cx - w / 2, cy + y, w, 2);
    const w2 = Math.max(0, w - 6);
    if (Math.abs(y) < r - 3) { c.fillStyle = face; c.fillRect(cx - w2 / 2, cy + y, w2, 2); }
  }
}

// The label on the Play disk.
export function swarmArt(x, g) {
  x.fillStyle = "#173a3a"; x.fillRect(0, 0, 168, 96);
  x.fillStyle = "#1c4442"; for (let i = 0; i < 14; i++) x.fillRect((i * 53) % 168, (i * 37) % 96, 6, 1);
  pad(x, 84, 52, 44, "#2f6a2c", "#4a9a3e");
  x.fillStyle = "#3f8a38";
  for (let a = 0; a < 8; a++) for (let s = 6; s < 40; s += 3) x.fillRect(Math.round(84 + Math.cos(a * 0.785 + 0.3) * s), Math.round(52 + Math.sin(a * 0.785 + 0.3) * s), 2, 2);
  draw(x, cells(g, "frog", {}), FW, FH, colours(g, null), 62, 36, 1);
  x.strokeStyle = "#9c2a52"; x.lineWidth = 3; x.lineCap = "round";
  x.beginPath(); x.moveTo(84, 54); x.lineTo(124, 30); x.stroke();
  x.strokeStyle = "#e2567a"; x.lineWidth = 1.5; x.stroke();
  const at = (k, f, bx, by) => SPRITES[k][f].forEach((row, ry) => [...row].forEach((ch, rx) => {
    if (ch !== ".") { x.fillStyle = BCOL[ch]; x.fillRect(bx + rx * 2, by + ry * 2, 2, 2); }
  }));
  at("fly", 0, 118, 24); at("wasp", 0, 16, 14); at("beetle", 0, 136, 66); at("moth", 1, 24, 62);
  [[56, 8], [70, 4], [148, 10], [10, 44], [150, 40], [110, 86]].forEach(([a, b], i) => at("gnat", i % 2, a, b));
  x.fillStyle = "rgba(255,226,122,0.3)"; x.fillRect(102, 66, 9, 9);
  x.fillStyle = "#ffe27a"; x.fillRect(105, 69, 3, 3);
  [[60, 72], [72, 80], [96, 78]].forEach(([a, b]) => { x.fillStyle = "#3a7aa0"; x.fillRect(a - 1, b - 2, 3, 5); x.fillStyle = "#9fdcf2"; x.fillRect(a - 1, b - 1, 2, 2); });
}

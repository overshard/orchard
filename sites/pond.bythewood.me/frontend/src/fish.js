import { sfx } from "./sound.js";
import { cells, colours, draw, W as FW, H as FH } from "./frog.js";

// Fishing. Tap the water to cast and the hook sinks to where you tapped. A fish
// that notices nibbles a time or two and then takes it, which is when to tap.
// Then hold to reel and let go while it runs, or the line snaps.
const W = 200, H = 320, SURF = 100, BOT = H - 14, JY = SURF - 10, ROUND = 60, GRACE = 20;
const TIP = { x: 84, y: 30 }, LAND = { x: 88, y: SURF + 4 }, LINE = 270, REEL = 100, SCHOOL = 8;
const TELL = 0.55, PERFECT = 0.3, SLACK = 1.8;

// w is how often one turns up, d how deep it swims, win how long the bite
// lasts, pull how fast the line strains when held through a run.
const KINDS = [
  { k: "minnow", one: "minnow", many: "minnows", pts: 2, w: 26, d: [0, 0.35], len: 8, c: "#9fb7c0", b: "#dfe9ec", c2: "#e9f2f4", pat: "line", v: [16, 26], win: 1.1, nib: 1, pull: 0.6, run: [0.4, 0.6], rest: [1, 1.6], rv: 32, stam: 0.6, heavy: 0.8, shy: 1.6 },
  { k: "roach", one: "roach", many: "roach", pts: 4, w: 20, d: [0.1, 0.5], len: 10, c: "#aab4b8", b: "#e6ecee", c2: "#7d8a90", fin: "#d2553a", pat: "back", v: [14, 24], win: 1, nib: 2, pull: 0.8, run: [0.4, 0.7], rest: [1, 1.6], rv: 38, stam: 1, heavy: 0.9, shy: 1.6 },
  { k: "perch", one: "perch", many: "perch", pts: 6, w: 18, d: [0.2, 0.6], len: 12, c: "#c9b04a", b: "#efe2a0", c2: "#3f5a2a", fin: "#d2553a", pat: "bars", v: [13, 22], win: 0.95, nib: 2, pull: 0.9, run: [0.5, 0.9], rest: [0.9, 1.5], rv: 44, stam: 1.4, heavy: 1, shy: 1.6 },
  { k: "bream", one: "bream", many: "bream", pts: 9, w: 13, d: [0.45, 0.85], len: 15, fat: 0.5, c: "#c9974f", b: "#ecd1a0", c2: "#8a5a2b", pat: "back", v: [10, 17], win: 0.95, nib: 2, pull: 1, run: [0.6, 1], rest: [0.9, 1.4], rv: 40, stam: 2, heavy: 1.15, shy: 1.4 },
  { k: "trout", one: "trout", many: "trout", pts: 12, w: 10, d: [0.15, 0.55], len: 16, fat: 0.34, c: "#8f9a6a", b: "#e8c9b4", c2: "#3a3324", pat: "spots", v: [20, 30], win: 0.8, nib: 1, pull: 1.2, run: [0.5, 0.9], rest: [0.7, 1.2], rv: 62, stam: 2, heavy: 1, shy: 1.2 },
  { k: "pike", one: "pike", many: "pike", pts: 15, w: 8, d: [0.35, 0.8], len: 22, fat: 0.3, c: "#6f9a4a", b: "#cfe0a8", c2: "#e9e3d6", pat: "spots", v: [18, 28], win: 0.8, nib: 1, pull: 1.4, run: [0.6, 1.1], rest: [0.7, 1.2], rv: 66, stam: 2.2, heavy: 1.1, shy: 1.2 },
  { k: "eel", one: "eel", many: "eels", pts: 16, w: 6, d: [0.7, 1], len: 26, fat: 0.14, c: "#4a4a3a", b: "#b9b08a", c2: "#2a2a20", pat: "back", v: [12, 20], win: 0.9, nib: 2, pull: 1.3, run: [0.5, 0.8], rest: [0.5, 0.9], rv: 50, stam: 2.6, heavy: 1, shy: 1.2 },
  { k: "catfish", one: "catfish", many: "catfish", pts: 18, w: 6, d: [0.85, 1], len: 19, c: "#6b5a4a", b: "#b9a58a", c2: "#2a2218", pat: "back", whisk: true, v: [7, 12], win: 1, nib: 3, pull: 1.2, run: [0.9, 1.5], rest: [0.8, 1.3], rv: 32, stam: 2.7, heavy: 1.5, shy: 1.3 },
  { k: "carp", one: "carp", many: "carp", pts: 25, w: 5, d: [0.5, 0.95], len: 26, fat: 0.46, c: "#a8853a", b: "#e6cf8a", c2: "#6e5520", pat: "scales", v: [8, 14], win: 0.9, nib: 3, pull: 1.3, run: [0.8, 1.3], rest: [0.8, 1.3], rv: 42, stam: 3.2, heavy: 1.6, shy: 1.1 },
  { k: "koi", one: "golden koi", many: "golden koi", pts: 40, w: 3, d: [0.1, 0.9], len: 14, c: "#ffd23a", b: "#fff6dc", c2: "#f07a2a", pat: "patch", v: [22, 32], win: 0.65, nib: 2, pull: 1.6, run: [0.5, 0.9], rest: [0.5, 0.9], rv: 80, stam: 2.4, heavy: 1, shy: 0.9 },
  { k: "monster", one: "monster pike", many: "monster pike", pts: 50, w: 2.5, d: [0.3, 0.8], len: 36, fat: 0.27, huge: true, c: "#56803a", b: "#c2d69a", c2: "#e9e3d6", pat: "spots", v: [14, 22], win: 0.8, nib: 1, pull: 1.6, run: [0.7, 1.2], rest: [0.7, 1.1], rv: 70, stam: 4.2, heavy: 1.9, shy: 1 },
  { k: "sturgeon", one: "sturgeon", many: "sturgeon", pts: 60, w: 2.5, d: [0.75, 1], len: 42, fat: 0.24, huge: true, c: "#5a6470", b: "#c9ced2", c2: "#d9dde0", pat: "ridge", v: [7, 12], win: 0.9, nib: 2, pull: 1.5, run: [1, 1.6], rest: [0.8, 1.3], rv: 46, stam: 5, heavy: 2.2, shy: 1 },
  { k: "wels", one: "giant catfish", many: "giant catfish", pts: 80, w: 1.5, d: [0.85, 1], len: 46, fat: 0.34, huge: true, c: "#4f463a", b: "#a8997e", c2: "#231c14", pat: "back", whisk: true, v: [5, 9], win: 0.9, nib: 3, pull: 1.7, run: [1.1, 1.8], rest: [0.8, 1.2], rv: 40, stam: 6, heavy: 2.6, shy: 0.9 },
];
const BOOT = { k: "boot", one: "old boot", many: "old boots", pts: 2, len: 10, heavy: 1.3 };

let G = null, spr = null, col = null;

const rnd = (a, b) => a + Math.random() * (b - a);

function pick() {
  let r = Math.random() * KINDS.reduce((a, b) => a + b.w, 0);
  for (const k of KINDS) { r -= k.w; if (r < 0) return k; }
  return KINDS[0];
}

function spawn(edge) {
  const sp = pick(), dir = Math.random() < 0.5 ? 1 : -1;
  const y = Math.min(BOT - sp.len * (sp.fat || 0.42) / 2 - 2, SURF + 10 + (BOT - SURF - 16) * rnd(sp.d[0], sp.d[1]));
  const x = edge ? (dir > 0 ? -sp.len : W + sp.len) : rnd(70, W - 10);
  G.fish.push({ sp, x, y, y0: y, dir, v: rnd(sp.v[0], sp.v[1]), ph: Math.random() * 6, st: "swim", t: 0, dip: 0 });
}

function pop(x, y, s, c) { G.pops.push({ x, y, s, c: c || "#ece5d8", t: 0 }); }
function ring(x) { G.rings.push({ x, t: 0 }); }

function cast(x, y) {
  if (G.taker && G.taker.st !== "hooked") flee(G.taker);
  G.taker = null;
  G.hook = { x: Math.max(LAND.x + 6, Math.min(W - 6, x)), ty: y < SURF + 8 ? SURF + 30 : Math.min(BOT - 4, y), y: TIP.y, k: 0 };
  G.st = "fly";
  sfx("cast");
}

function flee(f) {
  f.st = "flee";
  f.dir = f.x < G.hook.x ? -1 : 1;
  f.v = 80;
}

function hookIt(f) {
  G.perfect = f.t < PERFECT;
  G.st = "fight"; G.hold = true; G.ten = 0.2; G.slack = 0;
  f.st = "hooked"; f.ph = "rest"; f.pt = rnd(f.sp.rest[0], f.sp.rest[1]) * 0.6; f.stam = f.sp.stam * rnd(0.8, 1.2);
  pop(G.hook.x, SURF - 14, G.perfect ? "PERFECT" : "HOOKED", G.perfect ? "#ffb000" : "#ece5d8");
  sfx("catch");
}

function lose(why) {
  const f = G.taker;
  G.st = "lost"; G.lt = 0; G.streak = 0; G.hold = false; G.taker = null;
  if (f.boot) { G.boots.push({ x: f.x, y: BOT - 4 }); G.fish.splice(G.fish.indexOf(f), 1); } else flee(f);
  pop(f.x, f.y - 12, why, "#e2412d");
  sfx(why === "SNAP" ? "snap" : "bonk");
}

function land() {
  const f = G.taker, sp = f.sp;
  G.streak++;
  const pts = sp.pts + (G.perfect ? 2 : 0) + 2 * Math.max(0, Math.min(3, G.streak - 2));
  G.score += pts; G.caught++;
  G.bag[sp.k] = (G.bag[sp.k] || 0) + 1;
  G.st = "land"; G.lt = 0; G.hold = false;
  G.fish.splice(G.fish.indexOf(f), 1);
  G.landed = f; G.taker = null;
  pop(LAND.x + 10, SURF - 30, "+" + pts + " " + sp.one.toUpperCase(), sp.huge || sp.k === "koi" ? "#ffb000" : "#ece5d8");
  if (sp.huge) pop(LAND.x + 10, SURF - 46, "HUGE", "#ffb000");
  sfx("land");
}

function swim(f, dt) {
  f.t += dt;
  if (f.st === "swim") {
    f.x += f.dir * f.v * dt;
    f.y = f.y0 + Math.sin(f.t * 1.3 + f.ph) * 3;
    if (Math.random() < dt * 0.08) f.dir = -f.dir;
  } else if (f.st === "flee") {
    f.x += f.dir * f.v * dt;
    f.y += Math.sin(f.t * 8) * 0.4;
  } else if (f.st === "come") {
    const tx = G.hook.x - f.dir * (f.sp.len / 2 + 1), dx = tx - f.x, dy = G.hook.y - f.y, d = Math.hypot(dx, dy);
    if (Math.abs(G.hook.x - f.x) > 4) f.dir = G.hook.x > f.x ? 1 : -1;
    const s = Math.min(d, Math.max(40, f.v * 2.5) * dt);
    if (d < 1.5) { f.st = "nib"; f.n = Math.floor(Math.random() * (f.sp.nib + 1)); f.gap = rnd(0.5, 0.9); }
    else { f.x += dx / d * s; f.y += dy / d * s; }
  } else if (f.st === "nib") {
    f.dip = Math.max(0, f.dip - dt);
    f.gap -= dt;
    if (f.gap <= 0) {
      if (f.n > 0) { f.n--; f.dip = 0.3; f.gap = rnd(0.6, 1); sfx("plop"); }
      else { f.st = "bite"; f.t = 0; sfx("bite"); ring(G.hook.x); }
    }
  } else if (f.st === "bite") {
    if (f.t > f.sp.win) { flee(f); G.taker = null; pop(G.hook.x, SURF - 12, "MISSED", "#bcaf99"); }
  }
}

function fight(dt) {
  const f = G.taker, sp = f.sp, dx = LAND.x - f.x, dy = LAND.y - f.y, d = Math.hypot(dx, dy) || 1;
  if (!f.boot) {
    f.pt -= dt;
    if (f.ph === "rest" && f.pt <= 0 && f.stam > 0) { f.ph = "tell"; f.pt = TELL; sfx("hop"); }
    else if (f.ph === "tell" && f.pt <= 0) {
      const a = Math.atan2(-dy, -dx) + rnd(-0.8, 0.8);
      f.ph = "run"; f.pt = rnd(sp.run[0], sp.run[1]); f.rx = Math.cos(a); f.ry = Math.sin(a);
    } else if (f.ph === "run") {
      f.stam -= dt;
      if (f.pt <= 0 || f.stam <= 0) { f.ph = "rest"; f.pt = rnd(sp.rest[0], sp.rest[1]); }
    }
  }
  if (f.ph === "run") {
    const v = sp.rv * (G.hold ? 0.3 : 1);
    f.x += f.rx * v * dt; f.y += f.ry * v * dt;
    f.dir = f.rx > 0 ? 1 : -1;
    G.ten = Math.max(0.12, G.ten + (G.hold ? sp.pull : -1.3) * dt);
    G.slack = 0;
    if (f.y < SURF + 8 && Math.random() < dt * 10) ring(f.x);
  } else if (G.hold) {
    const s = Math.min(d, REEL / sp.heavy * dt);
    f.x += dx / d * s; f.y += dy / d * s;
    f.dir = dx > 0 ? 1 : -1;
    G.ten += (0.38 - G.ten) * Math.min(1, dt * 3);
    G.slack = 0;
    G.tick -= dt;
    if (G.tick <= 0) { G.tick = 0.07; sfx("reel"); }
  } else {
    f.x -= dx / d * 8 * dt; f.y -= dy / d * 8 * dt;
    G.ten = Math.max(0, G.ten - 0.8 * dt);
    if (G.ten === 0) G.slack += dt;
  }
  f.x = Math.max(4, Math.min(W - 4, f.x));
  f.y = Math.max(SURF + 3, Math.min(BOT - 3, f.y));
  if (G.ten >= 1) lose("SNAP");
  else if (G.slack > SLACK) lose("SHOOK OFF");
  else if (Math.hypot(f.x - TIP.x, f.y - TIP.y) > LINE) lose("LINE OUT");
  else if (Math.hypot(LAND.x - f.x, LAND.y - f.y) < 6) land();
}

export const fish = {
  key: "fish", W, H, title: "FISHING",
  help: ["TAP THE WATER TO CAST", "TAP WHEN THE BOBBER GOES UNDER", "HOLD TO REEL, LET GO WHEN IT RUNS", "TAP TO START"],

  reset(g, p) {
    const stage = ["egg", "tadpole"].includes(p.frog.stage) ? "froglet" : p.frog.stage;
    spr = cells(g, stage, { hat: p.frog.hat.shape });
    col = colours(g, p.frog.hat);
    G = { t: 0, score: 0, streak: 0, caught: 0, st: "ready", hook: { x: TIP.x, y: TIP.y, ty: 0, k: 1 }, fish: [], boots: [], taker: null, landed: null,
      hold: false, ten: 0, slack: 0, tick: 0, lt: 0, perfect: false, pops: [], rings: [], bag: {}, why: "", end: false, aim: { x: 150, y: SURF + 90 }, kb: false };
    for (let i = 0; i < SCHOOL; i++) spawn(false);
    if (Math.random() < 0.6) G.boots.push({ x: rnd(100, W - 20), y: BOT - 4 });
  },

  press(x, y) {
    if (!G || G.end) return;
    if (G.st === "fight") { G.hold = true; return; }
    if (G.st === "land" || G.st === "lost" || G.st === "fly") return;
    if (G.t >= ROUND) return;
    const f = G.taker;
    if (G.st === "wait" && f && f.st === "bite") { hookIt(f); return; }
    if (G.st === "wait" && f && f.st === "nib") { flee(f); G.taker = null; pop(G.hook.x, SURF - 12, "TOO SOON", "#bcaf99"); return; }
    cast(x, y);
  },

  release() { if (G) G.hold = false; },

  keys(code, down) {
    if (!G) return;
    const step = { ArrowLeft: [-12, 0], KeyA: [-12, 0], ArrowRight: [12, 0], KeyD: [12, 0], ArrowUp: [0, -12], KeyW: [0, -12], ArrowDown: [0, 12], KeyS: [0, 12] }[code];
    if (step && down) {
      G.kb = true;
      G.aim.x = Math.max(LAND.x + 6, Math.min(W - 6, G.aim.x + step[0]));
      G.aim.y = Math.max(SURF + 8, Math.min(BOT - 4, G.aim.y + step[1]));
    }
    if (code === "Space" || code === "Enter") {
      G.kb = true;
      if (down) fish.press(G.aim.x, G.aim.y); else fish.release();
    }
  },

  step(dt) {
    G.t += dt;
    G.fish.forEach((f) => swim(f, dt));
    G.fish = G.fish.filter((f) => f.st === "hooked" || (f.x > -30 && f.x < W + 30));
    if (G.fish.filter((f) => f.st !== "flee").length < SCHOOL) spawn(true);
    const h = G.hook;
    if (G.st === "fly") {
      h.k = Math.min(1, h.k + dt / 0.4);
      if (h.k >= 1) { G.st = "sink"; h.y = SURF; ring(h.x); sfx("plop"); }
    } else if (G.st === "sink") {
      h.y = Math.min(h.ty, h.y + 220 * dt);
      const boot = G.boots.find((b) => Math.abs(b.x - h.x) < 8 && Math.abs(b.y - h.y) < 6);
      if (boot) {
        G.boots.splice(G.boots.indexOf(boot), 1);
        const f = { sp: BOOT, boot: true, x: boot.x, y: boot.y, dir: -1, st: "hooked", ph: "rest", t: 0 };
        G.fish.push(f); G.taker = f; G.perfect = false;
        G.st = "fight"; G.hold = false; G.ten = 0.3; G.slack = -1;
        pop(h.x, SURF - 14, "SNAGGED", "#bcaf99");
      } else if (h.y >= h.ty) G.st = "wait";
    } else if (G.st === "wait" && !G.taker) {
      for (const f of G.fish) {
        if (f.st !== "swim" || Math.abs(f.x - h.x) > 70 || Math.abs(f.y - h.y) > 45) continue;
        if (Math.random() < dt * f.sp.shy * 1.8) { f.st = "come"; G.taker = f; break; }
      }
    } else if (G.st === "fight") fight(dt);
    else if (G.st === "land" || G.st === "lost") {
      G.lt += dt;
      if (G.lt > (G.st === "land" ? 0.9 : 0.7)) { G.st = "ready"; G.landed = null; G.hook = { x: TIP.x, y: TIP.y, ty: 0, k: 1 }; }
    }
    G.rings.forEach((r) => { r.t += dt; });
    G.rings = G.rings.filter((r) => r.t < 0.9);
    G.pops.forEach((p) => { p.t += dt; p.y -= dt * 14; });
    G.pops = G.pops.filter((p) => p.t < 1.3);
    const busy = G.st === "fight" || G.st === "land";
    if ((G.t >= ROUND && !busy) || G.t >= ROUND + GRACE) { G.end = true; G.why = G.caught ? "TIME'S UP" : "NOTHING BIT"; }
  },

  over() { return G && G.end; },
  score() { return G ? G.score : 0; },
  why() { return G ? G.why : ""; },

  tally() {
    if (!G) return [];
    const got = [...KINDS, BOOT].filter((k) => G.bag[k.k]).map((k) => G.bag[k.k] + " " + (G.bag[k.k] > 1 ? k.many : k.one));
    return got.length ? [got.join(", ")] : [];
  },

  draw(x, now) {
    const t = G ? G.t : now / 1000;
    scene(x, t);
    if (!G) return;
    G.boots.forEach((b) => drawBoot(x, b.x, b.y));
    G.fish.forEach((f) => {
      if (f === G.taker && G.st === "fight") return;
      x.globalAlpha = f.st === "flee" ? 0.7 : 1;
      drawFish(x, f.sp, f.x, f.y, f.dir, f.t, false);
      x.globalAlpha = 1;
    });
    draw(x, spr, FW, FH, col, 6, JY - FH + 3, 1);
    rod(x);
    const h = G.hook, f = G.taker;
    if (G.st === "fly") {
      const k = h.k, hx = TIP.x + (h.x - TIP.x) * k, hy = TIP.y + (SURF - TIP.y) * k - Math.sin(Math.PI * k) * 30;
      line(x, TIP.x, TIP.y, hx, hy, "#e9e3d6");
      bobber(x, hx, hy, false);
    } else if (G.st === "sink" || G.st === "wait") {
      const bite = f && f.st === "bite", dip = bite ? 11 : f && f.dip > 0 ? 3 : 0;
      sag(x, TIP.x, TIP.y, h.x, SURF - 1 + Math.min(dip, 4));
      x.fillStyle = "rgba(233,227,214,0.35)"; x.fillRect(Math.round(h.x), SURF + 2, 1, Math.max(0, Math.round(h.y - SURF - 2)));
      x.fillStyle = "#e2567a"; x.fillRect(Math.round(h.x) - 1, Math.round(h.y), 2, 2);
      bobber(x, h.x, SURF - 1 + dip, bite);
      if (bite) strike(x, h.x, 1 - f.t / f.sp.win);
    } else if (G.st === "fight" && f) {
      const mx = f.x + f.dir * f.sp.len / 2, danger = G.ten > 0.82 && Math.floor(G.t * 14) % 2;
      line(x, TIP.x, TIP.y, mx, f.y, G.ten > 0.82 ? (danger ? "#e2412d" : "#ffb000") : G.ten > 0.6 ? "#ffb000" : "#e9e3d6");
      if (f.boot) drawBoot(x, f.x, f.y);
      else drawFish(x, f.sp, f.x, f.y, f.dir, f.t * (f.ph === "run" ? 3 : 1), f.ph === "tell" && Math.floor(G.t * 20) % 2);
      if (f.ph === "tell") {
        const ty = Math.round(f.y - f.sp.len * (f.sp.fat || 0.42) / 2) - 16, tx = Math.round(f.x) - 2;
        x.fillStyle = "#0a0908"; x.fillRect(tx - 2, ty - 2, 8, 17);
        x.fillStyle = "#e2412d"; x.fillRect(tx, ty, 4, 8); x.fillRect(tx, ty + 10, 4, 3);
      }
      meter(x);
    } else if (G.st === "land" && G.landed) {
      const k = Math.min(1, G.lt / 0.3), lx = LAND.x + (TIP.x - LAND.x) * k, ly = LAND.y + (TIP.y + 22 - LAND.y) * k;
      line(x, TIP.x, TIP.y, lx, ly - 4, "#e9e3d6");
      const sp = G.landed.sp;
      if (G.landed.boot) drawBoot(x, lx, ly);
      else drawFish(x, sp, lx, ly, Math.floor(G.lt * 10) % 2 ? 1 : -1, G.lt * 4, false);
    } else {
      line(x, TIP.x, TIP.y, TIP.x, TIP.y + 4, "#e9e3d6");
      bobber(x, TIP.x, TIP.y + 14, false);
    }
    if (G.kb && G.st !== "fight") {
      x.fillStyle = "#ffb000";
      x.fillRect(G.aim.x - 4, G.aim.y, 3, 1); x.fillRect(G.aim.x + 2, G.aim.y, 3, 1);
      x.fillRect(G.aim.x, G.aim.y - 4, 1, 3); x.fillRect(G.aim.x, G.aim.y + 2, 1, 3);
    }
    G.rings.forEach((r) => {
      const s = Math.round(3 + r.t * 16);
      x.fillStyle = "rgba(207,227,234," + (0.8 - r.t * 0.8) + ")";
      x.fillRect(Math.round(r.x) - s, SURF, s - 1, 1); x.fillRect(Math.round(r.x) + 2, SURF, s - 1, 1);
    });
    x.font = "700 13px 'Space Grotesk', sans-serif"; x.textAlign = "center";
    x.lineWidth = 3; x.strokeStyle = "#0a0908";
    G.pops.forEach((p) => { x.strokeText(p.s, p.x, p.y); x.fillStyle = p.c; x.fillText(p.s, p.x, p.y); });
    hint(x);
    // The top row is left as sky, since a tall phone stretches it into the gap above.
    x.fillStyle = "#0a0908"; x.fillRect(0, 1, W, 4);
    x.fillStyle = G.t > ROUND ? "#e2412d" : "#ffb000"; x.fillRect(0, 1, Math.round(W * Math.max(0, 1 - G.t / ROUND)), 3);
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.font = "700 13px 'Space Grotesk', sans-serif"; x.fillText(String(G.score), 6, 20);
    x.textAlign = "right"; x.font = "500 8px 'JetBrains Mono', monospace"; x.fillStyle = "#bcaf99";
    if (G.t > ROUND && G.st === "fight") x.fillText("LAST FISH", W - 6, 18);
    else if (G.streak > 1) x.fillText(G.streak + " IN A ROW", W - 6, 18);
  },
};

// The first fish of a run gets told what to do at each step.
function hint(x) {
  if (G.caught > 0) return;
  const f = G.taker;
  let s = "";
  if (G.st === "ready") s = "TAP THE WATER TO CAST";
  else if (G.st === "wait" && f && f.st === "bite") s = "TAP NOW";
  else if (G.st === "wait") s = "WAIT FOR THE BOBBER TO GO UNDER";
  else if (G.st === "fight" && f && (f.ph === "tell" || f.ph === "run")) s = "IT'S RUNNING, LET GO";
  else if (G.st === "fight") s = "HOLD TO REEL IT IN";
  if (!s) return;
  x.font = "500 8px 'JetBrains Mono', monospace"; x.textAlign = "center";
  x.lineWidth = 3; x.strokeStyle = "#0a0908"; x.strokeText(s, W / 2, H - 22);
  x.fillStyle = "#ece5d8"; x.fillText(s, W / 2, H - 22);
}

function scene(x, t) {
  ["#2a5163", "#33606e", "#3f6f7a", "#56827f"].forEach((c, i) => { x.fillStyle = c; x.fillRect(0, i * 22, W, 22); });
  x.fillStyle = "#56827f"; x.fillRect(0, 88, W, SURF - 88);
  x.fillStyle = "#1f4a30";
  for (let i = 0; i < W; i += 4) { const h = 10 + ((i * 37) % 11) + ((i * 13) % 7); x.fillRect(i, SURF - h, 4, h); }
  x.fillStyle = "#2f5a3a";
  for (let i = 2; i < W; i += 6) { const h = 4 + ((i * 29) % 6); x.fillRect(i, SURF - h, 2, h); }
  const bands = ["#1d5163", "#184656", "#143c4a", "#10323e", "#0c2832"], bh = (BOT - SURF) / bands.length;
  bands.forEach((c, i) => { x.fillStyle = c; x.fillRect(0, Math.round(SURF + i * bh), W, Math.ceil(bh)); });
  x.fillStyle = "rgba(127,214,194,0.08)";
  for (let i = 0; i < 4; i++) { const rx = ((i * 61 + t * 6) % (W + 40)) - 20; x.fillRect(Math.round(rx), SURF + 2, 6, BOT - SURF - 2); }
  x.fillStyle = "#2a2218"; x.fillRect(0, BOT, W, H - BOT);
  x.fillStyle = "#3a2c1e"; for (let i = 0; i < W; i += 7) x.fillRect(i, BOT, 3, 1);
  x.fillStyle = "#2f6b33";
  [26, 66, 118, 150, 186].forEach((wx, i) => {
    for (let j = 0; j < 14 + (i % 3) * 6; j += 2) x.fillRect(wx + Math.round(Math.sin(t * 1.4 + i + j * 0.2) * (j / 8)), BOT - j, 2, 2);
  });
  x.fillStyle = "#7fd6c2";
  for (let i = 0; i < W; i += 10) x.fillRect((i + Math.round(t * 8)) % W, SURF, 6, 1);
  x.fillStyle = "#4a3018"; x.fillRect(10, JY, 4, BOT - JY); x.fillRect(46, JY, 4, BOT - JY);
  x.fillStyle = "#6b4422"; x.fillRect(0, JY + 3, 60, 3);
  x.fillStyle = "#8a5a2b"; x.fillRect(0, JY, 60, 3);
  x.fillStyle = "#6b4422"; for (let i = 8; i < 60; i += 12) x.fillRect(i, JY, 1, 3);
}

function rod(x) {
  line(x, 34, JY - 10, TIP.x, TIP.y, "#c9974f");
  x.fillStyle = "#8a5a2b"; x.fillRect(32, JY - 12, 4, 4);
  x.fillStyle = "#9aa0a6"; x.fillRect(38, JY - 15, 3, 3);
}

function bobber(x, bx, by, under) {
  bx = Math.round(bx); by = Math.round(by);
  x.globalAlpha = under ? 0.55 : 1;
  x.fillStyle = "#0a0908"; x.fillRect(bx - 3, by - 7, 7, 9);
  x.fillStyle = "#fff6dc"; x.fillRect(bx, by - 10, 1, 3);
  x.fillStyle = "#e2412d"; x.fillRect(bx - 2, by - 6, 5, 4);
  x.fillStyle = "#fff6dc"; x.fillRect(bx - 2, by - 2, 5, 3);
  x.globalAlpha = 1;
}

// A bite pulls the float under, so the splash, the mark and the bar that
// runs down with the time left are what say to tap.
function strike(x, bx, left) {
  bx = Math.round(bx);
  const on = Math.floor(G.t * 10) % 2;
  x.fillStyle = "#cfe3ea";
  [[-7, -3], [-4, -6], [4, -6], [7, -3]].forEach(([dx, dy]) => x.fillRect(bx + dx, SURF + dy - on, 2, 2));
  x.fillStyle = "#0a0908"; x.fillRect(bx - 4, SURF - 33, 8, 19);
  x.fillStyle = on ? "#ffb000" : "#fff6dc"; x.fillRect(bx - 2, SURF - 31, 4, 10); x.fillRect(bx - 2, SURF - 19, 4, 3);
  x.fillStyle = "#0a0908"; x.fillRect(bx - 13, SURF - 41, 26, 5);
  x.fillStyle = left < 0.35 ? "#e2412d" : "#ffb000"; x.fillRect(bx - 12, SURF - 40, Math.round(24 * Math.max(0, left)), 3);
}

function line(x, x0, y0, x1, y1, c) {
  const n = Math.max(1, Math.ceil(Math.max(Math.abs(x1 - x0), Math.abs(y1 - y0))));
  x.fillStyle = c;
  for (let i = 0; i <= n; i++) x.fillRect(Math.round(x0 + (x1 - x0) * i / n), Math.round(y0 + (y1 - y0) * i / n), 1, 1);
}

function sag(x, x0, y0, x1, y1) {
  const cx = (x0 + x1) / 2, cy = Math.max(y0, y1) + 4, n = Math.ceil(Math.hypot(x1 - x0, y1 - y0) / 1.2);
  x.fillStyle = "#e9e3d6";
  for (let i = 0; i <= n; i++) {
    const s = i / n, a = (1 - s) * (1 - s), b = 2 * s * (1 - s), c = s * s;
    x.fillRect(Math.round(a * x0 + b * cx + c * x1), Math.round(a * y0 + b * cy + c * y1), 1, 1);
  }
}

// The line's strain, with the top of it marked red where it snaps.
function meter(x) {
  const mx = W - 10, top = SURF + 12, h = 100;
  x.fillStyle = "#0a0908"; x.fillRect(mx - 1, top - 1, 6, h + 2);
  x.fillStyle = "#3a1712"; x.fillRect(mx, top, 4, 16);
  const f = Math.round(Math.min(1, G.ten) * h);
  x.fillStyle = G.ten > 0.82 ? "#e2412d" : G.ten > 0.6 ? "#ffb000" : "#56b45f";
  x.fillRect(mx, top + h - f, 4, f);
  if (G.slack > 0.3 && Math.floor(G.t * 10) % 2) { x.fillStyle = "#bcaf99"; x.fillRect(mx, top + h - 2, 4, 2); }
}

function drawBoot(x, bx, by) {
  bx = Math.round(bx); by = Math.round(by);
  x.fillStyle = "#5a3f24"; x.fillRect(bx - 3, by - 8, 4, 6); x.fillRect(bx - 3, by - 3, 9, 3);
  x.fillStyle = "#2a1c10"; x.fillRect(bx - 3, by, 9, 1);
  x.fillStyle = "#8a6a44"; x.fillRect(bx - 3, by - 8, 4, 1);
}

// A fish is drawn a column at a time from the nose back, then its tail.
function drawFish(x, sp, cx, cy, dir, t, flash) {
  const L = sp.len, hh = Math.max(3, Math.round(L * (sp.fat || 0.42))), x0 = Math.round(cx), y0 = Math.round(cy), half = Math.round(L / 2);
  const at = (i) => x0 + (dir > 0 ? half - i : i - half);
  const tall = (i) => Math.max(2, Math.round(hh * Math.pow(Math.sin(Math.PI * (i + 0.6) / (L + 0.4)), 0.55)));
  for (let i = 0; i < L; i++) {
    const h = tall(i), px = at(i), top = y0 - Math.floor(h / 2), belly = Math.max(1, Math.floor(h / 3));
    x.fillStyle = flash ? "#fff6dc" : sp.c; x.fillRect(px, top, 1, h);
    if (flash) continue;
    x.fillStyle = sp.b; x.fillRect(px, top + h - belly, 1, belly);
    x.fillStyle = sp.c2;
    if (sp.pat === "line" && i > 1 && i < L - 1) x.fillRect(px, y0, 1, 1);
    else if (sp.pat === "bars" && i > 2 && i % 3 === 1) x.fillRect(px, top, 1, Math.max(1, h - belly - 1));
    else if (sp.pat === "back" && i > 1) x.fillRect(px, top, 1, 1);
    else if (sp.pat === "spots" && i > 3 && (i * 7) % 5 === 0) x.fillRect(px, top + 1 + (i % 2), 1, 1);
    else if (sp.pat === "scales" && i > 3 && h > 4) x.fillRect(px, top + 1 + (i % 2) * 2, 1, 1);
    else if (sp.pat === "ridge" && i > 4 && i % 4 === 0) { x.fillRect(px, top - 1, 1, 2); x.fillRect(px, y0, 1, 1); }
    else if (sp.pat === "patch" && i > 2 && i % 5 < 2) x.fillRect(px, top, 1, Math.ceil(h / 2));
  }
  const wag = Math.round(Math.sin(t * 12) * (sp.huge ? 2 : 1)), fins = Math.max(3, Math.round(L / 7));
  x.fillStyle = flash ? "#fff6dc" : sp.fin || sp.c;
  for (let j = 0; j < fins; j++) { const h = 2 + j * 2; x.fillRect(at(L + j), y0 - Math.floor(h / 2) + wag, 1, h); }
  if (sp.huge && !flash) { x.fillStyle = sp.c; for (let j = 0; j < 4; j++) x.fillRect(at(Math.round(L * 0.45) + j), y0 - Math.floor(tall(Math.round(L * 0.45)) / 2) - 1 - Math.min(j, 2), 1, 2); }
  const ei = L > 30 ? 4 : L > 10 ? 2 : 1;
  x.fillStyle = "#0a0908"; x.fillRect(at(ei), y0 - Math.floor(tall(ei) / 2) + 1, sp.huge ? 2 : 1, sp.huge ? 2 : 1);
  if (sp.whisk) {
    const n = sp.huge ? 6 : 3;
    x.fillStyle = sp.c2; for (let j = 1; j <= n; j++) x.fillRect(at(-j), y0 + j - 1, 1, 1);
  }
  if (sp.k === "koi" && Math.floor(t * 3) % 4 === 0) { x.fillStyle = "#fff6dc"; x.fillRect(at(4), y0 - 2, 1, 1); }
}

import { sfx } from "./sound.js";
import { cells, colours, draw, W as FW, H as FH } from "./frog.js";

// Fishing. Tap the water to cast and the hook sinks to where you tapped. A fish
// that notices nibbles a time or two and then takes it, which is when to tap.
// Then hold to reel and let go while it runs, or the line snaps.
const W = 200, H = 320, SURF = 100, BOT = H - 14, JY = SURF - 10, ROUND = 45, GRACE = 15;
const TIP = { x: 84, y: 30 }, LAND = { x: 88, y: SURF + 4 }, LINE = 270, REEL = 100, SCHOOL = 7;

const KINDS = [
  { k: "minnow", one: "minnow", many: "minnows", pts: 1, w: 30, d: [0, 0.35], len: 8, c: "#9fb7c0", b: "#dfe9ec", c2: "#e9f2f4", v: [20, 32], win: 0.5, nib: 1, pull: 0.9, run: [0.3, 0.5], rest: [0.8, 1.4], rv: 40, stam: 0.6, heavy: 0.8, shy: 1.6 },
  { k: "perch", one: "perch", many: "perch", pts: 3, w: 24, d: [0.2, 0.6], len: 12, c: "#c9b04a", b: "#efe2a0", c2: "#3f5a2a", v: [16, 26], win: 0.42, nib: 2, pull: 1.5, run: [0.4, 0.8], rest: [0.7, 1.3], rv: 55, stam: 1.4, heavy: 1, shy: 1.6 },
  { k: "bream", one: "bream", many: "bream", pts: 5, w: 16, d: [0.45, 0.85], len: 15, c: "#c9974f", b: "#ecd1a0", c2: "#8a5a2b", v: [12, 20], win: 0.42, nib: 2, pull: 1.7, run: [0.5, 0.9], rest: [0.7, 1.2], rv: 50, stam: 2.0, heavy: 1.15, shy: 1.4 },
  { k: "pike", one: "pike", many: "pike", pts: 8, w: 10, d: [0.35, 0.8], len: 21, c: "#6f9a4a", b: "#cfe0a8", c2: "#e9e3d6", v: [22, 34], win: 0.34, nib: 1, pull: 2.4, run: [0.5, 1], rest: [0.5, 1], rv: 85, stam: 2.2, heavy: 1.1, shy: 1.2 },
  { k: "catfish", one: "catfish", many: "catfish", pts: 10, w: 7, d: [0.85, 1], len: 19, c: "#6b5a4a", b: "#b9a58a", c2: "#2a2218", v: [8, 14], win: 0.45, nib: 3, pull: 2, run: [0.8, 1.4], rest: [0.6, 1.1], rv: 40, stam: 2.7, heavy: 1.5, shy: 1.3 },
  { k: "koi", one: "golden koi", many: "golden koi", pts: 20, w: 3, d: [0.1, 0.9], len: 14, c: "#ffd23a", b: "#fff6dc", c2: "#f07a2a", v: [26, 38], win: 0.28, nib: 2, pull: 2.6, run: [0.4, 0.8], rest: [0.4, 0.8], rv: 100, stam: 2.4, heavy: 1, shy: 0.9 },
];
const BOOT = { k: "boot", one: "old boot", many: "old boots", pts: 1, len: 10, heavy: 1.3 };

let G = null, spr = null, col = null;

const rnd = (a, b) => a + Math.random() * (b - a);

function pick() {
  let r = Math.random() * KINDS.reduce((a, b) => a + b.w, 0);
  for (const k of KINDS) { r -= k.w; if (r < 0) return k; }
  return KINDS[0];
}

function spawn(edge) {
  const sp = pick(), dir = Math.random() < 0.5 ? 1 : -1;
  const y = SURF + 10 + (BOT - SURF - 16) * rnd(sp.d[0], sp.d[1]);
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
  G.perfect = f.t < 0.18;
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
  const pts = sp.pts + (G.perfect ? 1 : 0) + Math.max(0, Math.min(3, G.streak - 2));
  G.score += pts; G.caught++;
  G.bag[sp.k] = (G.bag[sp.k] || 0) + 1;
  G.st = "land"; G.lt = 0; G.hold = false;
  G.fish.splice(G.fish.indexOf(f), 1);
  G.landed = f; G.taker = null;
  pop(LAND.x + 10, SURF - 30, "+" + pts + " " + sp.one.toUpperCase(), sp.k === "koi" ? "#ffb000" : "#ece5d8");
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
    if (d < 1.5) { f.st = "nib"; f.n = Math.floor(Math.random() * (f.sp.nib + 1)); f.gap = rnd(0.2, 0.45); }
    else { f.x += dx / d * s; f.y += dy / d * s; }
  } else if (f.st === "nib") {
    f.dip = Math.max(0, f.dip - dt);
    f.gap -= dt;
    if (f.gap <= 0) {
      if (f.n > 0) { f.n--; f.dip = 0.18; f.gap = rnd(0.25, 0.55); sfx("plop"); }
      else { f.st = "bite"; f.t = 0; sfx("bite"); pop(G.hook.x, SURF - 12, "!", "#ffb000"); }
    }
  } else if (f.st === "bite") {
    if (f.t > f.sp.win) { flee(f); G.taker = null; pop(G.hook.x, SURF - 12, "MISSED", "#bcaf99"); }
  }
}

function fight(dt) {
  const f = G.taker, sp = f.sp, dx = LAND.x - f.x, dy = LAND.y - f.y, d = Math.hypot(dx, dy) || 1;
  if (!f.boot) {
    f.pt -= dt;
    if (f.ph === "rest" && f.pt <= 0 && f.stam > 0) { f.ph = "tell"; f.pt = 0.3; sfx("hop"); }
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
    G.ten = Math.max(0, G.ten - 1.1 * dt);
    if (G.ten === 0) G.slack += dt;
  }
  f.x = Math.max(4, Math.min(W - 4, f.x));
  f.y = Math.max(SURF + 3, Math.min(BOT - 3, f.y));
  if (G.ten >= 1) lose("SNAP");
  else if (G.slack > 1.1) lose("SHOOK OFF");
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
      h.k = Math.min(1, h.k + dt / 0.32);
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
      if (G.lt > (G.st === "land" ? 0.6 : 0.5)) { G.st = "ready"; G.landed = null; G.hook = { x: TIP.x, y: TIP.y, ty: 0, k: 1 }; }
    }
    G.rings.forEach((r) => { r.t += dt; });
    G.rings = G.rings.filter((r) => r.t < 0.9);
    G.pops.forEach((p) => { p.t += dt; p.y -= dt * 18; });
    G.pops = G.pops.filter((p) => p.t < 0.9);
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
      bobber(x, hx, hy);
    } else if (G.st === "sink" || G.st === "wait") {
      const dip = f && f.st === "bite" ? 5 : f && f.dip > 0 ? 2 : 0;
      sag(x, TIP.x, TIP.y, h.x, SURF - 1 + dip);
      x.fillStyle = "rgba(233,227,214,0.35)"; x.fillRect(Math.round(h.x), SURF + 2, 1, Math.max(0, Math.round(h.y - SURF - 2)));
      x.fillStyle = "#e2567a"; x.fillRect(Math.round(h.x) - 1, Math.round(h.y), 2, 2);
      bobber(x, h.x, SURF - 1 + dip);
      if (f && f.st === "bite" && Math.floor(G.t * 12) % 2) { x.fillStyle = "#ffb000"; x.fillRect(Math.round(h.x) - 4, SURF - 2, 9, 1); }
    } else if (G.st === "fight" && f) {
      const mx = f.x + f.dir * f.sp.len / 2, danger = G.ten > 0.82 && Math.floor(G.t * 14) % 2;
      line(x, TIP.x, TIP.y, mx, f.y, G.ten > 0.82 ? (danger ? "#e2412d" : "#ffb000") : G.ten > 0.6 ? "#ffb000" : "#e9e3d6");
      if (f.boot) drawBoot(x, f.x, f.y);
      else drawFish(x, f.sp, f.x, f.y, f.dir, f.t * (f.ph === "run" ? 3 : 1), f.ph === "tell" && Math.floor(G.t * 20) % 2);
      if (f.ph === "tell") { x.fillStyle = "#e2412d"; x.fillRect(Math.round(f.x), Math.round(f.y) - 12, 2, 5); x.fillRect(Math.round(f.x), Math.round(f.y) - 6, 2, 2); }
      meter(x);
    } else if (G.st === "land" && G.landed) {
      const k = Math.min(1, G.lt / 0.3), lx = LAND.x + (TIP.x - LAND.x) * k, ly = LAND.y + (TIP.y + 22 - LAND.y) * k;
      line(x, TIP.x, TIP.y, lx, ly - 4, "#e9e3d6");
      const sp = G.landed.sp;
      if (G.landed.boot) drawBoot(x, lx, ly);
      else drawFish(x, sp, lx, ly, Math.floor(G.lt * 10) % 2 ? 1 : -1, G.lt * 4, false);
    } else {
      line(x, TIP.x, TIP.y, TIP.x, TIP.y + 10, "#e9e3d6");
      bobber(x, TIP.x, TIP.y + 10);
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

function bobber(x, bx, by) {
  bx = Math.round(bx); by = Math.round(by);
  x.fillStyle = "#e2412d"; x.fillRect(bx - 1, by - 3, 3, 2);
  x.fillStyle = "#fff6dc"; x.fillRect(bx - 1, by - 1, 3, 2);
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
  const L = sp.len, hh = Math.max(3, Math.round(L * 0.42)), x0 = Math.round(cx), y0 = Math.round(cy), half = Math.round(L / 2);
  const at = (i) => x0 + (dir > 0 ? half - i : i - half);
  for (let i = 0; i < L; i++) {
    const s = Math.sin(Math.PI * (i + 0.6) / (L + 0.4)), h = Math.max(2, Math.round(hh * Math.pow(s, 0.55)));
    const px = at(i), top = y0 - Math.floor(h / 2), belly = Math.max(1, Math.floor(h / 3));
    x.fillStyle = flash ? "#fff6dc" : sp.c; x.fillRect(px, top, 1, h);
    if (flash) continue;
    x.fillStyle = sp.b; x.fillRect(px, top + h - belly, 1, belly);
    x.fillStyle = sp.c2;
    if (sp.k === "minnow" && i > 1 && i < L - 1) x.fillRect(px, y0, 1, 1);
    else if (sp.k === "perch" && i > 2 && i % 3 === 1) x.fillRect(px, top, 1, Math.max(1, h - belly - 1));
    else if (sp.k === "bream" && i > 1) x.fillRect(px, top, 1, 1);
    else if (sp.k === "pike" && i > 3 && (i * 7) % 5 === 0) x.fillRect(px, top + 1 + (i % 2), 1, 1);
    else if (sp.k === "catfish") x.fillRect(px, top, 1, 1);
    else if (sp.k === "koi" && i > 2 && i % 5 < 2) x.fillRect(px, top, 1, Math.ceil(h / 2));
  }
  const wag = Math.round(Math.sin(t * 12));
  x.fillStyle = flash ? "#fff6dc" : sp.c;
  for (let j = 0; j < 3; j++) { const h = 2 + j * 2; x.fillRect(at(L + j), y0 - Math.floor(h / 2) + wag, 1, h); }
  const eye = at(L > 10 ? 2 : 1), etop = y0 - Math.floor(Math.max(2, Math.round(hh * Math.pow(Math.sin(Math.PI * 2.6 / (L + 0.4)), 0.55))) / 2);
  x.fillStyle = "#0a0908"; x.fillRect(eye, etop + 1, 1, 1);
  if (sp.k === "catfish") { x.fillStyle = sp.c2; x.fillRect(at(-1), y0, 1, 1); x.fillRect(at(-2), y0 + 1, 1, 1); x.fillRect(at(-3), y0 + 2, 1, 1); }
  if (sp.k === "koi" && Math.floor(t * 3) % 4 === 0) { x.fillStyle = "#fff6dc"; x.fillRect(at(4), y0 - 2, 1, 1); }
}

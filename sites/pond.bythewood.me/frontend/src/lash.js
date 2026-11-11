import { cells, colours, draw, W as FW, H as FH } from "./frog.js";
import { feel } from "./feel.js";
import { reduced } from "./api.js";
import { sfx } from "./sound.js";

// Tongue lash. Bugs get tossed up out of the reeds at dusk and a swipe drags
// the frog's tongue along the finger, sticking every bug it touches. Lifting
// reels it in, and everything on it is one gulp, worth more the more it held.
const W = 200, H = 300, ROUND = 75, LIVES = 3, LAUNCH = H - 50;
const REACH = 380, HOLD = 1.1, REEL = 0.16, NEAR = 8, STUNG = 0.5, REGROW = 10;
const FONT = "700 13px 'Space Grotesk', sans-serif", SMALL = "500 8px 'JetBrains Mono', monospace";
const KINDS = {
  fly: { pts: 1, g: 150, r: 10, top: [50, 150] },
  gnat: { pts: 1, g: 120, r: 8, top: [60, 150] },
  moth: { pts: 2, g: 55, r: 12, top: [50, 130] },
  dragonfly: { pts: 3, g: 80, r: 12, top: [90, 170] },
  beetle: { pts: 2, g: 320, r: 10, top: [40, 120] },
  golden: { pts: 10, g: 70, r: 12, top: [40, 64] },
  wasp: { pts: 0, g: 140, r: 7, top: [50, 150] },
};
const MIX = [["fly", 38], ["gnat", 16], ["moth", 16], ["dragonfly", 12], ["beetle", 18]];
// How long a swarm's reeds or a golden beetle's twinkle shows before it comes.
const WARN = 1;

let G = null, spr = null, col = null;
const F = feel();

function rnd(a, b) { return a + Math.random() * (b - a); }

function mouth() { return { x: W / 2, y: H - 52 }; }

function pick() {
  let r = Math.random() * MIX.reduce((a, m) => a + m[1], 0);
  for (const [k, w] of MIX) { r -= w; if (r < 0) return k; }
  return "fly";
}

// A spot in the reed beds either side of the pad.
function bed(left) { return left ? rnd(6, 62) : rnd(138, 194); }

// toss throws a bug from x0 so the top of its arc is at ax, ay, falling at g
// or its kind's own pull.
function toss(k, x0, ax, ay, g) {
  const kind = KINDS[k], fall = g || kind.g, vy = -Math.sqrt(2 * fall * (LAUNCH - ay)), up = -vy / fall;
  G.bugs.push({ k, kind, g: fall, x: x0, y: LAUNCH, vx: (ax - x0) / up, vy, t: 0, ph: rnd(0, 6), dart: rnd(0.3, 0.6), near: false });
}

function volley(p) {
  const n = 1 + Math.floor(Math.random() * (1 + 2.6 * p)), wasps = 0.08 + 0.16 * p;
  for (let i = 0; i < n; i++) {
    const left = Math.random() < 0.5, x0 = bed(left);
    let k = Math.random() < wasps ? "wasp" : pick();
    if (k === "dragonfly" && p < 0.1) k = "fly";
    const kind = KINDS[k], ay = rnd(kind.top[0], kind.top[1]);
    // A dragonfly skims across the field and the rest go up and over a bit.
    const ax = k === "dragonfly" ? (left ? rnd(130, 186) : rnd(14, 70)) : Math.max(16, Math.min(W - 16, x0 + (left ? 1 : -1) * rnd(10, 90)));
    if (k === "gnat") for (let j = 0; j < 3; j++) toss(k, x0 + j * 6, ax + rnd(-12, 12), ay + rnd(-10, 10));
    else toss(k, x0, ax, ay);
  }
}

// A swarm goes up out of one bed together so one long lash can take most of it.
function swarm(s, p) {
  const n = 6 + (p > 0.5 ? 2 : 0), cx = rnd(60, 140), cy = rnd(70, 120);
  for (let i = 0; i < n; i++) toss(["fly", "gnat", "moth", "fly"][i % 4], bed(s.left), cx + rnd(-16, 16), cy + rnd(-12, 12), 110);
  if (p > 0.45) toss("wasp", bed(s.left), cx + (Math.random() < 0.5 ? -34 : 34), cy + rnd(-10, 10), 110);
}

function plan(at, span) {
  return { at: at + rnd(-span, span), left: Math.random() < 0.5, warned: false };
}

function segDist(px, py, ax, ay, bx, by) {
  const dx = bx - ax, dy = by - ay, l = dx * dx + dy * dy;
  const k = l ? Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / l)) : 0;
  return Math.hypot(px - ax - dx * k, py - ay - dy * k);
}

function whip(x, y) {
  const m = mouth();
  G.tongue = { pts: [m, { x, y }], len: Math.hypot(x - m.x, y - m.y), age: 0, bugs: [], out: true, k: 1, full: 0 };
  G.lashes++;
  sfx("whip");
  sweep(x, y, x, y);
}

// sweep sticks whatever the tongue's tip went over between two spots.
function sweep(ax, ay, bx, by) {
  const tg = G.tongue;
  for (const b of G.bugs.slice()) {
    if (b.k === "fled") continue;
    const d = segDist(b.x, b.y, ax, ay, bx, by);
    if (d < b.kind.r) {
      G.bugs.splice(G.bugs.indexOf(b), 1);
      if (b.k === "wasp") { sting(b); return; }
      b.s = tg.len;
      tg.bugs.push(b);
      F.freeze(2);
      F.burst(b.x, b.y, b.k === "golden" ? "#ffb000" : "#ffe27a", b.k === "golden" ? 10 : 4);
      sfx("catch", tg.bugs.length);
      if (tg.bugs.length > 1) F.pop(b.x, b.y - 8, "x" + tg.bugs.length, "#ffe27a");
    } else if (b.k === "wasp" && d < b.kind.r + NEAR && !b.near) {
      b.near = true;
      F.close(b.x, b.y - 10);
    }
  }
}

function sting(b) {
  G.lives--; G.wasps++; G.streak = 0; G.stung = STUNG;
  F.crash(b.x, b.y, { s: "WASP" });
  b.vy = -160; b.vx = b.x < W / 2 ? -60 : 60; b.k = "fled"; G.bugs.push(b);
  reel();
  if (G.lives <= 0) end("OUT OF LILIES");
}

function reel() {
  const tg = G.tongue;
  if (tg && tg.out) { tg.out = false; tg.full = length(tg.pts); }
}

function length(pts) {
  let n = 0;
  for (let i = 1; i < pts.length; i++) n += Math.hypot(pts[i].x - pts[i - 1].x, pts[i].y - pts[i - 1].y);
  return n;
}

// along is the spot s along the tongue, or its tip when it's shorter than that.
function along(pts, s) {
  for (let i = 1; i < pts.length; i++) {
    const a = pts[i - 1], b = pts[i], l = Math.hypot(b.x - a.x, b.y - a.y);
    if (s <= l) return { x: a.x + (b.x - a.x) * (s / (l || 1)), y: a.y + (b.y - a.y) * (s / (l || 1)) };
    s -= l;
  }
  return pts[pts.length - 1];
}

function gulp(tg) {
  const n = tg.bugs.length, m = mouth();
  if (!n) { G.streak = 0; return; }
  let pts = 0, gold = false;
  tg.bugs.forEach((b) => {
    pts += b.kind.pts;
    if (b.k === "golden") { G.golden++; gold = true; }
    if (b.k === "dragonfly") G.dragonflies++;
  });
  G.streak++; G.long = Math.max(G.long, G.streak);
  if (G.streak % REGROW === 0 && G.lives < LIVES) { G.lives++; F.pop(W - 12 - (G.lives - 1) * 13, 26, "+1", "#9fe08f"); }
  // Two bugs in one lash pay one more, three pay three more, and so on, and a
  // long run of gulps pays a little on top.
  const combo = n * (n - 1) / 2, hot = G.streak >= 15 ? 2 : G.streak >= 5 ? 1 : 0;
  pts += combo + hot;
  G.score += pts; G.bugsEaten += n; G.combo = Math.max(G.combo, n);
  F.got(m.x, m.y - 50, (n >= 2 ? n + " IN ONE +" : "+") + pts, { sound: "gulp", streak: Math.min(12, n), big: n >= 4 || gold, c: n >= 3 || gold ? "#ffb000" : "#ece5d8" });
}

function end(why) {
  if (G.st === "done") return;
  G.st = "done"; G.why = why;
  G.held = false;
  reel();
}

export const lash = {
  key: "lash", W, H, title: "TONGUE LASH", feel: F, timed: true, edge: ["#2e2640", "#102a26"],
  aria: "Swipe across the bugs to stick them to the tongue, and lift to gulp them. Mind the wasps.",
  help: ["SWIPE THROUGH THE BUGS", "LIFT TO GULP, MORE AT ONCE PAYS MORE", "A WASP COSTS A LILY", "TEN GULPS IN A ROW GROW ONE BACK", "TAP TO START"],

  reset(g, p) {
    const stage = ["egg", "tadpole"].includes(p.frog.stage) ? "froglet" : p.frog.stage;
    spr = cells(g, stage, { hat: p.frog.hat.shape });
    col = colours(g, p.frog.hat);
    G = {
      t: 0, st: "play", why: "", done_t: 0, bugs: [], next: 0.6, tongue: null, held: false, finger: null, stung: 0,
      lives: LIVES, score: 0, streak: 0, long: 0, combo: 0, golden: 0, dragonflies: 0, wasps: 0, bugsEaten: 0, lashes: 0,
      swarms: [plan(14, 1.5), plan(32, 1.5), plan(50, 1.5), plan(66, 1)], golds: [plan(24, 6), plan(54, 7)],
    };
  },

  demo() {
    if (!G) return { x: 30, y: 140, to: [170, 110] };
    const b = G.bugs.find((q) => q.k !== "wasp" && q.y < LAUNCH - 30);
    return b ? { x: b.x - 40, y: b.y + 10, to: [b.x + 40, b.y - 10] } : { x: 30, y: 140, to: [170, 110] };
  },

  press(x, y) {
    if (!G || G.st !== "play") return;
    G.held = true; G.finger = { x, y };
    if (!G.tongue && G.stung <= 0) whip(x, y);
  },

  drag(x, y) {
    if (!G) return;
    const was = G.finger;
    G.finger = { x, y };
    const tg = G.tongue;
    if (!G.held || !tg || !tg.out) return;
    const last = tg.pts[tg.pts.length - 1], d = Math.hypot(x - last.x, y - last.y);
    if (d < 1) return;
    tg.pts.push({ x, y });
    tg.len += d;
    sweep(was ? was.x : last.x, was ? was.y : last.y, x, y);
    if (tg.out && tg.len > REACH) reel();
  },

  release() {
    if (!G) return;
    G.held = false;
    reel();
  },

  step(dt) {
    G.t += dt;
    if (G.st === "done") G.done_t += dt;
    const p = Math.min(1, G.t / ROUND), bdt = dt * (1 + 0.2 * p);
    G.stung = Math.max(0, G.stung - dt);
    if (G.st === "play") {
      G.next -= dt;
      if (G.next <= 0) { volley(p); G.next = 1.5 - 0.75 * p + rnd(-0.2, 0.2); }
      G.swarms.concat(G.golds).forEach((s) => {
        if (!s.warned && G.t >= s.at - WARN) { s.warned = true; sfx("warn"); }
      });
      G.swarms = G.swarms.filter((s) => { if (G.t < s.at) return true; swarm(s, p); return false; });
      G.golds = G.golds.filter((s) => { if (G.t < s.at) return true; toss("golden", bed(s.left), rnd(40, 160), rnd(40, 64)); return false; });
    }
    G.bugs.forEach((b) => {
      b.t += bdt;
      b.vy += b.g * bdt;
      b.x += b.vx * bdt; b.y += b.vy * bdt;
      if (b.k === "moth") b.x += Math.sin(b.t * 4 + b.ph) * 16 * bdt;
      if (b.k === "wasp") b.x += Math.sin(b.t * 9 + b.ph) * 22 * bdt;
      if (b.k === "dragonfly" && (b.dart -= bdt) <= 0) {
        b.dart = rnd(0.3, 0.6); b.vx += rnd(-40, 40); b.vy -= 24;
        if (b.x < 12) b.vx = Math.abs(b.vx); else if (b.x > W - 12) b.vx = -Math.abs(b.vx);
      }
    });
    G.bugs = G.bugs.filter((b) => !(b.vy > 0 && b.y > LAUNCH + 6) && b.x > -20 && b.x < W + 20 && b.y > -60);
    const tg = G.tongue;
    if (tg) {
      tg.age += dt;
      if (tg.out) {
        // A tip held still is still sticky, so a bug can fly into it.
        const tip = tg.pts[tg.pts.length - 1];
        sweep(tip.x, tip.y, tip.x, tip.y);
        if (tg.out && tg.age > HOLD) reel();
      } else {
        tg.k = Math.max(0, tg.k - dt / REEL);
        if (tg.k <= 0) {
          gulp(tg);
          G.tongue = null;
          if (G.held && G.finger && G.st === "play" && G.stung <= 0) whip(G.finger.x, G.finger.y);
        }
      }
    }
    if (G.st === "play" && G.t >= ROUND) end("TIME'S UP");
  },

  pause() { if (G) { G.held = false; reel(); } },

  over() { return G && G.st === "done" && !G.tongue && G.done_t > 0.6; },
  score() { return G ? G.score : 0; },
  why() { return G ? G.why : ""; },
  report() { return { counts: { combo: G.combo, golden: G.golden, dragonflies: G.dragonflies, wasps: G.wasps, bugs: G.bugsEaten } }; },
  tally() {
    if (!G) return [];
    return [G.bugsEaten + " bugs gulped", "Most in one lash " + G.combo, "Longest run of lashes " + G.long];
  },

  draw(x) {
    const t = G ? G.t : 0;
    scene(x, t);
    if (!G) return;
    warnings(x);
    G.bugs.forEach((b) => bug(x, b.k, b.x, b.y, b.t, b.vx));
    frog(x);
    tongue(x);
    F.draw(x);
    x.font = FONT; x.textAlign = "left"; x.lineWidth = 3; x.strokeStyle = "#0a0908";
    x.strokeText(String(G.score), 6, 16); x.fillStyle = "#ece5d8"; x.fillText(String(G.score), 6, 16);
    for (let i = 0; i < LIVES; i++) lily(x, W - 12 - i * 13, 8, i < G.lives);
    if (G.streak >= 2) {
      x.font = SMALL; x.textAlign = "right"; x.fillStyle = G.streak >= 5 ? "#ffb000" : "#bcaf99";
      x.fillText(G.streak + " IN A ROW", W - 6, 26);
    }
    x.fillStyle = "#0a0908"; x.fillRect(0, H - 3, W, 3);
    x.fillStyle = "#ffb000"; x.fillRect(0, H - 3, Math.round(W * Math.max(0, 1 - G.t / ROUND)), 3);
  },
};

function frog(x) {
  const fx = W / 2 - FW, fy = H - 88, m = mouth();
  if (G && G.stung > 0 && Math.floor(G.stung * 12) % 2) x.globalAlpha = 0.5;
  F.squashed(x, W / 2, fy + FH * 2, () => {
    draw(x, spr, FW, FH, col, fx, fy, 2);
    if (G && G.tongue) { x.fillStyle = "#3a1020"; x.fillRect(m.x - 4, m.y - 2, 8, 4); }
  });
  x.globalAlpha = 1;
}

// The tongue is a fat pink line from the mouth along every spot the finger
// went, reeled in from the tip with whatever it stuck carried along on it.
function tongue(x) {
  const tg = G.tongue;
  if (!tg) return;
  const show = tg.out ? Infinity : tg.full * tg.k, pts = [tg.pts[0]];
  let left = show;
  for (let i = 1; i < tg.pts.length && left > 0; i++) {
    const a = tg.pts[i - 1], b = tg.pts[i], l = Math.hypot(b.x - a.x, b.y - a.y);
    if (l <= left) { pts.push(b); left -= l; } else { pts.push(along([a, b], left)); left = 0; }
  }
  // It whips out to the finger over its first few frames.
  const grow = Math.min(1, tg.age / 0.05), m = pts[0];
  const at = (q) => (tg.out && grow < 1 ? { x: m.x + (q.x - m.x) * grow, y: m.y + (q.y - m.y) * grow } : q);
  const line = (w, c) => {
    x.strokeStyle = c; x.lineWidth = w; x.lineCap = "round"; x.lineJoin = "round";
    x.beginPath(); x.moveTo(m.x, m.y);
    pts.slice(1).forEach((q) => { const r = at(q); x.lineTo(r.x, r.y); });
    x.stroke();
  };
  line(4, "#9c2a52"); line(2, "#e2567a");
  const tip = at(pts[pts.length - 1]), end = !tg.out || tg.age > HOLD * 0.75 || tg.len > REACH * 0.8;
  x.fillStyle = end && Math.floor(tg.age * 16) % 2 ? "#ffb7c9" : "#e2567a";
  x.fillRect(Math.round(tip.x) - 3, Math.round(tip.y) - 2, 6, 5);
  x.fillStyle = "#9c2a52"; x.fillRect(Math.round(tip.x) - 3, Math.round(tip.y) + 2, 6, 1);
  tg.bugs.forEach((b) => {
    const q = along(pts, Math.min(b.s, show)), r = at(q);
    bug(x, b.k, r.x, r.y, 0, b.vx);
  });
}

// A swarm's bed glows amber and a golden beetle twinkles where it'll come up.
function warnings(x) {
  const blink = reduced || Math.floor(G.t * 8) % 2;
  G.swarms.forEach((s) => {
    if (G.t < s.at - WARN) return;
    const x0 = s.left ? 4 : 136, wob = reduced ? 0 : Math.round(Math.sin(G.t * 30));
    x.fillStyle = "rgba(255,176,0,0.15)"; x.fillRect(x0, LAUNCH - 14, 60, 18);
    x.fillStyle = "#ffb000";
    for (let i = 0; i < 8; i++) x.fillRect(x0 + 4 + i * 7 + (i % 2 ? wob : -wob), LAUNCH - 10 - (i % 3) * 2, 2, 4);
    if (blink) { x.font = SMALL; x.textAlign = "center"; x.fillText("SWARM", x0 + 30, LAUNCH - 18); }
  });
  G.golds.forEach((s) => {
    if (G.t < s.at - WARN || !blink) return;
    const ex = s.left ? 34 : 166, ey = LAUNCH - 6;
    x.fillStyle = "#ffe27a"; x.fillRect(ex - 1, ey - 5, 2, 10); x.fillRect(ex - 5, ey - 1, 10, 2);
    x.fillStyle = "#fff6dc"; x.fillRect(ex - 1, ey - 1, 2, 2);
  });
}

// bug draws one kind facing the way it's going, centred on cx, cy, on a grid
// of 2 like the frog.
function bug(x, k, cx, cy, t, vx) {
  const X = Math.round(cx), Y = Math.round(cy), f = vx >= 0 ? 1 : -1, flap = Math.floor(t * 20) % 2;
  const r = (dx, dy, w, h, c) => { x.fillStyle = c; x.fillRect(f > 0 ? X + dx * 2 : X - (dx + w) * 2, Y + dy * 2, w * 2, h * 2); };
  if (k === "fly") {
    r(-3, flap ? -5 : -3, 3, flap ? 3 : 1, "#cfe3ea"); r(0, flap ? -5 : -3, 3, flap ? 3 : 1, "#cfe3ea");
    r(-3, -2, 6, 4, "#3a3f46"); r(2, -2, 2, 2, "#e2412d");
  } else if (k === "gnat") {
    r(-1, -1, 3, 2, "#2a2a30"); r(-1, flap ? -3 : -2, 1, 1, "#cfe3ea"); r(1, flap ? -3 : -2, 1, 1, "#cfe3ea");
  } else if (k === "moth") {
    const up = Math.floor(t * 7) % 2;
    r(-5, up ? -5 : -2, 4, up ? 5 : 4, "#d9c9a8"); r(1, up ? -5 : -2, 4, up ? 5 : 4, "#d9c9a8");
    r(-4, up ? -4 : -1, 1, 1, "#8a6a48"); r(3, up ? -4 : -1, 1, 1, "#8a6a48");
    r(-1, -2, 3, 5, "#8a6a48"); r(1, -4, 1, 2, "#5a4630"); r(2, -4, 1, 1, "#5a4630");
  } else if (k === "dragonfly") {
    r(-5, flap ? -4 : -3, 4, 2, "rgba(207,227,234,0.8)"); r(1, flap ? -4 : -3, 4, 2, "rgba(207,227,234,0.8)");
    r(-5, 1, 4, 1, "rgba(207,227,234,0.6)"); r(1, 1, 4, 1, "rgba(207,227,234,0.6)");
    r(-8, -1, 9, 2, "#3d8fd6"); r(1, -2, 3, 3, "#1f5a9a"); r(3, -2, 1, 1, "#7fd6c2");
  } else if (k === "beetle") {
    r(-3, -3, 6, 6, "#1f3a2c"); r(-2, -4, 4, 1, "#1f3a2c"); r(-2, -3, 2, 2, "#56b45f"); r(0, -3, 1, 6, "#0a0908");
    r(3, -1, 2, 2, "#0a0908");
  } else if (k === "golden") {
    const tw = Math.floor(t * 10) % 2;
    x.fillStyle = "rgba(255,176,0,0.3)"; x.fillRect(X - 12, Y - 12, 24, 24);
    r(-3, -3, 6, 6, "#c98a10"); r(-2, -4, 4, 1, "#c98a10"); r(-2, -3, 2, 2, "#fff6dc"); r(0, -3, 1, 6, "#8a5a10");
    r(3, -1, 2, 2, "#8a5a10");
    x.fillStyle = "#fff6dc"; x.fillRect(X - 11 + tw * 20, Y - 11 + tw * 4, 2, 2); x.fillRect(X + 9 - tw * 18, Y + 8, 2, 2);
  } else {
    r(-3, flap ? -5 : -3, 3, flap ? 3 : 1, "#cfe3ea"); r(1, flap ? -5 : -3, 3, flap ? 3 : 1, "#cfe3ea");
    r(-4, -2, 8, 4, "#f2c230"); r(-2, -2, 1, 4, "#0a0908"); r(1, -2, 1, 4, "#0a0908");
    r(3, -2, 2, 3, "#0a0908"); r(-5, 0, 1, 1, "#0a0908");
  }
}

function lily(x, cx, cy, on) {
  x.fillStyle = on ? "#3f9a4d" : "#24402c";
  x.fillRect(cx - 5, cy - 2, 11, 5); x.fillRect(cx - 4, cy - 3, 9, 7);
  x.fillStyle = on ? "#2a6a35" : "#1a2e20"; x.fillRect(cx, cy - 3, 1, 3);
}

// Dusk over the far bank, with the reed beds the bugs come out of either side
// of the frog's pad.
function scene(x, t) {
  ["#2e2640", "#3a2e4a", "#4a3650", "#5e4256", "#784e56"].forEach((c, i) => { x.fillStyle = c; x.fillRect(0, i * 22, W, 22); });
  x.fillStyle = "#e38a4a"; x.fillRect(132, 92, 22, 14); x.fillRect(134, 90, 18, 2);
  x.fillStyle = "#1c3226"; for (let i = 0; i < W; i += 2) { const h = 6 + ((i * 7919) % 7) + Math.round(4 * Math.sin(i / 13)); x.fillRect(i, 110 - h, 2, h); }
  x.fillStyle = "#1c3226"; x.fillRect(0, 108, W, 4);
  ["#2a4a56", "#22404c", "#1c3842", "#173038", "#132a30", "#102a26"].forEach((c, i) => { x.fillStyle = c; x.fillRect(0, 112 + i * 32, W, 32); });
  x.fillStyle = "rgba(227,138,74,0.35)"; for (let i = 0; i < 5; i++) x.fillRect(136 + ((i * 5) % 9), 116 + i * 6, 14 - i * 2, 1);
  x.fillStyle = "rgba(207,227,234,0.18)";
  for (let i = 0; i < 4; i++) { const k = (t * 0.4 + i * 0.25) % 1, w = Math.round(3 + k * 12); x.fillRect(30 + i * 40 - w, 150 + (i * 37) % 70, w * 2, 1); }
  x.fillStyle = "#3f9a4d"; x.fillRect(52, H - 28, 96, 10); x.fillRect(58, H - 31, 84, 15);
  x.fillStyle = "#2a6a35"; x.fillRect(98, H - 31, 3, 6); x.fillRect(58, H - 17, 84, 2);
  [[0, 66], [134, W]].forEach(([a, b]) => {
    for (let i = a; i < b; i += 3) {
      const h = 34 + ((i * 131) % 22), sway = reduced ? 0 : Math.round(Math.sin(t * 1.5 + i / 9));
      x.fillStyle = (i / 3) % 2 ? "#2f5a3a" : "#3a6a40"; x.fillRect(i + sway, H - h, 2, h);
      if ((i * 7) % 5 === 0) { x.fillStyle = "#6a4020"; x.fillRect(i + sway, H - h - 6, 3, 7); }
    }
  });
}

// The label on the Play disk.
export function lashArt(x, g) {
  ["#2e2640", "#3a2e4a", "#4a3650", "#5e4256"].forEach((c, i) => { x.fillStyle = c; x.fillRect(0, i * 14, 168, 14); });
  x.fillStyle = "#e38a4a"; x.fillRect(122, 44, 18, 10);
  x.fillStyle = "#1c3226"; for (let i = 0; i < 168; i += 2) x.fillRect(i, 50 - (i * 7919) % 6, 2, 8);
  x.fillStyle = "#1f3e48"; x.fillRect(0, 56, 168, 40);
  for (let i = 0; i < 168; i += 3) {
    if (i > 46 && i < 122) continue;
    const h = 20 + (i * 131) % 14;
    x.fillStyle = (i / 3) % 2 ? "#2f5a3a" : "#3a6a40"; x.fillRect(i, 96 - h, 2, h);
  }
  x.fillStyle = "#3f9a4d"; x.fillRect(60, 88, 48, 8);
  draw(x, cells(g, "frog", {}), FW, FH, colours(g, null), 62, 58, 1);
  const pts = [[84, 76], [72, 50], [70, 30], [92, 20], [118, 24], [140, 18]];
  x.strokeStyle = "#9c2a52"; x.lineWidth = 4; x.lineCap = "round"; x.lineJoin = "round";
  x.beginPath(); pts.forEach(([a, b], i) => (i ? x.lineTo(a, b) : x.moveTo(a, b))); x.stroke();
  x.strokeStyle = "#e2567a"; x.lineWidth = 2; x.stroke();
  [[70, 34, "#3a3f46"], [92, 20, "#d9c9a8"], [118, 24, "#3d8fd6"]].forEach(([a, b, c]) => { x.fillStyle = c; x.fillRect(a - 3, b - 2, 6, 4); });
  x.fillStyle = "#c98a10"; x.fillRect(137, 15, 6, 6); x.fillStyle = "#fff6dc"; x.fillRect(138, 16, 2, 2); x.fillRect(146, 12, 1, 1);
  x.fillStyle = "#f2c230"; x.fillRect(30, 30, 7, 4); x.fillStyle = "#0a0908"; x.fillRect(32, 30, 1, 4); x.fillRect(35, 30, 1, 4);
}

import { sfx } from "./sound.js";
import { colours } from "./frog.js";
import { drawTop } from "./top.js";
import { feel } from "./feel.js";
import { HOLES, R, CUP, CAP, PULL, DT, newBall, hit, roll, putBack, cupAt, driftAt, paddles, wheelShut, firstHit, rollFor, speedFor, pointsFor } from "./course.js";

// Pad putt. Nine holes of mini golf on the mud banks round the pond, each
// with one new thing on it. Drag back from anywhere and let go, and the frog
// flicks the ball with its tongue the other way.
const W = 200, H = 320, LEAST = 5, TRAIL = 9;
const FONT = "700 13px 'Space Grotesk', sans-serif";

let G = null, col = null;
const F = feel();

function hole() { return HOLES[G.hi]; }

function startHole() {
  const h = hole();
  G.ball = newBall(h, 0); G.hs = { open: false }; G.ht = 0; G.acc = 0;
  G.st = "aim"; G.st_t = 0; G.named = 1.8; G.strokes = 0; G.trail = []; G.rings = []; G.flash = {};
  G.armed = false; G.keyed = false; G.hop = 0;
  G.ka = toCup(); G.kk = 0.5;
  G.frog = spot(G.ka);
}

function toCup() {
  const c = cupAt(hole(), G.ht);
  return Math.atan2(c.y - G.ball.y, c.x - G.ball.x);
}

// The frog sits behind the ball, facing the way it's about to go.
function spot(a) { return { x: G.ball.x - Math.cos(a) * 10, y: G.ball.y - Math.sin(a) * 10, a }; }

function aim() {
  if (G.armed) {
    const dx = G.from[0] - G.to[0], dy = G.from[1] - G.to[1], len = Math.hypot(dx, dy);
    return len < LEAST ? null : { a: Math.atan2(dy, dx), k: Math.min(1, len / PULL) };
  }
  return G.keyed ? { a: G.ka, k: G.kk } : null;
}

function stroke(a) {
  if (!a || G.st !== "aim") return;
  G.frog = spot(a.a);
  hit(G.ball, a.a, a.k, G.ht, hole());
  G.strokes++; G.total++; G.st = "roll"; G.st_t = 0; G.keyed = false; G.lick = 0.14; G.trail = [];
  G.ka = a.a; G.kk = a.k;
  sfx("hop", 1 + Math.round(a.k * 6)); F.squash(0.3 + a.k * 0.5);
}

function sunk(x, y) {
  const h = hole(), pts = pointsFor(G.strokes, h.par);
  G.card[G.hi] = G.strokes; G.score += pts; G.st = "sink"; G.st_t = 0; G.sinkAt = [x, y];
  F.got(x, y, "+" + pts, { big: G.strokes < h.par, streak: G.hi + 1, sound: "land" });
  if (G.strokes === 1) F.pop(x, y - 28, "HOLE IN ONE", "#ffb000");
  else if (G.strokes < h.par) F.pop(x, y - 28, "UNDER PAR", "#ffb000");
}

function settle() {
  const c = cupAt(hole(), G.ht), b = G.ball;
  if (Math.hypot(c.x - b.x, c.y - b.y) < CUP + 8) F.close(b.x, b.y - 14);
  if (G.strokes >= CAP) { G.st = "out"; G.st_t = 0; return; }
  G.st = "aim"; G.st_t = 0;
  G.ka = toCup();
  G.hopFrom = { x: G.frog.x, y: G.frog.y }; G.hop = 0.24;
}

function events(ev) {
  for (const [kind, x, y, n] of ev) {
    if (kind === "wall" || kind === "rim" || kind === "pad") {
      sfx("kick", 1 + Math.min(7, Math.floor(n / 50)));
      F.squash(Math.min(0.8, n / 350));
      if (n > 140) F.burst(x, y, kind === "pad" ? "#56b45f" : "#c9974f", 3);
    } else if (kind === "bump") {
      sfx("boing"); F.freeze(2); F.hit(0.15);
      F.burst(x, y, "#fff6dc", 6);
      G.flash[x + "," + y] = 0.2;
    } else if (kind === "launch") sfx("hop", 5);
    else if (kind === "land") { sfx("plop"); F.burst(x, y, "#56b45f", 4); }
    else if (kind === "warp") { sfx("cast"); F.burst(x, y, "#8a5a2b", 5); }
    else if (kind === "out") { F.burst(x, y, "#c9974f", 5); G.trail = []; }
    else if (kind === "open") { sfx("land"); F.burst(x, y, "#ffe27a", 6); F.pop(x, y - 12, "OPEN", "#7fd6c2"); }
    else if (kind === "lip") { sfx("close"); F.pop(x, y - 12, "LIPPED", "#7fd6c2"); }
    else if (kind === "sink") sunk(x, y);
    else if (kind === "water") {
      G.strokes++; G.total++; G.st = "splash"; G.st_t = 0; G.rings.push({ x, y, t: 0 });
      sfx("plop"); F.burst(x, y, "#cfe3ea", 8); F.pop(x, y - 10, "SPLASH +1", "#7fd6c2");
    } else if (kind === "rest") settle();
  }
}

export const putt = {
  key: "putt", W, H, title: "PAD PUTT", feel: F, edge: ["#123a3d", "#123a3d"],
  aria: "Drag back from anywhere and let go to putt the ball the other way, or turn with the left and right keys, set the power with up and down and press space.",
  help: ["DRAG BACK AND LET GO TO PUTT", "A LONGER PULL HITS HARDER", "WATER COSTS A STROKE", "TAP TO START"],

  reset(g, p) {
    col = colours(g, p.frog.hat);
    G = { t: 0, hi: 0, total: 0, score: 0, par: HOLES.reduce((a, h) => a + h.par, 0), card: HOLES.map(() => null), from: [0, 0], to: [0, 0], turn: 0, push: 0, lick: 0 };
    startHole();
  },

  demo() {
    if (!G || G.hi || G.strokes || G.st !== "aim") return null;
    return { x: G.ball.x + 14, y: G.ball.y - 30, to: [G.ball.x + 14, G.ball.y + 4] };
  },

  press(x, y) {
    if (!G) return;
    if (G.st === "aim") { G.armed = true; G.from = [x, y]; G.to = [x, y]; G.keyed = false; }
  },
  drag(x, y) { if (G && G.armed) G.to = [x, y]; },
  release() {
    if (!G || !G.armed) return;
    const a = aim();
    G.armed = false;
    stroke(a);
  },

  keys(code, down) {
    if (!G) return;
    const turn = { ArrowLeft: -1, KeyA: -1, ArrowRight: 1, KeyD: 1 }[code], push = { ArrowUp: 1, KeyW: 1, ArrowDown: -1, KeyS: -1 }[code];
    if (turn) G.turn = down ? turn : G.turn === turn ? 0 : G.turn;
    if (push) G.push = down ? push : G.push === push ? 0 : G.push;
    if ((turn || push) && down) G.keyed = true;
    if (down && (code === "Space" || code === "Enter")) stroke(aim());
  },

  step(dt) {
    const h = hole();
    G.t += dt; G.st_t += dt;
    G.ka += G.turn * 1.4 * dt;
    G.kk = Math.max(0.1, Math.min(1, G.kk + G.push * 0.6 * dt));
    G.lick = Math.max(0, G.lick - dt); G.hop = Math.max(0, G.hop - dt);
    for (const k in G.flash) G.flash[k] = Math.max(0, G.flash[k] - dt);
    G.rings.forEach((r) => { r.t += dt; });
    G.rings = G.rings.filter((r) => r.t < 0.8);
    if (G.st === "done") return;
    G.acc += dt;
    while (G.acc >= DT) {
      G.acc -= DT; G.ht += DT;
      if (G.st === "sink" || G.st === "splash" || G.st === "out") continue;
      const ev = [], was = G.st;
      roll(h, G.hs, G.ball, G.ht, ev);
      if (was === "roll") events(ev);
    }
    if (G.st === "roll" && Math.floor(G.t * 60) !== G.lastDot) {
      G.lastDot = Math.floor(G.t * 60);
      G.trail.push([G.ball.x, G.ball.y, G.ball.air > 0]);
      if (G.trail.length > TRAIL) G.trail.shift();
    }
    G.named = Math.max(0, (G.named || 0) - dt);
    if (G.st === "splash" && G.st_t > 0.8) {
      putBack(h, G.ball, G.ht); G.trail = [];
      if (G.strokes >= CAP) { G.st = "out"; G.st_t = 0; } else { G.st = "aim"; G.st_t = 0; G.frog = spot(toCup()); }
    }
    if (G.st === "out" && G.st_t > 1) { G.card[G.hi] = 0; next(); }
    if (G.st === "sink" && G.st_t > 0.8) next();
  },

  over() { return G && G.st === "done" && G.st_t > 0.5; },
  score() { return G ? G.score : 0; },
  why() { return !G ? "" : G.total + " STROKES, PAR " + G.par; },
  report() {
    const done = G.card.filter((c) => c), under = G.card.filter((c, i) => c && c < HOLES[i].par).length;
    return { counts: { holes: done.length, under, beat: done.length === 9 ? Math.max(0, G.par - G.total) : 0 } };
  },
  tally() { return G ? [G.card.filter((c) => c).length + " of 9 holes sunk"] : []; },

  // The scorecard, a box a hole with its par over its strokes, amber under par.
  card() {
    if (!G) return null;
    const c = document.createElement("canvas"), x = c.getContext("2d"), cw = 58;
    c.width = cw * 9 + 4; c.height = 84;
    c.style.width = (c.width / 2) + "px";
    x.textAlign = "center";
    HOLES.forEach((h, i) => {
      const n = G.card[i], l = 2 + i * cw;
      x.fillStyle = "#0b1f17"; x.fillRect(l, 0, cw - 4, 84);
      x.fillStyle = "#7d9a90"; x.font = "700 24px 'JetBrains Mono', monospace"; x.fillText(String(h.par), l + cw / 2 - 2, 28);
      x.fillStyle = !n ? "#5b6470" : n < h.par ? "#ffb000" : n === h.par ? "#fff6dc" : "#bcaf99";
      x.font = "700 32px 'JetBrains Mono', monospace"; x.fillText(n === null ? "" : n ? String(n) : "-", l + cw / 2 - 2, 70);
    });
    return c;
  },

  draw(x) {
    water(x, G ? G.t : 0);
    if (!G) return;
    const h = hole(), t = G.ht, b = G.ball;
    course(x, h, G.hs, t, G.t, G.flash);
    G.rings.forEach((r) => ring(x, r.x, r.y, 3 + r.t * 18, 1 - r.t / 0.8));
    const a = G.st === "aim" && aim();
    if (a) guide(x, h, a, t);
    G.trail.forEach(([px, py, air], i) => {
      x.globalAlpha = (i + 1) / (TRAIL + 2) * 0.6;
      x.fillStyle = air ? "#0a1b1f" : "#fff6dc"; x.fillRect(Math.round(px) - 1, Math.round(py) - 1, 2, 2);
    });
    x.globalAlpha = 1;
    frogAt(x, a);
    if (G.st === "sink") {
      const k = Math.max(0, 1 - G.st_t / 0.25);
      if (k > 0) ballAt(x, G.sinkAt[0], G.sinkAt[1], k);
    } else if (G.st !== "splash") {
      const up = b.air > 0 ? Math.sin(Math.PI * (1 - b.air / b.flight)) * 14 : 0;
      if (up) { x.fillStyle = "rgba(10,27,31,0.5)"; x.fillRect(Math.round(b.x) - 2, Math.round(b.y) - 1, 5, 3); }
      F.squashed(x, b.x, b.y + R, () => ballAt(x, b.x, b.y - up, 1 + up / 30));
    }
    F.draw(x);
    hud(x, h, a);
    if (G.named > 0) banner(x, "HOLE " + (G.hi + 1) + "  " + h.name.toUpperCase(), Math.min(1, G.named / 0.4));
    if (G.st === "out") banner(x, "SIX STROKES, ON TO THE NEXT", 1);
  },
};

function next() {
  G.hi++;
  if (G.hi >= HOLES.length) { G.hi--; G.st = "done"; G.st_t = 0; return; }
  startHole();
}

// The aim, a band pulled back behind the ball for the power and dots out to
// the first thing the ball would meet or as far as it would roll on the flat.
function guide(x, h, a, t) {
  const b = G.ball, dx = Math.cos(a.a), dy = Math.sin(a.a), most = rollFor(speedFor(a.k));
  const to = firstHit(h, G.hs, b, a.a, Math.min(most, 400), t);
  x.fillStyle = "rgba(255,246,220,0.75)";
  for (let d = 7; d < to; d += 6) x.fillRect(Math.round(b.x + dx * d), Math.round(b.y + dy * d), 2, 2);
  const ex = Math.round(b.x + dx * to), ey = Math.round(b.y + dy * to);
  x.strokeStyle = "#fff6dc"; x.lineWidth = 1;
  if (to < most) { x.fillStyle = "#fff6dc"; x.fillRect(ex - 1, ey - 1, 3, 3); } else x.strokeRect(ex - 2.5, ey - 2.5, 5, 5);
  const band = 5 + a.k * 22;
  for (let d = 5; d < band; d += 2) {
    const k = d / 27;
    x.fillStyle = k > 0.8 ? "#e2412d" : k > 0.5 ? "#ffb000" : "#ffe27a";
    x.fillRect(Math.round(b.x - dx * d), Math.round(b.y - dy * d), 2, 2);
  }
}

function frogAt(x, a) {
  let f = G.frog, up = 0;
  if (G.st === "aim") {
    const want = spot(a ? a.a : G.ka);
    if (G.hop > 0) {
      const k = 1 - G.hop / 0.24;
      f = { x: G.hopFrom.x + (want.x - G.hopFrom.x) * k, y: G.hopFrom.y + (want.y - G.hopFrom.y) * k, a: want.a };
      up = Math.sin(Math.PI * k) * 8;
      x.fillStyle = "rgba(10,27,31,0.4)"; x.fillRect(Math.round(f.x) - 4, Math.round(f.y) - 1, 9, 4);
    } else f = want;
    G.frog = f;
  }
  const flip = Math.sin(f.a) > 0.2;
  if (G.lick > 0) {
    const mx = f.x + Math.cos(f.a) * 5, my = f.y + Math.sin(f.a) * 5, bx = G.ball.from.x, by = G.ball.from.y;
    x.fillStyle = "#e2567a";
    for (let k = 0; k <= 1; k += 0.1) x.fillRect(Math.round(mx + (bx - mx) * k), Math.round(my + (by - my) * k), 2, 2);
  }
  F.squashed(x, f.x, f.y + 6, () => drawTop(x, col, Math.round(f.x) - 6, Math.round(f.y - up) - 6, flip));
}

function ballAt(x, bx, by, s) {
  const r = Math.max(1, Math.round(R * s)), cx = Math.round(bx), cy = Math.round(by);
  for (let y = -r; y <= r; y++) {
    const hw = Math.round(Math.sqrt(r * r - y * y + r * 0.6));
    x.fillStyle = y > r / 3 ? "#cfc6b0" : "#fff6dc"; x.fillRect(cx - hw, cy + y, hw * 2 + 1, 1);
  }
  x.fillStyle = "#ffffff"; x.fillRect(cx - 1, cy - r + 1, 1, 1);
}

function hud(x, h, a) {
  x.fillStyle = "rgba(10,27,31,0.7)"; x.fillRect(0, 0, W, 20); x.fillRect(0, H - 18, W, 18);
  x.font = FONT; x.textAlign = "left"; x.fillStyle = "#ece5d8";
  x.fillText((G.hi + 1) + "/9", 6, 15);
  x.fillStyle = "#cfe3ea"; x.fillText("PAR " + h.par, 40, 15);
  // A pip a stroke, out of the six a hole allows.
  for (let i = 0; i < CAP; i++) {
    x.fillStyle = i < G.strokes ? (i < h.par ? "#ffe27a" : "#e2567a") : "#2d4a4c";
    x.fillRect(96 + i * 8, 7, 5, 5);
  }
  x.textAlign = "right"; x.fillStyle = "#ece5d8"; x.fillText(String(G.score), W - 6, 15);
  x.textAlign = "left"; x.fillStyle = "#cfe3ea"; x.fillText(h.name, 6, H - 5);
  if (!a) return;
  x.fillStyle = "#2d4a4c"; x.fillRect(W - 66, H - 12, 60, 6);
  x.fillStyle = a.k > 0.8 ? "#e2412d" : a.k > 0.5 ? "#ffb000" : "#ffe27a"; x.fillRect(W - 66, H - 12, Math.round(60 * a.k), 6);
}

// The strip sits over the tee end, so the hole's new thing shows while it's up.
// A thin strip under the top readout, so the tee and the ball stay clear and
// a putt can start while it's still showing.
function banner(x, text, alpha) {
  const y = 20;
  x.globalAlpha = alpha;
  x.fillStyle = "rgba(10,27,31,0.82)"; x.fillRect(0, y, W, 16);
  x.fillStyle = "#ffb000"; x.fillRect(0, y + 15, W, 1);
  x.textAlign = "center"; x.font = FONT; x.fillStyle = "#fff6dc"; x.fillText(text, W / 2, y + 12);
  x.globalAlpha = 1;
}

// Open water seen from above, with ripples drifting by.
function water(x, t) {
  x.fillStyle = "#123a3d"; x.fillRect(0, 0, W, H);
  x.fillStyle = "#103337";
  for (let i = 0, n = 7; i < 30; i++) { n = (n * 1103515245 + 12345) >>> 0; x.fillRect(n % W, (n >>> 10) % H, 10 + (n >>> 20) % 14, 2); }
  x.fillStyle = "rgba(127,214,194,0.22)";
  for (let i = 0; i < 26; i++) x.fillRect(Math.round(((i * 53 + t * 4) % W + W) % W), (i * 97) % H, 5, 1);
}

function ring(x, cx, cy, r, a) {
  x.globalAlpha = Math.max(0, a); x.fillStyle = "#cfe3ea";
  for (let i = 0; i < 16; i++) x.fillRect(Math.round(cx + Math.cos(i * Math.PI / 8) * r), Math.round(cy + Math.sin(i * Math.PI / 8) * r * 0.7), 1, 1);
  x.globalAlpha = 1;
}

// Moss on a mud bank, with the same speckle every time it's drawn.
function moss(x, z) {
  const [x0, y0, x1, y1] = z;
  x.fillStyle = "#5a4630"; x.fillRect(x0 - 2, y0 - 2, x1 - x0 + 4, y1 - y0 + 5);
  x.fillStyle = "#3f7f3e"; x.fillRect(x0, y0, x1 - x0, y1 - y0);
  x.fillStyle = "#4a8f46";
  for (let i = 0, n = x0 * 31 + y0; i < (x1 - x0) * (y1 - y0) / 90; i++) {
    n = (n * 1103515245 + 12345) >>> 0;
    x.fillRect(x0 + n % (x1 - x0 - 2), y0 + (n >>> 12) % (y1 - y0 - 1), 2, 1);
  }
}

function pool(x, z, t) {
  const [x0, y0, x1, y1] = z;
  x.fillStyle = "#123a3d"; x.fillRect(x0, y0, x1 - x0, y1 - y0);
  x.fillStyle = "rgba(127,214,194,0.3)";
  for (let i = 0; i < (x1 - x0) * (y1 - y0) / 500; i++) x.fillRect(x0 + Math.round(((i * 37 + t * 5) % (x1 - x0 - 5) + (x1 - x0)) % (x1 - x0 - 5)), y0 + 3 + (i * 23) % Math.max(1, y1 - y0 - 6), 5, 1);
}

// A slope is paler moss with chevrons walking downhill, so it reads before a putt.
function slope(x, z, t) {
  const [x0, y0, x1, y1] = z.at, m = Math.hypot(z.push[0], z.push[1]), ux = z.push[0] / m, uy = z.push[1] / m;
  x.fillStyle = "rgba(122,196,111,0.18)"; x.fillRect(x0, y0, x1 - x0, y1 - y0);
  x.fillStyle = "#6fae5f";
  const off = (t * 8) % 24;
  for (let cy = y0 + 10; cy < y1 - 4; cy += 22) for (let cx = x0 + 12; cx < x1 - 6; cx += 24) {
    const px = cx + ux * off - (ux ? 12 : 0), py = cy + uy * off - (uy ? 12 : 0);
    if (px < x0 + 3 || px > x1 - 6 || py < y0 + 4 || py > y1 - 4) continue;
    for (let i = -3; i <= 3; i++) {
      const s = 3 - Math.abs(i);
      x.fillRect(Math.round(px + ux * s - uy * i), Math.round(py + uy * s + ux * i), 2, 1);
    }
  }
}

function stream(x, z, t) {
  const [x0, y0, x1, y1] = z.at, dir = Math.sign(z.push[0]) || 1;
  x.fillStyle = "#1f5f66"; x.fillRect(x0, y0, x1 - x0, y1 - y0);
  x.fillStyle = "#2a737a"; x.fillRect(x0, y0, x1 - x0, 1); x.fillRect(x0, y1 - 1, x1 - x0, 1);
  x.fillStyle = "rgba(207,227,234,0.55)";
  for (let i = 0; i < 14; i++) {
    const w = x1 - x0, px = x0 + (((i * 41 + dir * t * 46) % w) + w) % w;
    const py = y0 + 4 + (i * 13) % (y1 - y0 - 8);
    x.fillRect(Math.round(px), py, 7, 1);
    x.fillRect(Math.round(px) + (dir > 0 ? 5 : 0), py - 1, 2, 3);
  }
}

// A log from above, a brown run with a pale top and cut rings at each end.
function log(x, w) {
  const [x1, y1, x2, y2] = w, l = Math.hypot(x2 - x1, y2 - y1), n = Math.max(1, Math.ceil(l / 2));
  for (let i = 0; i <= n; i++) {
    const px = Math.round(x1 + (x2 - x1) * i / n), py = Math.round(y1 + (y2 - y1) * i / n);
    x.fillStyle = "#4a2e16"; x.fillRect(px - 3, py - 3, 6, 6);
  }
  for (let i = 0; i <= n; i++) {
    const px = Math.round(x1 + (x2 - x1) * i / n), py = Math.round(y1 + (y2 - y1) * i / n);
    x.fillStyle = "#8a5a2b"; x.fillRect(px - 2, py - 3, 4, 4);
    if (i % 5 === 2) { x.fillStyle = "#6b4423"; x.fillRect(px - 2, py, 4, 1); }
  }
  [[x1, y1], [x2, y2]].forEach(([px, py]) => { x.fillStyle = "#c9974f"; x.fillRect(Math.round(px) - 2, Math.round(py) - 2, 4, 4); x.fillStyle = "#8a5a2b"; x.fillRect(Math.round(px) - 1, Math.round(py) - 1, 2, 2); });
}

// A mushroom cap from above, red with cream spots, lit for a moment when hit.
function mushroom(x, cx, cy, r, lit) {
  x.fillStyle = "rgba(10,27,31,0.4)"; disc(x, cx + 1, cy + 2, r);
  x.fillStyle = lit ? "#ff8a6a" : "#c8402c"; disc(x, cx, cy, r);
  x.fillStyle = lit ? "#ffb8a0" : "#e2563f"; disc(x, cx - 1, cy - 1, r - 3);
  x.fillStyle = "#fff6dc";
  [[-0.4, -0.3], [0.35, -0.45], [0.1, 0.35], [-0.5, 0.3], [0.55, 0.15]].forEach(([dx, dy]) => x.fillRect(Math.round(cx + dx * r), Math.round(cy + dy * r), 2, 2));
}

function disc(x, cx, cy, r) {
  for (let y = -r; y <= r; y++) { const hw = Math.round(Math.sqrt(r * r - y * y)); x.fillRect(Math.round(cx) - hw, Math.round(cy) + y, hw * 2, 1); }
}

// A lily pad from above, a disc with a notch cut out of it.
function lily(x, cx, cy, r, i) {
  cx = Math.round(cx); cy = Math.round(cy);
  x.fillStyle = "#2f6b33"; disc(x, cx, cy + 1, r);
  x.fillStyle = "#3f8a44"; disc(x, cx, cy, r);
  x.fillStyle = "#56b45f";
  for (let y = -r + 1; y < 0; y++) { const hw = Math.round(Math.sqrt((r - 2) * (r - 2) - y * y) * 0.7); x.fillRect(cx - hw - 2, cy + y, hw, 1); }
  const a = (i * 2.4) % (Math.PI * 2);
  x.fillStyle = "#123a3d";
  for (let d = 2; d <= r; d++) x.fillRect(Math.round(cx + Math.cos(a) * d), Math.round(cy + Math.sin(a) * d), 2, 2);
}

// The waterwheel turns over the gap, and a paddle down in it reads solid.
function wheel(x, w, t) {
  const { x: cx, y: cy, half } = w;
  x.fillStyle = "#4a2e16"; x.fillRect(cx - half - 2, cy - 1, half * 2 + 4, 3);
  paddles(w, t).forEach((a) => {
    const down = Math.abs(a - Math.PI) < 0.42;
    x.fillStyle = down ? "#c9974f" : Math.cos(a) > 0 ? "#8a5a2b" : "rgba(138,90,43,0.45)";
    x.fillRect(cx - half, cy + Math.round(Math.sin(a) * 9) - 1, half * 2, down ? 4 : 2);
  });
  x.fillStyle = "#6b4423"; x.fillRect(cx - half - 4, cy - 10, 3, 20); x.fillRect(cx + half + 1, cy - 10, 3, 20);
  x.fillStyle = wheelShut(w, t) ? "#e2567a" : "#56b45f"; x.fillRect(cx - half - 4, cy - 12, 3, 2); x.fillRect(cx + half + 1, cy - 12, 3, 2);
}

function hollow(x, cx, cy, dir) {
  const v = dir === "up";
  x.fillStyle = "#4a2e16"; x.fillRect(cx - 12, cy - 6, 24, 12);
  x.fillStyle = "#8a5a2b"; x.fillRect(cx - 12, cy - 6, 24, 3);
  x.fillStyle = "#c9974f"; x.fillRect(cx - 7, v ? cy - 6 : cy + 1, 14, 5);
  x.fillStyle = "#0a1b1f"; x.fillRect(cx - 5, v ? cy - 5 : cy + 2, 10, 3);
}

function reeds(x, g, open, t) {
  const [x1, y1, x2] = g;
  for (let px = x1 + 2; px < x2 - 1; px += 4) {
    if (open) { x.fillStyle = "#7d9a5a"; x.fillRect(px, y1 - 1, 3, 2); continue; }
    const sway = Math.round(Math.sin(t * 2 + px) * 0.6);
    x.fillStyle = "#2f6b33"; x.fillRect(px, y1 - 4, 2, 8);
    x.fillStyle = "#8a5a2b"; x.fillRect(px + sway, y1 - 7, 2, 4);
  }
}

function cup(x, c, t) {
  const cx = Math.round(c.x), cy = Math.round(c.y);
  x.fillStyle = "#2f6b33"; disc(x, cx, cy, CUP + 1);
  x.fillStyle = "#0a1b1f"; disc(x, cx, cy, CUP);
  x.fillStyle = "#3a2412"; x.fillRect(cx, cy - 22, 1, 22);
  const flap = Math.round(Math.sin(t * 3));
  for (let i = 0; i < 8; i++) {
    x.fillStyle = i % 4 < 2 ? "#e2567a" : "#f59ab3";
    x.fillRect(cx + 1 + i, cy - 22 + (i > 4 ? flap : 0), 1, Math.max(1, Math.round(6 * (1 - i / 8))));
  }
}

function course(x, h, s, t, now, flash) {
  h.ground.forEach((z) => moss(x, z));
  (h.streams || []).forEach((z) => stream(x, z, now));
  (h.water || []).forEach((z) => pool(x, z, now));
  (h.slopes || []).forEach((z) => slope(x, z, now));
  if (h.ramp) {
    const r = h.slopes[0].at;
    x.fillStyle = "#6b4423"; x.fillRect(r[0], r[1], r[2] - r[0], r[3] - r[1]);
    x.fillStyle = "#8a5a2b"; for (let y = r[1] + 1; y < r[3]; y += 5) x.fillRect(r[0], y, r[2] - r[0], 3);
  }
  if (h.pad) { const p = driftAt(h.pad, t); lily(x, p.x, p.y, p.r, 3); }
  if (h.drift) { const d = driftAt(h.drift, t); lily(x, d.x, d.y, d.r, 1); }
  if (h.button) {
    const [bx, by, br] = h.button;
    x.fillStyle = "#5b6470"; disc(x, bx, by, br);
    x.fillStyle = s.open ? "#ffb000" : "#8a9099"; disc(x, bx, by - 1, br - 2);
    x.fillStyle = s.open ? "#fff6dc" : "#e2567a"; x.fillRect(bx - 1, by - 2, 3, 3);
  }
  h.walls.forEach((w) => log(x, w));
  if (h.gate) reeds(x, h.gate, s.open, now);
  if (h.warp) {
    const [ax, ay] = h.warp.from, [bx, by] = h.warp.to, n = Math.round(Math.hypot(bx - ax, by - ay) / 6);
    x.fillStyle = "rgba(201,151,79,0.4)";
    for (let i = 1; i < n; i++) x.fillRect(Math.round(ax + (bx - ax) * i / n), Math.round(ay + (by - ay) * i / n), 2, 2);
    hollow(x, h.warp.from[0], h.warp.from[1] - 4, "down"); hollow(x, h.warp.to[0], h.warp.to[1] + 6, "up");
  }
  if (h.wheel) wheel(x, h.wheel, t);
  (h.bumpers || []).forEach(([cx, cy, r]) => mushroom(x, cx, cy, r, flash[cx + "," + cy] > 0));
  cup(x, cupAt(h, t), now);
}

// The label on the Play disk.
export function puttArt(x, g) {
  x.fillStyle = "#123a3d"; x.fillRect(0, 0, 168, 96);
  moss(x, [10, 8, 158, 88]);
  log(x, [10, 8, 158, 8]); log(x, [10, 88, 158, 88]); log(x, [10, 8, 10, 88]); log(x, [158, 8, 158, 88]);
  log(x, [70, 8, 70, 56]);
  mushroom(x, 104, 52, 8, false);
  cup(x, { x: 132, y: 30 }, 0);
  x.fillStyle = "rgba(255,246,220,0.75)"; for (let d = 0; d < 6; d++) x.fillRect(40 + d * 6, 68 - d * 3, 2, 2);
  ballAt(x, 36, 70, 1);
  drawTop(x, colours(g, null), 18, 70, false);
}

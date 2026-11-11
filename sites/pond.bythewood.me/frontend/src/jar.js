import { sfx, audioClock, croakAt } from "./sound.js";
import { colours } from "./frog.js";
import { drawTop } from "./top.js";
import { feel } from "./feel.js";
import { W, H, JL, JR, FLOOR, LINE, DROP, R, TOP, PAYS, POP, NAMES, world, drop, run, aimed, landing } from "./jarworld.js";

// Spawn jar. Frogspawn is dropped into a jar, and two of a stage that touch
// grow into one of the next, egg on up to a pond king, and two pond kings
// pop for a big bonus. Anything resting over the line ends it.
const TURN = Math.PI / 16, TINT = ["#cfe3ea", "#cfe3ea", "#8fce7f", null, "#e6d27a", "#d8cfb8", "#e3b23a", "#ffe27a", "#ffe27a"];

let G = null, col = null;
const F = feel();

function letGo() {
  if (!G || G.st !== "play" || !drop(G.w, G.aim)) return;
  sfx("plop"); F.squash(0.5);
}

function croak(s) {
  const ac = audioClock();
  if (ac) croakAt(300 - s * 26, ac.currentTime + 0.02, 0.1 + s * 0.02);
}

function heard(e) {
  if (e.k === "knock") {
    if (G.t - G.knockAt > 0.08 && e.v > 150) { G.knockAt = G.t; sfx("kick"); }
    return;
  }
  G.chain++;
  const tint = TINT[e.s] || col[1];
  if (e.k === "pop") {
    F.got(e.x, e.y, "+" + POP, { streak: G.chain, big: true, sound: "land", c: "#ffe27a" });
    F.burst(e.x, e.y, "#ffe27a", 24); F.burst(e.x, e.y, "#8fce7f", 16); F.hit(0.7);
    croak(TOP);
    return;
  }
  F.got(e.x, e.y, "+" + PAYS[e.s], { streak: G.chain, big: e.s >= 4 });
  F.burst(e.x, e.y, tint, 2 + e.s * 2);
  if (e.s >= 6) F.hit(0.2 * (e.s - 5));
  if (e.s >= 3) croak(e.s);
}

export const jar = {
  key: "jar", W, H, title: "SPAWN JAR", feel: F, edge: ["#2a5163", "#3a2a1a"],
  aria: "Drag along the top and let go to drop frogspawn into the jar, or use the arrow keys and space.",
  help: ["DRAG ALONG THE TOP AND LET GO", "TWO ALIKE GROW INTO THE NEXT", "EGG, TADPOLE, FROGLET, FROG, ON UP TO A POND KING", "TWO POND KINGS POP FOR " + POP, "KEEP UNDER THE LINE", "TAP TO START"],

  reset(g, p) {
    col = colours(g, p.frog.hat);
    G = { st: "play", t: 0, st_t: 0, aim: W / 2, dir: 0, armed: false, w: world(), chain: 0, knockAt: 0, warned: 0, end: 0 };
  },

  demo() { return G && !G.w.drops ? { x: 70, y: DROP - 20, to: [124, DROP - 20] } : null; },

  press() { if (G) G.armed = true; },
  drag(x) { if (G) G.aim = Math.max(JL, Math.min(JR, x)); },
  release() {
    if (!G || !G.armed) return;
    G.armed = false;
    letGo();
  },

  keys(code, down) {
    if (!G) return;
    if (code === "ArrowLeft" || code === "KeyA") G.dir = down ? -1 : G.dir < 0 ? 0 : G.dir;
    if (code === "ArrowRight" || code === "KeyD") G.dir = down ? 1 : G.dir > 0 ? 0 : G.dir;
    if (down && (code === "Space" || code === "Enter" || code === "ArrowDown")) letGo();
  },

  step(dt) {
    const w = G.w;
    G.t += dt; G.st_t += dt;
    G.aim = Math.max(JL, Math.min(JR, G.aim + G.dir * 110 * dt));
    if (G.st !== "play") { if (G.st_t > 1.4) G.end = 1; return; }
    run(w, dt);
    w.events.forEach(heard);
    w.events.length = 0;
    // A chain only counts while something is still settling, and a body is
    // only redrawn once it has moved most of a pixel or a turn, so a pile at
    // rest holds still.
    let moving = false;
    for (const b of w.bodies) {
      if (Math.hypot(b.vx, b.vy) > 25) moving = true;
      if (Math.abs(b.x - b.dx) > 0.6) b.dx = Math.round(b.x);
      if (Math.abs(b.y - b.dy) > 0.6) b.dy = Math.round(b.y);
      if (Math.abs(b.a - b.da) > TURN * 0.6) b.da = Math.round(b.a / TURN) * TURN;
    }
    if (!moving) G.chain = 0;
    const tick = Math.floor(w.over * 2);
    if (w.over > 0 && tick >= G.warned) { G.warned = tick + 1; sfx("warn"); }
    if (!w.over) G.warned = 0;
    if (w.full) { G.st = "full"; G.st_t = 0; F.crash(W / 2, LINE, { sound: "bonk" }); }
  },

  over() { return G && G.end; },
  score() { return G ? G.w.score : 0; },
  why() { return G ? "THE JAR IS FULL" : ""; },
  report() { const m = G.w.made; return { counts: { kings: m[TOP], elders: m[5], frogs: m[3], froglets: m[2] } }; },
  tally() {
    if (!G) return [];
    const l = [], w = G.w;
    if (w.pops) l.push(w.pops + (w.pops > 1 ? " pairs" : " pair") + " of pond kings popped");
    for (let s = TOP; s >= 1; s--) if (w.made[s]) l.push(w.made[s] + " " + NAMES[s].toLowerCase() + (w.made[s] > 1 ? "s" : "") + " grown");
    return l.slice(0, 3).map((s) => s.charAt(0).toUpperCase() + s.slice(1));
  },

  draw(x) {
    scene(x);
    if (!G) return;
    const w = G.w, warn = w.over > 0 && Math.floor(G.t * 6) % 2 === 0;
    x.fillStyle = warn ? "#e2412d" : "rgba(255,246,220,0.35)";
    for (let i = JL; i < JR; i += 6) x.fillRect(i, LINE, warn ? 4 : 3, warn ? 2 : 1);
    w.bodies.forEach((b) => spawn(x, b.dx, b.dy, b.s, b.r, b.da));
    glass(x);
    const cx = Math.round(aimed(w.hand, G.aim));
    if (G.st === "play") {
      const r = R[w.hand], end = landing(w, cx);
      x.fillStyle = "rgba(255,246,220,0.25)";
      for (let y = DROP + r + 3; y < end - 2; y += 6) x.fillRect(cx, y, 1, 3);
      if (w.wait <= 0) spawn(x, cx, DROP, w.hand, r, 0);
    }
    F.squashed(x, cx, 30, () => drawTop(x, col, cx - 6, 18, true));
    F.draw(x);
    x.font = "700 13px 'Space Grotesk', sans-serif";
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(String(w.score), 6, 15);
    x.textAlign = "right"; x.fillStyle = "#cfe3ea"; x.fillText("NEXT", W - 26, 15);
    spawn(x, W - 13, 12, w.next, Math.min(9, R[w.next]), 0);
  },
};

function disc(x, cx, cy, r, c) {
  x.fillStyle = c;
  for (let y = -r; y <= r; y++) { const hw = Math.round(Math.sqrt(Math.max(0, r * r - y * y))); x.fillRect(cx - hw, cy + y, hw * 2, 1); }
}

// Pixel art doesn't turn well, so the round body stays put and only what's
// on it moves round, which is enough to see a thing roll.
function turned(cx, cy, a) {
  const c = Math.cos(a), s = Math.sin(a);
  return (dx, dy) => [cx + Math.round(dx * c - dy * s), cy + Math.round(dx * s + dy * c)];
}

function dab(x, p, w, h, c) { x.fillStyle = c; x.fillRect(p[0] - (w >> 1), p[1] - (h >> 1), w, h); }

function eye(x, p, n, c) {
  dab(x, p, n, n, "#0a0908"); dab(x, p, n - 2, n - 2, c); dab(x, p, Math.max(1, Math.round(n / 5)), n - 2, "#0a0908");
}

function mouth(x, at, half, dy, c) { for (let i = -half; i < half; i++) dab(x, at(i, dy), 1, 1, c); }

// Every stage is a round thing drawn at its size, turned by a.
function spawn(x, cx, cy, s, r, a) {
  r = Math.round(r);
  const at = turned(cx, cy, a), k = r / R[s];
  const sp = (dx, dy) => at(Math.round(dx * k), Math.round(dy * k));
  if (s === 0) {
    disc(x, cx, cy, r, "rgba(207,227,234,0.45)"); disc(x, cx, cy, r - 2, "rgba(207,227,234,0.25)");
    const y = sp(1, 1); disc(x, y[0], y[1], 2, "#0a0908");
    x.fillStyle = "#fff6dc"; x.fillRect(cx - r + 2, cy - r + 2, 2, 1);
    return;
  }
  if (s === 1) {
    disc(x, cx, cy, r, "rgba(207,227,234,0.4)");
    const h = sp(-1, 0); disc(x, h[0], h[1], 3, "#3a2c22");
    [[3, 0], [4, 1], [5, 1], [6, 0]].forEach(([dx, dy]) => dab(x, sp(dx, dy), 2, 2, "#3a2c22"));
    dab(x, sp(-2, -1), 1, 1, "#e9e3d6");
    x.fillStyle = "#fff6dc"; x.fillRect(cx - r + 3, cy - r + 2, 2, 1);
    return;
  }
  if (s === 2) {
    disc(x, cx, cy, r, "#3f8a44");
    let p = sp(0, 3); disc(x, p[0], p[1], r - 5, "#8fce7f");
    p = sp(0, 10); disc(x, p[0], p[1], 2, "#3f8a44");
    [-4, 4].forEach((dx) => eye(x, sp(dx, -8), 5, "#e8c552"));
    return;
  }
  if (s === 3) {
    disc(x, cx, cy, r, col[1]);
    const p = sp(0, 4); disc(x, p[0], p[1], r - 6, col[2]);
    dab(x, sp(-11, -1), 3, 3, col[3]); dab(x, sp(10, 2), 3, 3, col[3]);
    [-6, 6].forEach((dx) => eye(x, sp(dx, -11), 5, col[4]));
    mouth(x, sp, 4, -1, col[6]);
    return;
  }
  big(x, s, r, sp);
}

// Bullfrog, elder, great frog and pond king, each a size up on the last.
const LOOK = [
  null, null, null, null,
  { skin: "#5f7d2c", belly: "#e6d27a", dot: "#3e5a1e", dots: [[-13, -3], [11, -1], [-6, -13], [7, 12]], eyes: [7, -15, 7, "#ffcf4a"], lip: "#2a3a12", wide: 6, ears: [13, -7] },
  { skin: "#7a6a3a", belly: "#d8cfb8", dot: "#5a4a28", dots: [[-19, -4], [16, 0], [-6, -19], [12, -15]], eyes: [9, -19, 7, "#ffe27a"], lip: "#fff6dc", wide: 6, brows: true },
  { skin: "#2e6b5a", belly: "#bfe3c0", dot: "#e3b23a", dots: [[-24, -6], [20, -10], [-13, -22], [25, 7], [-27, 9], [4, -27]], eyes: [12, -24, 9, "#ff8a3c"], lip: "#163a30", wide: 10 },
  { skin: "#4c9a3c", belly: "#f0e2b0", dot: "#2f6b33", dots: [[-32, -4], [28, 0], [-22, -22], [24, -20], [-30, 12], [30, 14]], eyes: [14, -27, 11, "#ffe27a"], lip: "#1d3a18", wide: 14, crown: true },
];

const CROWN = ["g...g...g", "gg.ggg.gg", "ggggggggg", "glgrgrglg", "ggggggggg"], GOLD = { g: "#e3b23a", l: "#fff6dc", r: "#e2412d" };

function big(x, s, r, sp) {
  const o = LOOK[s];
  disc(x, ...sp(0, 0), r, o.skin);
  let p = sp(0, Math.round(R[s] * 0.22)); disc(x, p[0], p[1], r - Math.round(r / 3.6), o.belly);
  o.dots.forEach(([dx, dy]) => dab(x, sp(dx, dy), s > 5 ? 4 : 3, 3, o.dot));
  if (o.ears) [-1, 1].forEach((m) => { p = sp(m * o.ears[0], o.ears[1]); disc(x, p[0], p[1], 3, "#8a6a2a"); dab(x, p, 1, 1, "#c9974f"); });
  const [gap, ey, n, c] = o.eyes;
  [-gap, gap].forEach((dx) => eye(x, sp(dx, ey), n, c));
  if (o.brows) [-gap, gap].forEach((dx) => dab(x, sp(dx, ey - 5), 4, 1, "#fff6dc"));
  mouth(x, sp, o.wide, 1, o.lip);
  if (o.brows) mouth(x, sp, 3, 2, o.lip);
  if (o.crown) CROWN.forEach((row, ry) => [...row].forEach((ch, rx) => { if (ch !== ".") dab(x, sp((rx - 4) * 2, -40 + ry * 2), 3, 3, GOLD[ch]); }));
}

// A shelf by the pond with the jar on it, water behind the glass.
function scene(x) {
  ["#2a5163", "#33606e"].forEach((c, i) => { x.fillStyle = c; x.fillRect(0, i * 22, W, 22); });
  x.fillStyle = "#1f4a30"; for (let i = 0; i < W; i += 4) { const h = 5 + ((i * 37) % 7); x.fillRect(i, 44 - h, 4, h); }
  x.fillStyle = "#1d5163"; x.fillRect(0, 44, W, FLOOR + 4 - 44);
  x.fillStyle = "#3a2a1a"; x.fillRect(0, FLOOR + 4, W, H - FLOOR - 4);
  x.fillStyle = "#5a4630"; x.fillRect(0, FLOOR + 4, W, 3);
  ["#16404c", "#123a44", "#0f323c", "#0c2832"].forEach((c, i) => { x.fillStyle = c; x.fillRect(JL, LINE - 20 + i * 56, JR - JL, 56); });
}

function glass(x) {
  x.fillStyle = "rgba(207,227,234,0.5)";
  x.fillRect(JL - 3, LINE - 24, 3, FLOOR - LINE + 27); x.fillRect(JR, LINE - 24, 3, FLOOR - LINE + 27);
  x.fillRect(JL - 3, FLOOR, JR - JL + 6, 4);
  x.fillStyle = "rgba(255,246,220,0.6)"; x.fillRect(JL - 2, LINE - 22, 1, FLOOR - LINE + 20); x.fillRect(JL + 6, LINE, 2, 40);
  x.fillStyle = "#8a9099"; x.fillRect(JL - 6, LINE - 28, 9, 5); x.fillRect(JR - 3, LINE - 28, 9, 5);
}

// The label on the Play disk.
export function jarArt(x, g) {
  col = colours(g, null);
  x.fillStyle = "#2a5163"; x.fillRect(0, 0, 168, 96);
  x.fillStyle = "#3a2a1a"; x.fillRect(0, 88, 168, 8);
  x.fillStyle = "#123a44"; x.fillRect(44, 24, 80, 64);
  [[5, 92, 61, 27, 0.4], [0, 52, 81, 7, 0], [1, 63, 80, 8, 0.8], [2, 58, 64, 10, 0], [3, 70, 46, 12, -0.4], [0, 116, 81, 6, 0]].forEach(([s, cx, cy, r, a]) => spawn(x, cx, cy, s, r, a));
  x.fillStyle = "rgba(207,227,234,0.5)"; x.fillRect(41, 20, 3, 70); x.fillRect(124, 20, 3, 70); x.fillRect(41, 88, 86, 3);
  x.fillStyle = "rgba(255,246,220,0.35)"; for (let i = 46; i < 122; i += 6) x.fillRect(i, 32, 3, 1);
  drawTop(x, col, 78, 4, true);
}

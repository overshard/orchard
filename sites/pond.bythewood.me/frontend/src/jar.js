import { sfx } from "./sound.js";
import { colours } from "./frog.js";
import { drawTop } from "./top.js";
import { feel } from "./feel.js";

// Spawn jar. Frogspawn is dropped into a jar, and two of a stage that touch
// grow into one of the next, egg to tadpole to froglet to frog to elder, and
// elders have grown as far as they go. Anything resting over the line ends it.
const W = 200, H = 320, JL = 22, JR = 178, FLOOR = 300, LINE = 88, DROP = 58;
const R = [7, 11, 16, 22, 29], PAYS = [0, 2, 6, 15, 40], NAMES = ["", "TADPOLE", "FROGLET", "FROG", "ELDER"];
const GRAV = 620, STEP = 1 / 240, ITER = 8, SLOP = 0.25, FAST = 420, GRIP = 0.05, WAIT = 0.45, GRACE = 1.2, FULL = 1.6;

let G = null, col = null;
const F = feel();

function body(x, y, s) { return { x, y, vx: 0, vy: 0, s, r: R[s], grow: 1, age: 0, dx: Math.round(x), dy: Math.round(y) }; }

// Eggs come most, then tadpoles, and now and then a froglet.
function deal() { const k = Math.random(); return k < 0.6 ? 0 : k < 0.9 ? 1 : 2; }

function letGo() {
  if (!G || G.st !== "play" || G.wait > 0) return;
  const s = G.hand, r = R[s], x = Math.max(JL + r, Math.min(JR - r, G.aim));
  G.bodies.push(body(x, DROP, s));
  G.hand = G.next; G.next = deal(); G.wait = WAIT; G.drops++;
  sfx("plop"); F.squash(0.5);
}

// One small step. Gravity, then each pair pushed apart along the line between
// them and the walls held, a few times over, so a pile comes to rest.
function physics() {
  const bs = G.bodies;
  for (const b of bs) {
    b.vy += GRAV * STEP;
    const v = Math.hypot(b.vx, b.vy);
    if (v > FAST) { b.vx *= FAST / v; b.vy *= FAST / v; }
    b.x += b.vx * STEP; b.y += b.vy * STEP;
    if (b.grow < 1) b.grow = Math.min(1, b.grow + STEP * 7);
    b.r = R[b.s] * (0.75 + 0.25 * b.grow);
  }
  for (const b of bs) b.x0 = b.x;
  for (let it = 0; it < ITER; it++) {
    for (let i = 0; i < bs.length; i++) for (let j = i + 1; j < bs.length; j++) collide(bs[i], bs[j]);
    for (const b of bs) walls(b);
  }
  // The jar's floor grips, so something wedged between two on the floor
  // can't inch them apart, while a real shove still moves them.
  for (const b of bs) if (b.y + b.r >= FLOOR - 0.5 && Math.abs(b.x - b.x0) < GRIP) { b.x = b.x0; b.vx = 0; walls(b); }
}

function collide(a, b) {
  let dx = b.x - a.x, dy = b.y - a.y, d = Math.hypot(dx, dy);
  const pen = a.r + b.r - d;
  if (pen <= 0) return;
  if (d < 1e-6) { dx = 0.01; dy = -1; d = 1; }
  const nx = dx / d, ny = dy / d, ia = 1 / (a.r * a.r), ib = 1 / (b.r * b.r), sum = ia + ib;
  // Only what's past the slop is corrected, so a resting pile doesn't shiver.
  const fix = Math.max(0, pen - SLOP) * 0.8 / sum;
  a.x -= nx * fix * ia; a.y -= ny * fix * ia; b.x += nx * fix * ib; b.y += ny * fix * ib;
  const rvx = b.vx - a.vx, rvy = b.vy - a.vy, vn = rvx * nx + rvy * ny;
  if (vn >= 0) return;
  const jn = -(1.1 * vn) / sum;
  a.vx -= jn * ia * nx; a.vy -= jn * ia * ny; b.vx += jn * ib * nx; b.vy += jn * ib * ny;
  // A little grip so a ball sat in a gap stops rolling.
  const tx = -ny, ty = nx, vt = rvx * tx + rvy * ty, jt = Math.max(-0.15 * jn, Math.min(0.15 * jn, -vt / sum));
  a.vx -= jt * ia * tx; a.vy -= jt * ia * ty; b.vx += jt * ib * tx; b.vy += jt * ib * ty;
}

function walls(b) {
  if (b.x - b.r < JL) { b.x = JL + b.r; if (b.vx < 0) b.vx *= -0.15; }
  if (b.x + b.r > JR) { b.x = JR - b.r; if (b.vx > 0) b.vx *= -0.15; }
  if (b.y + b.r > FLOOR) { b.y = FLOOR - b.r; if (b.vy > 0) b.vy *= -0.15; b.vx *= 0.985; }
}

// Two of a stage that touch become one of the next where they met.
function merge() {
  const bs = G.bodies;
  for (let i = 0; i < bs.length; i++) for (let j = i + 1; j < bs.length; j++) {
    const a = bs[i], b = bs[j];
    if (a.s !== b.s || a.s === R.length - 1 || a.gone || b.gone || Math.hypot(b.x - a.x, b.y - a.y) > a.r + b.r + 0.5) continue;
    a.gone = b.gone = true;
    G.chain++;
    const n = body((a.x + b.x) / 2, (a.y + b.y) / 2, a.s + 1), x = n.x, y = n.y;
    n.vx = (a.vx + b.vx) / 2; n.vy = (a.vy + b.vy) / 2; n.grow = 0; n.r = R[n.s] * 0.75;
    walls(n);
    bs.push(n);
    G.score += PAYS[n.s]; G.made[n.s]++; G.best = Math.max(G.best, n.s);
    F.got(x, y, "+" + PAYS[n.s], { streak: G.chain, big: n.s >= 3 });
  }
  G.bodies = bs.filter((b) => !b.gone);
}

export const jar = {
  key: "jar", W, H, title: "SPAWN JAR", feel: F, edge: ["#2a5163", "#3a2a1a"],
  aria: "Drag along the top and let go to drop frogspawn into the jar, or use the arrow keys and space.",
  help: ["DRAG ALONG THE TOP AND LET GO", "TWO ALIKE GROW INTO THE NEXT", "EGG, TADPOLE, FROGLET, FROG, ELDER", "KEEP UNDER THE LINE", "TAP TO START"],

  reset(g, p) {
    col = colours(g, p.frog.hat);
    G = { st: "play", t: 0, st_t: 0, aim: W / 2, dir: 0, armed: false, bodies: [], hand: 0, next: deal(), wait: 0, acc: 0, score: 0, made: [0, 0, 0, 0, 0], best: 0, drops: 0, chain: 0, over: 0, end: 0 };
  },

  demo() { return G && !G.drops ? { x: 70, y: DROP - 20, to: [124, DROP - 20] } : null; },

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
    G.t += dt; G.st_t += dt;
    G.aim = Math.max(JL, Math.min(JR, G.aim + G.dir * 110 * dt));
    G.wait = Math.max(0, G.wait - dt);
    if (G.st !== "play") { if (G.st_t > 1.4) G.end = 1; return; }
    G.acc += dt;
    while (G.acc >= STEP) { G.acc -= STEP; physics(); merge(); }
    // A chain only counts while something is still settling.
    let moving = false, high = false;
    for (const b of G.bodies) {
      b.age += dt;
      const v = Math.hypot(b.vx, b.vy);
      if (v > 25) moving = true;
      if (b.age > GRACE && b.y - b.r < LINE && v < 60) high = true;
      if (Math.abs(b.x - b.dx) > 0.6) b.dx = Math.round(b.x);
      if (Math.abs(b.y - b.dy) > 0.6) b.dy = Math.round(b.y);
    }
    if (!moving) G.chain = 0;
    G.over = high ? G.over + dt : Math.max(0, G.over - dt * 2);
    if (G.over > FULL) { G.st = "full"; G.st_t = 0; F.crash(W / 2, LINE, { sound: "bonk" }); }
  },

  over() { return G && G.end; },
  score() { return G ? G.score : 0; },
  why() { return G ? "THE JAR IS FULL" : ""; },
  tally() {
    if (!G) return [];
    const l = [];
    for (let s = R.length - 1; s >= 1; s--) if (G.made[s]) l.push(G.made[s] + " " + NAMES[s].toLowerCase() + (G.made[s] > 1 ? "s" : "") + " grown");
    return l.slice(0, 3).map((s) => s.charAt(0).toUpperCase() + s.slice(1));
  },

  draw(x) {
    scene(x);
    if (!G) return;
    const warn = G.over > 0 && Math.floor(G.t * 6) % 2 === 0;
    x.fillStyle = warn ? "#e2412d" : "rgba(255,246,220,0.35)";
    for (let i = JL; i < JR; i += 6) x.fillRect(i, LINE, 3, 1);
    G.bodies.forEach((b) => spawn(x, b.dx, b.dy, b.s, b.r));
    glass(x);
    const fx = Math.round(G.aim);
    if (G.st === "play") {
      const r = R[G.hand], cx = Math.max(JL + r, Math.min(JR - r, fx));
      x.fillStyle = "rgba(255,246,220,0.2)";
      for (let y = DROP + r + 3; y < FLOOR; y += 6) x.fillRect(cx, y, 1, 3);
      if (G.wait <= 0) spawn(x, cx, DROP, G.hand, r);
    }
    F.squashed(x, fx, 30, () => drawTop(x, col, fx - 6, 18, true));
    F.draw(x);
    x.font = "700 13px 'Space Grotesk', sans-serif";
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(String(G.score), 6, 15);
    x.textAlign = "right"; x.fillStyle = "#cfe3ea"; x.fillText("NEXT", W - 26, 15);
    spawn(x, W - 13, 11, G.next, Math.min(9, R[G.next]));
  },
};

function disc(x, cx, cy, r, c) {
  x.fillStyle = c;
  for (let y = -r; y <= r; y++) { const hw = Math.round(Math.sqrt(Math.max(0, r * r - y * y))); x.fillRect(cx - hw, cy + y, hw * 2, 1); }
}

// Every stage is a round thing in its jelly, drawn at its size.
function spawn(x, cx, cy, s, r) {
  r = Math.round(r);
  if (s === 0) {
    disc(x, cx, cy, r, "rgba(207,227,234,0.45)"); disc(x, cx, cy, r - 2, "rgba(207,227,234,0.25)");
    disc(x, cx, cy, 2, "#0a0908"); x.fillStyle = "#fff6dc"; x.fillRect(cx - r + 2, cy - r + 2, 2, 1);
    return;
  }
  if (s === 1) {
    disc(x, cx, cy, r, "rgba(207,227,234,0.4)");
    x.fillStyle = "#3a2c22"; x.fillRect(cx - 4, cy - 3, 6, 5); x.fillRect(cx - 3, cy - 4, 4, 7);
    x.fillRect(cx + 2, cy - 1, 2, 2); x.fillRect(cx + 4, cy, 2, 1); x.fillRect(cx + 5, cy - 1, 2, 1);
    x.fillStyle = "#e9e3d6"; x.fillRect(cx - 3, cy - 2, 1, 1);
    return;
  }
  if (s === 2) {
    disc(x, cx, cy, r, "#3f8a44"); disc(x, cx, cy + 3, r - 5, "#8fce7f");
    x.fillStyle = "#3f8a44"; x.fillRect(cx - 2, cy + r - 2, 4, 3);
    eyes(x, cx, cy - r + 4, 4, "#e8c552");
    return;
  }
  if (s === 3) {
    disc(x, cx, cy, r, col[1]); disc(x, cx, cy + 4, r - 6, col[2]);
    x.fillStyle = col[3]; x.fillRect(cx - r + 4, cy - 2, 3, 3); x.fillRect(cx + r - 7, cy + 1, 3, 3);
    eyes(x, cx, cy - r + 5, 6, col[4]);
    x.fillStyle = col[6]; x.fillRect(cx - 4, cy - 1, 8, 1);
    return;
  }
  elder(x, cx, cy, r);
}

function elder(x, cx, cy, r) {
  disc(x, cx, cy, r, "#7a6a3a"); disc(x, cx, cy + 5, r - 7, "#d8cfb8");
  x.fillStyle = "#5a4a28"; x.fillRect(cx - r + 5, cy - 4, 4, 3); x.fillRect(cx + r - 9, cy, 4, 3);
  eyes(x, cx, cy - r + 6, Math.max(5, Math.round(r / 3.4)), "#ffe27a");
  x.fillStyle = "#fff6dc"; x.fillRect(cx - 5, cy + 1, 10, 1); x.fillRect(cx - 3, cy + 2, 6, 1);
}

function eyes(x, cx, cy, gap, c) {
  [-gap, gap].forEach((dx) => {
    x.fillStyle = "#0a0908"; x.fillRect(cx + dx - 2, cy - 2, 5, 5);
    x.fillStyle = c; x.fillRect(cx + dx - 1, cy - 1, 3, 3);
    x.fillStyle = "#0a0908"; x.fillRect(cx + dx, cy - 1, 1, 3);
  });
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
  [[0, 56, 81, 7], [1, 74, 79, 10], [2, 101, 75, 14], [3, 72, 58, 19], [0, 98, 54, 7], [1, 108, 40, 10]].forEach(([s, cx, cy, r]) => spawn(x, cx, cy, s, r));
  x.fillStyle = "rgba(207,227,234,0.5)"; x.fillRect(41, 20, 3, 70); x.fillRect(124, 20, 3, 70); x.fillRect(41, 88, 86, 3);
  x.fillStyle = "rgba(255,246,220,0.35)"; for (let i = 46; i < 122; i += 6) x.fillRect(i, 32, 3, 1);
  drawTop(x, col, 78, 4, true);
}

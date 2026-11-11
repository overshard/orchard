import { sfx } from "./sound.js";
import { colours } from "./frog.js";
import { drawTop } from "./top.js";
import { feel } from "./feel.js";

// Pebble drop. Five pebbles a round, each let go from a spot along the top
// to bounce down through rows of reed stubs into five pots. The middle pot
// pays most, and the day's deal nudges each row so no spot is best for good.
const W = 200, H = 320, TOP = 44, ROW0 = 74, ROWS = 9, GAP = 18, PITCH = 20;
const RIM = 262, FLOOR = 312, POT = W / 5, PAYS = [1, 3, 8, 3, 1], PEBBLES = 5;
const GRAV = 420, R = 4, PEG = 3, BOUNCE = 0.4, GRIP = 0.75, STEP = 1 / 240;

let G = null, col = null;
const F = feel();

// The pegs for a deal, one sideways nudge a row.
function pegsOf(deal) {
  const out = [];
  for (let r = 0; r < ROWS; r++) {
    const dx = (deal && deal[r]) || 0;
    for (let i = -1; i < 12; i++) {
      const x = (r % 2 ? 0 : PITCH / 2) + i * PITCH + dx;
      if (x >= 12 && x <= W - 12) out.push({ x, y: ROW0 + r * GAP, hit: 0 });
    }
  }
  return out;
}

function bounce(p, cx, cy, rad) {
  const dx = p.x - cx, dy = p.y - cy, d = Math.hypot(dx, dy);
  if (d >= rad + R || d === 0) return false;
  const nx = dx / d, ny = dy / d, vn = p.vx * nx + p.vy * ny;
  p.x = cx + nx * (rad + R); p.y = cy + ny * (rad + R);
  if (vn >= 0) return false;
  p.vx = (p.vx - (1 + BOUNCE) * vn * nx) * GRIP; p.vy -= (1 + BOUNCE) * vn * ny;
  // No two bounces are quite alike, so one pixel never wins every time.
  const a = (Math.random() - 0.5) * 0.3, c = Math.cos(a), s = Math.sin(a), vx = p.vx;
  p.vx = vx * c - p.vy * s; p.vy = vx * s + p.vy * c;
  if (Math.abs(p.vx) < 6) p.vx += (Math.random() - 0.5) * 24;
  return vn < -30;
}

function land(p) {
  const i = Math.max(0, Math.min(4, Math.floor(p.x / POT))), pay = PAYS[i], cx = i * POT + POT / 2;
  G.pots[i]++; G.score += pay; G.peb = null;
  if (i === 2) G.mid++;
  if (pay === 8) F.got(cx, RIM, "+8", { streak: G.mid, big: true });
  else if (pay === 3) F.got(cx, RIM, "+3", { sound: "hop" });
  else { sfx("plop"); F.burst(cx, RIM + 10, "#cfe3ea", 3); F.pop(cx, RIM - 10, "+1", "#bcaf99"); }
  if (pay === 3 && Math.abs(p.x - W / 2) < POT / 2 + 5) F.close(cx, RIM - 24);
  G.st = "rest"; G.st_t = 0;
}

// One small step of the fall. A pebble that's out of the reeds only has the
// pot walls left to meet.
function fall(p) {
  p.vy += GRAV * STEP;
  p.x += p.vx * STEP; p.y += p.vy * STEP;
  if (p.x < R) { p.x = R; p.vx = Math.abs(p.vx) * BOUNCE; }
  if (p.x > W - R) { p.x = W - R; p.vx = -Math.abs(p.vx) * BOUNCE; }
  G.pegs.forEach((q) => {
    if (bounce(p, q.x, q.y, PEG)) { q.hit = 0.18; sfx("kick", 1 + (G.ticks++ % 8)); }
  });
  for (let i = 1; i < 5; i++) {
    const wx = i * POT;
    if (p.y < RIM) bounce(p, wx, RIM, 2);
    else if (Math.abs(p.x - wx) < R + 1) { p.x = wx + (p.x < wx ? -1 : 1) * (R + 1); p.vx = -p.vx * 0.3; }
  }
  // One sat still on a stub gets a nudge off it.
  p.still = Math.hypot(p.vx, p.vy) < 14 ? p.still + STEP : 0;
  if (p.still > 0.25) { p.vx += Math.random() < 0.5 ? -34 : 34; p.still = 0; }
  if (p.y >= FLOOR - R) land(p);
}

function letGo() {
  if (!G || G.st !== "aim") return;
  G.peb = { x: G.aim, y: TOP, vx: 0, vy: 0, still: 0 };
  G.left--; G.st = "fall"; G.ticks = 0; G.acc = 0;
  sfx("cast"); F.squash(0.6);
}

export const drop = {
  key: "drop", W, H, title: "PEBBLE DROP", feel: F, edge: ["#2a5163", "#0c2832"],
  aria: "Drag along the top and let go to drop a pebble, or use the arrow keys and space.",
  help: ["DRAG ALONG THE TOP AND LET GO", "FIVE PEBBLES A ROUND", "THE MIDDLE POT PAYS 8", "TAP TO START"],

  reset(g, p) {
    col = colours(g, p.frog.hat);
    const a = (p.arcade || []).find((x) => x.key === "drop");
    G = { t: 0, st: "aim", st_t: 0, aim: W / 2, armed: false, dir: 0, left: PEBBLES, peb: null, pegs: pegsOf(a && a.deal), pots: [0, 0, 0, 0, 0], score: 0, mid: 0, ticks: 0, acc: 0, end: 0 };
  },

  demo() { return G && G.st === "aim" && G.left === PEBBLES ? { x: 64, y: TOP - 4, to: [112, TOP - 4] } : null; },

  press() { if (G) G.armed = true; },
  drag(x) { if (G) G.aim = Math.max(R + 2, Math.min(W - R - 2, x)); },
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
    G.aim = Math.max(R + 2, Math.min(W - R - 2, G.aim + G.dir * 90 * dt));
    G.pegs.forEach((q) => { q.hit = Math.max(0, q.hit - dt); });
    if (G.st === "fall") {
      // The last pebble falls slowly, and slower still over the pots.
      const slow = G.left > 0 ? 1 : G.peb.y > ROW0 + (ROWS - 1) * GAP ? 0.3 : 0.5;
      G.acc += dt * slow;
      while (G.acc >= STEP && G.peb) { G.acc -= STEP; fall(G.peb); }
    } else if (G.st === "rest" && G.st_t > (G.left ? 0.35 : 1)) {
      if (G.left) { G.st = "aim"; G.st_t = 0; } else G.end = 1;
    }
  },

  over() { return G && G.end; },
  score() { return G ? G.score : 0; },
  why() { return !G ? "" : G.mid === PEBBLES ? "FIVE IN THE MIDDLE" : "ALL FIVE DOWN"; },
  tally() { return G ? [G.mid + " of " + PEBBLES + " in the middle pot"] : []; },

  draw(x) {
    scene(x, G ? G.t : 0);
    if (!G) return;
    G.pegs.forEach((q) => stub(x, q.x, q.y, q.hit > 0));
    pots(x, G.pots);
    if (G.peb) pebble(x, G.peb.x, G.peb.y);
    const fx = Math.round(G.aim);
    F.squashed(x, fx, 30, () => drawTop(x, col, fx - 6, 18, true));
    if (G.st === "aim") {
      pebble(x, fx, TOP - 6);
      x.fillStyle = "rgba(255,246,220,0.25)";
      for (let y = TOP + 2; y < ROW0 - 8; y += 5) x.fillRect(fx, y, 1, 2);
    }
    F.draw(x);
    x.font = "700 13px 'Space Grotesk', sans-serif";
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(String(G.score), 6, 14);
    for (let i = 0; i < G.left - (G.st === "aim" ? 1 : 0); i++) pebble(x, W - 8 - i * 9, 9);
  },
};

// The label on the Play disk.
export function dropArt(x, g) {
  x.fillStyle = "#143c4a"; x.fillRect(0, 0, 168, 96);
  x.fillStyle = "#2a5163"; x.fillRect(0, 0, 168, 14);
  for (let r = 0; r < 4; r++) for (let i = 0; i < 7; i++) {
    const px = 24 + i * 22 + (r % 2 ? 11 : 0), py = 24 + r * 13;
    if (px > 150) continue;
    x.fillStyle = "#2f6b33"; x.fillRect(px, py + 2, 1, 5);
    x.fillStyle = "#8a5a2b"; x.fillRect(px - 2, py - 2, 5, 5);
    x.fillStyle = "#c9974f"; x.fillRect(px - 1, py - 2, 2, 1);
  }
  for (let i = 0; i < 5; i++) {
    x.fillStyle = i === 2 ? "#e3b23a" : "#8a9099"; x.fillRect(14 + i * 28, 78, 28, 3);
    x.fillStyle = "#3a3f46"; x.fillRect(16 + i * 28, 81, 24, 15);
  }
  [[86, 40], [61, 59], [92, 88], [80, 90], [36, 90]].forEach(([px, py]) => { x.fillStyle = "#c9ccd0"; x.fillRect(px, py, 5, 4); x.fillStyle = "#fff6dc"; x.fillRect(px + 1, py, 2, 1); });
  drawTop(x, colours(g, null), 78, 1, true);
}

function scene(x, t) {
  ["#2a5163", "#33606e"].forEach((c, i) => { x.fillStyle = c; x.fillRect(0, i * 22, W, 22); });
  x.fillStyle = "#1f4a30"; for (let i = 0; i < W; i += 4) { const h = 5 + ((i * 37) % 7); x.fillRect(i, TOP - h, 4, h); }
  ["#1d5163", "#17475a", "#143c4a", "#103440", "#0c2832"].forEach((c, i) => { x.fillStyle = c; x.fillRect(0, TOP + i * 56, W, 56); });
  x.fillStyle = "#7fd6c2"; for (let i = 0; i < W; i += 14) x.fillRect((i + Math.round(t * 5)) % W, TOP + 1, 6, 1);
}

// A cut reed from above, a brown stub with its stalk going down into the dark.
function stub(x, cx, cy, lit) {
  cx = Math.round(cx); cy = Math.round(cy);
  x.fillStyle = "#2f6b33"; x.fillRect(cx, cy + 3, 1, 6);
  x.fillStyle = lit ? "#e8c48a" : "#8a5a2b"; x.fillRect(cx - 3, cy - 2, 6, 4); x.fillRect(cx - 2, cy - 3, 4, 6);
  x.fillStyle = lit ? "#fff6dc" : "#c9974f"; x.fillRect(cx - 1, cy - 2, 2, 1);
}

function pebble(x, cx, cy) {
  cx = Math.round(cx); cy = Math.round(cy);
  x.fillStyle = "#8a9099"; x.fillRect(cx - 4, cy - 2, 8, 5); x.fillRect(cx - 3, cy - 3, 6, 7);
  x.fillStyle = "#c9ccd0"; x.fillRect(cx - 3, cy - 2, 5, 3);
  x.fillStyle = "#fff6dc"; x.fillRect(cx - 2, cy - 2, 2, 1);
}

// Five clay pots with what each pays on its side and what's landed in it.
function pots(x, held) {
  for (let i = 0; i < 5; i++) {
    const l = i * POT, mid = i === 2;
    x.fillStyle = "#0a1b1f"; x.fillRect(l + 2, RIM, POT - 4, FLOOR - RIM);
    for (let n = 0; n < held[i]; n++) pebble(x, l + 11 + (n % 3) * 9, FLOOR - 4 - Math.floor(n / 3) * 7);
    x.fillStyle = mid ? "#8a5a2b" : "#5a4630"; x.fillRect(l + 1, FLOOR, POT - 2, H - FLOOR);
    x.fillStyle = mid ? "#e3b23a" : "#8a9099"; x.fillRect(l, RIM - 2, 3, FLOOR - RIM + 2); x.fillRect(l + POT - 3, RIM - 2, 3, FLOOR - RIM + 2);
    x.font = "700 13px 'Space Grotesk', sans-serif"; x.textAlign = "center";
    x.fillStyle = mid ? "#ffe27a" : "#bcaf99"; x.globalAlpha = held[i] ? 0.35 : 1;
    x.fillText(String(PAYS[i]), l + POT / 2, RIM + 30);
    x.globalAlpha = 1;
  }
}

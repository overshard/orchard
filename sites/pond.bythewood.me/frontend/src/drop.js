import { sfx, chime, soundOn } from "./sound.js";
import { cells, colours, draw as drawFrog, croak, W as FW, H as FH } from "./frog.js";
import { drawTop } from "./top.js";
import { feel } from "./feel.js";
import { reduced } from "./api.js";

// Pebble drop. Ten pebbles a round, let go from spots along the top as fast
// as you like, to bounce down through rows of reed stubs into five pots. The
// middle pot pays most, and the day's deal nudges each row so no spot is best for good.
const W = 200, H = 320, TOP = 44, ROW0 = 74, ROWS = 9, GAP = 18, PITCH = 20;
const RIM = 262, FLOOR = 312, POT = W / 5, PAYS = [1, 3, 8, 3, 1], PEBBLES = 10, GAPS = 0.15;
const GRAV = 420, R = 4, PEG = 3, BOUNCE = 0.4, GRIP = 0.75, STEP = 1 / 240, KICK = 70, AIM = 14;
// Every reed lit pays LIT, the frog hands back FREE pebbles at most and the
// buds split SPLITS at most, so a round is never more than 13 landings.
const LIT = 10, BRUSH = 11, FREE = 3, SPLITS = 3, PAD = RIM - 5, SLOW = 0.4;
// A major pentatonic, and each pebble climbs its own stretch of it.
const PENTA = [0, 2, 4, 7, 9, 12, 14, 16, 19, 21, 24, 26, 28, 31, 33];

let G = null, col = null, spr = null, frogG = null, box = null;
const F = feel();

// The pegs for a deal, one sideways nudge a row, with a bud or two on
// pegs picked off the deal so everybody has them in the same places.
function pegsOf(deal) {
  const out = [];
  for (let r = 0; r < ROWS; r++) {
    const dx = (deal && deal[r]) || 0;
    for (let i = -1; i < 12; i++) {
      const x = (r % 2 ? 0 : PITCH / 2) + i * PITCH + dx;
      if (x >= 8 && x <= W - 8) out.push({ x, y: ROW0 + r * GAP, r, hit: 0, lit: false, bud: false, pop: -1 });
    }
  }
  let h = 17;
  (deal || []).forEach((v) => { h = (h * 7 + v + 3) % 1000003; });
  const buds = 1 + (h % 2);
  for (let b = 0; b < buds; b++) {
    const row = b ? 5 + ((h >> 2) % 2) : 2 + ((h >> 3) % 3), mine = out.filter((q) => q.r === row);
    mine[1 + ((h >> (5 + b * 3)) % (mine.length - 2))].bud = true;
  }
  return out;
}

function bounce(p, cx, cy, rad) {
  const dx = p.x - cx, dy = p.y - cy, d = Math.hypot(dx, dy);
  if (d >= rad + R || d === 0) return 0;
  const nx = dx / d, ny = dy / d, vn = p.vx * nx + p.vy * ny;
  p.x = cx + nx * (rad + R); p.y = cy + ny * (rad + R);
  if (vn >= 0) return 1;
  p.vx = (p.vx - (1 + BOUNCE) * vn * nx) * GRIP; p.vy -= (1 + BOUNCE) * vn * ny;
  // No two bounces are quite alike, so one pixel never wins every time.
  const a = (Math.random() - 0.5) * 0.3, c = Math.cos(a), s = Math.sin(a), vx = p.vx;
  p.vx = vx * c - p.vy * s; p.vy = vx * s + p.vy * c;
  if (Math.abs(p.vx) < 6) p.vx += (Math.random() - 0.5) * 24;
  return vn < -30 ? 2 : 1;
}

function pebbleAt(x, y, vx, vy) {
  const k = G.made++, lo = (k % 4) * 2;
  return { x, y, vx, vy, still: 0, note: lo, top: lo + 8 };
}

function ring(p) {
  if (G.t - G.chimed < 0.025) return;
  G.chimed = G.t;
  chime(PENTA[p.note], 0.028);
  p.note = Math.min(p.top, p.note + 1);
}

// A brush past lights a reed, and only a real knock rings it.
function struck(p, q, hard) {
  if (hard) { q.hit = 0.2; F.burst(q.x, q.y - 1, q.bud ? "#ffd0ea" : "#fff6dc", 2); ring(p); }
  if (!q.lit) {
    q.lit = true; G.lit++;
    if (G.lit === G.pegs.length) {
      G.score += LIT; G.all = true;
      sfx("perk"); F.pop(W / 2, ROW0 - 10, "ALL LIT +" + LIT, "#ffe27a");
    }
  }
  if (hard && q.bud && G.splits < SPLITS) {
    G.splits++;
    const v = 45 + Math.random() * 20, twin = pebbleAt(p.x, p.y, v, p.vy);
    p.vx = -v;
    G.pebs.push(twin);
    sfx("boing"); F.burst(q.x, q.y, "#e8559a", 5); F.pop(q.x, q.y - 8, "SPLIT", "#ffd0ea");
  }
}

// Where the frog's pad is at a time. Two swells, so its drift is hard to learn.
function frogX(t) { return W / 2 + Math.sin(t * 0.8) * 58 + Math.sin(t * 2.1 + 1) * 12; }

function caught(p) {
  if (G.caught >= FREE || G.frogT < 0) return false;
  const fx = frogX(G.frogT), top = PAD - (FH - box.y0) + 6;
  if (p.y < top || p.y > PAD || Math.abs(p.x - fx) > (box.x1 - box.x0) / 2 - 9) return false;
  p.done = true; G.left++; G.caught++; G.hop = 0.25;
  if (G.caught >= FREE) G.gone = 0;
  if (soundOn("sound")) croak(frogG); else sfx("gulp");
  F.burst(fx, top + 4, "#9fd27a", 6); F.pop(fx, top - 4, "+1 PEBBLE", "#cfe8a0");
  return true;
}

function land(p) {
  const i = Math.max(0, Math.min(4, Math.floor(p.x / POT))), cx = i * POT + POT / 2;
  // Three in a row in the middle three pots start a run that pays a little more.
  G.run = i >= 1 && i <= 3 ? G.run + 1 : 0;
  const more = G.run >= 6 ? 2 : G.run >= 3 ? 1 : 0, pay = PAYS[i] + more;
  G.pots[i]++; G.score += pay; p.done = true;
  if (G.run >= 2) G.rings.push({ x: cx, t: 0, n: Math.min(3, G.run - 1) });
  if (i === 2) G.mid++;
  if (G.slowing === p) {
    G.slowing = null;
    if (i === 2) { sfx("perk"); if (!reduced) G.wobble = 0.4; }
  }
  if (i === 2) F.got(cx, RIM, "+" + pay, { streak: G.mid, big: true });
  else if (PAYS[i] === 3) F.got(cx, RIM, "+" + pay, { sound: "hop" });
  else { sfx("plop"); F.burst(cx, RIM + 10, "#cfe3ea", 3); F.pop(cx, RIM - 10, "+" + pay, "#bcaf99"); }
  if (PAYS[i] === 3 && Math.abs(p.x - W / 2) < POT / 2 + 5) F.close(cx, RIM - 24);
}

// One small step of the fall. A pebble that's out of the reeds only has the
// frog and the pot walls left to meet.
function fall(p) {
  p.vy += GRAV * STEP;
  p.x += p.vx * STEP; p.y += p.vy * STEP;
  // The walls push back out, so nothing slides down the side past the reeds.
  if (p.x < R) { p.x = R; p.vx = Math.max(KICK, Math.abs(p.vx) * BOUNCE); }
  if (p.x > W - R) { p.x = W - R; p.vx = -Math.max(KICK, Math.abs(p.vx) * BOUNCE); }
  if (p.y < RIM - 30) G.pegs.forEach((q) => { const b = bounce(p, q.x, q.y, PEG); if (b || (!q.lit && Math.hypot(p.x - q.x, p.y - q.y) < BRUSH)) struck(p, q, b > 1); });
  else if (caught(p)) return;
  for (let i = 1; i < 5; i++) {
    const wx = i * POT;
    if (p.y < RIM) bounce(p, wx, RIM, 2);
    else if (Math.abs(p.x - wx) < R + 1) { p.x = wx + (p.x < wx ? -1 : 1) * (R + 1); p.vx = -p.vx * 0.3; }
  }
  // One sat still on a stub gets a nudge off it.
  p.still = Math.hypot(p.vx, p.vy) < 14 ? p.still + STEP : 0;
  if (p.still > 0.25) { p.vx += p.x < 20 ? 34 : p.x > W - 20 ? -34 : Math.random() < 0.5 ? -34 : 34; p.still = 0; }
  // The last pebble lined up on the 8 pot in the bottom rows falls slow.
  if (!G.slowing && !G.left && G.pebs.length === 1 && p.y > ROW0 + 7 * GAP - 4 && Math.abs(p.x - W / 2) < POT / 2 + 4) G.slowing = p;
  if (p.y >= FLOOR - R) land(p);
}

function letGo() {
  if (!G || G.left <= 0 || G.wait > 0 || G.st !== "play") return;
  G.pebs.push(pebbleAt(G.aim, TOP, 0, 0));
  G.left--; G.wait = GAPS;
  sfx("cast"); F.squash(0.6);
}

// The lit reeds go out one after another from the middle pot, once all is down.
function popAll() {
  const lit = G.pegs.filter((q) => q.lit).sort((a, b) => Math.hypot(a.x - W / 2, a.y - RIM) - Math.hypot(b.x - W / 2, b.y - RIM));
  lit.forEach((q, n) => { q.pop = 0.05 + (n / Math.max(1, lit.length)) * 0.7; q.n = n; });
}

export const drop = {
  key: "drop", W, H, title: "PEBBLE DROP", feel: F, edge: ["#2a5163", "#0c2832"],
  aria: "Drag along the top and let go to drop a pebble, or use the arrow keys and space. Light every reed and land pebbles on your frog for more.",
  help: ["DRAG ALONG THE TOP AND LET GO", "TEN PEBBLES, AS FAST AS YOU LIKE", "LIGHT EVERY REED FOR 10 MORE", "YOUR FROG CATCHES THREE BACK", "TAP TO START"],

  reset(g, p) {
    const stage = ["egg", "tadpole"].includes(p.frog.stage) ? "froglet" : p.frog.stage;
    spr = cells(g, stage, { hat: p.frog.hat.shape });
    col = colours(g, p.frog.hat);
    frogG = g;
    box = { x0: FW, x1: 0, y0: FH };
    for (let y = 0; y < FH; y++) for (let x = 0; x < FW; x++) {
      if (spr[y * FW + x]) { box.x0 = Math.min(box.x0, x); box.x1 = Math.max(box.x1, x); box.y0 = Math.min(box.y0, y); }
    }
    const a = (p.arcade || []).find((x) => x.key === "drop");
    G = {
      t: 0, st: "play", st_t: 0, aim: W / 2, armed: false, dir: 0, left: PEBBLES, pebs: [], wait: 0, pegs: pegsOf(a && a.deal),
      pots: [0, 0, 0, 0, 0], score: 0, mid: 0, acc: 0, end: 0, made: 0, chimed: -1, lit: 0, all: false, splits: 0,
      caught: 0, frogT: 0, hop: 0, gone: -1, run: 0, rings: [], slow: 1, slowing: null, wobble: 0,
    };
  },

  demo() { return G && G.st === "play" && G.left === PEBBLES && !G.pebs.length ? { x: 64, y: TOP - 4, to: [112, TOP - 4] } : null; },

  press() { if (G) G.armed = true; },
  drag(x) { if (G) G.aim = Math.max(AIM, Math.min(W - AIM, x)); },
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
    G.slow += ((G.slowing ? SLOW : 1) - G.slow) * Math.min(1, dt * 8);
    const sdt = dt * G.slow;
    G.aim = Math.max(AIM, Math.min(W - AIM, G.aim + G.dir * 90 * dt));
    G.pegs.forEach((q) => { q.hit = Math.max(0, q.hit - dt); });
    G.wait = Math.max(0, G.wait - dt);
    G.hop = Math.max(0, G.hop - dt);
    G.wobble = Math.max(0, G.wobble - dt);
    if (G.gone >= 0) G.gone += dt;
    if (G.gone < 0) G.frogT += sdt;
    G.rings.forEach((r) => { r.t += dt; });
    G.rings = G.rings.filter((r) => r.t < 0.8);
    G.acc += sdt;
    while (G.acc >= STEP) { G.acc -= STEP; G.pebs.forEach((p) => { if (!p.done) fall(p); }); }
    G.pebs = G.pebs.filter((p) => !p.done);
    if (G.slowing && G.slowing.done) G.slowing = null;
    if (G.st === "play" && !G.left && !G.pebs.length) { G.st = "pop"; G.st_t = 0; popAll(); }
    if (G.st === "pop") {
      G.pegs.forEach((q) => {
        if (q.pop < 0 || q.pop > G.st_t) return;
        q.pop = -1; q.lit = false; q.hit = 0.15;
        F.burst(q.x, q.y - 1, q.bud ? "#ffd0ea" : "#ffe27a", 2);
        if (q.n % 3 === 0) chime(PENTA[Math.min(PENTA.length - 1, 2 + Math.floor(q.n / 9))], 0.02);
      });
      if (G.st_t > 1.4) G.end = 1;
    }
  },

  over() { return G && G.end; },
  score() { return G ? G.score : 0; },
  why() { return !G ? "" : G.all ? "EVERY REED LIT" : G.mid >= PEBBLES ? "TEN IN THE MIDDLE" : "ALL DOWN"; },
  report() { return { counts: { mid: G.mid, pots: G.pots.filter((n) => n).length, lit: G.all ? 1 : 0, caught: G.caught, split: G.splits } }; },
  tally() {
    if (!G) return [];
    const out = [G.mid + " in the middle pot", G.lit + " of " + G.pegs.length + " reeds lit"];
    if (G.caught) out.push(G.caught + " caught by your frog");
    return out;
  },

  draw(x) {
    scene(x, G ? G.t : 0);
    if (!G) return;
    const z = reduced ? 0 : (1 - G.slow) / (1 - SLOW);
    x.save();
    if (z > 0.01) { x.translate(W / 2, RIM); x.scale(1 + 0.08 * z, 1 + 0.08 * z); x.translate(-W / 2, -RIM); }
    G.pegs.forEach((q) => stub(x, q, G.splits < SPLITS));
    rings(x);
    pots(x, G.pots, G.wobble);
    frog(x);
    G.pebs.forEach((p) => pebble(x, p.x, p.y));
    x.restore();
    const fx = Math.round(G.aim);
    F.squashed(x, fx, 30, () => drawTop(x, col, fx - 6, 18, true));
    if (G.left > 0) {
      if (!G.wait) pebble(x, fx, TOP - 6);
      x.fillStyle = "rgba(255,246,220,0.25)";
      for (let y = TOP + 2; y < ROW0 - 8; y += 5) x.fillRect(fx, y, 1, 2);
    }
    F.draw(x);
    x.font = "700 13px 'Space Grotesk', sans-serif";
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(String(G.score), 6, 14);
    x.font = "700 9px 'Space Grotesk', sans-serif";
    x.fillStyle = G.all ? "#ffe27a" : "#bcaf99"; x.fillText(G.lit + "/" + G.pegs.length + " LIT", 6, 26);
    for (let i = 0; i < G.left - 1; i++) pebble(x, W - 8 - (i % 5) * 9, 9 + Math.floor(i / 5) * 8);
  },
};

// The label on the Play disk.
export function dropArt(x, g) {
  x.fillStyle = "#143c4a"; x.fillRect(0, 0, 168, 96);
  x.fillStyle = "#2a5163"; x.fillRect(0, 0, 168, 14);
  for (let r = 0; r < 4; r++) for (let i = 0; i < 7; i++) {
    const px = 24 + i * 22 + (r % 2 ? 11 : 0), py = 24 + r * 13, lit = (r + i) % 3 === 0;
    if (px > 150) continue;
    x.fillStyle = "#2f6b33"; x.fillRect(px, py + 2, 1, 5);
    x.fillStyle = lit ? "#e3b23a" : "#8a5a2b"; x.fillRect(px - 2, py - 2, 5, 5);
    x.fillStyle = lit ? "#fff6dc" : "#c9974f"; x.fillRect(px - 1, py - 2, 2, 1);
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

// A cut reed from above, a brown stub with its stalk going down into the
// dark. A lit one is amber, and a bud is pink while it can still split.
function stub(x, q, open) {
  const cx = Math.round(q.x), cy = Math.round(q.y), flash = q.hit > 0;
  x.fillStyle = "#2f6b33"; x.fillRect(cx, cy + 3, 1, 6);
  if (q.bud) {
    x.fillStyle = flash ? "#fff6dc" : open ? "#e8559a" : "#4f9a4a"; x.fillRect(cx - 3, cy - 2, 6, 4); x.fillRect(cx - 2, cy - 3, 4, 6);
    x.fillStyle = open ? "#ffd0ea" : q.lit ? "#e3b23a" : "#2f6b33"; x.fillRect(cx - 1, cy - 1, 2, 2);
    if (open) { x.fillStyle = "#4f9a4a"; x.fillRect(cx - 3, cy + 1, 2, 1); x.fillRect(cx + 1, cy + 1, 2, 1); }
    return;
  }
  x.fillStyle = flash ? "#fff6dc" : q.lit ? "#e3b23a" : "#8a5a2b"; x.fillRect(cx - 3, cy - 2, 6, 4); x.fillRect(cx - 2, cy - 3, 4, 6);
  x.fillStyle = flash || q.lit ? "#fff6dc" : "#c9974f"; x.fillRect(cx - 1, cy - 2, 2, 1);
}

function pebble(x, cx, cy) {
  cx = Math.round(cx); cy = Math.round(cy);
  x.fillStyle = "#8a9099"; x.fillRect(cx - 4, cy - 2, 8, 5); x.fillRect(cx - 3, cy - 3, 6, 7);
  x.fillStyle = "#c9ccd0"; x.fillRect(cx - 3, cy - 2, 5, 3);
  x.fillStyle = "#fff6dc"; x.fillRect(cx - 2, cy - 2, 2, 1);
}

// The frog on its pad, drifting over the pots. Once it's handed back all it
// can it slips under with the pad.
function frog(x) {
  if (G.gone > 0.6) return;
  const fx = Math.round(frogX(G.frogT)), sink = G.gone > 0 ? Math.round(G.gone * 30) : 0;
  const lift = G.hop > 0 ? Math.round(Math.sin((G.hop / 0.25) * Math.PI) * 3) : 0;
  x.save();
  x.beginPath(); x.rect(0, 0, W, RIM - 1); x.clip();
  x.fillStyle = "#2f6b33"; x.fillRect(fx - 22, PAD + 1 + sink, 44, 3); x.fillRect(fx - 18, PAD + 4 + sink, 36, 1);
  x.fillStyle = "#4f9a4a"; x.fillRect(fx - 20, PAD - 1 + sink, 40, 3); x.fillRect(fx - 22, PAD + sink, 44, 2);
  x.fillStyle = "#143c4a"; x.fillRect(fx + 9, PAD - 1 + sink, 3, 2);
  drawFrog(x, spr, FW, FH, col, fx - FW / 2, PAD - FH + 2 + sink - lift, 1);
  x.restore();
}

// Rings on the water over a middle pot while a run of them goes on.
function rings(x) {
  G.rings.forEach((r) => {
    for (let k = 0; k < r.n; k++) {
      const w = Math.round(6 + (r.t + k * 0.12) * 30);
      x.globalAlpha = Math.max(0, 0.7 - r.t);
      x.strokeStyle = "#7fd6c2"; x.lineWidth = 1;
      x.beginPath(); x.ellipse(r.x, RIM - 6, w, w / 3, 0, 0, Math.PI * 2); x.stroke();
    }
  });
  x.globalAlpha = 1;
}

// Five clay pots with what each pays on its side and what's landed in it.
function pots(x, held, wobble) {
  for (let i = 0; i < 5; i++) {
    const mid = i === 2, l = i * POT + (mid && wobble ? Math.round(Math.sin(wobble * 60) * wobble * 4) : 0);
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

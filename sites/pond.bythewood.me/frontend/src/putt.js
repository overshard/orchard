import { sfx } from "./sound.js";
import { colours } from "./frog.js";
import { drawTop } from "./top.js";
import { feel } from "./feel.js";

// Pad putt. Nine holes of lily pads dealt by the server for the day, and the
// frog hops pad to pad to the flag. Drag back and let go to hop, the longer
// the pull the longer the hop, and the day's wind carries it a little.
const W = 200, H = 320, REACH = 120, PULL = 1.5, LEAST = 10, OVER = 4;
const FONT = "700 13px 'Space Grotesk', sans-serif";

let G = null, col = null;
const F = feel();

function hole() { return G.holes && G.holes[G.hi]; }
function pad(i) { return hole().pads[i]; }

// Each hole pays most in its par and two less a hop past it, down to 2.
function pointsFor(hops, par) { return Math.max(2, Math.min(12, 10 - 2 * (hops - par))); }

function windWord(w) { const m = Math.hypot(w[0], w[1]); return m < 1 ? "CALM" : m < 15 ? "BREEZY" : "WINDY"; }

function startHole() {
  const p = pad(0);
  G.st = "intro"; G.st_t = 0; G.at = 0; G.hops = 0; G.fx = p.x; G.fy = p.y; G.armed = false;
}

function aim() {
  if (G.armed) {
    const dx = G.from[0] - G.to[0], dy = G.from[1] - G.to[1], len = Math.hypot(dx, dy) * PULL;
    return len < LEAST ? null : { a: Math.atan2(dy, dx), len: Math.min(REACH, len) };
  }
  return G.keyed ? { a: G.ka, len: G.kl } : null;
}

function hop(a) {
  if (!a || G.st !== "aim") return;
  const w = hole().wind, k = a.len / REACH;
  G.fly = { x0: G.fx, y0: G.fy, x1: G.fx + Math.cos(a.a) * a.len + w[0] * k, y1: G.fy + Math.sin(a.a) * a.len + w[1] * k, t: 0, dur: 0.32 + 0.38 * k, len: a.len };
  G.hops++; G.total++; G.st = "fly"; G.st_t = 0; G.keyed = false;
  sfx("hop", Math.min(8, G.hops)); F.squash(0.6);
}

function landed() {
  const f = G.fly, h = hole(), last = h.pads.length - 1;
  G.fx = f.x1; G.fy = f.y1; G.fly = null; G.st_t = 0;
  const i = h.pads.findIndex((p) => Math.hypot(p.x - G.fx, p.y - G.fy) <= p.r + 3);
  if (i < 0) {
    G.st = "splash";
    sfx("plop"); F.burst(G.fx, G.fy, "#cfe3ea", 8); F.pop(G.fx, G.fy - 8, "SPLASH", "#7fd6c2");
    return;
  }
  // The frog settles onto the pad, never hanging off its edge.
  const p = h.pads[i], d = Math.hypot(G.fx - p.x, G.fy - p.y), room = Math.max(0, p.r - 4);
  if (d > room) { G.fx = p.x + (G.fx - p.x) / d * room; G.fy = p.y + (G.fy - p.y) / d * room; }
  G.at = i;
  if (i === last) {
    const pts = pointsFor(G.hops, h.par);
    G.card[G.hi] = G.hops; G.score += pts; G.st = "holed";
    F.got(p.x, p.y, "+" + pts, { big: G.hops <= h.par, streak: G.hi + 1, sound: "land" });
    if (G.hops < h.par) F.pop(p.x, p.y - 26, "UNDER PAR", "#ffb000");
    return;
  }
  F.squash(0.8); F.burst(G.fx, G.fy + 4, "#56b45f", 4); sfx("kick");
  if (Math.hypot(h.pads[last].x - G.fx, h.pads[last].y - G.fy) < 60 && G.hops === 1) F.close(G.fx, G.fy - 16);
  G.st = G.hops >= h.par + OVER ? "out" : "aim";
}

export const putt = {
  key: "putt", W, H, title: "PAD PUTT", feel: F, edge: ["#123a3d", "#123a3d"],
  aria: "Drag back from anywhere and let go to hop pad to pad to the flag, or turn with the arrow keys and press space.",
  help: ["DRAG BACK AND LET GO TO HOP", "REACH THE FLAG IN FEW HOPS", "THE WIND CARRIES A HOP", "TAP TO START"],

  reset(g, p) {
    col = colours(g, p.frog.hat);
    G = { st: "wait", st_t: 0, t: 0, holes: null, hi: 0, at: 0, hops: 0, total: 0, par: 0, score: 0, card: [], fx: W / 2, fy: H - 30, armed: false, from: [0, 0], to: [0, 0], keyed: false, ka: -Math.PI / 2, kl: 70, turn: 0, push: 0, fly: null };
  },

  dealt(d) {
    if (!G || !d || !d.putt) return;
    G.holes = d.putt; G.par = d.putt.reduce((a, h) => a + h.par, 0);
    G.card = d.putt.map(() => null);
    startHole();
  },

  demo() {
    if (!G || G.hi || G.hops || (G.st !== "aim" && G.st !== "wait" && G.st !== "intro")) return null;
    return { x: G.fx - 30, y: G.fy - 76, to: [G.fx - 24, G.fy - 40] };
  },

  press(x, y) { if (G && G.st === "aim") { G.armed = true; G.from = [x, y]; G.to = [x, y]; G.keyed = false; } },
  drag(x, y) { if (G && G.armed) G.to = [x, y]; },
  release() {
    if (!G || !G.armed) return;
    const a = aim();
    G.armed = false;
    hop(a);
  },

  keys(code, down) {
    if (!G) return;
    const turn = { ArrowLeft: -1, KeyA: -1, ArrowRight: 1, KeyD: 1 }[code], push = { ArrowUp: 1, KeyW: 1, ArrowDown: -1, KeyS: -1 }[code];
    if (turn) G.turn = down ? turn : G.turn === turn ? 0 : G.turn;
    if (push) G.push = down ? push : G.push === push ? 0 : G.push;
    if ((turn || push) && down) G.keyed = true;
    if (down && (code === "Space" || code === "Enter")) hop(aim());
  },

  step(dt) {
    G.t += dt; G.st_t += dt;
    G.ka += G.turn * 1.6 * dt;
    G.kl = Math.max(LEAST, Math.min(REACH, G.kl + G.push * 60 * dt));
    if (G.st === "intro" && G.st_t > 1.1) { G.st = "aim"; G.st_t = 0; }
    if (G.st === "fly") {
      G.fly.t += dt;
      if (G.fly.t >= G.fly.dur) landed();
    }
    if (G.st === "splash" && G.st_t > 0.7) {
      const p = pad(G.at);
      G.fx = p.x; G.fy = p.y;
      G.st = G.hops >= hole().par + OVER ? "out" : "aim"; G.st_t = 0;
    }
    if (G.st === "out" && G.st_t > 0.9) { G.card[G.hi] = 0; next(); }
    if (G.st === "holed" && G.st_t > 1.2) next();
  },

  over() { return G && G.st === "done" && G.st_t > 0.5; },
  score() { return G ? G.score : 0; },
  why() { return !G || !G.holes ? "" : G.total + " HOPS, PAR " + G.par; },
  tally() { return G ? [G.card.filter((c) => c).length + " of 9 holes reached"] : []; },

  // The scorecard, a box a hole with its par over its hops, amber under par.
  card() {
    if (!G || !G.holes) return null;
    const c = document.createElement("canvas"), x = c.getContext("2d"), cw = 58;
    c.width = cw * 9 + 4; c.height = 84;
    c.style.width = (c.width / 2) + "px";
    x.textAlign = "center";
    G.holes.forEach((h, i) => {
      const n = G.card[i], l = 2 + i * cw;
      x.fillStyle = "#0b1f17"; x.fillRect(l, 0, cw - 4, 84);
      x.fillStyle = "#7d9a90"; x.font = "700 24px 'JetBrains Mono', monospace"; x.fillText(String(h.par), l + cw / 2 - 2, 28);
      x.fillStyle = !n ? "#5b6470" : n < h.par ? "#ffb000" : n === h.par ? "#fff6dc" : "#bcaf99";
      x.font = "700 32px 'JetBrains Mono', monospace"; x.fillText(n === null ? "" : n ? String(n) : "-", l + cw / 2 - 2, 70);
    });
    return c;
  },

  draw(x) {
    water(x, G ? G.t : 0, G && G.holes ? hole().wind : [0, 0]);
    if (!G) return;
    if (!G.holes) { lily(x, W / 2, H - 30, 13, 0, true); frog(x, G.fx, G.fy, 0); return; }
    const h = hole(), last = h.pads.length - 1;
    h.pads.forEach((p, i) => lily(x, p.x, p.y, p.r, i, i === 0));
    flag(x, h.pads[last], h.wind, G.t);
    const a = G.st === "aim" && aim();
    if (a) {
      const ex = G.fx + Math.cos(a.a) * a.len, ey = G.fy + Math.sin(a.a) * a.len;
      x.fillStyle = "rgba(255,246,220,0.7)";
      for (let d = 10; d < a.len - 4; d += 6) x.fillRect(Math.round(G.fx + Math.cos(a.a) * d), Math.round(G.fy + Math.sin(a.a) * d), 2, 2);
      x.strokeStyle = "#fff6dc"; x.lineWidth = 1; x.strokeRect(Math.round(ex) - 3.5, Math.round(ey) - 3.5, 7, 7);
    }
    if (G.fly) {
      const k = Math.min(1, G.fly.t / G.fly.dur), up = Math.sin(Math.PI * k) * (6 + G.fly.len * 0.12);
      const fx = G.fly.x0 + (G.fly.x1 - G.fly.x0) * k, fy = G.fly.y0 + (G.fly.y1 - G.fly.y0) * k;
      x.fillStyle = "rgba(10,27,31,0.5)"; x.fillRect(Math.round(fx) - 5, Math.round(fy) - 2, 10, 5);
      frog(x, fx, fy, up);
    } else if (G.st !== "splash") F.squashed(x, G.fx, G.fy + 6, () => frog(x, G.fx, G.fy, 0));
    F.draw(x);
    hud(x, h);
    if (G.st === "intro") banner(x, "HOLE " + (G.hi + 1), "PAR " + h.par);
    if (G.st === "out") banner(x, "NEXT HOLE", "");
  },
};

function next() {
  G.hi++;
  if (G.hi >= G.holes.length) { G.hi--; G.st = "done"; G.st_t = 0; return; }
  startHole();
}

function frog(x, fx, fy, up) { drawTop(x, col, Math.round(fx) - 6, Math.round(fy - up) - 6, false); }

function hud(x, h) {
  x.font = FONT; x.textAlign = "left"; x.fillStyle = "#ece5d8";
  x.fillText((G.hi + 1) + "/9", 6, 15);
  x.fillStyle = "#cfe3ea"; x.fillText("PAR " + h.par, 40, 15);
  x.fillStyle = "#ffe27a"; x.fillText("HOP " + G.hops, 92, 15);
  x.textAlign = "right"; x.fillStyle = "#ece5d8"; x.fillText(String(G.score), W - 6, 15);
  // The windsock in the bottom corner, pointing where the wind blows.
  const m = Math.hypot(h.wind[0], h.wind[1]);
  x.textAlign = "left"; x.fillStyle = "#cfe3ea"; x.fillText(windWord(h.wind), 22, H - 7);
  x.fillStyle = "#8a5a2b"; x.fillRect(9, H - 20, 2, 16);
  if (m < 1) { x.fillStyle = "#e2567a"; x.fillRect(10, H - 19, 2, 7); return; }
  const ux = h.wind[0] / m, uy = h.wind[1] / m;
  for (let i = 0; i < 4 + Math.round(m / 5); i++) { x.fillStyle = i % 2 ? "#fff6dc" : "#e2567a"; x.fillRect(Math.round(10 + ux * i * 1.5), Math.round(H - 18 + uy * i * 1.5), 2, 2); }
}

function banner(x, a, b) {
  x.fillStyle = "rgba(10,27,31,0.85)"; x.fillRect(0, H / 2 - 24, W, 42);
  x.fillStyle = "#ffb000"; x.fillRect(0, H / 2 - 24, W, 1); x.fillRect(0, H / 2 + 17, W, 1);
  x.textAlign = "center"; x.font = "700 18px 'Space Grotesk', sans-serif"; x.fillStyle = "#fff6dc"; x.fillText(a, W / 2, H / 2 - 2);
  x.font = FONT; x.fillStyle = "#cfe3ea"; x.fillText(b, W / 2, H / 2 + 13);
}

// Open water seen from above, with ripples drifting on the wind.
function water(x, t, w) {
  x.fillStyle = "#123a3d"; x.fillRect(0, 0, W, H);
  x.fillStyle = "#103337";
  for (let i = 0, n = 7; i < 30; i++) { n = (n * 1103515245 + 12345) >>> 0; x.fillRect(n % W, (n >>> 10) % H, 10 + (n >>> 20) % 14, 2); }
  const vx = 4 + w[0] * 1.2, vy = w[1] * 1.2;
  x.fillStyle = "rgba(127,214,194,0.28)";
  for (let i = 0; i < 26; i++) {
    const px = ((i * 53 + t * vx) % W + W) % W, py = ((i * 97 + t * vy) % H + H) % H;
    x.fillRect(Math.round(px), Math.round(py), 5, 1);
  }
}

// A lily pad from above, a disc with a notch cut out of it.
function lily(x, cx, cy, r, i, tee) {
  for (let y = -r; y <= r; y++) {
    const hw = Math.round(Math.sqrt(r * r - y * y));
    x.fillStyle = tee ? "#4f9a4a" : "#3f8a44"; x.fillRect(cx - hw, cy + y, hw * 2, 1);
  }
  x.fillStyle = tee ? "#7ac46f" : "#56b45f";
  for (let y = -r + 1; y < 0; y++) { const hw = Math.round(Math.sqrt((r - 2) * (r - 2) - y * y) * 0.7); x.fillRect(cx - hw - 2, cy + y, hw, 1); }
  const a = (i * 2.4) % (Math.PI * 2);
  x.fillStyle = "#123a3d";
  for (let d = 2; d <= r; d++) x.fillRect(Math.round(cx + Math.cos(a) * d), Math.round(cy + Math.sin(a) * d), 2, 2);
  x.fillStyle = "#2f6b33"; x.fillRect(cx - r, cy + r, r * 2, 1);
}

// The flag streams the way the wind blows and hangs on a calm day.
function flag(x, p, w, t) {
  const bx = p.x + 2, top = p.y - 22, m = Math.hypot(w[0], w[1]), dir = w[0] < 0 ? -1 : 1;
  x.fillStyle = "#3a2412"; x.fillRect(bx, top, 1, 23);
  const long = m < 1 ? 5 : 7 + Math.round(m / 6), flap = m < 1 ? 0 : Math.round(Math.sin(t * (4 + m / 3)));
  for (let i = 0; i < long; i++) {
    x.fillStyle = i % 4 < 2 ? "#e2567a" : "#f59ab3";
    x.fillRect(bx + dir * (i + 1), top + (m < 1 ? i : i > long / 2 ? flap : 0), 1, Math.max(1, Math.round(6 * (1 - i / long))));
  }
}

// The label on the Play disk.
export function puttArt(x, g) {
  x.fillStyle = "#123a3d"; x.fillRect(0, 0, 168, 96);
  x.fillStyle = "#0e2c30"; for (let i = 0; i < 16; i++) x.fillRect((i * 67) % 168, (i * 41) % 96, 14, 3);
  [[30, 78, 11], [76, 56, 9], [118, 34, 9], [146, 70, 7], [52, 22, 6]].forEach(([cx, cy, r], i) => lily(x, cx, cy, r, i, i === 0));
  flag(x, { x: 118, y: 34 }, [8, 0], 0);
  x.fillStyle = "rgba(255,246,220,0.7)"; for (let d = 0; d < 7; d++) x.fillRect(36 + d * 6, 70 - d * 3, 2, 2);
  drawTop(x, colours(g, null), 24, 72, false);
}

import { sfx } from "./sound.js";
import { sideCells, colours, draw, SW, SH } from "./frog.js";
import { feel } from "./feel.js";

// Lily stack. A pad slides over the stack and a tap drops it, and whatever
// hangs past the pad below falls in the pond. Three perfect drops in a row
// start growing the pad back. The frog hops up onto every new pad.
const W = 180, H = 300, T = 10, BASE = 84, Y0 = H - 46, NEAR = 2.5;

let G = null, spr = null, col = null;
const F = feel();

function top() { return G.pads[G.pads.length - 1]; }

function next() {
  const n = G.pads.length, t = top(), left = n % 2 === 0;
  G.mv = { x: left ? -t.w * 0.5 : W - t.w * 0.5, w: t.w, dir: left ? 1 : -1, v: Math.min(230, 75 + n * 5) };
}

function drop() {
  const a = top(), b = G.mv, lo = Math.max(a.x, b.x), hi = Math.min(a.x + a.w, b.x + b.w);
  const y = Y0 - G.pads.length * T;
  if (hi - lo <= 0) {
    G.falls.push({ x: b.x, w: b.w, y, vy: 0 });
    G.mv = null; G.dead = 0.01; G.why = "SPLASH";
    F.crash(b.x + b.w / 2, y + T / 2);
    return;
  }
  let pts = 1;
  if (Math.abs(b.x - a.x) <= NEAR) {
    G.perf++; pts = 2;
    b.x = a.x;
    if (G.perf >= 3) { const w = Math.min(BASE, a.w + 6); b.x = a.x - (w - a.w) / 2; b.w = w; }
    F.got(b.x + b.w / 2, y - 4, G.perf >= 3 ? "PERFECT +" : "PERFECT", { streak: G.perf, big: G.perf >= 3 });
    G.ring = 0.01;
  } else {
    G.perf = 0;
    G.falls.push(b.x < a.x ? { x: b.x, w: a.x - b.x, y, vy: 0 } : { x: hi, w: b.x + b.w - hi, y, vy: 0 });
    b.x = lo; b.w = hi - lo;
    sfx("hop");
  }
  G.pads.push({ x: b.x, w: b.w, flower: G.perf > 0 });
  G.score += pts;
  G.hop = 0.01;
  next();
}

export const stack = {
  key: "stack", W, H, title: "LILY STACK", feel: F,
  aria: "Tap to drop the pad.",
  help: ["TAP TO DROP THE PAD", "WHAT HANGS OVER FALLS OFF", "LINE THEM UP FOR A PERFECT", "TAP TO START"],

  reset(g, p) {
    spr = sideCells(g, p.frog.stage === "egg" ? "tadpole" : p.frog.stage, false);
    col = colours(g, p.frog.hat);
    G = { t: 0, pads: [{ x: (W - BASE) / 2, w: BASE }], mv: null, falls: [], score: 0, perf: 0, cam: 0, hop: 0, dead: 0, ring: 0, why: "" };
    next();
  },

  demo() { return { x: W / 2, y: H - 70 }; },

  press() { if (G && G.mv && !G.dead) drop(); },
  keyPress() { stack.press(); },

  step(dt) {
    G.t += dt;
    if (G.dead) G.dead += dt;
    const m = G.mv;
    if (m) {
      m.x += m.dir * m.v * dt;
      if (m.x < -m.w * 0.5) { m.x = -m.w * 0.5; m.dir = 1; }
      if (m.x > W - m.w * 0.5) { m.x = W - m.w * 0.5; m.dir = -1; }
    }
    G.falls.forEach((f) => { f.vy += 700 * dt; f.y += f.vy * dt; });
    G.falls = G.falls.filter((f) => f.y + G.cam < H + 30);
    if (G.hop >= 1) { G.hop = 0; F.squash(0.7); }
    else if (G.hop) G.hop = Math.min(1, G.hop + dt / 0.22);
    if (G.ring) G.ring = G.ring > 0.6 ? 0 : G.ring + dt;
    const want = Math.max(0, G.pads.length * T - (Y0 - H * 0.5));
    G.cam += (want - G.cam) * Math.min(1, dt * 6);
  },

  over() { return G && G.dead > 0.5; },
  score() { return G ? G.score : 0; },
  why() { return G ? G.why : ""; },
  tally() { return G ? [plural(G.pads.length - 1, "pad") + " high"] : []; },

  draw(x) {
    const cam = G ? G.cam : 0, n = G ? G.pads.length : 1;
    const sky = n > 40 ? ["#0a1220", "#0c1830"] : n > 20 ? ["#0e2440", "#123050"] : ["#123a3d", "#16454a"];
    for (let i = 0; i < 10; i++) { x.fillStyle = sky[i % 2]; x.fillRect(0, i * 30, W, 30); }
    if (n > 20) {
      x.fillStyle = "#fff6dc";
      for (let i = 0; i < 30; i++) { const sy = ((i * 53 + cam * 0.3) % H); x.fillRect((i * 71) % W, Math.round(sy), 1, 1); }
    }
    x.fillStyle = "#1f4a30";
    for (let i = 0; i < W; i += 4) { const h = 14 + ((i * 37) % 9); x.fillRect(i, Math.round(Y0 + 12 - h + cam * 0.5), 4, h); }
    const wy = Math.round(Y0 + T + cam);
    x.fillStyle = "#1d5163"; x.fillRect(0, wy, W, H);
    x.fillStyle = "#7fd6c2"; for (let i = 0; i < W; i += 10) x.fillRect((i + Math.round((G ? G.t : 0) * 8)) % W, wy, 6, 1);
    x.fillStyle = "#2f6b33";
    [6, 14, 160, 170].forEach((rx, i) => x.fillRect(rx, wy - 26 - i * 3, 2, 26 + i * 3));
    if (!G) return;
    G.pads.forEach((p, i) => pad(x, p.x, Y0 - i * T + cam, p.w, i, p.flower));
    if (G.mv) pad(x, G.mv.x, Y0 - G.pads.length * T + cam, G.mv.w, G.pads.length, false);
    G.falls.forEach((f) => pad(x, f.x, f.y + cam, f.w, 1, false));
    const t = top(), fy = Y0 - (G.pads.length - 1) * T + cam - SH + 1 - (G.hop ? Math.sin(Math.PI * G.hop) * 10 : 0);
    F.squashed(x, t.x + t.w / 2, fy + SH, () => draw(x, spr, SW, SH, col, Math.round(t.x + t.w / 2 - SW / 2), Math.round(fy), 1));
    if (G.ring) {
      const s = Math.round(G.ring * 40), y = Math.round(Y0 - (G.pads.length - 1) * T + cam + 4);
      x.fillStyle = "rgba(255,176,0," + (1 - G.ring / 0.6) + ")";
      x.fillRect(Math.round(t.x - s), y, Math.round(t.w + s * 2), 1);
    }
    F.draw(x, 0, cam);
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(String(G.score), 6, 18);
    x.textAlign = "right"; x.font = "500 8px 'JetBrains Mono', monospace"; x.fillStyle = "#bcaf99";
    if (G.perf > 1) x.fillText(G.perf + " PERFECT", W - 6, 16);
  },
};

function plural(n, w) { return n + " " + w + (n === 1 ? "" : "s"); }

// A pad seen from the side, a dark rim under a green top with a vein.
function pad(x, px, py, w, i, flower) {
  px = Math.round(px); py = Math.round(py); w = Math.round(w);
  if (w <= 0) return;
  x.fillStyle = i % 2 ? "#1f5a2c" : "#2a6a35"; x.fillRect(px, py, w, T);
  x.fillStyle = i % 2 ? "#56b45f" : "#62bf6a"; x.fillRect(px, py, w, 4);
  x.fillStyle = "#7fcf6a"; x.fillRect(px + 2, py + 1, Math.max(0, w - 4), 1);
  x.fillStyle = "#123a3d"; if (w > 12) x.fillRect(px + Math.round(w * 0.6), py, 2, 2);
  if (flower && w > 8) { x.fillStyle = "#f2a6c8"; x.fillRect(px + 3, py - 3, 4, 3); x.fillStyle = "#ffe27a"; x.fillRect(px + 4, py - 2, 2, 1); }
}

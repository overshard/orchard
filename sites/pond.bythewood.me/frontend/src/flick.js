import { cells, colours, draw, W as FW, H as FH } from "./frog.js";
import { feel } from "./feel.js";

// Tongue flick. The frog sits on a pad and bugs cross over it for thirty
// seconds. A tap shoots the tongue at that spot, a bug near the tip comes
// back, and a wasp stings and stops the tongue for a moment.
const W = 180, H = 270, ROUND = 30, REACH = 210, OUT = 0.1, BACK = 0.13;
const KINDS = [
  { k: "fly", pts: 1, w: 52, v: [34, 58], c: "#3a3f46", wing: "#cfe3ea" },
  { k: "mayfly", pts: 2, w: 20, v: [64, 90], c: "#c9974f", wing: "#f6f0e6" },
  { k: "firefly", pts: 3, w: 10, v: [40, 56], c: "#ffe27a", wing: "#fff6dc" },
  { k: "wasp", pts: 0, w: 18, v: [44, 70], c: "#f2c230", wing: "#cfe3ea" },
];

let G = null, spr = null, col = null;
const F = feel();

function pick() {
  let r = Math.random() * KINDS.reduce((a, b) => a + b.w, 0);
  for (const k of KINDS) { r -= k.w; if (r < 0) return k; }
  return KINDS[0];
}

function spawn() {
  const kind = pick(), right = Math.random() < 0.5, v = kind.v[0] + Math.random() * (kind.v[1] - kind.v[0]);
  G.bugs.push({ kind, x: right ? W + 12 : -12, y0: 24 + Math.random() * 150, vx: right ? -v : v, amp: 4 + Math.random() * 10, ph: Math.random() * 6, t: 0, y: 0 });
}

function mouth() { return { x: W / 2, y: H - 58 }; }

export const flick = {
  key: "flick", W, H, title: "TONGUE FLICK", feel: F, timed: true,
  aria: "Tap a bug to catch it.",
  edge: ["#24485a", "#0e2c30"],
  help: ["TAP A BUG TO FLICK AT IT", "FIREFLIES PAY THE MOST", "WASPS STING", "TAP TO START"],

  reset(g, p) {
    const stage = ["egg", "tadpole"].includes(p.frog.stage) ? "froglet" : p.frog.stage;
    spr = cells(g, stage, { hat: p.frog.hat.shape });
    col = colours(g, p.frog.hat);
    G = { t: 0, bugs: [], next: 0.3, tongue: null, streak: 0, score: 0, stun: 0, why: "", fireflies: 0, row: 0, wasps: 0 };
  },

  demo() {
    const b = G && G.bugs.find((q) => q.kind.k !== "wasp" && q.x > 20 && q.x < W - 20);
    return b ? { x: b.x, y: b.y } : { x: W / 2 + 30, y: 90 };
  },

  press(x, y) {
    if (!G || G.tongue || G.stun > 0) return;
    const m = mouth(), dx = x - m.x, dy = Math.min(y - m.y, -4), d = Math.hypot(dx, dy);
    G.tongue = { ux: dx / d, uy: dy / d, len: Math.min(REACH, d), k: 0, out: true, bug: null };
  },

  step(dt) {
    G.t += dt;
    G.stun = Math.max(0, G.stun - dt);
    G.next -= dt;
    if (G.next <= 0) { spawn(); G.next = 0.45 + Math.random() * 0.45 - Math.min(0.2, G.t / 100); }
    G.bugs.forEach((b) => { b.t += dt; b.x += b.vx * dt; b.y = b.y0 + Math.sin(b.t * 3 + b.ph) * b.amp; });
    G.bugs = G.bugs.filter((b) => b.x > -16 && b.x < W + 16);
    const tg = G.tongue;
    if (tg) {
      if (tg.out) {
        tg.k = Math.min(1, tg.k + dt / OUT);
        const m = mouth(), tx = m.x + tg.ux * tg.len * tg.k, ty = m.y + tg.uy * tg.len * tg.k;
        const hit = G.bugs.find((b) => Math.hypot(b.x - tx, b.y - ty) < 13);
        if (hit) {
          G.bugs.splice(G.bugs.indexOf(hit), 1);
          if (hit.kind.k === "wasp") {
            G.stun = 1.1; G.streak = 0; G.wasps++;
            F.crash(tx, ty, { s: "OW" });
          } else tg.bug = hit;
          tg.out = false;
        } else if (tg.k >= 1) tg.out = false;
      } else {
        tg.k = Math.max(0, tg.k - dt / BACK);
        if (tg.k <= 0) {
          if (tg.bug) {
            G.streak++; G.row = Math.max(G.row, G.streak);
            if (tg.bug.kind.k === "firefly") G.fireflies++;
            let pts = tg.bug.kind.pts;
            if (G.streak % 5 === 0) pts += 5;
            G.score += pts;
            const m = mouth(), five = G.streak % 5 === 0;
            F.got(m.x, m.y - 20, "+" + pts, { streak: G.streak, big: five, c: five ? "#ffb000" : "#ece5d8" });
          } else if (G.stun === 0) G.streak = 0;
          G.tongue = null;
        }
      }
    }
    if (G.t >= ROUND) G.why = "TIME'S UP";
  },

  over() { return G && G.t >= ROUND; },
  score() { return G ? G.score : 0; },
  why() { return G ? G.why : ""; },
  report() { return { counts: { fireflies: G.fireflies, row: G.row, wasps: G.wasps } }; },

  draw(x) {
    x.fillStyle = "#1b3a4a"; x.fillRect(0, 0, W, H);
    x.fillStyle = "#24485a"; for (let i = 0; i < 6; i++) x.fillRect(0, i * 34, W, 14);
    x.fillStyle = "#2f5a3a"; for (let r = 0; r < W; r += 6) x.fillRect(r, H - 92 - ((r * 7) % 9), 2, 20);
    x.fillStyle = "#0e2c30"; x.fillRect(0, H - 70, W, 70);
    x.fillStyle = "#1f5a2c"; x.fillRect(W / 2 - 46, H - 30, 92, 12);
    x.fillStyle = "#56b45f"; x.fillRect(W / 2 - 42, H - 32, 84, 10);
    if (!G) return;
    G.bugs.forEach((b) => {
      const flap = Math.floor(b.t * 18) % 2;
      // Drawn on a grid of 2, so a bug is twice the size of a frog pixel.
      x.save(); x.translate(Math.round(b.x), Math.round(b.y)); x.scale(2, 2);
      if (b.kind.k === "firefly") { x.fillStyle = "rgba(255,226,122,0.25)"; x.fillRect(-6, -6, 12, 12); }
      x.fillStyle = b.kind.wing;
      if (flap) { x.fillRect(-4, -5, 3, 3); x.fillRect(1, -5, 3, 3); } else { x.fillRect(-5, -3, 4, 1); x.fillRect(1, -3, 4, 1); }
      x.fillStyle = b.kind.c; x.fillRect(-4, -2, 8, 4);
      x.fillStyle = "#1a1a1a"; x.fillRect(b.vx > 0 ? 3 : -4, -2, 1, 2);
      if (b.kind.k === "wasp") { x.fillRect(-2, -2, 1, 4); x.fillRect(1, -2, 1, 4); x.fillRect(b.vx > 0 ? -5 : 4, 0, 1, 1); }
      x.restore();
    });
    const fx = Math.round(W / 2 - FW), fy = H - 30 - FH * 2 + 4;
    if (G.stun > 0 && Math.floor(G.stun * 10) % 2) x.globalAlpha = 0.5;
    F.squashed(x, W / 2, fy + FH * 2, () => draw(x, spr, FW, FH, col, fx, fy, 2));
    x.globalAlpha = 1;
    const tg = G.tongue;
    if (tg) {
      const m = mouth(), n = Math.max(1, Math.round(tg.len * tg.k / 2));
      x.fillStyle = "#e2567a";
      for (let i = 0; i <= n; i++) x.fillRect(Math.round(m.x + tg.ux * i * 2) - 1, Math.round(m.y + tg.uy * i * 2) - 1, 2, 2);
      const tx = m.x + tg.ux * tg.len * tg.k, ty = m.y + tg.uy * tg.len * tg.k;
      x.fillStyle = "#b3306d"; x.fillRect(Math.round(tx) - 2, Math.round(ty) - 2, 4, 4);
      if (tg.bug) { x.fillStyle = tg.bug.kind.c; x.fillRect(Math.round(tx) - 4, Math.round(ty) - 2, 8, 4); }
    }
    F.draw(x);
    x.fillStyle = "#0a0908"; x.fillRect(0, 0, W, 4);
    x.fillStyle = "#ffb000"; x.fillRect(0, 0, Math.round(W * Math.max(0, 1 - G.t / ROUND)), 4);
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(String(G.score), 6, 20);
    x.textAlign = "right"; x.font = "500 8px 'JetBrains Mono', monospace"; x.fillStyle = "#bcaf99";
    if (G.streak > 1) x.fillText(G.streak + " IN A ROW", W - 6, 18);
  },
};

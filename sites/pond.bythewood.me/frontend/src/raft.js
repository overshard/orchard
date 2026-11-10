import { colours } from "./frog.js";
import { drawTop } from "./top.js";

// Raft run. The frog rides a leaf down a river that keeps narrowing. Hold a
// finger where it should go, or the arrow keys, and thread the gaps in the
// reed weirs while missing rocks and the banks. Three bumps and it's over.
const W = 180, H = 270, FY = H - 56, STEER = 150, KEYS = 120;

let G = null, col = null;

function centre(y) { return W / 2 + Math.sin(y / 170) * 30 + Math.sin(y / 53) * 9; }
function half(y) { return Math.max(40, 74 - y / 260); }
function screenY(wy) { return H - (wy - G.dist); }

function spawn() {
  const y = G.nextAt, c = centre(y), h = half(y);
  const r = Math.random();
  if (G.since >= 3 || r < 0.3) {
    const gap = Math.max(26, 40 - G.dist / 500);
    G.things.push({ k: "weir", y, gap, gx: c - h + 8 + Math.random() * (h * 2 - gap - 16) });
    G.since = 0;
  } else {
    const n = r < 0.75 ? 1 : 2;
    for (let i = 0; i < n; i++) G.things.push({ k: "rock", y: y + i * 18, x: c - h + 10 + Math.random() * (h * 2 - 20), r: 5 + Math.floor(Math.random() * 3) });
    if (Math.random() < 0.5) G.things.push({ k: "pebble", y: y + 40, x: c - h + 12 + Math.random() * (h * 2 - 24) });
    G.since++;
  }
  G.nextAt += 70 + Math.random() * 40;
}

function bump(why) {
  if (G.safe > 0) return;
  G.lives--; G.safe = 1.2; G.why = why; G.shake = 0.25;
}

export const raft = {
  key: "raft", W, H, title: "RAFT RUN",
  help: ["HOLD WHERE THE RAFT SHOULD GO", "THREAD THE GAPS IN THE REEDS", "THREE BUMPS AND YOU'RE OUT", "TAP TO START"],

  reset(g) {
    col = colours(g, null);
    G = { t: 0, dist: 0, v: 52, fx: centre(56), want: null, keys: { left: false, right: false }, things: [], nextAt: 260, since: 0, lives: 3, safe: 0, shake: 0, bonus: 0, why: "", pops: [] };
    while (G.nextAt < G.dist + H + 40) spawn();
  },

  drag(x) { if (G) G.want = x; },
  release() { if (G) G.want = null; },
  hold(dir, on) { if (G) G.keys[dir] = on; },

  step(dt) {
    G.t += dt;
    G.v = Math.min(118, 52 + G.t * 1.6);
    G.dist += G.v * dt;
    G.safe = Math.max(0, G.safe - dt);
    G.shake = Math.max(0, G.shake - dt);
    if (G.want !== null) {
      const d = G.want - G.fx;
      G.fx += Math.sign(d) * Math.min(Math.abs(d), STEER * dt);
    }
    if (G.keys.left) G.fx -= KEYS * dt;
    if (G.keys.right) G.fx += KEYS * dt;
    while (G.nextAt < G.dist + H + 40) spawn();
    const fy = G.dist + (H - FY), c = centre(fy), h = half(fy);
    if (Math.abs(G.fx - c) > h - 7) { bump("ON THE BANK"); G.fx = c + Math.sign(G.fx - c) * (h - 9); }
    for (const t of G.things) {
      if (t.done) continue;
      if (t.k === "weir" && fy >= t.y) {
        t.done = true;
        if (G.fx - 5 > t.gx && G.fx + 5 < t.gx + t.gap) {
          G.bonus++; G.pops.push({ x: G.fx, y: FY - 12, s: "+1", t: 0 });
        } else bump("IN THE REEDS");
      } else if (t.k === "rock" && Math.abs(fy - t.y) < t.r + 5 && Math.abs(G.fx - t.x) < t.r + 5) {
        t.done = true; bump("ON A ROCK");
      } else if (t.k === "pebble" && Math.abs(fy - t.y) < 8 && Math.abs(G.fx - t.x) < 9) {
        t.done = true; G.bonus += 2; G.pops.push({ x: t.x, y: FY - 12, s: "+2", t: 0 });
      }
    }
    G.things = G.things.filter((t) => screenY(t.y) < H + 20);
    G.pops.forEach((p) => { p.t += dt; p.y -= dt * 20; });
    G.pops = G.pops.filter((p) => p.t < 0.8);
  },

  over() { return G && G.lives <= 0 && G.shake === 0; },
  score() { return G ? Math.floor(G.dist / 150) + G.bonus : 0; },
  why() { return G ? G.why : ""; },

  draw(x) {
    const dist = G ? G.dist : 0, sh = G && G.shake > 0 ? Math.round(Math.sin(G.shake * 60) * 2) : 0;
    x.save(); x.translate(sh, 0);
    x.fillStyle = "#3f8a44"; x.fillRect(-4, 0, W + 8, H);
    for (let s = 0; s < H; s += 2) {
      const wy = dist + (H - s), c = centre(wy), h = half(wy);
      x.fillStyle = "#2f6b33"; x.fillRect(Math.round(c - h - 4), s, Math.round(h * 2 + 8), 2);
      x.fillStyle = "#1d5163"; x.fillRect(Math.round(c - h), s, Math.round(h * 2), 2);
      if (Math.floor(wy / 2) % 23 === 0) { x.fillStyle = "#4f9a4a"; x.fillRect(Math.round(c - h - 14), s, 2, 4); x.fillRect(Math.round(c + h + 12), s, 2, 4); }
      if (Math.floor(wy / 2) % 17 === 0) { x.fillStyle = "#7fd6c2"; x.fillRect(Math.round(c - h / 2 + Math.sin(wy) * h / 3), s, 6, 1); }
    }
    if (!G) { x.restore(); return; }
    for (const t of G.things) {
      const sy = Math.round(screenY(t.y));
      if (t.k === "rock") {
        x.fillStyle = "#3a3f46"; x.fillRect(Math.round(t.x - t.r), sy - t.r + 2, t.r * 2, t.r * 2 - 2);
        x.fillStyle = "#8a9099"; x.fillRect(Math.round(t.x - t.r + 1), sy - t.r, t.r * 2 - 2, t.r * 2 - 3);
        x.fillStyle = "#b6bcc4"; x.fillRect(Math.round(t.x - t.r + 2), sy - t.r + 1, 3, 2);
        x.fillStyle = "#7fd6c2"; x.fillRect(Math.round(t.x - t.r - 1), sy + t.r - 1, t.r * 2 + 2, 1);
      } else if (t.k === "pebble" && !t.done) {
        x.fillStyle = "#d5d8dc"; x.fillRect(Math.round(t.x) - 3, sy - 2, 6, 4); x.fillStyle = "#9aa0a6"; x.fillRect(Math.round(t.x) - 1, sy, 4, 2);
      } else if (t.k === "weir") {
        const c = centre(t.y), h = half(t.y);
        for (let rx = Math.round(c - h); rx < c + h; rx += 3) {
          if (rx + 2 > t.gx && rx < t.gx + t.gap) continue;
          x.fillStyle = "#56b45f"; x.fillRect(rx, sy - 8, 2, 10);
          if (rx % 2) { x.fillStyle = "#8a5a2b"; x.fillRect(rx, sy - 11, 2, 4); }
        }
        x.fillStyle = t.done ? "#2f6b33" : "#ffe27a"; x.fillRect(Math.round(t.gx) - 1, sy - 6, 1, 6); x.fillRect(Math.round(t.gx + t.gap), sy - 6, 1, 6);
      }
    }
    if (G.lives > 0 && !(G.safe > 0 && Math.floor(G.safe * 10) % 2)) {
      const fx = Math.round(G.fx);
      x.fillStyle = "#1f5a2c"; x.fillRect(fx - 9, FY - 6, 18, 20);
      x.fillStyle = "#56b45f"; x.fillRect(fx - 8, FY - 7, 16, 20);
      x.fillStyle = "#3f9a4d"; x.fillRect(fx, FY - 6, 1, 18);
      x.fillStyle = "#7fd6c2"; x.fillRect(fx - 7, FY + 14 + (Math.floor(G.t * 8) % 2), 3, 1); x.fillRect(fx + 4, FY + 14 + (Math.floor(G.t * 8 + 1) % 2), 3, 1);
      drawTop(x, col, fx - 6, FY - 4, false);
    }
    x.font = "700 13px 'Space Grotesk', sans-serif"; x.textAlign = "center";
    G.pops.forEach((p) => { x.fillStyle = "#ffb000"; x.fillText(p.s, p.x, p.y); });
    x.restore();
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.font = "700 13px 'Space Grotesk', sans-serif"; x.fillText(String(raft.score()), 6, 16);
    x.fillStyle = "#e2567a"; for (let i = 0; i < G.lives; i++) x.fillRect(W - 12 - i * 9, 6, 6, 6);
  },
};

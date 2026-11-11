import { sfx } from "./sound.js";
import { cells, colours, draw, W as FW, H as FH } from "./frog.js";
import { feel } from "./feel.js";

// Lily leap. The frog bounces off every pad it lands on and climbs as high as
// it can. Hold a finger to one side, or the arrow keys, to steer, and the
// field wraps at the edges. Brown pads sink once used and dragonflies launch.
const W = 220, H = 330, GRAV = 560, BOUNCE = 340, LAUNCH = 620, RUN = 190;
const PW = 36, FOOT = 12, TAP = 0.09;

let G = null, spr = null, col = null;
const F = feel();

function pad(y) {
  const up = G ? G.top : 0, r = Math.random();
  const kind = up > 600 && r < Math.min(0.3, up / 8000) ? "sink" : up > 300 && r < 0.45 ? "drift" : "pad";
  const p = { x: 10 + Math.random() * (W - PW - 20), y, kind, v: kind === "drift" ? (Math.random() < 0.5 ? -1 : 1) * (24 + Math.random() * 30) : 0, gone: 0 };
  if (Math.random() < 0.07) p.fly = true;
  else if (Math.random() < 0.12) p.bug = true;
  return p;
}

function fill() {
  while (G.highest > G.cam - 40) {
    const gap = Math.min(96, 46 + G.top / 90) * (0.6 + Math.random() * 0.4);
    G.highest -= gap;
    G.pads.push(pad(G.highest));
  }
}

export const leap = {
  key: "leap", W, H, title: "LILY LEAP", feel: F,
  aria: "Hold left or right, or use the arrow keys, to steer.",
  edge: ["#123a3d", "#123a3d"],
  help: ["HOLD LEFT OR RIGHT TO STEER", "BOUNCE UP THE PADS", "BROWN PADS SINK, DRAGONFLIES LAUNCH", "TAP TO START"],

  reset(g, p) {
    const stage = ["egg", "tadpole"].includes(p.frog.stage) ? "froglet" : p.frog.stage;
    spr = cells(g, stage, { hat: p.frog.hat.shape });
    col = colours(g, p.frog.hat);
    G = { t: 0, x: W / 2, y: H - 40, vx: 0, vy: -BOUNCE, cam: 0, top: 0, highest: H - 30, pads: [], dir: 0, want: null, keys: { left: false, right: false }, bonus: 0, why: "", fell: 0, row: 0, last: null, downAt: -9, lift: 0 };
    G.pads.push({ x: W / 2 - PW / 2, y: H - 30, kind: "pad", v: 0, gone: 0 });
    fill();
  },

  demo() {
    const p = G && G.pads.filter((q) => q.y < G.y - 20).sort((a, b) => b.y - a.y)[0];
    return p ? { x: p.x + PW / 2, y: H - 70, hold: true } : null;
  },

  press() { if (G) { G.downAt = G.t; G.lift = 0; } },
  drag(x) { if (G) G.want = x; },
  // A tap too quick to steer by still pulls for a moment.
  release() {
    if (!G) return;
    if (G.t - G.downAt < TAP) G.lift = G.downAt + TAP; else G.want = null;
  },
  hold(dir, on) { if (G) G.keys[dir] = on; },

  step(dt) {
    G.t += dt;
    if (G.fell > 0) { G.fell += dt; G.vy += GRAV * dt; G.y += G.vy * dt; return; }
    if (G.lift && G.t >= G.lift) { G.want = null; G.lift = 0; }
    let steer = (G.keys.right ? 1 : 0) - (G.keys.left ? 1 : 0);
    if (G.want !== null) steer = Math.max(-1, Math.min(1, (G.want - G.x) / 24));
    G.vx += (steer * RUN - G.vx) * Math.min(1, dt * 10);
    G.x += G.vx * dt;
    if (G.x < -10) G.x += W + 20;
    if (G.x > W + 10) G.x -= W + 20;
    const was = G.y;
    G.vy += GRAV * dt;
    G.y += G.vy * dt;
    for (const p of G.pads) {
      if (p.v) { p.x += p.v * dt; if (p.x < 0 || p.x > W - PW) p.v = -p.v; }
      if (p.gone) { p.gone += dt; p.y += 40 * dt; continue; }
      if (G.vy > 0 && was <= p.y && G.y >= p.y && G.x + FOOT > p.x && G.x - FOOT < p.x + PW) {
        G.y = p.y; G.vy = -BOUNCE;
        // A pad higher than the last one carries the run of notes on.
        G.row = G.last && p.y < G.last.y ? G.row % 8 + 1 : 1;
        G.last = p;
        sfx("hop", G.row); F.squash(0.8);
        if (p.kind === "sink") p.gone = 0.01;
        if (p.fly) { p.fly = false; G.vy = -LAUNCH; F.got(G.x, G.y - 20, "WHEE", { big: true }); }
        if (p.bug) { p.bug = false; G.bonus += 2; F.got(G.x, G.y - 20, "+2 M", { streak: G.row }); }
      }
    }
    G.cam = Math.min(G.cam, G.y - H * 0.45);
    G.top = Math.max(G.top, H - 40 - G.y);
    G.pads = G.pads.filter((p) => p.y - G.cam < H + 20 && p.gone < 1.5);
    fill();
    if (G.y - G.cam > H + 10) { G.fell = 0.01; G.why = "SPLASH"; F.crash(G.x, G.cam + H - 8); }
  },

  over() { return G && G.fell > 0.45; },
  score() { return G ? Math.floor(G.top / 10) + G.bonus : 0; },
  why() { return G ? G.why : ""; },

  draw(x) {
    const cam = G ? G.cam : 0;
    x.fillStyle = "#123a3d"; x.fillRect(0, 0, W, H);
    x.fillStyle = "#0e2c30";
    for (let i = 0; i < 9; i++) { const y = ((i * 47 - cam * 0.4) % H + H) % H; x.fillRect(0, Math.round(y), W, 10); }
    x.fillStyle = "#1d5163";
    for (let i = 0; i < 14; i++) { const y = ((i * 31 - cam * 0.7) % H + H) % H; x.fillRect((i * 53) % W, Math.round(y), 8, 1); }
    if (!G) return;
    for (const p of G.pads) {
      const px = Math.round(p.x), py = Math.round(p.y - cam);
      if (p.gone) x.globalAlpha = Math.max(0, 1 - p.gone / 1.5);
      x.fillStyle = "#0a1b1f"; x.fillRect(px + 2, py + 3, PW - 2, 5);
      x.fillStyle = p.kind === "sink" ? "#6b4a2a" : p.kind === "drift" ? "#3f9a4d" : "#1f5a2c"; x.fillRect(px, py, PW, 6);
      x.fillStyle = p.kind === "sink" ? "#8a5a2b" : "#56b45f"; x.fillRect(px + 2, py - 1, PW - 4, 5);
      x.fillStyle = "#123a3d"; x.fillRect(px + PW / 2, py - 1, 4, 3);
      x.globalAlpha = 1;
      if (p.fly) {
        const f = Math.floor(G.t * 16) % 2;
        x.fillStyle = "#3d8fd6"; x.fillRect(px + 10, py - 8, 14, 3);
        x.fillStyle = "#cfe3ea"; x.fillRect(px + 13, py - 12 + f, 4, 3); x.fillRect(px + 18, py - 12 + f, 4, 3);
      }
      if (p.bug) {
        x.fillStyle = "rgba(255,226,122,0.3)"; x.fillRect(px + 22, py - 14, 10, 10);
        x.fillStyle = "#ffe27a"; x.fillRect(px + 25, py - 11 + Math.round(Math.sin(G.t * 5)), 4, 3);
      }
    }
    const fx = Math.round(G.x - FW / 2), fy = Math.round(G.y - cam - FH);
    [0, W + 20, -W - 20].forEach((o) => {
      if (fx + o + FW > 0 && fx + o < W) F.squashed(x, fx + o + FW / 2, fy + FH, () => draw(x, spr, FW, FH, col, fx + o, fy, 1));
    });
    F.draw(x, 0, -cam);
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(leap.score() + " M", 6, 16);
  },
};

import { colours } from "./frog.js";
import { drawTop } from "./top.js";

// Crossing. The frog starts on the field side of a lane and wants the pond
// on the far side of a stream. Hop a row at a time, past traffic on the lane
// and over logs and pads on the water, and every trip home goes a bit faster.
const C = 14, COLS = 13, ROWS = 14, W = C * COLS, H = C * ROWS, HOP = 0.11;
const HOME = 1, START = 12, VERGE = 7;

// Each lane is a row, a speed in cells a second, and what's in it, as runs of
// length and gap the lane repeats across a wrap a little wider than the field.
const LANES = {
  11: { kind: "car", v: -1.6, run: [[1, 4], [1, 3], [1, 5]], c: "#e2412d" },
  10: { kind: "tractor", v: 0.8, run: [[2, 6], [2, 5]], c: "#3f9a4d" },
  9: { kind: "bike", v: 2.6, run: [[1, 6], [1, 7]], c: "#3d8fd6" },
  8: { kind: "van", v: -1.2, run: [[2, 4], [2, 5]], c: "#e9e3d6" },
  6: { kind: "log", v: 1.0, run: [[3, 3], [3, 4]], c: "#8a5a2b" },
  5: { kind: "pads", v: -1.4, run: [[2, 3], [3, 3]], c: "#56b45f" },
  4: { kind: "log", v: 0.7, run: [[4, 3], [3, 4]], c: "#6b4a2a" },
  3: { kind: "pads", v: 1.6, run: [[2, 4], [2, 3]], c: "#56b45f" },
  2: { kind: "log", v: -1.1, run: [[3, 3], [2, 4]], c: "#8a5a2b" },
};
const WRAP = COLS + 6;

let G = null, col = null;

function water(row) { return row >= 2 && row <= 6; }
function road(row) { return row >= 8 && row <= 11; }

function build() {
  G.things = [];
  for (const [row, l] of Object.entries(LANES)) {
    let x = Math.random() * 3, i = 0;
    while (x < WRAP) {
      const [len, gap] = l.run[i++ % l.run.length];
      G.things.push({ row: +row, x, len, lane: l });
      x += len + gap;
    }
  }
}

function home() {
  G.fx = Math.floor(COLS / 2); G.row = START; G.best = START; G.hop = 0;
}

function under() {
  return G.things.find((t) => t.row === G.row && G.fx + 0.5 > t.x && G.fx + 0.5 < t.x + t.len);
}

function lose(why) {
  G.lives--; G.flash = 0.6; G.why = why;
  if (G.lives > 0) { G.fx = Math.floor(COLS / 2); G.row = START; G.hop = 0; }
}

function scatter() {
  G.pebble = Math.random() < 0.6 ? { col: Math.floor(Math.random() * COLS), row: [7, 8, 9, 10, 11][Math.floor(Math.random() * 5)] } : null;
  G.fly = Math.random() < 0.5 ? Math.floor(Math.random() * COLS) : -1;
}

export const cross = {
  key: "cross", W, H, title: "CROSSING",
  help: ["SWIPE OR TAP TO HOP", "GET TO THE POND UP TOP", "THREE LIVES, IT SPEEDS UP", "TAP TO START"],

  reset(g, p) {
    col = colours(g, null);
    G = { score: 0, lives: 3, trip: 0, flash: 0, why: "", pops: [], t: 0 };
    build(); home(); scatter();
  },

  swipe(dir) {
    if (!G || G.lives <= 0 || G.hop > 0 || G.flash > 0.3) return;
    const d = { up: [0, -1], down: [0, 1], left: [-1, 0], right: [1, 0] }[dir];
    const nx = Math.round(G.fx) + d[0], nr = G.row + d[1];
    if (nx < 0 || nx >= COLS || nr > START || nr < HOME) return;
    G.fx = d[0] ? nx : Math.max(0, Math.min(COLS - 1, Math.round(G.fx)));
    G.row = nr; G.hop = HOP; G.face = dir;
    if (G.row < G.best) { G.best = G.row; G.score++; }
    if (G.pebble && G.pebble.row === G.row && G.pebble.col === Math.round(G.fx)) {
      G.score += 3; G.pops.push({ x: G.fx * C + 7, y: G.row * C, s: "+3", t: 0 }); G.pebble = null;
    }
    if (G.row === HOME) {
      let pts = 10;
      if (G.fly === Math.round(G.fx)) pts += 5;
      G.score += pts; G.trip++;
      G.pops.push({ x: G.fx * C + 7, y: G.row * C + 4, s: "+" + pts, t: 0 });
      home(); scatter();
    }
  },

  step(dt) {
    G.t += dt;
    G.hop = Math.max(0, G.hop - dt);
    G.flash = Math.max(0, G.flash - dt);
    const fast = 1 + G.trip * 0.12;
    G.things.forEach((t) => {
      t.x += t.lane.v * fast * dt;
      if (t.x > WRAP - 3) t.x -= WRAP;
      if (t.x < -3 - t.len + 0) t.x += WRAP;
    });
    G.pops.forEach((p) => { p.t += dt; p.y -= dt * 18; });
    G.pops = G.pops.filter((p) => p.t < 0.8);
    if (G.lives <= 0 || G.flash > 0.3) return;
    const on = under();
    if (road(G.row) && on) lose("BONK");
    else if (water(G.row) && G.hop === 0) {
      if (!on) lose("SPLASH");
      else {
        G.fx += on.lane.v * fast * dt;
        if (G.fx < -0.4 || G.fx > COLS - 0.6) lose("WASHED AWAY");
      }
    }
  },

  over() { return G && G.lives <= 0 && G.flash === 0; },
  score() { return G ? G.score : 0; },
  why() { return G ? G.why : ""; },

  draw(x) {
    for (let r = 0; r < ROWS; r++) {
      x.fillStyle = r === 0 ? "#0a1b1f" : r === HOME ? "#2f6b33" : water(r) ? (r % 2 ? "#1b4a5a" : "#1d5163") : road(r) ? "#3a3a40" : "#4f9a4a";
      x.fillRect(0, r * C, W, C);
    }
    x.fillStyle = "#0e2c30"; x.fillRect(0, 0, W, C - 3);
    x.fillStyle = "#3f9a4d"; for (let c = 0; c < W; c += 5) x.fillRect(c, HOME * C - 3, 2, 5);
    x.fillStyle = "#cfc7a8"; for (let r = 8; r < 11; r++) for (let c = 2; c < W; c += 14) x.fillRect(c, (r + 1) * C - 1, 7, 1);
    x.fillStyle = "#6c8a3a"; for (let c = 3; c < W; c += 9) { x.fillRect(c, VERGE * C + 4, 1, 3); x.fillRect(c + 4, START * C + 8, 1, 3); }
    if (!G) return;
    if (G.fly >= 0) { x.fillStyle = "#3a3f46"; x.fillRect(G.fly * C + 5, HOME * C + 5, 4, 3); x.fillStyle = "#cfe3ea"; x.fillRect(G.fly * C + 5, HOME * C + 3, 4, 2); }
    if (G.pebble) { x.fillStyle = "#d5d8dc"; x.fillRect(G.pebble.col * C + 4, G.pebble.row * C + 5, 6, 4); x.fillStyle = "#9aa0a6"; x.fillRect(G.pebble.col * C + 6, G.pebble.row * C + 7, 4, 2); }
    G.things.forEach((t) => {
      const px = Math.round(t.x * C), py = t.row * C, w = t.len * C, l = t.lane;
      if (l.kind === "log") {
        x.fillStyle = l.c; x.fillRect(px, py + 2, w, C - 4);
        x.fillStyle = "#c9974f"; x.fillRect(px + w - 3, py + 3, 3, C - 6);
        x.fillStyle = "#5a3f24"; x.fillRect(px + 2, py + 5, w - 6, 1); x.fillRect(px + 4, py + 9, w - 10, 1);
      } else if (l.kind === "pads") {
        for (let i = 0; i < t.len; i++) {
          x.fillStyle = "#1f5a2c"; x.fillRect(px + i * C + 1, py + 2, C - 2, C - 4);
          x.fillStyle = l.c; x.fillRect(px + i * C + 2, py + 3, C - 4, C - 6);
          x.fillStyle = "#1f5a2c"; x.fillRect(px + i * C + 7, py + 3, 1, 4);
        }
      } else if (l.kind === "bike") {
        x.fillStyle = "#1a1a1a"; x.fillRect(px + 1, py + 8, 4, 4); x.fillRect(px + 9, py + 8, 4, 4);
        x.fillStyle = l.c; x.fillRect(px + 3, py + 6, 8, 2); x.fillRect(px + 6, py + 2, 3, 5);
      } else {
        const dir = l.v > 0;
        x.fillStyle = "#1a1a1a"; x.fillRect(px + 2, py + 10, 4, 3); x.fillRect(px + w - 6, py + 10, 4, 3);
        x.fillStyle = l.c; x.fillRect(px + 1, py + 3, w - 2, 8);
        x.fillStyle = "#9fd8e8"; x.fillRect(dir ? px + w - 6 : px + 2, py + 4, 4, 3);
        if (l.kind === "tractor") { x.fillStyle = "#1a1a1a"; x.fillRect(dir ? px + 2 : px + w - 9, py + 6, 7, 7); x.fillStyle = "#ffe27a"; x.fillRect(dir ? px + w - 2 : px, py + 5, 2, 2); }
        if (l.kind === "van") { x.fillStyle = "#c9c3b6"; x.fillRect(px + 3, py + 5, w - 12, 1); }
      }
    });
    if (G.lives > 0 && !(G.flash > 0 && Math.floor(G.flash * 12) % 2)) {
      drawTop(x, col, Math.round(G.fx * C) + 1, G.row * C + 1 - (G.hop > 0 ? 2 : 0), G.face === "down");
    }
    x.font = "700 13px 'Space Grotesk', sans-serif"; x.textAlign = "center";
    G.pops.forEach((p) => { x.fillStyle = "#ffb000"; x.fillText(p.s, p.x, p.y); });
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.font = "700 10px 'Space Grotesk', sans-serif"; x.fillText(String(G.score), 4, 9);
    x.fillStyle = "#e2567a"; for (let i = 0; i < G.lives; i++) x.fillRect(W - 10 - i * 8, 3, 5, 5);
    x.textAlign = "center"; x.font = "500 8px 'JetBrains Mono', monospace"; x.fillStyle = "#bcaf99"; x.fillText("TRIP " + (G.trip + 1), W / 2, 9);
  },
};

import { sfx } from "./sound.js";
import { colours } from "./frog.js";
import { drawTop } from "./top.js";
import { feel } from "./feel.js";

// Crossing. The frog starts on the field side of a lane and wants the pond
// on the far side of a stream. Hop a row at a time, past traffic on the lane
// and over logs and pads on the water, and every trip home goes a bit faster.
const C = 14, COLS = 13, W = C * COLS, HOP = 0.11, HOME = 1;
// A tall window gets more rows, so the field fills it at the same cell size.
const FEWEST = 14, MOST = 26;
let ROWS = FEWEST, START = ROWS - 2;

// Each lane is a speed in cells a second and what's in it, as runs of length
// and gap the lane repeats across a wrap a little wider than the field.
const STREAM = [
  { kind: "log", v: -1.1, run: [[3, 3], [2, 4]], c: "#8a5a2b" },
  { kind: "pads", v: 1.6, run: [[2, 4], [2, 3]], c: "#56b45f" },
  { kind: "log", v: 0.7, run: [[4, 3], [3, 4]], c: "#6b4a2a" },
  { kind: "pads", v: -1.4, run: [[2, 3], [3, 3]], c: "#56b45f" },
  { kind: "log", v: 1.0, run: [[3, 3], [3, 4]], c: "#8a5a2b" },
];
const LANE = [
  { kind: "van", v: -1.2, run: [[2, 4], [2, 5]], c: "#e9e3d6" },
  { kind: "bike", v: 2.6, run: [[1, 6], [1, 7]], c: "#3d8fd6" },
  { kind: "tractor", v: 0.8, run: [[2, 6], [2, 5]], c: "#3f9a4d" },
  { kind: "car", v: -1.6, run: [[1, 4], [1, 3], [1, 5]], c: "#e2412d" },
];
const WRAP = COLS + 6, CARS = ["#e2412d", "#ffb000", "#3d8fd6", "#e2567a", "#e9e3d6"];

let G = null, col = null, LANES = {};
const F = feel();
// A wheel has to be this far into the frog to count, and a log holds a frog
// hanging this far off its end. NEAR is how close a miss gets a CLOSE.
const INSIDE = 0.14, HANG = 0.2, NEAR = 0.75;

// From the pond down it's a stream, a verge, a lane and a verge, over again
// for as many rows as there are.
function lay(rows) {
  ROWS = rows; START = rows - 2; LANES = {};
  const every = STREAM.length + LANE.length + 2;
  for (let r = HOME + 1; r < START; r++) {
    const i = (r - HOME - 1) % every;
    if (i < STREAM.length) LANES[r] = STREAM[i];
    else if (i > STREAM.length && i < every - 1) LANES[r] = LANE[i - STREAM.length - 1];
  }
}
lay(FEWEST);

function water(row) { const l = LANES[row]; return !!l && (l.kind === "log" || l.kind === "pads"); }
function road(row) { return !!LANES[row] && !water(row); }

function build() {
  G.things = [];
  for (const [row, l] of Object.entries(LANES)) {
    let x = Math.random() * 3, i = 0;
    while (x < WRAP) {
      const [len, gap] = l.run[i++ % l.run.length];
      G.things.push({ row: +row, x, len, lane: l, c: l.kind === "car" ? CARS[Math.floor(Math.random() * CARS.length)] : l.c });
      x += len + gap;
    }
  }
}

function home() {
  G.fx = Math.floor(COLS / 2); G.row = START; G.best = START; G.hop = 0;
}

function under() {
  const m = water(G.row) ? -HANG : INSIDE;
  return G.things.find((t) => t.row === G.row && G.fx + 0.5 > t.x + m && G.fx + 0.5 < t.x + t.len - m);
}

// How far the frog's middle is from the nearest thing on a lane, in cells.
function gap(row) {
  if (!road(row)) return 9;
  const c = G.fx + 0.5;
  return Math.min(9, ...G.things.filter((t) => t.row === row).map((t) => Math.max(t.x - c, c - t.x - t.len, 0)));
}

function lose(why) {
  G.lives--; G.flash = G.lives > 0 ? 0.6 : 0.45; G.why = why;
  F.crash(G.fx * C + 7, G.row * C + 7);
  if (G.lives > 0) { G.fx = Math.floor(COLS / 2); G.row = START; G.hop = 0; }
}

function scatter() {
  const dry = [];
  for (let r = HOME + 1; r < START; r++) if (!water(r)) dry.push(r);
  G.pebble = Math.random() < 0.6 ? { col: Math.floor(Math.random() * COLS), row: dry[Math.floor(Math.random() * dry.length)] } : null;
  G.fly = Math.random() < 0.5 ? Math.floor(Math.random() * COLS) : -1;
}

export const cross = {
  key: "cross", W, H: C * FEWEST, title: "CROSSING", feel: F,
  edge: ["#0e2c30", "#4f9a4a"],

  fit(w, h) {
    lay(Math.max(FEWEST, Math.min(MOST, Math.floor(h / (w / COLS)))));
    cross.H = C * ROWS;
  },
  help: ["SWIPE OR TAP TO HOP", "GET TO THE POND UP TOP", "THREE LIVES, IT SPEEDS UP", "TAP TO START"],

  reset(g, p) {
    col = colours(g, null);
    G = { score: 0, lives: 3, trip: 0, flash: 0, why: "", t: 0 };
    build(); home(); scatter();
  },

  demo() { return G ? { x: G.fx * C + 24, y: G.row * C + 7, to: [G.fx * C + 24, G.row * C - 21] } : null; },

  swipe(dir) {
    if (!G || G.lives <= 0 || G.flash > 0.3) return;
    // A swipe that lands just before the hop is over waits for it.
    if (G.hop > 0) { F.buffer(dir); return; }
    const d = { up: [0, -1], down: [0, 1], left: [-1, 0], right: [1, 0] }[dir];
    const nx = Math.round(G.fx) + d[0], nr = G.row + d[1];
    if (nx < 0 || nx >= COLS || nr > START || nr < HOME) return;
    const left = gap(G.row);
    G.fx = d[0] ? nx : Math.max(0, Math.min(COLS - 1, Math.round(G.fx)));
    G.row = nr; G.hop = HOP; G.face = dir;
    if (Math.min(left, under() ? 9 : gap(G.row)) < NEAR) F.close(G.fx * C + 7, G.row * C - 4);
    const fresh = G.row < G.best;
    sfx("hop", fresh ? START - G.row : 0);
    if (fresh) { G.best = G.row; G.score++; }
    if (G.pebble && G.pebble.row === G.row && G.pebble.col === Math.round(G.fx)) {
      G.score += 3; G.pebble = null;
      F.got(G.fx * C + 7, G.row * C + 7, "+3");
    }
    if (G.row === HOME) {
      let pts = 10;
      if (G.fly === Math.round(G.fx)) pts += 5;
      G.score += pts; G.trip++;
      F.got(G.fx * C + 7, G.row * C + 10, "+" + pts, { big: true, streak: G.trip });
      home(); scatter();
    }
  },

  step(dt) {
    G.t += dt;
    const mid = G.hop > 0;
    G.hop = Math.max(0, G.hop - dt);
    if (mid && G.hop === 0) F.squash(0.6);
    const early = G.hop === 0 && F.take();
    if (early) cross.swipe(early);
    G.flash = Math.max(0, G.flash - dt);
    const fast = 1 + G.trip * 0.12;
    G.things.forEach((t) => {
      t.x += t.lane.v * fast * dt;
      if (t.x > WRAP - 3) t.x -= WRAP;
      if (t.x < -3 - t.len + 0) t.x += WRAP;
    });
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
    for (let r = HOME + 1; r < ROWS; r++) {
      if (water(r)) {
        x.fillStyle = "rgba(127,214,194,0.18)";
        for (let c = (r * 5) % 11; c < W; c += 22) x.fillRect(c, r * C + 6 + (r % 2) * 3, 5, 1);
      } else if (road(r)) {
        x.fillStyle = "#2c2c31";
        if (!road(r - 1)) x.fillRect(0, r * C, W, 1);
        if (!road(r + 1)) x.fillRect(0, (r + 1) * C - 1, W, 1);
        else { x.fillStyle = "#cfc7a8"; for (let c = 2; c < W; c += 14) x.fillRect(c, (r + 1) * C - 1, 7, 1); }
      } else {
        x.fillStyle = "#6c8a3a";
        for (let c = 3 + (r % 2) * 4; c < W; c += 9) x.fillRect(c, r * C + 4 + (r % 2) * 4, 1, 3);
      }
    }
    if (!G) return;
    if (G.fly >= 0) { x.fillStyle = "#3a3f46"; x.fillRect(G.fly * C + 5, HOME * C + 5, 4, 3); x.fillStyle = "#cfe3ea"; x.fillRect(G.fly * C + 5, HOME * C + 3, 4, 2); }
    if (G.pebble) { x.fillStyle = "#d5d8dc"; x.fillRect(G.pebble.col * C + 4, G.pebble.row * C + 5, 6, 4); x.fillStyle = "#9aa0a6"; x.fillRect(G.pebble.col * C + 6, G.pebble.row * C + 7, 4, 2); }
    G.things.forEach((t) => drawThing(x, t));
    if (G.lives > 0 && !(G.flash > 0 && Math.floor(G.flash * 12) % 2)) {
      const fx = Math.round(G.fx * C) + 1, fy = G.row * C + 1 - (G.hop > 0 ? 2 : 0);
      F.squashed(x, fx + 6, fy + 12, () => drawTop(x, col, fx, fy, G.face === "down"));
    }
    F.draw(x);
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.font = "700 10px 'Space Grotesk', sans-serif"; x.fillText(String(G.score), 4, 9);
    x.fillStyle = "#e2567a"; for (let i = 0; i < G.lives; i++) x.fillRect(W - 10 - i * 8, 3, 5, 5);
    x.textAlign = "center"; x.font = "500 8px 'JetBrains Mono', monospace"; x.fillStyle = "#bcaf99"; x.fillText("TRIP " + (G.trip + 1), W / 2, 9);
  },
};

// Everything in a lane is drawn facing right and flipped for the lanes that
// run left, so f takes an offset from the back of the thing.
function drawThing(x, t) {
  const px = Math.round(t.x * C), py = t.row * C, w = t.len * C, l = t.lane, dir = l.v > 0;
  const f = (c, ox, oy, ww, hh) => { x.fillStyle = c; x.fillRect(dir ? px + ox : px + w - ox - ww, py + oy, ww, hh); };
  const wheel = (ox, oy, s) => { f("#141414", ox + 1, oy, s - 2, s); f("#141414", ox, oy + 1, s, s - 2); f("#8a9099", ox + (s >> 1) - 1, oy + (s >> 1) - 1, 2, 2); };
  const DARK = "rgba(0,0,0,0.28)", GLASS = "#9fd8e8", LAMP = "#ffe27a";
  if (l.kind === "log") {
    f(l.c, 1, 2, w - 2, C - 4); f(l.c, 0, 3, w, C - 6);
    f("rgba(255,255,255,0.14)", 2, 2, w - 4, 1); f(DARK, 1, C - 4, w - 2, 2);
    f("#5a3f24", 4, 5, w - 14, 1); f("#5a3f24", 9, 8, w - 16, 1); f("#5a3f24", w - 12, 6, 3, 2);
    f("#c9974f", w - 3, 3, 3, C - 6); f("#8a5a2b", w - 2, 5, 1, C - 10);
    f("#5a3f24", 0, 3, 1, C - 6);
  } else if (l.kind === "pads") {
    for (let i = 0; i < t.len; i++) {
      const o = i * C;
      f("#1f5a2c", o + 2, 1, C - 4, C - 2); f("#1f5a2c", o + 1, 2, C - 2, C - 4);
      f(l.c, o + 3, 2, C - 6, C - 4); f(l.c, o + 2, 3, C - 4, C - 6);
      f("#7fd87a", o + 4, 3, 4, 1); f("#1d5163", o + 7, 1, 2, 4);
      f("#2f7a3f", o + 6, 7, 1, 4); f("#2f7a3f", o + 4, 9, 5, 1);
      if ((i + t.row) % 3 === 0) { f("#f3a9c6", o + 9, 8, 3, 3); f("#ffe27a", o + 10, 9, 1, 1); }
    }
  } else if (l.kind === "bike") {
    f(DARK, 1, 12, 12, 1);
    wheel(0, 7, 5); wheel(9, 7, 5);
    f(l.c, 3, 7, 7, 1); f(l.c, 5, 5, 2, 3); f(l.c, 10, 5, 1, 3); f("#141414", 9, 4, 3, 1);
    f("#56b45f", 5, 2, 3, 4); f("#56b45f", 7, 4, 3, 1); f("#62bf6a", 5, 0, 3, 2); f("#0a0908", 7, 0, 1, 1);
    f("#141414", 4, 5, 3, 1);
  } else if (l.kind === "car") {
    f(DARK, 1, 12, 12, 1);
    f(t.c, 3, 2, 7, 5); f(t.c, 0, 6, 14, 5); f(t.c, 2, 3, 1, 3);
    f(GLASS, 4, 3, 2, 3); f(GLASS, 7, 3, 3, 3); f("#eaf7fb", 7, 3, 1, 1);
    f(DARK, 0, 10, 14, 1); f(DARK, 6, 6, 1, 4);
    f(LAMP, 13, 7, 1, 2); f("#7a1410", 0, 7, 1, 2);
    wheel(1, 9, 4); wheel(9, 9, 4);
  } else if (l.kind === "van") {
    f(DARK, 1, 12, w - 2, 1);
    f(l.c, 0, 1, 20, 10); f(l.c, 20, 3, 6, 8); f(l.c, 26, 6, 2, 5);
    f(GLASS, 21, 4, 4, 3); f("#eaf7fb", 21, 4, 1, 1);
    f("#3d8fd6", 0, 6, 20, 2); f("#c9c3b6", 0, 1, 20, 1); f(DARK, 0, 10, w, 1); f(DARK, 19, 2, 1, 8); f(DARK, 10, 2, 1, 4);
    f(LAMP, 27, 7, 1, 2); f("#7a1410", 0, 8, 1, 2);
    wheel(3, 9, 4); wheel(21, 9, 4);
  } else {
    f(DARK, 1, 13, w - 2, 1);
    f(l.c, 5, 0, 9, 7); f(GLASS, 7, 1, 6, 4); f("#2a6a35", 5, 0, 9, 1); f(l.c, 9, 1, 1, 4);
    f(l.c, 10, 5, 15, 5); f("#2a6a35", 10, 9, 15, 1); f("#62bf6a", 14, 5, 11, 1);
    f("#2a2a2a", 20, 1, 1, 4); f("#bcaf99", 20 - (Math.floor(G.t * 4) % 3), -1, 2, 2);
    f(LAMP, 25, 6, 1, 2); f("#1f5a2c", 16, 7, 6, 1);
    wheel(1, 5, 9); f("#ffb000", 4, 8, 3, 3); f("#141414", 5, 9, 1, 1);
    wheel(20, 9, 5);
  }
}

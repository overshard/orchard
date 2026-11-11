import { colours } from "./frog.js";
import { feel } from "./feel.js";

// Tadpoles. Lead a school round the shallows eating water fleas, and every
// flea adds a tadpole to the tail. Swipe or tap to turn, and running into the
// reeds, a stone or your own school ends it.
const C = 16, COLS = 11, ROWS = 16, W = C * COLS, H = C * ROWS, M = C / 2;
const DIRS = { up: [0, -1], down: [0, 1], left: [-1, 0], right: [1, 0] };
// The school is at nearly its full speed within a dozen fleas and then holds.
const SLOW = 0.2, FAST = 0.1, QUICKEN = 0.84;

let G = null, col = null;
const F = feel();

function free() {
  for (let i = 0; i < 200; i++) {
    const p = { x: 1 + Math.floor(Math.random() * (COLS - 2)), y: 1 + Math.floor(Math.random() * (ROWS - 2)) };
    if (!blocked(p.x, p.y) && !(G.flea && G.flea.x === p.x && G.flea.y === p.y)) return p;
  }
  return { x: 1, y: 1 };
}

function blocked(x, y) {
  if (x < 0 || y < 0 || x >= COLS || y >= ROWS) return true;
  return G.body.some((b) => b.x === x && b.y === y) || G.stones.some((s) => s.x === x && s.y === y);
}

export const school = {
  key: "school", W, H, title: "TADPOLES", feel: F,
  edge: ["#2f6b33", "#2f6b33"],
  help: ["SWIPE OR TAP TO TURN", "EAT FLEAS, GROW THE SCHOOL", "MISS REEDS, STONES AND TAIL", "TAP TO START"],

  reset(g) {
    col = colours(g, null);
    const cx = Math.floor(COLS / 2);
    G = { t: 0, tick: 0, every: SLOW, dir: "up", turns: [], body: [{ x: cx, y: ROWS - 5 }, { x: cx, y: ROWS - 4 }, { x: cx, y: ROWS - 3 }], from: null, grow: 0, score: 0, stones: [], flea: null, gold: null, why: "", dead: 0, fleas: 0, row: 0, ateAt: -9 };
    G.flea = free();
  },

  demo() {
    if (!G) return null;
    const h = G.body[0], x = h.x * C + M, y = h.y * C + M + 34;
    return { x, y, to: [x + (G.flea.x < h.x ? -36 : 36), y] };
  },

  swipe(dir) {
    if (!G || G.dead) return;
    const last = G.turns.length ? G.turns[G.turns.length - 1] : G.dir, a = DIRS[last], b = DIRS[dir];
    if (a[0] === -b[0] && a[1] === -b[1]) return;
    if (dir !== last && G.turns.length < 2) G.turns.push(dir);
  },

  // A tap turns toward it, across the way the school is going.
  tap(x, y) {
    if (!G) return;
    const h = G.body[0], hx = h.x * C + C / 2, hy = h.y * C + C / 2, d = DIRS[G.turns.length ? G.turns[G.turns.length - 1] : G.dir];
    if (d[0] === 0) school.swipe(x < hx ? "left" : "right");
    else school.swipe(y < hy ? "up" : "down");
  },

  step(dt) {
    G.t += dt;
    if (G.dead) { G.dead += dt; return; }
    if (G.gold) { G.gold.left -= dt; if (G.gold.left <= 0) G.gold = null; }
    G.tick += dt;
    if (G.tick < G.every) return;
    G.tick -= G.every;
    if (G.turns.length) G.dir = G.turns.shift();
    const d = DIRS[G.dir], h = G.body[0], n = { x: h.x + d[0], y: h.y + d[1] };
    const tail = G.body[G.body.length - 1];
    const intoTail = n.x === tail.x && n.y === tail.y && G.grow === 0;
    if (!intoTail && blocked(n.x, n.y)) {
      G.dead = 0.01; G.from = null;
      F.crash(h.x * C + M, h.y * C + M);
      G.why = n.x < 0 || n.y < 0 || n.x >= COLS || n.y >= ROWS ? "INTO THE REEDS" : G.stones.some((s) => s.x === n.x && s.y === n.y) ? "BONK" : "TANGLED UP";
      return;
    }
    G.from = G.body.map((b) => ({ x: b.x, y: b.y }));
    G.body.unshift(n);
    if (G.grow > 0) G.grow--; else G.body.pop();
    if (n.x === G.flea.x && n.y === G.flea.y) {
      G.score++; G.grow++; G.fleas++;
      // Fleas eaten one soon after another carry the run of notes on.
      G.row = G.t - G.ateAt < 4 ? G.row % 8 + 1 : 1;
      G.ateAt = G.t;
      F.got(n.x * C + M, n.y * C + M, "+1", { streak: G.row });
      G.every = FAST + (SLOW - FAST) * Math.pow(QUICKEN, G.fleas);
      if (G.score % 6 === 0 && G.stones.length < 7) G.stones.push(free());
      G.flea = free();
      if (!G.gold && Math.random() < 0.18) G.gold = { ...free(), left: 6 };
    } else if (G.gold && n.x === G.gold.x && n.y === G.gold.y) {
      G.score += 3; G.grow += 2;
      F.got(n.x * C + M, n.y * C + M, "+3", { big: true, streak: G.row });
      G.gold = null;
    }
  },

  over() { return G && G.dead > 0.45; },
  score() { return G ? G.score : 0; },
  why() { return G ? G.why : ""; },

  draw(x) {
    x.fillStyle = "#2f6b33"; x.fillRect(0, 0, W, H);
    x.fillStyle = "#1d5163"; x.fillRect(3, 3, W - 6, H - 6);
    x.fillStyle = "#1b4a5a";
    for (let r = 0; r < ROWS; r++) for (let c = (r % 2); c < COLS; c += 2) x.fillRect(c * C + 3, r * C + 3, C - 6, C - 6);
    x.fillStyle = "#56b45f";
    for (let i = 0; i < W; i += 5) { x.fillRect(i, 0, 2, 4); x.fillRect(i + 2, H - 4, 2, 4); }
    for (let i = 0; i < H; i += 5) { x.fillRect(0, i, 4, 2); x.fillRect(W - 4, i + 2, 4, 2); }
    if (!G) return;
    for (const s of G.stones) {
      x.fillStyle = "#3a3f46"; x.fillRect(s.x * C + 1, s.y * C + 3, C - 2, C - 4);
      x.fillStyle = "#8a9099"; x.fillRect(s.x * C + 2, s.y * C + 2, C - 4, C - 6);
      x.fillStyle = "#b6bcc4"; x.fillRect(s.x * C + 4, s.y * C + 3, 4, 2);
    }
    const bob = Math.floor(G.t * 6) % 2, fx = G.flea.x * C, fy = G.flea.y * C + bob;
    x.fillStyle = "#e9e3d6"; x.fillRect(fx + 5, fy + 6, 6, 4); x.fillRect(fx + 6, fy + 5, 4, 6);
    x.fillStyle = "#3a3f46"; x.fillRect(fx + 7, fy + 7, 2, 2); x.fillRect(fx + 4, fy + 4, 1, 2); x.fillRect(fx + 11, fy + 4, 1, 2);
    if (G.gold && (G.gold.left > 2 || Math.floor(G.gold.left * 8) % 2)) {
      x.fillStyle = "rgba(255,226,122,0.3)"; x.fillRect(G.gold.x * C, G.gold.y * C, C, C);
      x.fillStyle = "#ffe27a"; x.fillRect(G.gold.x * C + 4, G.gold.y * C + 5 + bob, 8, 6);
    }
    const flash = G.dead && Math.floor(G.dead * 12) % 2;
    // Each one is a tadpole, a round head and a tail that tapers and wiggles.
    // The school moves a cell at a time, and each is drawn part way there.
    const k = G.from ? Math.min(1, G.tick / G.every) : 1;
    for (let i = G.body.length - 1; i >= 0 && !flash; i--) {
      const b = G.body[i], was = (G.from && G.from[i]) || b;
      const dx = b.x - was.x || (i ? 0 : DIRS[G.dir][0]), dy = b.y - was.y || (i || dx ? 0 : DIRS[G.dir][1]);
      const cx = Math.round((was.x + (b.x - was.x) * k) * C + M), cy = Math.round((was.y + (b.y - was.y) * k) * C + M);
      const one = () => {
        for (let k = 1; k <= 3; k++) {
          const wig = Math.round(Math.sin(G.t * 14 + i - k) * k * 0.8), sz = 4 - k;
          const tx = cx - dx * (3 + k * 3) + (dy ? wig : 0), ty = cy - dy * (3 + k * 3) + (dx ? wig : 0);
          x.fillStyle = col[3]; x.fillRect(Math.round(tx - sz / 2), Math.round(ty - sz / 2), sz + 1, sz + 1);
        }
        x.fillStyle = col[3]; x.fillRect(cx - 4, cy - 3, 8, 6); x.fillRect(cx - 3, cy - 4, 6, 8);
        x.fillStyle = col[1]; x.fillRect(cx - 3, cy - 2, 6, 4); x.fillRect(cx - 2, cy - 3, 4, 6);
        x.fillStyle = "rgba(255,246,220,0.45)"; x.fillRect(cx - 2 - dy, cy - 2 + dx, 2, 1);
        x.fillStyle = "#0a0908";
        if (dx) { x.fillRect(cx + dx * 2, cy - 2, 1, 1); x.fillRect(cx + dx * 2, cy + 1, 1, 1); }
        else { x.fillRect(cx - 2, cy + dy * 2, 1, 1); x.fillRect(cx + 1, cy + dy * 2, 1, 1); }
      };
      if (i) one(); else F.squashed(x, cx, cy + 4, one);
    }
    F.draw(x);
    x.font = "700 12px 'Space Grotesk', sans-serif"; x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(String(G.score), 6, 15);
  },
};

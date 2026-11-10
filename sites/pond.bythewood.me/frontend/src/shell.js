import { sfx } from "./sound.js";
import { colours } from "./frog.js";

// Shell game. A tadpole hides under one snail shell, the shells shuffle, and
// a tap picks one. Each round shuffles more and faster, a fourth shell joins at
// round four and a fifth at eight, and round r pays r. One wrong pick ends it.
const W = 200, H = 300, GROUND = 196;

let G = null, col = null;

function count(r) { return r < 4 ? 3 : r < 8 ? 4 : 5; }
function slotX(i, n) { const gap = Math.min(56, 168 / (n - 1)); return W / 2 + (i - (n - 1) / 2) * gap; }

function round() {
  const n = count(G.r);
  while (G.shells.length < n) G.shells.push({ slot: G.shells.length, lift: 0, dy: 0 });
  G.shells.forEach((s) => { s.lift = 0; s.dy = 0; s.x = slotX(s.slot, n); });
  G.prize = Math.floor(Math.random() * n);
  G.cur = Math.min(G.cur, n - 1);
  G.picked = -1;
  const k = 3 + Math.floor(G.r * 1.5), d = Math.max(0.16, 0.48 - G.r * 0.03);
  G.swaps = [];
  for (let i = 0; i < k; i++) {
    const a = Math.floor(Math.random() * n);
    let b = Math.floor(Math.random() * (n - 1));
    if (b >= a) b++;
    const pair = [[a, b]];
    if (n >= 4 && G.r >= 6 && Math.random() < 0.3) {
      const rest = [...Array(n).keys()].filter((s) => s !== a && s !== b);
      pair.push([rest[0], rest[1]]);
    }
    G.swaps.push({ pairs: pair, d });
  }
  G.st = "show"; G.st_t = 0;
}

function at(slot) { return G.shells.find((s) => s.slot === slot); }

function pick(i) {
  if (G.st !== "pick") return;
  G.picked = i; G.st = "reveal"; G.st_t = 0;
  const s = at(i);
  if (G.shells.indexOf(s) === G.prize) {
    G.score += G.r; G.won++;
    G.pops.push({ x: slotX(i, G.shells.length), y: GROUND - 40, s: "+" + G.r, c: "#ffb000", t: 0 });
    sfx("catch");
  } else {
    G.why = "IT WAS THIS ONE";
    sfx("bonk");
  }
}

export const shell = {
  key: "shell", W, H, title: "SHELL GAME",
  help: ["WATCH THE SHELL THE TADPOLE'S UNDER", "TAP IT AFTER THE SHUFFLE", "ONE WRONG PICK ENDS IT", "TAP TO START"],

  reset(g, p) {
    col = colours(g, p.frog.hat);
    G = { t: 0, r: 1, shells: [], prize: 0, swaps: [], st: "show", st_t: 0, picked: -1, cur: 1, score: 0, won: 0, pops: [], why: "", end: 0 };
    round();
  },

  press(x, y) {
    if (!G || G.st !== "pick" || y < GROUND - 50 || y > GROUND + 20) return;
    const n = G.shells.length, gap = Math.min(56, 168 / (n - 1));
    for (let i = 0; i < n; i++) if (Math.abs(x - slotX(i, n)) < gap / 2) { pick(i); return; }
  },

  keys(code, down) {
    if (!G || !down) return;
    const n = G.shells.length;
    if (code === "ArrowLeft" || code === "KeyA") G.cur = Math.max(0, G.cur - 1);
    if (code === "ArrowRight" || code === "KeyD") G.cur = Math.min(n - 1, G.cur + 1);
    if (code === "Space" || code === "Enter") pick(G.cur);
    G.kb = true;
  },

  step(dt) {
    G.t += dt; G.st_t += dt;
    const n = G.shells.length;
    if (G.st === "show") {
      const s = G.shells[G.prize], t = G.st_t;
      s.lift = t < 0.25 ? t / 0.25 : t < 0.95 ? 1 : Math.max(0, 1 - (t - 0.95) / 0.25);
      if (t > 1.3) { s.lift = 0; G.st = "shuffle"; G.st_t = 0; }
    } else if (G.st === "shuffle") {
      const sw = G.swaps[0];
      if (!sw) { G.st = "pick"; G.st_t = 0; return; }
      if (!sw.from) {
        sw.from = sw.pairs.map(([a, b]) => [at(a), at(b)]);
        sfx("hop");
      }
      const k = Math.min(1, G.st_t / sw.d);
      sw.pairs.forEach(([a, b], j) => {
        const [sa, sb] = sw.from[j];
        sa.x = slotX(a, n) + (slotX(b, n) - slotX(a, n)) * ease(k); sa.dy = -Math.sin(Math.PI * k) * 12;
        sb.x = slotX(b, n) + (slotX(a, n) - slotX(b, n)) * ease(k); sb.dy = Math.sin(Math.PI * k) * 6;
      });
      if (k >= 1) {
        sw.pairs.forEach(([a, b], j) => { const [sa, sb] = sw.from[j]; sa.slot = b; sb.slot = a; sa.dy = sb.dy = 0; });
        G.swaps.shift(); G.st_t = 0;
      }
    } else if (G.st === "reveal") {
      const t = G.st_t, s = at(G.picked), right = G.shells.indexOf(s) === G.prize;
      s.lift = Math.min(1, t / 0.2);
      if (!right && t > 0.4) G.shells[G.prize].lift = Math.min(1, (t - 0.4) / 0.2);
      if (right && t > 0.9) { G.r++; round(); }
      if (!right && t > 1.6) G.end = 1;
    }
    G.shells.forEach((s) => { if (G.st !== "shuffle" || !G.swaps[0] || !G.swaps[0].from || !G.swaps[0].from.flat().includes(s)) { s.x = slotX(s.slot, n); s.dy = 0; } });
    G.pops.forEach((p) => { p.t += dt; p.y -= dt * 20; });
    G.pops = G.pops.filter((p) => p.t < 0.8);
  },

  over() { return G && G.end; },
  score() { return G ? G.score : 0; },
  why() { return G ? G.why : ""; },
  tally() { return G ? ["Found it " + G.won + (G.won === 1 ? " time" : " times")] : []; },

  draw(x) {
    ["#2a5163", "#33606e", "#3f6f7a"].forEach((c, i) => { x.fillStyle = c; x.fillRect(0, i * 30, W, 30); });
    x.fillStyle = "#1d5163"; x.fillRect(0, 90, W, 70);
    x.fillStyle = "#7fd6c2"; for (let i = 0; i < W; i += 12) x.fillRect((i + Math.round((G ? G.t : 0) * 6)) % W, 92 + (i % 3) * 18, 6, 1);
    x.fillStyle = "#1f4a30"; for (let i = 0; i < W; i += 4) { const h = 8 + ((i * 37) % 9); x.fillRect(i, 90 - h, 4, h); }
    x.fillStyle = "#4a3a28"; x.fillRect(0, 160, W, H - 160);
    x.fillStyle = "#5a4630"; x.fillRect(0, 160, W, 6);
    x.fillStyle = "#3a2c1e"; for (let i = 0; i < 40; i++) x.fillRect((i * 47) % W, 172 + ((i * 29) % (H - 180)), 2, 1);
    x.fillStyle = "#2f6b33"; [8, 14, 180, 190].forEach((rx, i) => x.fillRect(rx, 128 - i * 4, 2, 34 + i * 4));
    if (!G) return;
    const order = [...G.shells].sort((a, b) => a.dy - b.dy);
    const prize = G.shells[G.prize];
    if (prize.lift > 0) tadpole(x, prize.x, GROUND, G.t);
    order.forEach((s) => snail(x, s.x, GROUND + s.dy - s.lift * 22, s.dy < 0));
    if (G.st === "pick" && G.kb) {
      const cx = Math.round(slotX(G.cur, G.shells.length));
      x.fillStyle = "#ffb000"; x.fillRect(cx - 3, GROUND + 8, 7, 2); x.fillRect(cx - 1, GROUND + 6, 3, 2);
    }
    x.font = "700 13px 'Space Grotesk', sans-serif"; x.textAlign = "center";
    x.lineWidth = 3; x.strokeStyle = "#0a0908";
    G.pops.forEach((p) => { x.strokeText(p.s, p.x, p.y); x.fillStyle = p.c; x.fillText(p.s, p.x, p.y); });
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(String(G.score), 6, 18);
    x.textAlign = "right"; x.font = "500 8px 'JetBrains Mono', monospace"; x.fillStyle = "#bcaf99"; x.fillText("ROUND " + G.r, W - 6, 16);
    const s = G.st === "show" ? "WATCH THE TADPOLE" : G.st === "pick" ? "WHICH SHELL?" : "";
    if (s) {
      x.textAlign = "center"; x.strokeText(s, W / 2, 250); x.fillStyle = "#ece5d8"; x.fillText(s, W / 2, 250);
    }
  },
};

function ease(k) { return k < 0.5 ? 2 * k * k : 1 - 2 * (1 - k) * (1 - k); }

function tadpole(x, cx, by, t) {
  cx = Math.round(cx); by = Math.round(by);
  x.fillStyle = col[3]; x.fillRect(cx - 4, by - 6, 7, 5);
  x.fillStyle = col[1]; x.fillRect(cx - 3, by - 6, 5, 4);
  x.fillStyle = "#0a0908"; x.fillRect(cx - 3, by - 5, 1, 1);
  const w = Math.round(Math.sin(t * 14));
  x.fillStyle = col[3]; x.fillRect(cx + 3, by - 4 + w, 3, 2); x.fillRect(cx + 6, by - 4 - w, 2, 1);
}

// A snail shell from the side, a dome with a spiral and a lip along the base.
function snail(x, cx, by, front) {
  cx = Math.round(cx); by = Math.round(by);
  x.fillStyle = "rgba(10,9,8,0.35)"; x.fillRect(cx - 12, Math.max(by, GROUND) + 1, 24, 2);
  for (let k = 0; k < 16; k++) {
    const hw = Math.round(12 * Math.sqrt(1 - (k / 16) * (k / 16)));
    x.fillStyle = front ? "#d6a35a" : "#c9974f"; x.fillRect(cx - hw, by - k - 1, hw * 2, 1);
  }
  x.fillStyle = "#8a5a2b";
  for (let a = 0; a < 14; a += 0.35) {
    const r = 1 + a * 0.62, px = cx + Math.round(Math.cos(a) * r), py = by - 7 + Math.round(Math.sin(a) * r * 0.85);
    if (py < by - 1 && py > by - 16) x.fillRect(px, py, 1, 1);
  }
  x.fillStyle = "#e8c48a"; x.fillRect(cx - 6, by - 13, 3, 1); x.fillRect(cx - 8, by - 11, 2, 1);
  x.fillStyle = "#6b4422"; x.fillRect(cx - 12, by - 1, 24, 1);
}

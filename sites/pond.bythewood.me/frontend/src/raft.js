import { colours } from "./frog.js";
import { drawTop } from "./top.js";
import { feel } from "./feel.js";
import { sfx, chime } from "./sound.js";
import { reduced } from "./api.js";
import { PENTA } from "./drop.js";
import { W, H, FY, SELF, RING, EYE, PAY, EDDY, centre, half, flow, speed, world, run, score, metres, over } from "./raftworld.js";

// Raft run. The frog rides a leaf down a river that keeps narrowing and
// steers it itself, and a held finger or the arrow keys take over until let go.
const LAMP = "#ffcf6a", SURF = "#7fd6c2";

let G = null, col = null;
const F = feel();

function heard(e) {
  if (e.k === "bump") F.crash(e.x, FY);
  else if (e.k === "tip") { F.crash(e.x, FY, { sound: "splash" }); F.squash(1); }
  else if (e.k === "weir") { F.got(e.x, FY - 2, "+" + PAY.weir, { streak: e.n }); if (e.close) F.close(e.x, FY - 26); }
  else if (e.k === "close") F.close(e.x, FY - 26);
  else if (e.k === "spun") F.got(e.x, FY - 2, "SPUN +" + PAY.spun, { big: true, streak: e.n });
  else if (e.k === "pebble") F.got(e.x, FY - 2, "+" + PAY.pebble, { streak: e.n });
  else if (e.k === "shell") F.got(e.x, FY - 2, "+" + PAY.shell, { streak: e.n, sound: "pip" });
  else if (e.k === "fly") {
    chime(PENTA[Math.min(PENTA.length - 1, 1 + e.n)], 0.04);
    F.burst(e.x, FY - 4, LAMP, 5); F.pop(e.x, FY - 12, "+" + PAY.fly, LAMP);
  } else if (e.k === "warn") sfx("warn");
}

// Pale streaks ride the current, faster down the middle than at the banks,
// so the water can be seen to move.
function streaks() {
  const s = [];
  for (let i = 0; i < 28; i++) s.push({ k: (i * 0.618) % 2 - 1, y: (i * 97) % (H + 20) - 10, n: 3 + (i % 4) });
  return s;
}

export const raft = {
  key: "raft", W, H, title: "RAFT RUN", feel: F, edge: ["#3f8a44", "#3f8a44"],
  line: SELF ? "Drift down the river, steer if you like." : "Steer a leaf down the river.",
  aria: SELF ? "The frog steers the leaf. Hold where it should go, or the arrow keys, to take over until you let go." : "Hold where the raft should go, or use the arrow keys.",
  help: SELF
    ? ["THE FROG STEERS ITSELF", "HOLD WHERE IT SHOULD GO TO TAKE OVER", "CATCH FIREFLIES, SHELLS AND PEBBLES", "A ROCK HIT HEAD ON TIPS THE LEAF", "TAP TO START"]
    : ["HOLD WHERE THE RAFT SHOULD GO", "THREAD THE GAPS IN THE REEDS", "CATCH FIREFLIES, SHELLS AND PEBBLES", "A ROCK HIT HEAD ON TIPS THE LEAF", "TAP TO START"],

  reset(g) {
    col = colours(g, null);
    G = { w: world(), want: null, keys: { left: false, right: false }, touched: false, streaks: streaks() };
  },

  demo() {
    if (!G || G.touched) return null;
    const w = G.w.things.find((t) => t.k === "weir" && !t.done);
    return w ? { x: w.gx + w.gap / 2, y: FY - 30, hold: true } : null;
  },

  drag(x) { if (G) { G.want = x; G.touched = true; } },
  release() { if (G) G.want = null; },
  hold(dir, on) { if (G) { G.keys[dir] = on; if (on) G.touched = true; } },
  pause() { if (G) { G.want = null; G.keys = { left: false, right: false }; } },

  step(dt) {
    const w = G.w;
    run(w, dt, { want: G.want, left: G.keys.left, right: G.keys.right });
    w.events.forEach(heard);
    w.events.length = 0;
    for (const s of G.streaks) {
      const x = centre(s.y) + s.k * half(s.y);
      if (!w.dead) s.y += speed(w.t) * flow(x, s.y) * 1.3 * dt;
      const sy = H - (s.y - w.dist);
      if (sy < -10) s.y = w.dist - 10 + Math.random() * 8;
      else if (sy > H + 10) s.y = w.dist + H + 4;
    }
  },

  over() { return G && over(G.w); },
  score() { return G ? score(G.w) : 0; },
  why() { return G ? G.w.why : ""; },
  report() {
    const w = G.w;
    return { counts: { weirs: w.weirs, pebbles: w.pebbles, clean: w.clean < 0 ? score(w) : w.clean, whirls: w.whirls, flies: w.flies, far: Math.floor(metres(w) / 10) } };
  },
  tally() {
    if (!G) return [];
    const w = G.w, l = ["Drifted " + metres(G.w) + " metres"];
    if (w.flies) l.push(w.flies + (w.flies > 1 ? " fireflies" : " firefly") + " caught");
    if (w.shells) l.push(w.shells + (w.shells > 1 ? " shells" : " shell") + " picked up");
    return l;
  },

  draw(x) {
    const w = G ? G.w : null, dist = w ? w.dist : 0;
    x.fillStyle = "#3f8a44"; x.fillRect(-4, 0, W + 8, H);
    for (let s = 0; s < H; s += 2) {
      const wy = dist + (H - s), c = centre(wy), h = half(wy);
      x.fillStyle = "#2f6b33"; x.fillRect(Math.round(c - h - 4), s, Math.round(h * 2 + 8), 2);
      x.fillStyle = "#1d5163"; x.fillRect(Math.round(c - h), s, Math.round(h * 2), 2);
      // The slower water along each bank is a shade darker.
      x.fillStyle = "#1a4858"; x.fillRect(Math.round(c - h), s, Math.round(h * 0.28), 2); x.fillRect(Math.round(c + h * 0.72), s, Math.round(h * 0.28), 2);
      if (Math.floor(wy / 2) % 23 === 0) { x.fillStyle = "#4f9a4a"; x.fillRect(Math.round(c - h - 14), s, 2, 4); x.fillRect(Math.round(c + h + 12), s, 2, 4); }
    }
    if (!w) return;
    x.fillStyle = "#4f93a0";
    for (const s of G.streaks) {
      const sy = Math.round(H - (s.y - dist)), sx = Math.round(centre(s.y) + s.k * half(s.y));
      x.fillRect(sx, sy, 1, Math.round(s.n * flow(sx, s.y)));
    }
    for (const t of w.things) {
      const sy = Math.round(H - (t.y - dist));
      if (t.k === "whirl") drawWhirl(x, t.x, sy, w.t);
      else if (t.k === "rock") rock(x, t, sy, w.t);
      else if (t.k === "pebble" && !t.done) {
        x.fillStyle = "#d5d8dc"; x.fillRect(Math.round(t.x) - 3, sy - 2, 6, 4); x.fillStyle = "#9aa0a6"; x.fillRect(Math.round(t.x) - 1, sy, 4, 2);
      } else if (t.k === "shell" && !t.done) shell(x, t.x, sy + Math.round(Math.sin(w.t * 3 + t.y) * 0.6));
      else if (t.k === "fly" && !t.done) firefly(x, t.x, sy - 6, w.t + t.ph);
      else if (t.k === "weir") {
        const c = centre(t.y), h = half(t.y);
        for (let rx = Math.round(c - h); rx < c + h; rx += 3) {
          if (rx + 2 > t.gx && rx < t.gx + t.gap) continue;
          x.fillStyle = "#56b45f"; x.fillRect(rx, sy - 8, 2, 10);
          if (rx % 2) { x.fillStyle = "#8a5a2b"; x.fillRect(rx, sy - 11, 2, 4); }
        }
        x.fillStyle = t.done ? "#2f6b33" : "#ffe27a"; x.fillRect(Math.round(t.gx) - 1, sy - 6, 1, 6); x.fillRect(Math.round(t.gx + t.gap), sy - 6, 1, 6);
      }
    }
    if (w.tipped) tipped(x, w);
    else if (w.lives > 0 && !(w.safe > 0 && Math.floor(w.safe * 10) % 2)) leaf(x, w);
    F.draw(x);
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.font = "700 13px Inter, sans-serif"; x.fillText(String(score(w)), 6, 16);
    x.fillStyle = "#e2567a"; for (let i = 0; i < w.lives; i++) x.fillRect(W - 12 - i * 9, 6, 6, 6);
  },
};

// The leaf, lit lamp yellow round its rim while a finger has it, and with the
// frog's paddle out on the side it's turning to while the frog has it.
function leaf(x, w) {
  const fx = Math.round(w.fx);
  x.fillStyle = SURF; x.fillRect(fx - 7, FY + 14 + (Math.floor(w.t * 8) % 2), 3, 1); x.fillRect(fx + 4, FY + 14 + (Math.floor(w.t * 8 + 1) % 2), 3, 1);
  if (w.mine && G.want !== null) { x.fillStyle = LAMP; x.fillRect(Math.round(G.want) - 1, FY + 18, 3, 1); }
  F.squashed(x, fx, FY + 4, () => {
    if (w.glow > 0) {
      x.fillStyle = LAMP;
      for (let i = 0; i < 24; i += 3) { x.fillRect(fx - 11 + i, FY - 10, 1, 1); x.fillRect(fx - 11 + i, FY + 16, 1, 1); }
      for (let i = 0; i < 26; i += 3) { x.fillRect(fx - 12, FY - 9 + i, 1, 1); x.fillRect(fx + 11, FY - 9 + i, 1, 1); }
    }
    x.fillStyle = w.mine ? LAMP : "#1f5a2c"; x.fillRect(fx - 9, FY - 6, 18, 20);
    x.fillStyle = w.glow > 0.25 ? "#8fd27a" : "#56b45f"; x.fillRect(fx - 8, FY - 7, 16, 20);
    x.fillStyle = "#3f9a4d"; x.fillRect(fx, FY - 6, 1, 18);
    drawTop(x, col, fx - 6, FY - 4, false);
    if (!w.mine && Math.abs(w.turn) > 0.3) {
      const side = w.turn > 0 ? -1 : 1, px = fx + side * 10 + (side < 0 ? -1 : 0), dip = Math.floor(w.t * 6) % 2;
      x.fillStyle = "#8a5a2b"; x.fillRect(px, FY + 2 + dip, 2, 6);
      x.fillStyle = SURF; x.fillRect(px - 1, FY + 8 + dip, 4, 1);
    }
  });
}

// A leaf hit head on goes over, its pale underside up, and the frog goes in.
function tipped(x, w) {
  const fx = Math.round(w.fx), p = Math.min(1, w.dead / 0.5), wide = Math.max(1, Math.round(16 * Math.abs(Math.cos(p * Math.PI))));
  x.fillStyle = p < 0.5 ? "#56b45f" : "#a7c98a"; x.fillRect(fx - Math.round(wide / 2), FY - 7 + Math.round(p * 4), wide, 20);
  if (p < 0.5) drawTop(x, col, fx - 6, FY - 4 - Math.round(p * 10), false);
  const r = Math.round(4 + w.dead * 18);
  if (!reduced || w.dead < 0.3) {
    x.fillStyle = SURF;
    for (let a = 0; a < 12; a++) x.fillRect(Math.round(fx + Math.cos(a / 12 * 6.28) * r), Math.round(FY + 16 + Math.sin(a / 12 * 6.28) * r / 2), 2, 1);
  }
}

// A rock, with its eddy curling round in the water just past it.
function rock(x, t, sy, now) {
  const r = t.r, rx = Math.round(t.x);
  x.fillStyle = "#4f93a0";
  for (let k = 0; k < 3; k++) {
    const a = now * 3 + k * 2.1, d = 2 + ((now * 10 + k * 7) % (EDDY - 6));
    x.fillRect(rx + Math.round(Math.cos(a) * (r - 1)), sy - r - Math.round(d), 2, 1);
  }
  x.fillStyle = "#3a3f46"; x.fillRect(rx - r, sy - r + 2, r * 2, r * 2 - 2);
  x.fillStyle = "#8a9099"; x.fillRect(rx - r + 1, sy - r, r * 2 - 2, r * 2 - 3);
  x.fillStyle = "#b6bcc4"; x.fillRect(rx - r + 2, sy - r + 1, 3, 2);
  x.fillStyle = SURF; x.fillRect(rx - r - 1, sy + r - 1, r * 2 + 2, 1);
}

function shell(x, sx, sy) {
  sx = Math.round(sx);
  x.fillStyle = "#e9d6c0"; x.fillRect(sx - 3, sy - 2, 7, 4); x.fillRect(sx - 2, sy - 3, 5, 1);
  x.fillStyle = "#c79a7a"; x.fillRect(sx - 2, sy - 1, 1, 3); x.fillRect(sx, sy - 2, 1, 4); x.fillRect(sx + 2, sy - 1, 1, 3);
  x.fillStyle = SURF; x.fillRect(sx - 4, sy + 2, 9, 1);
}

function firefly(x, fx, fy, t) {
  fx = Math.round(fx); fy = Math.round(fy + Math.sin(t * 4) * 1.5);
  if (Math.sin(t * 5) > -0.3) { x.fillStyle = "#c98a2a"; x.fillRect(fx - 3, fy, 7, 1); x.fillRect(fx, fy - 3, 1, 7); }
  x.fillStyle = LAMP; x.fillRect(fx - 1, fy - 1, 3, 3);
  x.fillStyle = "#fff6dc"; x.fillRect(fx, fy, 1, 1);
}

// A whirlpool is rings of pale water turning round a dark eye, drawn a dash
// at a time so it reads as pixels.
function drawWhirl(x, wx, wy, t) {
  for (let dy = -RING / 2; dy < RING / 2; dy++) {
    const k = (dy + 0.5) / (RING / 2), hw = Math.round(RING * Math.sqrt(1 - k * k));
    x.fillStyle = Math.abs(k) < 0.45 && hw > RING * 0.7 ? "#123a44" : "#16404d";
    x.fillRect(Math.round(wx) - hw, Math.round(wy) + dy, hw * 2, 1);
  }
  x.fillStyle = "#0a1b1f"; x.fillRect(Math.round(wx - EYE), Math.round(wy - 2), EYE * 2, 4);
  x.fillStyle = SURF;
  for (let r = 7; r <= RING; r += 6) {
    for (let k = 0; k < 3; k++) {
      const a = t * (4 - r / 8) + k * 2.1 + r;
      x.fillRect(Math.round(wx + Math.cos(a) * r), Math.round(wy + Math.sin(a) * r / 2), 3, 1);
    }
  }
}

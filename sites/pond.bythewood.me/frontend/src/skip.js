import { sfx } from "./sound.js";
import { sideCells, colours, draw, SW, SH } from "./frog.js";
import { feel } from "./feel.js";

// Pebble skip. Drag back from anywhere and let go to throw, flat and hard
// skips best. A tap just before the pebble meets the water keeps more of its
// speed and scores double, a skip off a lily pad scores 3, and a duck stops
// it dead. Three pebbles a run, with the pads and ducks dealt fresh for each.
const W = 200, H = 300, SURF = 200, GRAV = 380, MAXA = 0.5, MINV = 55, NICE = 0.14, THROWS = 3, M = 8;
const L = { x: 30, y: SURF - 20 };

let G = null, spr = null, col = null;
const F = feel();

function aimFrom(p0, p1) {
  const dx = p0.x - p1.x, dy = p0.y - p1.y, power = Math.min(1, Math.hypot(dx, dy) / 70);
  const elev = Math.max(-0.15, Math.min(1, Math.atan2(-dy, Math.max(dx, 1))));
  return { power, elev };
}

function deal() {
  G.things = [];
  for (let x = L.x + 70 + Math.random() * 60; x < 1400; x += 60 + Math.random() * 110) {
    G.things.push({ x, duck: x > 220 && Math.random() < 0.28, used: false, ph: Math.random() * 6 });
  }
}

function launch(a) {
  const v = 120 + a.power * 280;
  G.peb = { x: L.x, y: L.y, vx: v * Math.cos(a.elev), vy: -v * Math.sin(a.elev), skips: 0, spin: 0 };
  G.st = "fly"; G.tapAt = null; G.aim = null;
  sfx("cast"); F.squash(0.8);
}

// Whether a throw would skip at its first touch, for colouring the aim line.
function skips(a) {
  const v = 120 + a.power * 280, vx = v * Math.cos(a.elev), vy0 = -v * Math.sin(a.elev);
  const vy = Math.sqrt(vy0 * vy0 + 2 * GRAV * (SURF - L.y));
  return vx > MINV && Math.atan2(vy, vx) < MAXA;
}

function touch() {
  const p = G.peb, good = G.tapAt !== null && G.t - G.tapAt <= NICE, early = G.tapAt !== null && !good;
  G.tapAt = null;
  G.rings.push({ x: p.x, t: 0 });
  const hit = G.things.find((o) => !o.used && Math.abs(o.x - p.x) < (o.duck ? 6 : 9));
  if (hit && hit.duck) {
    hit.used = true;
    F.crash(p.x, SURF - 6, { s: "QUACK" });
  } else if (hit || (Math.atan2(p.vy, p.vx) < MAXA && p.vx > MINV)) {
    const chop = 0.97 + Math.random() * 0.06;
    p.vy = -Math.max(40, p.vy * (hit ? 0.8 : good ? 0.72 : 0.58));
    p.vx *= (hit ? 0.95 : good ? 0.9 : 0.82) * chop;
    p.y = SURF; p.skips++;
    const pts = (good ? 2 : 1) + (hit ? 2 : 0);
    if (hit) hit.used = true;
    G.score += pts;
    if (hit || good) F.got(p.x, SURF - 4, hit ? "PAD +" + pts : "NICE +2", { streak: p.skips, big: !!hit });
    else {
      sfx("hop", p.skips);
      F.burst(p.x, SURF - 2, "#cfe3ea", 3);
      F.pop(p.x, SURF - 14, early ? "EARLY +1" : "+1", early ? "#bcaf99" : "#ece5d8");
    }
    return;
  }
  const m = Math.round((p.x - L.x) / M);
  G.best = Math.max(G.best, p.skips); G.far = Math.max(G.far, m);
  F.pop(p.x, SURF - 18, p.skips ? p.skips + " SKIPS, " + m + "M" : "PLOP", "#ece5d8");
  G.st = "sunk"; G.st_t = 0;
  sfx("plop");
}

export const skip = {
  key: "skip", W, H, title: "PEBBLE SKIP", feel: F,
  aria: "Drag back and let go to throw.",
  help: ["DRAG BACK AND LET GO TO THROW", "FLAT AND HARD SKIPS BEST", "TAP AS IT HITS THE WATER FOR DOUBLE", "PADS PAY 3, DUCKS STOP IT", "TAP TO START"],

  reset(g, p) {
    spr = sideCells(g, p.frog.stage === "egg" ? "tadpole" : p.frog.stage, false);
    col = colours(g, p.frog.hat);
    G = { t: 0, st: "aim", st_t: 0, cam: 0, peb: null, aim: null, rings: [], score: 0, thrown: 0, best: 0, far: 0, tapAt: null, why: "", end: false, kang: 0.2, charge: -1, things: [] };
    deal();
  },

  demo() { return G && G.st === "aim" && !G.aim ? { x: 124, y: 150, to: [64, 172] } : null; },

  press(x, y) {
    if (!G) return;
    if (G.st === "aim") G.aim = { p0: { x, y }, p1: { x, y } };
    else if (G.st === "fly" && G.tapAt === null) G.tapAt = G.t;
  },
  drag(x, y) { if (G && G.aim && G.aim.p0) G.aim.p1 = { x, y }; },
  release() {
    if (!G || !G.aim || !G.aim.p0) return;
    const a = aimFrom(G.aim.p0, G.aim.p1);
    G.aim = null;
    if (a.power > 0.08) launch(a);
  },

  keys(code, down) {
    if (!G) return;
    if (down && (code === "ArrowUp" || code === "KeyW")) G.kang = Math.min(1, G.kang + 0.05);
    if (down && (code === "ArrowDown" || code === "KeyS")) G.kang = Math.max(-0.15, G.kang - 0.05);
    if (code !== "Space" && code !== "Enter") return;
    if (G.st === "fly" && down) { skip.press(0, 0); return; }
    if (G.st !== "aim") return;
    if (down && G.charge < 0) G.charge = 0;
    else if (!down && G.charge >= 0) { launch({ power: power(G.charge), elev: G.kang }); G.charge = -1; }
  },

  step(dt) {
    G.t += dt; G.st_t += dt;
    if (G.charge >= 0) G.charge += dt;
    const p = G.peb;
    if (G.st === "fly") {
      p.vy += GRAV * dt; p.x += p.vx * dt; p.y += p.vy * dt; p.spin += dt;
      if (p.y >= SURF && p.vy > 0) touch();
      G.cam = Math.max(0, p.x - 70);
    } else if (G.st === "sunk" && G.st_t > 1) {
      G.thrown++;
      if (G.thrown >= THROWS) { G.end = true; G.why = "OUT OF PEBBLES"; }
      else { G.st = "back"; G.st_t = 0; }
    } else if (G.st === "back") {
      G.cam += (0 - G.cam) * Math.min(1, dt * 6);
      if (G.cam < 1) { G.cam = 0; G.st = "aim"; G.peb = null; deal(); }
    }
    G.rings.forEach((r) => { r.t += dt; });
    G.rings = G.rings.filter((r) => r.t < 1.2);
  },

  over() { return G && G.end; },
  score() { return G ? G.score : 0; },
  why() { return G ? G.why : ""; },
  tally() { return G && G.best ? ["Best throw " + G.best + " skips, farthest " + G.far + "m"] : []; },

  draw(x) {
    const cam = G ? G.cam : 0, t = G ? G.t : 0;
    ["#3f6f7a", "#4f7f85", "#638e8a", "#7a9e8e"].forEach((c, i) => { x.fillStyle = c; x.fillRect(0, i * 40, W, 40); });
    x.fillStyle = "#7a9e8e"; x.fillRect(0, 160, W, 40);
    x.fillStyle = "#2f5a3a";
    for (let i = -8; i < W + 8; i += 4) { const wx = i + cam * 0.3, h = 12 + Math.abs(Math.floor(wx * 37) % 13); x.fillRect(i - Math.round((cam * 0.3) % 4), SURF - h, 4, h); }
    x.fillStyle = "#1d5163"; x.fillRect(0, SURF, W, H - SURF);
    x.fillStyle = "#184656"; x.fillRect(0, SURF + 40, W, 30);
    x.fillStyle = "#143c4a"; x.fillRect(0, SURF + 70, W, H - SURF - 70);
    x.fillStyle = "#7fd6c2";
    for (let i = 0; i < 16; i++) { const wx = ((i * 97 - cam) % (W + 20) + W + 20) % (W + 20) - 10; x.fillRect(Math.round(wx), SURF + 4 + (i * 13) % 80, 6, 1); }
    for (let i = 0; i < W; i += 10) x.fillRect(((i - Math.round(cam) + Math.round(t * 6)) % W + W) % W, SURF, 6, 1);
    x.font = "500 8px 'JetBrains Mono', monospace"; x.textAlign = "center";
    for (let m = 10; m < 400; m += 10) {
      const sx = Math.round(L.x + m * M - cam);
      if (sx < -10 || sx > W + 10) continue;
      x.fillStyle = "#e9e3d6"; x.fillRect(sx, SURF - 8, 1, 8);
      x.fillStyle = m === G?.far ? "#ffb000" : "#e2412d"; x.fillRect(sx + 1, SURF - 8, 4, 3);
      x.fillStyle = "#e9e3d6"; x.fillText(m + "M", sx, SURF - 11);
    }
    if (G && G.far) {
      const sx = Math.round(L.x + G.far * M - cam);
      x.fillStyle = "#ffb000"; x.fillRect(sx, SURF - 22, 1, 22); x.fillRect(sx + 1, SURF - 22, 6, 4);
    }
    const bx = -Math.round(cam);
    x.fillStyle = "#4a3a28"; x.fillRect(bx, SURF - 6, 46, H);
    x.fillStyle = "#4f9a4a"; x.fillRect(bx, SURF - 8, 46, 3);
    x.fillStyle = "#2f6b33"; x.fillRect(bx + 40, SURF - 30, 2, 24); x.fillRect(bx + 43, SURF - 24, 2, 18);
    if (!G) return;
    F.squashed(x, bx + 6 + SW / 2, SURF - 7, () => draw(x, spr, SW, SH, col, bx + 6, SURF - 8 - SH + 1, 1));
    G.things.forEach((o) => {
      const sx = Math.round(o.x - cam);
      if (sx < -12 || sx > W + 12) return;
      if (o.duck) duck(x, sx, SURF + Math.round(Math.sin(t * 2 + o.ph)), o.used);
      else { x.fillStyle = o.used ? "#2f6b33" : "#3f9a4d"; x.fillRect(sx - 9, SURF, 18, 2); x.fillStyle = o.used ? "#3f9a4d" : "#56b45f"; x.fillRect(sx - 8, SURF - 1, 16, 2); x.fillStyle = "#1d5163"; x.fillRect(sx + 2, SURF - 1, 2, 1); }
    });
    G.rings.forEach((r) => {
      const s = Math.round(3 + r.t * 12), rx = Math.round(r.x - cam);
      x.fillStyle = "rgba(207,227,234," + (0.8 - r.t * 0.66) + ")";
      x.fillRect(rx - s, SURF + 1, s - 1, 1); x.fillRect(rx + 2, SURF + 1, s - 1, 1);
    });
    if (G.st === "aim") aimLine(x);
    const p = G.peb;
    if (p && G.st === "fly") {
      const px = Math.round(p.x - cam), py = Math.round(p.y);
      if (py < 0) { x.fillStyle = "#c9ccd0"; x.fillRect(px - 1, 2, 3, 2); }
      else {
        x.fillStyle = "#8a9099";
        if (Math.floor(p.spin * 20) % 2) x.fillRect(px - 2, py - 1, 4, 2); else x.fillRect(px - 1, py - 2, 3, 3);
        x.fillStyle = "#c9ccd0"; x.fillRect(px - 1, py - 1, 2, 1);
      }
    }
    F.draw(x, -cam, 0);
    x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(String(G.score), 6, 18);
    x.textAlign = "right"; x.font = "500 8px 'JetBrains Mono', monospace"; x.fillStyle = "#bcaf99";
    x.fillText("PEBBLE " + Math.min(THROWS, G.thrown + 1) + "/" + THROWS, W - 6, 16);
    if (p && G.st === "fly" && p.skips) x.fillText(p.skips + " SKIPS", W - 6, 28);
    if (G.thrown === 0 && !G.best) {
      const s = G.st === "aim" ? (G.aim ? "LET GO TO THROW" : "DRAG BACK, THEN LET GO") : G.st === "fly" ? "TAP AS IT HITS THE WATER" : "";
      if (s) { x.textAlign = "center"; x.strokeText(s, W / 2, H - 20); x.fillStyle = "#ece5d8"; x.fillText(s, W / 2, H - 20); }
    }
  },
};

function duck(x, dx, dy, hit) {
  x.fillStyle = "#6b4a2a"; x.fillRect(dx - 5, dy - 4, 10, 4);
  x.fillStyle = "#8a6a44"; x.fillRect(dx - 4, dy - 5, 6, 2);
  x.fillStyle = "#2f6b33"; x.fillRect(dx + 3, dy - 9, 4, 4);
  x.fillStyle = "#f2c230"; x.fillRect(dx + 7, dy - 7, 2, 1);
  x.fillStyle = "#0a0908"; x.fillRect(dx + 5, dy - 8, 1, 1);
  if (hit) { x.fillStyle = "#e2412d"; x.fillRect(dx + 1, dy - 14, 1, 3); x.fillRect(dx + 1, dy - 10, 1, 1); }
}

function power(ct) { const k = (ct / 0.8) % 2; return Math.max(0.1, k < 1 ? k : 2 - k); }

// The first stretch of the throw as dots, green when it would skip.
function aimLine(x) {
  let a = null;
  if (G.aim && G.aim.p0) a = aimFrom(G.aim.p0, G.aim.p1);
  else if (G.charge >= 0) a = { power: power(G.charge), elev: G.kang };
  else a = { power: 0.5, elev: G.kang, ghost: true };
  const v = 120 + a.power * 280, vx = v * Math.cos(a.elev), vy = -v * Math.sin(a.elev);
  x.fillStyle = a.ghost ? "rgba(236,229,216,0.35)" : skips(a) ? "#7fd67f" : "#e2412d";
  for (let i = 1; i < 10; i++) {
    const s = i * 0.04, px = L.x + vx * s, py = L.y + vy * s + GRAV * s * s / 2;
    if (py > SURF) break;
    x.fillRect(Math.round(px), Math.round(py), 2, 2);
  }
  if (!a.ghost) {
    x.fillStyle = "#0a0908"; x.fillRect(4, SURF - 60, 6, 32);
    x.fillStyle = "#ffb000"; const f = Math.round(a.power * 30); x.fillRect(5, SURF - 29 - f, 4, f);
  }
}

import { sfx, chime, audioClock, croakAt } from "./sound.js";
import { cells, colours, W as FW, H as FH } from "./frog.js";
import { draw as drawFrog } from "./lit.js";
import { feel } from "./feel.js";
import { reduced } from "./api.js";
import { PENTA } from "./drop.js";
import {
  W, H, CX, BR, BALLS, PAY, LANE, MOUTH, FROG, PADS, FLIES, SLINGS, POSTS, WALLS,
  world, run, flipper,
} from "./pinworld.js";

// Lily pinball. The frog works both flippers and a held half of the field
// takes that flipper over until it's let go. Three balls a game.
const SLOW = 0.45, LAMP = "#ffcf6a", CREAM = "#fff6dc", SURF = "#7fd6c2";
const WATER = ["#123a3d", "#10333a", "#0e2c34", "#0c252e", "#0a1f28"], BANK = "#16261c", DRAIN = "#06121c";

let G = null, col = null, spr = null, eyes = null, mouthAt = null;
const F = feel();

// Where the frog's pupils and mouth sit in its sprite, so its eyes can follow
// the ball and its mouth can open without changing frog.js.
function study(c) {
  const e = [[], []];
  let my = 0, mx0 = FW, mx1 = 0, n = 0;
  for (let y = 0; y < FH; y++) for (let x = 0; x < FW; x++) {
    const v = c[y * FW + x];
    if (v === 4 || v === 5) e[x < FW / 2 ? 0 : 1].push([x, y, v]);
    if (v === 6) { my += y; n++; mx0 = Math.min(mx0, x); mx1 = Math.max(mx1, x); }
  }
  eyes = e;
  mouthAt = n ? { y: Math.round(my / n), x0: mx0, x1: mx1 } : { y: 24, x0: 17, x1: 27 };
}

// The sprite with its pupils moved a pixel toward where it's looking.
function looking(dx, dy) {
  const c = spr.slice();
  for (const eye of eyes) {
    const has = new Set(eye.map(([x, y]) => x + "," + y));
    for (const [x, y, v] of eye) if (v === 5) c[y * FW + x] = 4;
    for (const [x, y, v] of eye) {
      if (v !== 5) continue;
      const tx = x + dx, ty = y + dy;
      c[(has.has(tx + "," + ty) ? ty * FW + tx : y * FW + x)] = 5;
    }
  }
  return c;
}

function croak(p) {
  const ac = audioClock();
  if (ac) croakAt(p, ac.currentTime + 0.02, 0.14);
}

function holding() {
  const h = [G.key[0], G.key[1]];
  for (const s of G.ptr.values()) h[s] = true;
  return h;
}

function heard(e) {
  const w = G.w;
  if (e.k === "pad") {
    G.flash[e.i] = 0.15; G.streak++;
    G.rings.push({ x: e.x, y: e.y, t: 0, r: PADS[e.i].r });
    sfx("boing", G.streak); F.freeze(2);
    F.burst(e.x, e.y, LAMP, 4);
    return;
  }
  if (e.k === "sling") {
    G.sling[e.i] = 0.15;
    sfx("twang"); F.freeze(1); F.burst(e.x, e.y, "#a8743a", 3);
    return;
  }
  if (e.k === "fly") {
    G.fly[e.i] = 0.5;
    chime(PENTA[2 + e.n * 2], 0.04);
    F.burst(e.x, e.y, LAMP, 6); F.pop(e.x, e.y - 8, "+" + PAY.fly, LAMP);
    return;
  }
  if (e.k === "set") {
    sfx("perk"); F.hit(0.25);
    F.pop(CX, 92, "ALL LIT +" + PAY.set, LAMP);
    if (e.multi) { G.banner = { s: "MULTIBALL", t: 1.6 }; G.multis++; }
    FLIES.forEach((f, i) => { G.fly[i] = 0.8; F.burst(f.x, f.y, LAMP, 4); });
    return;
  }
  if (e.k === "drop" || e.k === "spit") {
    G.gape = 0.3;
    if (e.k === "drop") { F.freeze(4); F.hit(0.3); F.burst(e.x, e.y, "#9fd27a", 14); sfx("pop"); croak(260); }
    else { sfx("pop"); F.burst(e.x, e.y, "#9fd27a", 6); }
    return;
  }
  if (e.k === "gulp") {
    G.gulp = 0.8; G.mouths++;
    F.got(e.x, e.y + 6, "GULP +" + PAY.mouth, { sound: "gulp", big: true, c: LAMP });
    F.freeze(6); F.squash(1); croak(180);
    return;
  }
  if (e.k === "drain") {
    sfx("bonk"); F.hit(0.3); F.burst(Math.max(20, Math.min(170, e.x)), H - 8, "#e2567a", 6);
    if (e.close) F.close(Math.max(30, Math.min(160, e.x)), 250);
    return;
  }
  if (e.k === "saved") { G.banner = { s: "THE FROG CAUGHT IT", t: 1.5 }; sfx("land"); return; }
  if (e.k === "plunge") { sfx("cast"); G.spring = 0.2; return; }
  if (e.k === "next") { G.streak = 0; G.banner = { s: "BALL " + (BALLS - w.left + 1), t: 1.1 }; return; }
  if (e.k === "flip") { sfx("flip"); if (e.mine) G.touched = true; return; }
  if (e.k === "knock") { if (G.t - G.knockAt > 0.08 && e.v > 220) { G.knockAt = G.t; sfx("kick"); } return; }
  if (e.k === "over") { G.st = "over"; G.st_t = 0; }
}

// A ball heading into the frog's mouth is the thing everybody's watching.
function watching() {
  if (G.w.held) return false;
  return G.w.balls.some((b) => {
    const dx = MOUTH.x - b.x, dy = MOUTH.y - b.y, d = Math.hypot(dx, dy);
    return d < 34 && d > 4 && (b.vx * dx + b.vy * dy) / d > 120;
  });
}

export const pin = {
  key: "pin", W, H, title: "LILY PINBALL", feel: F, edge: [BANK, DRAIN],
  aria: "The frog plays the flippers. Hold the left or right half, or the arrow keys, to take that flipper over until you let go.",
  help: ["THE FROG PLAYS THE FLIPPERS", "HOLD A SIDE TO TAKE ONE OVER", "LIGHT ALL FIVE FIREFLIES FOR MULTIBALL", "SINK ONE IN THE FROG'S MOUTH FOR " + PAY.mouth, "TAP TO START"],

  reset(g, p) {
    const stage = ["egg", "tadpole"].includes(p.frog.stage) ? "froglet" : p.frog.stage;
    spr = cells(g, stage, { hat: p.frog.hat.shape });
    col = colours(g, p.frog.hat);
    study(spr);
    G = {
      st: "play", st_t: 0, t: 0, end: 0, w: world(), ptr: new Map(), key: [false, false], touched: false,
      flash: PADS.map(() => 0), sling: [0, 0], fly: FLIES.map(() => 0), rings: [], streak: 0, knockAt: 0,
      gulp: 0, gape: 0, spring: 0, banner: null, slow: 1, mouths: 0, multis: 0, trail: new Map(),
    };
  },

  demo() { return G && !G.touched && G.st === "play" ? { x: 40, y: 250, hold: true } : null; },

  press(x, y, id) { if (G) { G.ptr.set(id === undefined ? "m" : id, x < CX ? 0 : 1); G.touched = true; } },
  release(id) {
    if (!G) return;
    if (id === undefined) G.ptr.clear();
    else G.ptr.delete(id);
  },
  keys(code, down) {
    if (!G) return;
    if (code === "ArrowLeft" || code === "KeyA") G.key[0] = down;
    if (code === "ArrowRight" || code === "KeyD") G.key[1] = down;
  },
  pause() { if (G) { G.ptr.clear(); G.key = [false, false]; } },

  step(dt) {
    const w = G.w;
    G.t += dt; G.st_t += dt;
    G.slow += ((watching() ? SLOW : 1) - G.slow) * Math.min(1, dt * 10);
    for (const k of ["flash", "sling", "fly"]) G[k] = G[k].map((v) => Math.max(0, v - dt));
    G.gulp = Math.max(0, G.gulp - dt); G.gape = Math.max(0, G.gape - dt); G.spring = Math.max(0, G.spring - dt);
    if (G.banner && (G.banner.t -= dt) <= 0) G.banner = null;
    G.rings.forEach((r) => { r.t += dt; });
    G.rings = G.rings.filter((r) => r.t < 0.7);
    if (G.st === "over") { if (G.st_t > 1.4) G.end = 1; return; }
    run(w, dt * G.slow, holding());
    w.events.forEach(heard);
    w.events.length = 0;
    const seen = new Set();
    for (const b of w.balls) {
      seen.add(b.id);
      const tr = G.trail.get(b.id) || [];
      tr.unshift([b.x, b.y]);
      if (tr.length > 4) tr.pop();
      G.trail.set(b.id, tr);
    }
    for (const id of G.trail.keys()) if (!seen.has(id)) G.trail.delete(id);
  },

  over() { return G && G.end; },
  score() { return G ? G.w.score : 0; },
  why() { return G ? "ALL THREE BALLS GONE" : ""; },
  report() { const c = G.w.counts; return { counts: { lights: c.lights, mouth: c.mouth, multi: c.multi, long: c.long } }; },
  tally() {
    if (!G) return [];
    const c = G.w.counts, l = [];
    if (c.mouth) l.push(c.mouth + " into the frog's mouth");
    if (c.multi) l.push(c.multi + (c.multi > 1 ? " multiballs" : " multiball"));
    l.push("Longest ball " + c.long + " seconds");
    return l;
  },

  draw(x) {
    table(x);
    if (!G) return;
    const w = G.w, z = reduced ? 0 : (1 - G.slow) / (1 - SLOW);
    x.save();
    if (z > 0.01) { x.translate(CX, MOUTH.y + 10); x.scale(1 + 0.08 * z, 1 + 0.08 * z); x.translate(-CX, -MOUTH.y - 10); }
    rings(x);
    lilySave(x, w.saveT, w.saved);
    FLIES.forEach((f, i) => firefly(x, f.x, f.y, w.lit[i] || G.fly[i] > 0, G.fly[i], G.t + i * 0.7));
    PADS.forEach((p, i) => pad(x, p, G.flash[i]));
    SLINGS.forEach((q, i) => cattail(x, q, i, G.sling[i]));
    POSTS.forEach((p) => post(x, p.x, p.y));
    plunger(x, w);
    const h = holding();
    for (let i = 0; i < 2; i++) leaf(x, flipper(w, i), h[i]);
    const b = w.balls.filter((o) => !o.lane).sort((a, c) => a.y - c.y)[0] || w.balls[0];
    const look = b ? [Math.sign(Math.round((b.x - CX) / 20)), b.y > 60 ? 1 : 0] : [0, 1];
    F.squashed(x, CX, 40, () => frog(x, CX, look[0], look[1], !w.held));
    for (const o of w.balls) pebble(x, o, G.trail.get(o.id));
    x.restore();
    F.draw(x);
    hud(x, w);
  },
};

// The pond at night with the bank round it, the lane up the right and the
// darker water of the drain under the flippers.
function table(x) {
  x.fillStyle = BANK; x.fillRect(0, 0, W, H);
  x.save();
  x.beginPath(); x.moveTo(6, H); x.lineTo(6, 54);
  [[14, 32], [28, 16], [48, 8], [140, 8], [164, 12], [182, 24], [193, 40], [197, 58], [197, H]].forEach(([a, b]) => x.lineTo(a, b));
  x.closePath(); x.clip();
  WATER.forEach((c, i) => { x.fillStyle = c; x.fillRect(0, i * 64, W, 64); });
  x.fillStyle = DRAIN; x.fillRect(0, 290, W, 30);
  x.fillStyle = "#081822"; x.fillRect(LANE.x0, LANE.gate[3], LANE.x1 - LANE.x0, LANE.floor - LANE.gate[3]);
  const t = G ? G.t : 0;
  x.fillStyle = "rgba(127,214,194,0.18)";
  for (let i = 0; i < 9; i++) {
    const yy = 30 + i * 30, xx = ((i * 53 + Math.round(t * 4)) % 170) + 10;
    x.fillRect(xx, yy, 7, 1); x.fillRect(xx + 2, yy + 1, 3, 1);
  }
  x.restore();
  // The bank's edge, the outlane bumps and the cattails' backs.
  x.fillStyle = BANK;
  SLINGS.forEach((q, i) => {
    const m = i ? -1 : 1, bx = i ? 2 * CX - 52 : 52;
    x.beginPath(); x.moveTo(q[0], q[1]); x.lineTo(q[2], q[3]); x.lineTo(bx, 262); x.lineTo(bx + 2 * m, 268); x.lineTo(q[0] - m, q[1]); x.fill();
  });
  x.strokeStyle = "#3a5a2c"; x.lineWidth = 2; x.lineCap = "round";
  WALLS.forEach((q) => { x.beginPath(); x.moveTo(q[0], q[1]); x.lineTo(q[2], q[3]); x.stroke(); });
  x.strokeStyle = "#5a7a3a"; x.lineWidth = 1;
  x.beginPath(); x.moveTo(LANE.gate[0], LANE.gate[1]); x.lineTo(LANE.gate[2], LANE.gate[3]); x.stroke();
  // Reeds along the bank.
  x.fillStyle = "#2f5a2a";
  for (let y = 60; y < 300; y += 9) { x.fillRect(2, y, 1, 5); x.fillRect(4, y + 4, 1, 4); }
  for (let i = 0; i < 26; i += 1) { const xx = 30 + i * 5; x.fillRect(xx, 2 + (i % 3), 1, 4); }
}

function disc(x, cx, cy, r, c) {
  x.fillStyle = c;
  for (let y = -r; y <= r; y++) { const hw = Math.round(Math.sqrt(Math.max(0, r * r - y * y))); x.fillRect(cx - hw, cy + y, hw * 2, 1); }
}

// A lily pad from above with its notch and a vein, lamp yellow round the
// edge for a moment when it's hit.
function pad(x, p, flash) {
  const cx = Math.round(p.x), cy = Math.round(p.y);
  if (flash > 0) disc(x, cx, cy, p.r + 2, LAMP);
  disc(x, cx, cy + 1, p.r, "#24502c");
  disc(x, cx, cy, p.r, flash > 0.08 ? "#a8e8a0" : "#3f8a4a");
  disc(x, cx - 1, cy - 1, p.r - 3, flash > 0.08 ? "#d8f5c8" : "#56b45f");
  x.fillStyle = WATER[2];
  for (let i = 0; i < p.r; i++) x.fillRect(cx + Math.round(i * 0.35) - Math.round(i * 0.18), cy + i, Math.max(1, Math.round(i * 0.36)), 1);
  x.fillStyle = "#3f8a4a";
  x.fillRect(cx - 6, cy - 1, 5, 1); x.fillRect(cx + 1, cy - 4, 1, 4);
  x.fillStyle = "#e2567a"; x.fillRect(cx - 5, cy - 6, 2, 2);
  x.fillStyle = "#f6b8cc"; x.fillRect(cx - 5, cy - 6, 1, 1);
}

// Rings spread on the water under a pad that was hit.
function rings(x) {
  G.rings.forEach((r) => {
    const s = Math.round(r.r + 2 + r.t * 28);
    x.globalAlpha = Math.max(0, 0.6 - r.t);
    x.strokeStyle = SURF; x.lineWidth = 1;
    x.beginPath(); x.ellipse(r.x, r.y + 2, s, s * 0.7, 0, 0, Math.PI * 2); x.stroke();
  });
  x.globalAlpha = 1;
}

// A firefly, dim until a ball rolls over it and glowing after, with a twinkle.
function firefly(x, cx, cy, lit, pop, t) {
  if (lit) {
    const g = 0.2 + 0.08 * Math.sin(t * 6) + pop * 0.4;
    disc(x, cx, cy, 6 + Math.round(pop * 6), "rgba(255,207,106," + g.toFixed(2) + ")");
    disc(x, cx, cy + 1, 3, "rgba(255,226,122," + (g + 0.25).toFixed(2) + ")");
  }
  x.fillStyle = lit ? "#e9f0f4" : "#6d7a88"; x.fillRect(cx - 3, cy - 2, 2, 1); x.fillRect(cx + 2, cy - 2, 2, 1);
  x.fillStyle = "#3a2e22"; x.fillRect(cx - 1, cy - 3, 3, 2);
  x.fillStyle = lit ? (Math.sin(t * 9) > -0.6 ? "#fff6d0" : LAMP) : "#5a4a38"; x.fillRect(cx - 1, cy - 1, 3, 3);
}

// A cattail's stalk along each slingshot's face, its brown head at the top,
// bowing in and lamp yellow when it kicks.
function cattail(x, q, i, flash) {
  const [ax, ay, bx, by] = q, bow = flash > 0 ? 2 : 0, nx = i ? -1 : 1;
  const n = Math.ceil(Math.hypot(bx - ax, by - ay));
  for (let k = 0; k <= n; k++) {
    const t = k / n, push = Math.sin(t * Math.PI) * bow;
    const px = Math.round(ax + (bx - ax) * t - nx * push), py = Math.round(ay + (by - ay) * t);
    x.fillStyle = flash > 0 ? LAMP : "#4f9a4a"; x.fillRect(px, py, 2, 1);
    x.fillStyle = flash > 0 ? CREAM : "#6fd08c"; x.fillRect(px + (i ? 1 : 0), py, 1, 1);
  }
  x.fillStyle = "#6a4428"; x.fillRect(ax - 2, ay - 9, 4, 8);
  x.fillStyle = "#8a5a32"; x.fillRect(ax - 1, ay - 9, 2, 6);
  x.fillStyle = "#4f9a4a"; x.fillRect(ax, ay - 12, 1, 3);
}

function post(x, cx, cy) {
  disc(x, cx, cy, 3, "#5a4630"); x.fillStyle = "#8a7050"; x.fillRect(cx - 1, cy - 2, 2, 1);
}

// A flipper is a long reed leaf, cream while the frog has it and lamp yellow
// while it's held.
function leaf(x, f, held) {
  const [ax, ay, bx, by] = f, dx = bx - ax, dy = by - ay, l2 = dx * dx + dy * dy;
  const x0 = Math.floor(Math.min(ax, bx) - 5), x1 = Math.ceil(Math.max(ax, bx) + 5), y0 = Math.floor(Math.min(ay, by) - 5), y1 = Math.ceil(Math.max(ay, by) + 5);
  const fill = held ? LAMP : "#cfe8a0", top = held ? CREAM : "#efffd8", low = held ? "#c98a2a" : "#7fae5a";
  for (let y = y0; y <= y1; y++) for (let xx = x0; xx <= x1; xx++) {
    const px = xx + 0.5, py = y + 0.5;
    const t = Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / l2));
    const d = Math.hypot(px - ax - dx * t, py - ay - dy * t), r = 4 - t * 1.6;
    if (d > r + 1) continue;
    if (d > r) { x.fillStyle = "#0a0d16"; x.fillRect(xx, y, 1, 1); continue; }
    const side = (px - ax - dx * t) * -dy + (py - ay - dy * t) * dx;
    x.fillStyle = d > r - 1 ? (side > 0 ? low : top) : fill;
    x.fillRect(xx, y, 1, 1);
  }
  disc(x, Math.round(ax), Math.round(ay), 2, held ? "#c98a2a" : "#5a7a3a");
}

// A pebble with a pixel of shine and a short smear behind it when it's fast.
function pebble(x, b, tr) {
  if (tr && !reduced && Math.hypot(b.vx, b.vy) > 300) {
    tr.slice(1).forEach(([tx, ty], i) => {
      x.globalAlpha = 0.3 - i * 0.09;
      disc(x, Math.round(tx), Math.round(ty), BR - 2 - i, SURF);
    });
    x.globalAlpha = 1;
  }
  const cx = Math.round(b.x), cy = Math.round(b.y);
  disc(x, cx, cy, BR, "#0a0d16");
  disc(x, cx, cy, BR - 1, "#7f8a99");
  disc(x, cx - 1, cy - 1, BR - 2, "#b9c3cf");
  x.fillStyle = "#ffffff"; x.fillRect(cx - 2, cy - 3, 1, 1);
}

// The ball save is a lily pad across the drain, sinking as its time runs out.
function lilySave(x, left, used) {
  if (used || left <= 0) return;
  if (left < 1.5 && Math.floor(left * 8) % 2) return;
  const sink = left < 1 ? Math.round((1 - left) * 4) : 0, y = 298 + sink;
  x.globalAlpha = left < 1 ? left : 1;
  x.fillStyle = "#24502c"; x.fillRect(CX - 26, y + 1, 52, 4);
  x.fillStyle = "#3f8a4a"; x.fillRect(CX - 28, y - 1, 56, 4); x.fillRect(CX - 24, y - 2, 48, 1);
  x.fillStyle = DRAIN; x.fillRect(CX + 8, y - 2, 4, 3);
  x.globalAlpha = 1;
}

// The frog plunges with a coiled reed at the bottom of the lane.
function plunger(x, w) {
  const b = w.balls.find((o) => o.lane && o.wait > 0), pull = b ? Math.round(Math.min(1, 1 - b.wait / 0.8) * 5) : 0;
  const top = LANE.floor + 1 + pull - (G.spring > 0 ? 3 : 0), mx = Math.round((LANE.x0 + LANE.x1) / 2);
  x.fillStyle = "#8a5a32"; x.fillRect(mx - 5, top, 11, 2);
  x.fillStyle = "#4f9a4a";
  for (let y = top + 3; y < H; y += 3) x.fillRect(mx - 3 + ((y / 3) % 2 ? 1 : 0), y, 6, 1);
}

// The player's own frog at the top on its pad, its mouth open for the ball
// and its eyes on the nearest one. It shuts its mouth while it gulps.
function frog(x, cx, lx, ly, open) {
  const ox = cx - FW / 2, oy = FROG.y - 18;
  x.fillStyle = "#24502c"; x.beginPath(); x.ellipse(cx, FROG.y + 2, 24, 15, 0, 0, Math.PI * 2); x.fill();
  x.fillStyle = "#3f8a4a"; x.beginPath(); x.ellipse(cx, FROG.y + 1, 23, 14, 0, 0, Math.PI * 2); x.fill();
  drawFrog(x, G ? looking(lx, ly) : spr, FW, FH, col, ox, oy, 1);
  const m = mouthAt, mw = m.x1 - m.x0 + 1, gape = G && G.gape > 0 ? 2 : 0;
  if (open) {
    x.fillStyle = "#2a0a14"; x.fillRect(ox + m.x0, oy + m.y, mw, 3 + gape); x.fillRect(ox + m.x0 + 1, oy + m.y + 3 + gape, mw - 2, 1);
    x.fillStyle = "#e2567a"; x.fillRect(ox + m.x0 + 2, oy + m.y + 2 + gape, mw - 4, 1);
  } else if (G && G.gulp > 0) {
    const puff = Math.round(Math.sin(G.gulp * 18) + 1);
    x.fillStyle = col[2]; x.fillRect(ox + m.x0 - 2 - puff, oy + m.y - 2, 3 + puff, 3); x.fillRect(ox + m.x1, oy + m.y - 2, 3 + puff, 3);
  }
}

function hud(x, w) {
  x.font = "700 13px Inter, sans-serif"; x.textAlign = "left"; x.lineWidth = 3; x.strokeStyle = "#0a0908";
  x.strokeText(String(w.score), 6, 16); x.fillStyle = "#ece5d8"; x.fillText(String(w.score), 6, 16);
  const waiting = w.left - 1 + (w.balls.some((b) => b.lane && b.wait > 0) ? 1 : 0);
  for (let i = 0; i < Math.max(0, waiting); i++) { disc(x, W - 8 - i * 9, 7, 3, "#0a0d16"); disc(x, W - 8 - i * 9, 7, 2, "#b9c3cf"); }
  if (w.saveT > 0 && !w.saved) {
    x.font = "700 9px Inter, sans-serif"; x.textAlign = "center"; x.fillStyle = SURF;
    x.fillText("BALL SAVE", CX, 319);
  }
  if (G.banner) {
    x.font = "700 13px Inter, sans-serif"; x.textAlign = "center";
    const k = Math.min(1, G.banner.t / 0.25);
    x.globalAlpha = k;
    x.strokeText(G.banner.s, CX, 190); x.fillStyle = LAMP; x.fillText(G.banner.s, CX, 190);
    x.globalAlpha = 1;
  }
}

// The picture on the title card.
export function pinArt(x, g) {
  const c = cells(g, "frog", {});
  x.fillStyle = BANK; x.fillRect(0, 0, 168, 96);
  x.fillStyle = WATER[1]; x.fillRect(8, 4, 152, 92);
  x.fillStyle = DRAIN; x.fillRect(8, 86, 152, 10);
  [[50, 52, 9], [118, 52, 9], [84, 70, 9]].forEach(([px, py, r]) => pad(x, { x: px, y: py, r }, px === 118 ? 0.1 : 0));
  [24, 52, 84, 116, 144].forEach((fx, i) => firefly(x, fx, 32, i % 2 === 0, 0, i));
  leaf(x, [50, 84, 72, 92], false);
  leaf(x, [118, 84, 96, 92], true);
  drawFrog(x, c, FW, FH, colours(g, null), 84 - FW / 2, -10, 1);
  pebble(x, { x: 104, y: 60, vx: 0, vy: 0 });
}

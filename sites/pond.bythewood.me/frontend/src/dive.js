import { $, post, state, setPond, watch } from "./api.js";
import { genome, sideCells, draw as drawCells, SW, SH } from "./frog.js";

// Dive is played in a 320 by 200 field and drawn at twice that. The frog
// floats up by itself, a press kicks it down, and the numbers below are the
// feel of the game, so they change together or not at all.
const TOP = 20, OW = 16;
// The field takes its size from the window, see fit. PW is how wide it is,
// GH how deep, and FXP where the frog swims.
let PW = 266, FXP = 70, GH = 200, BOT = 186;
const BUOY = 330, RISE_MAX = 150, KICK = 138, GATE_EVERY = 118;
const MONO = "500 8px 'JetBrains Mono', monospace", BIG = "700 22px 'Space Grotesk', sans-serif", HEAD = "700 13px 'Space Grotesk', sans-serif";

let gc, gx, spr = null, sprKey = "", net = Promise.resolve();
const G = { state: "idle", score: 0, fy: 100, vy: 0, kick: 0, gates: [], bub: [], dist: 0, next: 0, last: 0, overAt: 0, rank: 0, t: 0, why: "", lastC: 100, flash: 0, paid: null, best: false };

function best() { return state.pond ? state.pond.dive.best : 0; }

function sprites() {
  const f = state.pond.frog, key = f.seed + f.stage;
  if (sprKey !== key) { sprKey = key; spr = [sideCells(state.g, f.stage, false), sideCells(state.g, f.stage, true)]; }
  return spr;
}

function hsl(h, s, l) { return "hsl(" + h.toFixed(0) + " " + Math.max(0, Math.min(100, s)).toFixed(0) + "% " + Math.max(0, Math.min(100, l)).toFixed(0) + "%)"; }

// A shade lighter than on the bank, so the frog reads against deep water.
function frogCols(g) {
  const m = g.morph;
  return { 1: hsl(g.hue, g.sat, g.lit + 4), 2: hsl(g.hue + 18, g.sat * 0.45, Math.min(92, g.lit + 32)), 3: hsl(g.hue - 8, g.sat, g.lit - 14), 4: m.name === "ALBINO" ? "#f6c9cc" : "#fff6dc", 5: "#141210", 6: hsl(g.hue, g.sat * 0.6, 14), 8: m.limb != null ? hsl(m.limb, 70, 50) : hsl(g.hue, g.sat, g.lit - 7), 9: hsl(g.hue, g.sat * 0.5, 9) };
}

// Ten other frogs swim every run, a little behind. Nobody is steering them.
// Each aims for the gaps, a little sloppily, and the server deals how many
// gates each will clear before it hits one, which is how it knows who placed.
const GHOSTS_IN = 10;
let others = [];

function ghostSprite(g, kick) {
  const cv = document.createElement("canvas");
  cv.width = SW; cv.height = SH;
  drawCells(cv.getContext("2d"), sideCells(g, state.pond.frog.stage, kick), SW, SH, frogCols(g), 0, 0, 1);
  return cv;
}

function spawnOthers() {
  others = [];
  for (let i = 0; i < GHOSTS_IN; i++) {
    const g = genome(1 + Math.floor(Math.random() * 4294967295));
    others.push({
      name: g.name, spr: [ghostSprite(g, false), ghostSprite(g, true)],
      x: FXP - 12 - i * Math.min(60, FXP - 24) / GHOSTS_IN - Math.random() * 3, y: TOP + 30 + Math.random() * (BOT - TOP - 60),
      vy: 0, kick: 0, passed: 0, clears: 99, aim: Math.random() * 22 - 8,
      out: false, fade: 1,
    });
  }
}

// Until the deal arrives a ghost clears everything, and the first gate is
// seconds away, so it always has. With no answer the page deals its own.
function deal(clears) {
  others.forEach((o, i) => { o.clears = clears && clears[i] != null ? clears[i] : Math.floor(Math.random() * 12); });
}

const ORD = ["", "1ST", "2ND", "3RD"];
function ordinal(n) { return ORD[n] || n + "TH"; }

function stepOthers(dt, sp) {
  others.forEach((o) => {
    if (o.out) { o.x -= sp * dt; o.y -= 18 * dt; o.fade -= dt * 1.4; return; }
    const gate = G.gates.find((g) => g.x + OW > o.x - 8);
    // One that has cleared all it's going to drifts up into the stems.
    let want = (TOP + BOT) / 2 + o.aim * 3;
    if (gate) want = o.passed >= o.clears ? gateC(gate) - gate.gap / 2 - 10 : gateC(gate) - 9 + o.aim;
    o.vy -= BUOY * dt; if (o.vy < -RISE_MAX) o.vy = -RISE_MAX;
    if (o.y < want && o.vy < 30) { o.vy = KICK; o.kick = 0.16; }
    o.y += o.vy * dt; o.kick -= dt;
    if (o.y > BOT - 6) { o.y = BOT - 6; o.vy = Math.min(0, o.vy); }
    if (!gate) return;
    if (gate.x + OW < o.x - 8 && gate.seen !== undefined && !gate.seen[o.name + o.x]) { gate.seen[o.name + o.x] = true; o.passed++; }
    const c = gateC(gate), hit = o.x + 7 > gate.x && o.x - 8 < gate.x + OW && (o.y - 4 < c - gate.gap / 2 || o.y + 5 > c + gate.gap / 2);
    if (hit || o.y < TOP + 5) {
      o.out = true;
      for (let i = 0; i < 4; i++) G.bub.push({ x: o.x - 6 + Math.random() * 12, y: o.y - 4 + Math.random() * 8, a: 0.9 });
    }
  });
}

function drawOthers() {
  gx.font = "500 6px 'JetBrains Mono', monospace"; gx.textAlign = "center"; gx.fillStyle = "#ece5d8";
  others.forEach((o) => {
    if (o.fade <= 0) return;
    gx.globalAlpha = 0.6 * o.fade;
    gx.drawImage(o.spr[o.kick > 0 ? 1 : 0], Math.round(o.x - 15), Math.round(o.y - 9));
    // On a phone they swim too close together for ten names to be read.
    if (PW >= 400) gx.fillText(o.name, Math.round(o.x), Math.round(o.y - 12));
  });
  gx.globalAlpha = 1;
  if (G.state === "idle") return;
  const still = others.filter((o) => !o.out).length;
  gx.textAlign = "left"; gx.font = MONO; gx.fillStyle = "#bfeee2";
  gx.fillText(still + " OF " + others.length + " STILL IN", 6, TOP + 12);
}

function reset() {
  G.fy = 96; G.vy = 0; G.kick = 0; G.gates = []; G.bub = []; G.dist = 0; G.next = 150; G.score = 0; G.cleared = 0; G.place = 0; G.bonus = 0; G.hat = null; G.had = false; G.t = 0; G.lastC = GH / 2; G.flash = 0; G.paid = null; G.best = false;
}

let started = null;

function die(now, why) {
  G.state = "over"; G.overAt = now; G.why = why;
  G.rank = others.filter((o) => o.out).length;
  const score = G.score, gates = G.cleared;
  // Requests go one after another, so a finish always lands before the start
  // of the run after it.
  net = started.then((run) => run ? post("/api/dive/finish", { run, score, gates }) : null).then((d) => {
    if (!d) { G.paid = 0; return; }
    G.paid = d.paid; G.best = d.best; G.place = d.place; G.bonus = d.bonus; G.hat = d.hat; G.had = d.had;
    setPond(d.pond);
  }, () => { G.paid = 0; }).then(() => { if (G.state === "over") draw(performance.now()); });
}

function press() {
  if (!state.pond) return;
  if (G.state !== "play") {
    if (G.state === "over" && performance.now() - G.overAt < 500) return;
    reset(); spawnOthers(); G.state = "play"; G.last = performance.now();
    started = net = net.then(() => post("/api/dive/start")).then((d) => { deal(d.ghosts); return d.run; }, () => { deal(null); return null; });
    requestAnimationFrame(step);
  }
  G.vy = KICK; G.kick = 0.16;
  for (let i = 0; i < 3; i++) G.bub.push({ x: FXP - 10 - Math.random() * 6, y: G.fy - 2 + Math.random() * 6, a: 1 });
}

function speed() { return 84 + Math.min(46, G.score * 1.6); }

function addGate() {
  const gap = Math.max(44, 60 - G.score * 0.6), lo = TOP + 16 + gap / 2, hi = BOT - 16 - gap / 2;
  let c = G.lastC + (Math.random() * 2 - 1) * 62;
  c = Math.max(lo, Math.min(hi, c)); G.lastC = c;
  G.gates.push({ x: PW + 10, c: c, gap: gap, bob: G.score > 12 ? 6 + Math.random() * 8 : 0, ph: Math.random() * 6, done: false, bug: Math.random() < 0.6 ? (Math.random() < 0.5 ? -1 : 1) : 0, seen: {} });
}

function gateC(g) { return g.c + (g.bob ? Math.sin(G.t * 1.7 + g.ph) * g.bob : 0); }

function step(now) {
  if (G.state !== "play") return;
  const dt = Math.min(0.04, (now - G.last) / 1000); G.last = now; G.t += dt;
  const sp = speed();
  G.vy -= BUOY * dt; if (G.vy < -RISE_MAX) G.vy = -RISE_MAX;
  G.fy += G.vy * dt; G.kick -= dt; G.flash -= dt;
  if (G.fy > BOT - 6) { G.fy = BOT - 6; G.vy = Math.min(0, G.vy); }
  G.dist += sp * dt; G.next -= sp * dt;
  if (G.next <= 0) { addGate(); G.next = GATE_EVERY; }
  let dead = "";
  if (G.fy < TOP + 5) dead = "THE HERON SAW YOU";
  for (let i = G.gates.length - 1; i >= 0; i--) {
    const g = G.gates[i]; g.x -= sp * dt;
    if (g.x + OW < -6) { G.gates.splice(i, 1); continue; }
    const c = gateC(g), topEnd = c - g.gap / 2, botStart = c + g.gap / 2;
    if (FXP + 7 > g.x && FXP - 8 < g.x + OW && (G.fy - 4 < topEnd || G.fy + 5 > botStart)) dead = G.fy - 4 < topEnd ? "TANGLED IN THE STEMS" : "BONKED A ROCK";
    if (g.bug && Math.abs(g.x + OW / 2 - FXP) < 9 && Math.abs(c + g.bug * (g.gap / 2 - 9) - G.fy) < 9) { g.bug = 0; G.score++; G.flash = 0.25; }
    if (!g.done && g.x + OW < FXP - 8) { g.done = true; G.score++; G.cleared++; }
  }
  for (let q = G.bub.length - 1; q >= 0; q--) { const b = G.bub[q]; b.x -= sp * dt; b.y -= 26 * dt; b.a -= dt * 1.5; if (b.a <= 0) G.bub.splice(q, 1); }
  stepOthers(dt, sp);
  if (dead) die(now, dead);
  draw(now);
  if (G.state === "play") requestAnimationFrame(step);
}

function px(x, y, w, h) { gx.fillRect(Math.round(x), Math.round(y), w, h); }

function draw(now) {
  if (!state.pond) return;
  gx.setTransform(2, 0, 0, 2, 0, 0);
  // Off, or a ghost drawn from its own small canvas comes out smeared.
  gx.imageSmoothingEnabled = false;
  const bands = ["#123a3d", "#103336", "#0e2c30", "#0c262a", "#0b2024", "#0a1b1f"];
  const bh = (BOT - TOP) / 6;
  for (let b = 0; b < 6; b++) { gx.fillStyle = bands[b]; gx.fillRect(0, Math.floor(TOP + b * bh), PW, Math.ceil(bh) + 1); }
  // The sky strip, where the heron's legs wade past slowly.
  gx.fillStyle = "#1c2a33"; gx.fillRect(0, 0, PW, TOP);
  const hx = ((PW + 60 - (G.dist * 0.5) % (PW + 120)) + PW + 120) % (PW + 120) - 30;
  gx.fillStyle = "#d9c9a8"; px(hx, 4, 2, 44); px(hx + 9, 8, 2, 36); gx.fillStyle = "#e9e3d6"; px(hx - 4, 0, 22, 6);
  gx.fillStyle = "#7fd6c2"; const so = G.dist % 16;
  for (let x = -so; x < PW; x += 16) { px(x, TOP, 9, 1); px(x + 9, TOP + 1, 7, 1); }
  gx.fillStyle = "rgba(160,230,210,0.045)";
  for (let s = 0; s < 4; s++) { const sx = ((s * 83 - G.dist * 0.25) % (PW + 40) + PW + 40) % (PW + 40) - 20; gx.beginPath(); gx.moveTo(sx, TOP); gx.lineTo(sx + 14, TOP); gx.lineTo(sx - 16, BOT); gx.lineTo(sx - 36, BOT); gx.fill(); }
  gx.fillStyle = "#3b3226"; gx.fillRect(0, BOT, PW, GH - BOT);
  gx.fillStyle = "#5a4a34"; const fo = G.dist % 20; for (let f = -fo; f < PW; f += 20) { px(f, BOT, 8, 2); px(f + 11, BOT + 5, 5, 2); }
  // Gates are lily stems down from a pad and stones stacked up from the mud.
  G.gates.forEach((g) => {
    const c = gateC(g), topEnd = c - g.gap / 2, botStart = c + g.gap / 2, x0 = Math.round(g.x);
    gx.fillStyle = "#2f7a3f"; px(x0 + 2, TOP, 3, topEnd - TOP - 6); px(x0 + 11, TOP, 3, topEnd - TOP - 6);
    gx.fillStyle = "#49a557"; px(x0 + 6, TOP, 4, topEnd - TOP - 4);
    gx.fillStyle = "#6cc76a"; px(x0 + 7, TOP, 1, topEnd - TOP - 6);
    gx.fillStyle = "#e27aa5"; px(x0 + 1, topEnd - 9, OW - 2, 6); px(x0 + 3, topEnd - 3, OW - 6, 3);
    gx.fillStyle = "#f3a9c6"; px(x0 + 3, topEnd - 8, 4, 2); px(x0 + 6, topEnd - 2, 3, 1);
    gx.fillStyle = "#58b85a"; px(x0 - 5, TOP - 2, OW + 10, 3); gx.fillStyle = "#7fd87a"; px(x0 - 3, TOP - 2, 8, 1);
    for (let y = botStart, k = 0; y < BOT; y += 11, k++) {
      gx.fillStyle = k % 2 ? "#6f7a80" : "#8a949a"; px(x0 + (k % 2), y, OW - (k % 2) * 2, 10);
      gx.fillStyle = k % 2 ? "#8b969c" : "#a9b3b8"; px(x0 + 2 + (k % 2), y + 1, 6, 2);
      gx.fillStyle = "#4f585e"; px(x0 + (k % 2), y + 9, OW - (k % 2) * 2, 1);
    }
    gx.fillStyle = "#58b85a"; px(x0, botStart - 2, OW, 3); gx.fillStyle = "#7fd87a"; px(x0 + 2, botStart - 2, 5, 1);
    if (g.bug) {
      const by = c + g.bug * (g.gap / 2 - 9), bx = x0 + OW / 2, fl = Math.floor(now / 80) % 2;
      gx.fillStyle = "#ffe27a"; px(bx - 1, by - 1, 3, 3); gx.fillStyle = "#fff6dc";
      if (fl) { px(bx - 3, by - 3, 2, 2); px(bx + 2, by - 3, 2, 2); } else { px(bx - 4, by - 1, 2, 2); px(bx + 3, by - 1, 2, 2); }
    }
  });
  gx.fillStyle = "#bfeee2"; G.bub.forEach((bb) => { gx.globalAlpha = Math.max(0, bb.a) * 0.8; px(bb.x, bb.y, 2, 2); }); gx.globalAlpha = 1;
  drawOthers();
  const show = G.state !== "over" || now - G.overAt > 500 || Math.floor((now - G.overAt) / 80) % 2 === 0;
  if (show) drawCells(gx, sprites()[G.kick > 0 ? 1 : 0], SW, SH, frogCols(state.g), Math.round(FXP - 15), Math.round(G.fy - 9), 1);
  gx.textAlign = "center"; gx.font = BIG;
  gx.fillStyle = "#0a1b1f"; gx.fillText(G.score, PW / 2 + 1, 48); gx.fillStyle = G.flash > 0 ? "#ffe27a" : "#fff6dc"; gx.fillText(G.score, PW / 2, 47);
  overlay();
  if (G.state === "over" && now - G.overAt < 600) requestAnimationFrame(draw);
}

// What the end card says about the podium: the place, and what it added.
function placeLine() {
  const place = G.place || 1 + others.filter((o) => !o.out).length;
  let line = ordinal(place) + " OF " + (others.length + 1);
  if (G.paid == null || place > 3) return line;
  if (G.had) return line + ", THAT PRIZE IS WON FOR TODAY";
  if (G.bonus) line += ", +" + G.bonus + " PEBBLES" + (G.hat ? " AND A HAT" : "");
  return line;
}

function overlay() {
  if (G.state === "play") return;
  const oy = Math.round(GH * 0.31), tall = G.state === "over" && G.hat ? 84 : 72;
  const ow = Math.min(PW - 20, 260), ox = Math.round((PW - ow) / 2);
  gx.fillStyle = "rgba(10,9,8,0.82)"; gx.fillRect(ox, oy, ow, tall);
  gx.fillStyle = "#ffb000"; gx.fillRect(ox, oy, 2, tall);
  gx.textAlign = "center"; gx.fillStyle = "#ece5d8"; gx.font = HEAD;
  if (G.state === "idle") {
    gx.fillText("TAP TO KICK DOWN", PW / 2, oy + 24);
    gx.fillStyle = "#bcaf99"; gx.font = MONO;
    gx.fillText("THE FROG FLOATS UP BY ITSELF", PW / 2, oy + 40); gx.fillText("OUTLAST THE OTHERS FOR A PRIZE", PW / 2, oy + 54);
    return;
  }
  gx.fillText(G.why, PW / 2, oy + 22);
  gx.fillStyle = "#bcaf99"; gx.font = MONO;
  const pay = G.paid == null ? "COUNTING PEBBLES" : "+" + G.paid + (G.paid === 1 ? " PEBBLE" : " PEBBLES") + (G.best ? ", A NEW BEST" : "");
  gx.fillText(G.score + (G.score === 1 ? " POINT, " : " POINTS, ") + pay, PW / 2, oy + 36);
  gx.fillStyle = (G.place || 9) <= 3 ? "#ffe27a" : "#bcaf99";
  gx.fillText(placeLine(), PW / 2, oy + 48);
  let y = oy + 62;
  if (G.hat) { gx.fillText(G.hat.hat_name.toUpperCase() + (G.hat.spare ? ", A SPARE" : ", NEW"), PW / 2, oy + 60); y += 12; }
  gx.fillStyle = "#ffb000"; gx.fillText("TAP TO GO AGAIN", PW / 2, y);
}

// The strip under the water: the best run, pebbles earned against the day's
// cap, and what a point is worth on the next run.
function note(p) {
  const d = p.dive;
  $("dive-best").textContent = d.best;
  $("dive-today").textContent = d.paid + "/" + d.cap;
  $("dive-pays").textContent = d.paid >= d.cap ? "0" : d.runs < d.full ? "1" : "1/4";
}

export function initDive() {
  gc = $("dive"); gx = gc.getContext("2d");
  reset();
  // The way out is a button, so a press on it must not also be a kick.
  $("dive-exit").addEventListener("pointerdown", (e) => e.stopPropagation());
  $("dive-exit").addEventListener("click", () => document.querySelector("#tabs [data-view=pond]").click());
  watch((p) => { note(p); if (G.state !== "play" && !$("v-dive").hidden) draw(performance.now()); });
  $("v-dive").addEventListener("pointerdown", (e) => { e.preventDefault(); press(); });
  $("v-dive").addEventListener("contextmenu", (e) => e.preventDefault());
  document.addEventListener("keydown", (e) => {
    if ($("v-dive").hidden) return;
    if (e.code === "Space" || e.key === " " || e.key === "Enter" || e.key === "ArrowDown") { e.preventDefault(); if (!e.repeat) press(); }
  });
  if (document.fonts && document.fonts.ready) document.fonts.ready.then(() => { if (G.state !== "play" && !$("v-dive").hidden) draw(performance.now()); });
}

// Dive takes the whole window. A tall phone sets the scale by its width and
// gets a deep pond, a wide desktop sets it by its height and gets a long one.
// The strip left under the water holds the readout and the way out.
function fit() {
  $("v-dive").classList.add("full");
  const w = window.innerWidth, h = window.innerHeight - 76, scale = Math.min(w / 266, h / 300);
  PW = Math.min(640, Math.floor(w / scale));
  GH = Math.max(200, Math.min(520, Math.floor(h / scale)));
  BOT = GH - 14;
  FXP = Math.max(70, Math.round(PW * 0.22));
  gc.width = PW * 2; gc.height = GH * 2;
  gc.style.width = Math.round(PW * scale) + "px";
  reset();
}

export function enterDive() { if (G.state !== "play") fit(); draw(performance.now()); }

// Leaving the view mid run ends it, so the points still count.
export function leaveDive() {
  if (G.state === "play") die(performance.now(), "CLIMBED OUT");
  $("v-dive").classList.remove("full");
}

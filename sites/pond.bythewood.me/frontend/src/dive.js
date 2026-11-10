import { $, post, state, setPond, watch } from "./api.js";
import { sideCells, draw as drawCells, SW, SH } from "./frog.js";

// Dive is played in a 320 by 200 field and drawn at twice that. The frog
// floats up by itself, a press kicks it down, and the numbers below are the
// feel of the game, so they change together or not at all.
const GW = 320, GH = 200, TOP = 20, BOT = 186, FXP = 70, OW = 16, RAILW = 54;
const BUOY = 330, RISE_MAX = 150, KICK = 138, GATE_EVERY = 118;
const GN = ["DOT", "PIP", "MOSS", "BEAN", "WREN", "TULLY", "FENN", "BRINDLE"];
const GF = [0.3, 0.46, 0.62, 0.78, 0.94, 1.1, 1.3, 1.62];
const FONT = "'Silkscreen', monospace";

let rail = true, gc, gx, GHOSTS = [], RMAX = 20, spr = null, sprKey = "", net = Promise.resolve();
const G = { state: "idle", score: 0, fy: 100, vy: 0, kick: 0, gates: [], bub: [], dist: 0, next: 0, last: 0, overAt: 0, rank: 0, t: 0, why: "", lastC: 100, flash: 0, paid: null, best: false };

function best() { return state.pond ? state.pond.dive.best : 0; }

// The neighbours sit around the player's best, with a floor so a first run
// has somebody to chase.
function tune() {
  const b = Math.max(8, best());
  GHOSTS = GF.map((f, i) => Math.round(b * f + (i % 3)));
  RMAX = Math.max(14, Math.round(GHOSTS[GHOSTS.length - 1] * 1.08));
}

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

function reset() {
  G.fy = 96; G.vy = 0; G.kick = 0; G.gates = []; G.bub = []; G.dist = 0; G.next = 150; G.score = 0; G.t = 0; G.lastC = 100; G.flash = 0; G.paid = null; G.best = false;
}

let started = null;

function die(now, why) {
  G.state = "over"; G.overAt = now; G.why = why;
  G.rank = GHOSTS.filter((s) => G.score > s).length;
  const score = G.score;
  // Requests go one after another, so a finish always lands before the start
  // of the run after it.
  net = started.then((run) => run ? post("/api/dive/finish", { run, score }) : null).then((d) => {
    if (!d) { G.paid = 0; return; }
    G.paid = d.paid; G.best = d.best;
    setPond(d.pond);
    tune();
  }, () => { G.paid = 0; }).then(() => { if (G.state === "over") draw(performance.now()); });
}

function press() {
  if (!state.pond) return;
  if (G.state !== "play") {
    if (G.state === "over" && performance.now() - G.overAt < 500) return;
    reset(); G.state = "play"; G.last = performance.now();
    started = net = net.then(() => post("/api/dive/start")).then((d) => d.run, () => null);
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
  G.gates.push({ x: GW + 10, c: c, gap: gap, bob: G.score > 12 ? 6 + Math.random() * 8 : 0, ph: Math.random() * 6, done: false, bug: Math.random() < 0.6 ? (Math.random() < 0.5 ? -1 : 1) : 0 });
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
    if (!g.done && g.x + OW < FXP - 8) { g.done = true; G.score++; }
  }
  for (let q = G.bub.length - 1; q >= 0; q--) { const b = G.bub[q]; b.x -= sp * dt; b.y -= 26 * dt; b.a -= dt * 1.5; if (b.a <= 0) G.bub.splice(q, 1); }
  if (dead) die(now, dead);
  draw(now);
  if (G.state === "play") requestAnimationFrame(step);
}

function railY(s) { return 172 - Math.min(RMAX, s) / RMAX * 130; }
function px(x, y, w, h) { gx.fillRect(Math.round(x), Math.round(y), w, h); }

function draw(now) {
  if (!state.pond) return;
  gx.setTransform(2, 0, 0, 2, 0, 0);
  const PW = GW - RAILW, bands = ["#123a3d", "#103336", "#0e2c30", "#0c262a", "#0b2024", "#0a1b1f"];
  for (let b = 0; b < 6; b++) { gx.fillStyle = bands[b]; gx.fillRect(0, TOP + b * 28, PW, 28); }
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
  const show = G.state !== "over" || now - G.overAt > 500 || Math.floor((now - G.overAt) / 80) % 2 === 0;
  if (show) drawCells(gx, sprites()[G.kick > 0 ? 1 : 0], SW, SH, frogCols(state.g), Math.round(FXP - 15), Math.round(G.fy - 9), 1);
  gx.textAlign = "center"; gx.font = "22px " + FONT;
  gx.fillStyle = "#0a1b1f"; gx.fillText(G.score, PW / 2 + 1, 48); gx.fillStyle = G.flash > 0 ? "#ffe27a" : "#fff6dc"; gx.fillText(G.score, PW / 2, 47);
  if (rail) drawRail(PW);
  overlay(PW);
  if (G.state === "over" && now - G.overAt < 600) requestAnimationFrame(draw);
}

// The rail down the right is where the neighbours got to.
function drawRail(PW) {
  gx.fillStyle = "#0a0908"; gx.fillRect(PW, 0, RAILW, GH);
  gx.fillStyle = "#2f2820"; gx.fillRect(GW - 12, 40, 1, 133);
  gx.fillStyle = "#9c8f79"; gx.font = "8px " + FONT; gx.textAlign = "right";
  let ly = 999;
  GHOSTS.forEach((sc, i) => { const y = railY(sc); gx.fillRect(GW - 16, y, 9, 1); if (ly - y >= 9) { gx.fillText(GN[i], GW - 19, y + 3); ly = y; } });
  gx.fillStyle = "#ffe27a"; gx.fillRect(GW - 20, railY(G.score) - 1, 13, 3);
  gx.fillStyle = "#9c8f79"; gx.textAlign = "center"; gx.fillText("POND", PW + RAILW / 2, 16); gx.fillText("BESTS", PW + RAILW / 2, 26);
}

function overlay(PW) {
  if (G.state !== "play") {
    gx.fillStyle = "rgba(10,27,31,0.78)"; gx.fillRect(10, 62, PW - 20, 70);
    gx.textAlign = "center"; gx.fillStyle = "#fff6dc"; gx.font = "16px " + FONT;
    if (G.state === "idle") {
      gx.fillText("TAP TO KICK DOWN", PW / 2, 86);
      gx.fillStyle = "#bfeee2"; gx.font = "8px " + FONT;
      gx.fillText("THE FROG FLOATS UP BY ITSELF", PW / 2, 102); gx.fillText("STAY UNDER THE SURFACE", PW / 2, 116);
    } else {
      gx.fillText(G.why, PW / 2, 84);
      gx.fillStyle = "#bfeee2"; gx.font = "8px " + FONT;
      const pay = G.paid == null ? "COUNTING PEBBLES" : "+" + G.paid + (G.paid === 1 ? " PEBBLE" : " PEBBLES") + (G.best ? ", A NEW BEST" : "");
      gx.fillText(G.score + (G.score === 1 ? " POINT, " : " POINTS, ") + pay, PW / 2, 98);
      gx.fillText("AHEAD OF " + G.rank + " OF " + GHOSTS.length + " NEIGHBOURS", PW / 2, 109);
      gx.fillStyle = "#ffe27a"; gx.fillText("TAP TO GO AGAIN", PW / 2, 122);
    }
  }
}

function note(p) {
  const d = p.dive, left = d.cap - d.paid;
  let s = "Best " + d.best + ". ";
  if (left <= 0) s += "Today's " + d.cap + " pebbles from diving are all earned, so runs are for the score now.";
  else s += d.paid + " of " + d.cap + " pebbles earned today. " + (d.runs < d.full ? "A pebble a point for the first " + d.full + " runs of a day, a quarter after." : "Runs pay a quarter for the rest of today.");
  $("dive-note").textContent = s;
}

export function initDive() {
  gc = $("dive"); gx = gc.getContext("2d");
  // A phone gives the whole width to the water. The field is the same size
  // either way, so the game plays the same.
  rail = window.innerWidth >= 560;
  gc.width = (rail ? GW : GW - RAILW) * 2;
  reset();
  watch((p) => { note(p); if (G.state !== "play") { tune(); if (!$("v-dive").hidden) draw(performance.now()); } });
  $("v-dive").addEventListener("pointerdown", (e) => { e.preventDefault(); press(); });
  $("v-dive").addEventListener("contextmenu", (e) => e.preventDefault());
  document.addEventListener("keydown", (e) => {
    if ($("v-dive").hidden) return;
    if (e.code === "Space" || e.key === " " || e.key === "Enter" || e.key === "ArrowDown") { e.preventDefault(); if (!e.repeat) press(); }
  });
  if (document.fonts && document.fonts.ready) document.fonts.ready.then(() => { if (G.state !== "play" && !$("v-dive").hidden) draw(performance.now()); });
}

export function enterDive() { tune(); draw(performance.now()); }

// Leaving the view mid run ends it, so the points still count.
export function leaveDive() { if (G.state === "play") die(performance.now(), "CLIMBED OUT"); }

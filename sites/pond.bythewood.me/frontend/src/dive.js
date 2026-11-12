import { $, post, state, setPond, watch, room, onResize, payWord, aimList } from "./api.js";
import { genome, sideCells, draw as drawCells, SW, SH } from "./frog.js";
import { raceStart, raceOut, raceOrder, raceResults, raceHide, countdown, callout, suffix } from "./race.js";
import { feel, finger, unseen, sawIt } from "./feel.js";
import { sfx } from "./sound.js";

// Dive is played in a 320 by 200 field and drawn at twice that. The frog
// floats up by itself, a press kicks it down, and the numbers below are the
// feel of the game, so they change together or not at all.
const TOP = 20, OW = 16;
// The field takes its size from the window, see fit. PW is how wide it is,
// GH how deep, and FXP where the frog swims.
let PW = 266, FXP = 70, GH = 200, BOT = 186;
const BUOY = 330, RISE_MAX = 150, KICK = 138, GATE_EVERY = 118;
const BIG = "700 22px 'Space Grotesk', sans-serif";
// The frog is hit well inside its sprite, this far ahead, behind, over and
// under where it swims. NEAR is how close a gate can pass and get a CLOSE.
const AHEAD = 5, BEHIND = 6, OVER = 3, UNDER = 4, NEAR = 4;
// Every ten seconds or so the heron leans over the water ahead, and its beak
// stabs down DEPTH under the surface as it passes over the frog.
const HERON = 10, DEPTH = 46, STAB = 0.7;
// A run that won nothing new ends on a short card that a tap clears.
const QUICK_AT = 350, QUICK_LOCK = 450, LOCK = 900;

const F = feel();
let gc, gx, spr = null, sprKey = "", net = Promise.resolve(), first = unseen("dive");
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

// Nine other frogs swim every run, some ahead and some behind, drifting
// through the pack. Each aims for the gaps a little sloppily, and the server
// deals how many gates each will clear, which is how it knows who placed.
const GHOSTS_IN = 9;
let others = [];

// The daily race is Dive against the residents on the day's course. Its
// gates come off the server's seed, so every pond meets the same ones.
let mode = "dive", rng = Math.random;
const RACE_BAND = 140;
export function diveMode(m) { if (G.state !== "play" && G.state !== "count") mode = m; }

function seeded(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function board() { return state.pond.dive.race.board; }

// The residents are grown frogs, and a pond's own ghosts are its frog's age.
function rivalStage() { return mode === "race" ? "frog" : state.pond.frog.stage; }

function ghostSprite(g, kick) {
  const cv = document.createElement("canvas");
  cv.width = SW; cv.height = SH;
  drawCells(cv.getContext("2d"), sideCells(g, rivalStage(), kick), SW, SH, frogCols(g), 0, 0, 1);
  return cv;
}

function spawnOthers() {
  others = [];
  const rivals = mode === "race" ? board() : null, n = rivals ? rivals.length : GHOSTS_IN;
  for (let i = 0; i < n; i++) {
    const g = genome(rivals ? rivals[i].seed : 1 + Math.floor(Math.random() * 4294967295));
    others.push({
      id: i, g, name: rivals ? rivals[i].name.toUpperCase() : g.name, spr: [ghostSprite(g, false), ghostSprite(g, true)],
      off: 0, want: 0, wantAt: 0, x: FXP, y: TOP + 30 + Math.random() * (BOT - TOP - 60),
      vy: 0, kick: 0, passed: 0, clears: 99, aim: Math.random() * 4,
      out: false, fade: 1,
    });
  }
  // A grid on the line, so you start somewhere in the middle of it.
  const order = others.map((o) => o).sort(() => Math.random() - 0.5), back = behind(), fwd = ahead();
  order.forEach((o, i) => {
    const slot = (i + 0.5) / others.length;
    o.off = o.want = slot < 0.5 ? -back * (1 - slot * 2) : fwd * (slot * 2 - 1);
    o.x = FXP + o.off;
  });
}

// How far a ghost can drift behind or ahead of you, which on a narrow phone
// field is less.
function behind() { return Math.max(16, Math.min(56, FXP - 16)); }
function ahead() { return Math.max(16, Math.min(60, PW - FXP - 40)); }

// Until the deal arrives a ghost clears everything, and the first gate is
// seconds away, so it always has. With no answer the page deals its own.
function deal(clears) {
  others.forEach((o, i) => { o.clears = clears && clears[i] != null ? clears[i] : Math.floor(Math.random() * 12); });
}

// A rival that went out is carried back on the stems it hit for this long.
const OUT_FOR = 0.9;

function stepOthers(dt, sp) {
  others.forEach((o) => {
    if (o.out) {
      if (o.fade > 0) { o.fade -= dt / OUT_FOR; o.x -= sp * dt; o.y += 10 * dt; }
      return;
    }
    // Each one now and then picks a new spot in the pack and swims to it
    // slowly, which is all an overtake is.
    if (G.t > o.wantAt) { o.want = -behind() + Math.random() * (behind() + ahead()); o.wantAt = G.t + 2 + Math.random() * 4; }
    o.off += Math.max(-9 * dt, Math.min(9 * dt, o.want - o.off));
    o.x = FXP + o.off;
    G.gates.forEach((g) => { if (!g.seen[o.id] && g.x + OW < o.x - 8) { g.seen[o.id] = true; o.passed++; } });
    const gate = G.gates.find((g) => !g.seen[o.id]), done = o.passed >= o.clears;
    // One that has cleared all it's going to swims up into the stems.
    let want = (TOP + BOT) / 2 + o.aim * 3;
    // A kick carries about 29 down, so this keeps the whole bob in the gap.
    if (gate) want = done ? gateC(gate) - gate.gap / 2 - 10 : gateC(gate) - gate.gap / 2 + 6 + o.aim;
    o.vy -= BUOY * dt; if (o.vy < -RISE_MAX) o.vy = -RISE_MAX;
    if (o.y < want && o.vy < 30) { o.vy = KICK; o.kick = 0.16; }
    o.y += o.vy * dt; o.kick -= dt;
    o.y = Math.max(TOP + 6, Math.min(BOT - 6, o.y));
    if (!gate) return;
    // Nothing touches a ghost before its dealt gate and it always goes out
    // on that one, so the race the page shows is the one the server scores.
    const c = gateC(gate), at = o.x + 7 > gate.x;
    if (at && !done) o.y = Math.max(c - gate.gap / 2 + 4, Math.min(c + gate.gap / 2 - 5, o.y));
    if (at && done) {
      o.out = true; o.x = gate.x - 7;
      o.y = o.y < c ? Math.min(o.y, c - gate.gap / 2 - 2) : Math.max(o.y, c + gate.gap / 2 + 3);
      if (G.state === "play") raceOut(o.id);
      for (let i = 0; i < 14; i++) G.bub.push({ x: o.x - 9 + Math.random() * 18, y: o.y - 8 + Math.random() * 16, a: 1 });
    }
  });
  if (G.state === "play") raceOrder(liveOrder());
}

// Everybody still in, with you last, since going out now would place you
// behind all of them.
function liveOrder() {
  return others.filter((o) => !o.out).map((o) => o.id).concat(["you"]);
}

function drawOthers() {
  gx.font = "500 6px 'JetBrains Mono', monospace"; gx.textAlign = "center"; gx.fillStyle = "#ece5d8";
  others.forEach((o) => {
    if (o.fade <= 0) return;
    gx.globalAlpha = 0.14 * o.fade;
    gx.drawImage(o.spr[o.kick > 0 ? 1 : 0], Math.round(o.x - 15), Math.round(o.y - 9));
    // On a phone they swim too close together for ten names to be read.
    if (PW >= 400) gx.fillText(o.name, Math.round(o.x), Math.round(o.y - 12));
  });
  gx.globalAlpha = 1;
}

function reset() {
  F.reset(); G.quick = false;
  G.fy = 96; G.vy = 0; G.kick = 0; G.gates = []; G.bub = []; G.dist = 0; G.next = 150; G.score = 0; G.cleared = 0; G.place = 0; G.bonus = 0; G.hat = null; G.had = false; G.t = 0; G.lastC = GH / 2; G.flash = 0; G.paid = null; G.rested = 0; G.best = false; G.toldBest = false; G.aims = []; G.made = 0; G.lastF = 0.5; G.prize = 0; G.board = false; G.heronAt = 7 + Math.random() * 3; G.beak = null;
}

let started = null;

// Everybody in the run as the results want them. A ghost still swimming when
// you went out finishes on its dealt gate count, the way the server scores it.
function field() {
  const f = state.pond.frog;
  const you = { id: "you", you: true, name: f.name, g: state.g, stage: f.stage, hat: f.hat, spr: youIcon(), gates: G.cleared };
  return [you].concat(others.map((o) => ({ id: o.id, name: o.name, g: o.g, stage: rivalStage(), hat: null, spr: o.spr[0], gates: Math.min(o.clears, 999) })));
}

function youIcon() {
  const cv = document.createElement("canvas");
  cv.width = SW; cv.height = SH;
  drawCells(cv.getContext("2d"), sprites()[0], SW, SH, frogCols(state.g), 0, 0, 1);
  return cv;
}

function results(again) {
  if ($("v-dive").hidden) return;
  raceResults({ racers: field(), why: G.why, place: G.place, paid: G.paid, rested: G.rested, best: G.best, bonus: G.bonus, hat: G.hat, had: G.had, score: G.score, aims: G.aims, race: mode === "race" ? { live: G.live, prize: G.prize, board: G.board } : null, again });
}

function die(now, why) {
  G.state = "over"; G.overAt = now; G.why = why;
  const score = G.score, gates = G.cleared;
  // Anyone due out at the gate you went out on is behind you on a tie, so
  // they go out now and your place shows what the server will give.
  others.forEach((o) => { if (!o.out && o.clears <= gates) { o.out = true; raceOut(o.id); } });
  raceOrder(liveOrder());
  G.quick = mode === "race" ? !G.live && place() > 3 : score <= best() && place() > 3;
  F.crash(FXP, G.fy);
  // The flash of the frog going out plays first, then the results come up.
  setTimeout(() => { if (G.state === "over" && G.overAt === now && !G.quick) results(false); }, 650);
  // Requests go one after another, so a finish always lands before the start
  // of the run after it.
  const finish = mode === "race" ? "/api/race/finish" : "/api/dive/finish";
  net = started.then((run) => run ? post(finish, { run, score, gates }) : null).then((d) => {
    if (!d) { G.paid = 0; return; }
    if (mode === "race") {
      G.paid = d.prize; G.prize = d.prize; G.rested = d.rested; G.place = d.place; G.board = d.live; G.live = false;
      setPond(d.pond);
      return;
    }
    G.paid = d.paid; G.rested = d.rested; G.best = d.best; G.place = d.place; G.bonus = d.bonus; G.hat = d.hat; G.had = d.had; G.aims = d.aims || [];
    setPond(d.pond);
  }, () => { G.paid = 0; }).then(() => {
    if (G.state !== "over" || G.overAt !== now) return;
    // A prize the page didn't see coming still gets the whole card.
    if (G.quick && (G.best || G.bonus || G.hat || G.aims.length)) G.quick = false;
    if (!G.quick && performance.now() - now >= 650) results(true);
    draw(performance.now());
  });
}

// Where this run would place if it ended now, off the deal.
function place() { return 1 + others.filter((o) => o.clears > G.cleared).length; }

function kick() {
  G.vy = KICK; G.kick = 0.16;
  sfx("kick"); F.squash(0.4);
  if (first) { first = false; sawIt("dive"); }
  for (let i = 0; i < 3; i++) G.bub.push({ x: FXP - 10 - Math.random() * 6, y: G.fy - 2 + Math.random() * 6, a: 1 });
}

// Three lights and GO, with everybody bobbing on the line.
const COUNT_STEP = 600;

function press() {
  if (!state.pond) return;
  // A kick a touch before GO is kept for it.
  if (G.state === "count") { F.buffer(1); return; }
  if (G.state === "paused") {
    G.state = "play"; G.last = performance.now();
    requestAnimationFrame(step);
    return;
  }
  if (G.state !== "play") {
    if (G.state === "over" && performance.now() - G.overAt < (G.quick ? QUICK_LOCK : LOCK)) return;
    reset(); spawnOthers(); G.state = "count"; G.countAt = performance.now(); G.last = G.countAt;
    raceStart(field().map((r) => ({ id: r.id, name: r.name, spr: r.spr, you: r.you })), liveOrder());
    rng = Math.random;
    if (mode === "race") {
      G.live = !state.pond.dive.race.place;
      started = net = net.then(() => post("/api/race/start")).then((d) => {
        rng = seeded(d.seed); G.live = d.live;
        deal(d.race.board.map((r) => r.gates));
        return d.run;
      }, () => { deal(board().map((r) => r.gates)); return null; });
    } else started = net = net.then(() => post("/api/dive/start")).then((d) => { deal(d.ghosts); return d.run; }, () => { deal(null); return null; });
    requestAnimationFrame(counting);
    return;
  }
  kick();
}

function counting(now) {
  if (G.state !== "count") return;
  const k = Math.floor((now - G.countAt) / COUNT_STEP);
  countdown(Math.min(k, 3));
  G.t = (now - G.countAt) / 1000;
  G.fy = 96 + Math.sin(G.t * 4) * 3;
  others.forEach((o, i) => { o.y += Math.sin(G.t * 4 + i) * 0.2; });
  if (k >= 3) {
    G.state = "play"; G.last = now; G.t = 0;
    if (F.take()) kick();
    setTimeout(() => { if (G.state !== "count") countdown(-1); }, 450);
    requestAnimationFrame(step);
  } else requestAnimationFrame(counting);
  draw(now);
}

function speed() { return 84 + Math.min(46, G.score * 1.6); }

// A race gate sits in a band of the same depth on any window, so every pond
// has the same course, and it narrows by its place in the course.
function addGate() {
  const race = mode === "race", k = race ? G.made : G.score, gap = Math.max(44, 60 - k * 0.6), lo = TOP + 16 + gap / 2, hi = BOT - 16 - gap / 2;
  const r = [rng(), rng(), rng(), rng(), rng()];
  let c;
  if (race) {
    G.lastF = Math.max(0, Math.min(1, G.lastF + (r[0] * 2 - 1) * 0.45));
    const span = Math.min(hi - lo, RACE_BAND);
    c = (lo + hi - span) / 2 + span * G.lastF;
  } else c = Math.max(lo, Math.min(hi, G.lastC + (r[0] * 2 - 1) * 62));
  G.lastC = c; G.made++;
  G.gates.push({ x: PW + 10, c: c, gap: gap, bob: k > 12 ? 6 + r[1] * 8 : 0, ph: r[2] * 6, done: false, bug: r[3] < 0.6 ? (r[4] < 0.5 ? -1 : 1) : 0, seen: {}, room: 99 });
}

function gateC(g) { return g.c + (g.bob ? Math.sin(G.t * 1.7 + g.ph) * g.bob : 0); }

function step(now) {
  if (G.state !== "play") return;
  const dt = Math.min(0.04, (now - G.last) / 1000); G.last = now;
  if (!F.tick(dt)) { draw(now); requestAnimationFrame(step); return; }
  G.t += dt;
  const sp = speed();
  G.vy -= BUOY * dt; if (G.vy < -RISE_MAX) G.vy = -RISE_MAX;
  G.fy += G.vy * dt; G.kick -= dt; G.flash -= dt;
  if (G.fy > BOT - 6) { G.fy = BOT - 6; G.vy = Math.min(0, G.vy); }
  G.dist += sp * dt; G.next -= sp * dt;
  if (G.next <= 0) { addGate(); G.next = GATE_EVERY; }
  let dead = "";
  if (G.fy < TOP + 5) dead = "THE HERON SAW YOU";
  if (G.t >= G.heronAt && !G.beak) {
    let x = FXP + sp * 1.6;
    const near = G.gates.find((g) => Math.abs(g.x + OW / 2 - x) < 30);
    if (near) x = near.x + OW / 2 + GATE_EVERY / 2;
    G.beak = { x, t: -1, d: 0 };
    sfx("warn");
  }
  const bk = G.beak;
  if (bk) {
    bk.x -= sp * dt;
    if (bk.t < 0 && bk.x <= FXP + 28) bk.t = 0;
    if (bk.t >= 0) {
      bk.t += dt;
      bk.d = depth() * Math.min(1, bk.t / 0.12, Math.max(0, (STAB - bk.t) / 0.2));
      if (Math.abs(bk.x - FXP) < 9 && G.fy - OVER < TOP + bk.d) dead = "THE HERON GOT YOU";
      if (bk.t >= STAB) { G.beak = null; G.heronAt = G.t + HERON - 1 + Math.random() * 2; }
    }
  }
  for (let i = G.gates.length - 1; i >= 0; i--) {
    const g = G.gates[i]; g.x -= sp * dt;
    if (g.x + OW < -6) { G.gates.splice(i, 1); continue; }
    const c = gateC(g), topEnd = c - g.gap / 2, botStart = c + g.gap / 2;
    if (FXP + AHEAD > g.x && FXP - BEHIND < g.x + OW) {
      const room = Math.min(G.fy - OVER - topEnd, botStart - G.fy - UNDER);
      g.room = Math.min(g.room, room);
      if (room < 0) dead = G.fy - OVER < topEnd ? "TANGLED IN THE STEMS" : "BONKED A ROCK";
    }
    if (g.bug && Math.abs(g.x + OW / 2 - FXP) < 9 && Math.abs(c + g.bug * (g.gap / 2 - 9) - G.fy) < 9) {
      F.got(g.x + OW / 2, c + g.bug * (g.gap / 2 - 9), "+1", { streak: G.cleared + 1 });
      g.bug = 0; G.score++; G.flash = 0.25;
    }
    if (!g.done && g.x + OW < FXP - 8) {
      g.done = true; G.score++; G.cleared++;
      sfx("catch", G.cleared);
      if (g.room < NEAR && !dead) F.close(FXP, G.fy - 16);
    }
    if (!G.toldBest && best() > 0 && G.score > best()) { G.toldBest = true; callout("New best", "up"); }
  }
  for (let q = G.bub.length - 1; q >= 0; q--) { const b = G.bub[q]; b.x -= sp * dt; b.y -= 26 * dt; b.a -= dt * 1.5; if (b.a <= 0) G.bub.splice(q, 1); }
  stepOthers(dt, sp);
  if (dead) die(now, dead);
  draw(now);
  if (G.state === "play") requestAnimationFrame(step);
}

function px(x, y, w, h) { gx.fillRect(Math.round(x), Math.round(y), w, h); }

// The heron's neck and head over the spot, a dotted line blinking down to how
// deep the beak goes while it waits, and the beak in the water as it stabs.
function heron(b, now) {
  const x = Math.round(b.x), d = depth();
  gx.fillStyle = "#e9e3d6"; px(x - 1, 0, 4, TOP - 10); px(x - 6, TOP - 14, 12, 7); px(x - 3, TOP - 16, 7, 2);
  gx.fillStyle = "#1c2a33"; px(x - 4, TOP - 12, 2, 2); px(x + 2, TOP - 16, 4, 1);
  gx.fillStyle = "#ffb000"; px(x - 1, TOP - 7, 3, 7 + b.d); px(x, TOP + b.d, 1, 3);
  if (b.t < 0) {
    gx.fillStyle = Math.floor(now / 140) % 2 ? "rgba(255,176,0,0.8)" : "rgba(255,176,0,0.35)";
    for (let y = TOP + 3; y < TOP + d; y += 5) px(x, y, 1, 2);
    px(x - 3, TOP + d, 7, 1);
  }
}

function depth() { return Math.max(DEPTH, (BOT - TOP) * 0.25); }

function draw(now) {
  if (!state.pond) return;
  // Nothing else steps the bits and the shake once the run is over.
  if (G.state === "over") { F.tick(Math.min(0.04, (now - (G.drawn || now)) / 1000)); }
  G.drawn = now;
  const [sx, sy] = F.offset();
  gx.setTransform(2, 0, 0, 2, sx * 2, sy * 2);
  // Off, or a ghost drawn from its own small canvas comes out smeared.
  gx.imageSmoothingEnabled = false;
  const bands = ["#123a3d", "#103336", "#0e2c30", "#0c262a", "#0b2024", "#0a1b1f"];
  const bh = (BOT - TOP) / 6;
  for (let b = 0; b < 6; b++) { gx.fillStyle = bands[b]; gx.fillRect(0, Math.floor(TOP + b * bh), PW, Math.ceil(bh) + 1); }
  // The sky strip, where the heron's legs wade past slowly.
  gx.fillStyle = "#1c2a33"; gx.fillRect(0, 0, PW, TOP);
  const hx = ((PW + 60 - (G.dist * 0.5) % (PW + 120)) + PW + 120) % (PW + 120) - 30;
  gx.fillStyle = "#d9c9a8"; px(hx, 4, 2, 44); px(hx + 9, 8, 2, 36); gx.fillStyle = "#e9e3d6"; px(hx - 4, 0, 22, 6);
  if (G.beak) heron(G.beak, now);
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
  if (show) F.squashed(gx, FXP, G.fy + 9, () => drawCells(gx, sprites()[G.kick > 0 ? 1 : 0], SW, SH, frogCols(state.g), Math.round(FXP - 15), Math.round(G.fy - 9), 1));
  F.draw(gx);
  if (first && G.state !== "over" && G.state !== "paused") finger(gx, { x: Math.round(PW * 0.62), y: Math.round(GH * 0.62) }, now / 1000);
  gx.textAlign = "center"; gx.font = BIG;
  gx.fillStyle = "#0a1b1f"; gx.fillText(G.score, PW / 2 + 1, 48); gx.fillStyle = G.flash > 0 ? "#ffe27a" : "#fff6dc"; gx.fillText(G.score, PW / 2, 47);
  overlay(now);
  if (G.state === "over" && now - G.overAt < 600) requestAnimationFrame(draw);
}

// The finger taps over the still water until a tap starts the lights.
function demo(now) {
  if (!first || G.state !== "idle" || $("v-dive").hidden) return;
  draw(now);
  requestAnimationFrame(demo);
}

// The title is the same card the side games open on, and a paused run shows
// in it too.
function overlay(now) {
  const card = $("dive-card"), again = G.state === "over" && G.quick && now - G.overAt >= QUICK_AT;
  const want = again ? "again" + G.paid : (G.state === "idle" && !first) || G.state === "paused" ? G.state : "";
  card.hidden = !want;
  if (card.hidden) return;
  card.classList.toggle("quick", again);
  if (again && card.dataset.shows !== want) {
    card.dataset.shows = want;
    const span = (cls, text) => { const e = document.createElement("span"); e.className = cls; e.textContent = text; return e; };
    const r = span("readout big", ""), p = document.createElement("p"), key = document.createElement("b"), n = place();
    r.append(span("k", n + suffix(n) + " of " + (others.length + 1)), span("v", String(G.score)));
    p.className = G.paid ? "won" : "dim"; p.textContent = G.paid === null ? "Counting..." : mode === "race" && !G.board && !G.paid ? "Practice" : "+" + G.paid + " pebbles" + (G.rested ? ", well rested" : "");
    key.className = "key"; key.textContent = "AGAIN";
    card.replaceChildren(r, p, key);
  }
  if (again) return;
  const aims = state.pond.dive.aims || [], rc = state.pond.dive.race, shows = want + mode + rc.place + aims.map((a) => a.done ? 1 : 0).join("");
  if (card.dataset.shows === shows) { card.querySelector("canvas").getContext("2d").drawImage($(mode === "race" ? "play-race-art" : "play-dive-art"), 0, 0); return; }
  card.dataset.shows = shows;
  const win = document.createElement("div"), art = document.createElement("canvas");
  win.className = "arcade-win"; art.width = 168; art.height = 96;
  art.getContext("2d").drawImage($(mode === "race" ? "play-race-art" : "play-dive-art"), 0, 0);
  win.append(art);
  const line = (tag, cls, text) => { const e = document.createElement(tag); e.className = cls; e.textContent = text; return e; };
  if (want === "paused") card.replaceChildren(win, line("b", "", "PAUSED"), line("p", "again", "Tap to carry on"));
  else if (mode === "race") card.replaceChildren(win, line("b", "", "DAILY RACE"), line("p", "dim", "Today's course, the same for every pond"),
    line("p", "dim", rc.place ? "On the board " + rc.place + suffix(rc.place) + " with " + rc.gates + " gates, the rest is practice" : "Your first run today goes on the board"),
    line("p", "dim", "The top three win a prize"), line("p", "again", "Tap to start"));
  else card.replaceChildren(win, line("b", "", "DIVE"), line("p", "dim", "Tap to kick down"), line("p", "dim", "The frog floats up by itself"),
    line("p", "dim", "Outlast the others for a prize"), line("p", "dim", "Keep under the heron's beak"), aimList(aims), line("p", "again", "Tap to start"));
}

// The strip under the water: the best run, pebbles earned against the day's
// cap, and what a point is worth on the next run.
function note(p) {
  const d = p.dive;
  $("dive-best").textContent = d.best;
  $("dive-today").textContent = d.paid + "/" + d.cap;
  $("dive-pays").textContent = payWord(d, "Half");
}

export function initDive() {
  gc = $("dive"); gx = gc.getContext("2d");
  reset();
  // The way out is a button, so a press on it must not also be a kick.
  $("dive-exit").addEventListener("pointerdown", (e) => e.stopPropagation());
  $("dive-exit").addEventListener("click", () => { location.hash = "#play"; });
  watch((p) => { note(p); if (G.state !== "play" && !$("v-dive").hidden) draw(performance.now()); });
  $("v-dive").addEventListener("pointerdown", (e) => { e.preventDefault(); press(); });
  $("v-dive").addEventListener("contextmenu", (e) => e.preventDefault());
  document.addEventListener("keydown", (e) => {
    if ($("v-dive").hidden) return;
    if (e.code === "Space" || e.key === " " || e.key === "Enter" || e.key === "ArrowDown") { e.preventDefault(); if (!e.repeat) press(); }
  });
  if (document.fonts && document.fonts.ready) document.fonts.ready.then(() => { if (G.state !== "play" && !$("v-dive").hidden) draw(performance.now()); });
  // A run stops while the page is hidden and waits for a tap. The lights
  // start over, since they'd have run out behind the hidden page.
  document.addEventListener("visibilitychange", () => {
    if (document.hidden && G.state === "play") { G.state = "paused"; draw(performance.now()); }
    if (!document.hidden && G.state === "count") G.countAt = performance.now();
  });
}

// Dive takes the whole window. A tall phone sets the scale by its width and
// gets a deep pond, a wide desktop sets it by its height and gets a long one.
// The strip left under the water holds the readout and the way out.
function fit() {
  $("v-dive").classList.add("full");
  const { w, h } = room($("v-dive")), scale = Math.min(w / 266, h / 300);
  PW = Math.min(640, Math.floor(w / scale));
  GH = Math.max(200, Math.min(800, Math.floor(h / scale)));
  BOT = GH - 14;
  FXP = Math.max(70, Math.round(PW * 0.22));
  gc.width = PW * 2; gc.height = GH * 2;
  gc.style.width = Math.round(PW * scale) + "px";
  reset();
}

// Mid run only the drawn size follows the window, since a refit resets the water.
onResize(() => {
  if ($("v-dive").hidden) return;
  if (G.state === "idle") { fit(); draw(performance.now()); return; }
  const { w, h } = room($("v-dive"));
  gc.style.width = Math.round(PW * Math.min(w / PW, h / GH)) + "px";
});

export function enterDive() {
  if (G.state !== "play") { G.state = "idle"; raceHide(); fit(); }
  draw(performance.now());
  requestAnimationFrame(demo);
}

// Leaving the view mid run ends it, so the points still count.
export function leaveDive() {
  if (G.state === "play" || G.state === "paused") die(performance.now(), "CLIMBED OUT");
  if (G.state === "count") G.state = "idle";
  raceHide();
  $("v-dive").classList.remove("full");
}

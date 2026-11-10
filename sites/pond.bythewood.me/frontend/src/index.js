import { $, post, state, setPond, watch, reduced, titled, heldNow, TIERS } from "./api.js";
import { genome, bred, cells, sideCells, colours, draw, croak, W, H, SW, SH } from "./frog.js";
import { drawIcon, iconCanvas, drawFurn } from "./icons.js";
import { initDive, enterDive, leaveDive } from "./dive.js";
import { arcade } from "./arcade.js";
import { flick } from "./flick.js";
import { cross } from "./cross.js";
import { initWheel, enterWheel } from "./wheel.js";
import { initReeds } from "./reeds.js";
import { initBank, enterBank, leaveBank } from "./bank.js";
import { initShops } from "./shops.js";
import { initUI, reveal, thingArt, drawBox } from "./ui.js";
import { initBag } from "./bag.js";
import { initCollect } from "./collect.js";
import { initTown, enterTown, leaveTown } from "./town.js";
import { initNeighbours } from "./neighbours.js";
import { initSettings } from "./settings.js";

const VIEWS = ["pond", "play", "dive", "cross", "flick", "wheel", "bag", "collect", "reeds", "garden", "bank", "town", "settings"];
// Places that open from another light that one's key.
const TAB_OF = { dive: "play", wheel: "play", cross: "play", flick: "play" };
let view = "pond", fetchedAt = Date.now();
const GAMES = { flick: arcade(flick), cross: arcade(cross) };

function show(next) {
  if (!VIEWS.includes(next)) next = "pond";
  if (view === "dive" && next !== "dive") leaveDive();
  if (GAMES[view] && next !== view) GAMES[view].leave();
  view = next;
  VIEWS.forEach((v) => { $("v-" + v).hidden = v !== next; });
  const lit = TAB_OF[next] || next;
  document.querySelectorAll("#tabs button").forEach((b) => {
    b.setAttribute("aria-pressed", b.dataset.view === lit ? "true" : "false");
  });
  $("gear").classList.toggle("lit", next === "settings");
  if (next === "dive") enterDive();
  if (GAMES[next]) GAMES[next].enter();
  if (next === "wheel") enterWheel();
  if (next === "play") drawPlay();
  if (next === "garden") enterBank(); else leaveBank();
  if (next === "town") enterTown(); else leaveTown();
  freshen();
  // A view's name is also the id of its canvas, and the browser scrolls to
  // that when the address changes, a frame after this runs.
  window.scrollTo(0, 0);
  requestAnimationFrame(() => window.scrollTo(0, 0));
}

window.addEventListener("load", () => window.scrollTo(0, 0));

$("tabs").addEventListener("click", (e) => {
  const b = e.target.closest("button"), v = b && b.dataset.view;
  if (v) { history.replaceState(null, "", v === "pond" ? location.pathname : "#" + v); show(v); }
});

window.addEventListener("hashchange", () => show(location.hash.slice(1)));

// ---------------------------------------------------------------- the pond

const scene = $("scene"), sx = scene.getContext("2d");
const HEART = ["01010", "11111", "11111", "01110", "00100"];
let greetAt = 0, croakUntil = 0, lastTick = 0;

function grown(stage) { return stage === "froglet" || stage === "frog" || stage === "elder"; }

const WATER = {
  "": ["#123a3d", "#103336", "#0e2c30", "#0c262a"],
  clear: ["#15484b", "#13403f", "#103838", "#0d3033"],
  murky: ["#28311f", "#232b1c", "#1e2518", "#192014"],
};

// A fixed scatter, so a thing on the water keeps its place from frame to frame.
function scatter(i, salt) {
  const n = Math.sin(i * 127.1 + salt * 311.7) * 43758.5453;
  return n - Math.floor(n);
}

function block(x, y, c, n) {
  sx.fillStyle = c;
  sx.fillRect(Math.round(x / 4) * 4, Math.round(y / 4) * 4, (n || 1) * 4, (n || 1) * 4);
}

function drawPad(day) {
  for (let y = 222; y < 268; y += 4) for (let x = 20; x < 332; x += 4) {
    const a = (x + 2 - 176) / 150, b = (y + 2 - 246) / 20, d = a * a + b * b;
    if (d > 1 || (x > 286 && Math.abs(y - 240) < (x - 286) * 0.3)) continue;
    sx.fillStyle = d > 0.82 ? (day.cold && scatter(x, y) < 0.6 ? "#cfe3ea" : "#1f5a2c") : (y < 236 ? "#56b45f" : "#3f9a4d");
    sx.fillRect(x, y, 4, 4);
  }
}

// What the real day puts on the water behind the frog.
const MOON = ["..aaa..", ".aabaa.", "aaaaaba", "abaaaaa", "aaaabaa", ".aaaaa.", "..aaa.."];
const SPAWN = [[8, 152], [300, 132], [36, 64]];

function drawBehind(day, t) {
  if (day.spawn) SPAWN.forEach(([x, y], n) => {
    for (let i = 0; i < 9; i++) {
      const ex = x + (i % 3) * 12 + (Math.floor(i / 3) % 2) * 4, ey = y + Math.floor(i / 3) * 12;
      block(ex, ey, "rgba(230,240,232,0.55)", 2); block(ex + 4, ey + 4, "#0a0908");
    }
  });
  if (day.heron) {
    const hx = ((t * 9) % 560) - 120;
    sx.fillStyle = "rgba(0,0,0,0.22)";
    sx.beginPath(); sx.ellipse(hx, 150, 70, 22, 0, 0, Math.PI * 2); sx.fill();
    sx.fillRect(hx + 52, 112, 10, 34); sx.fillRect(hx + 52, 104, 36, 10);
  }
  if (day.water === "murky") for (let i = 0; i < 3; i++) {
    const bx = 30 + ((scatter(i, 3) * 290 + t * (4 + i)) % 300), by = 200 + scatter(i, 4) * 50;
    block(bx, by, "#14110c", 2); block(bx + 2, by, "#5b6b3a");
  }
  if (day.swarm) {
    const cx = 260 + Math.sin(t * 0.4) * 50, cy = 60 + Math.cos(t * 0.3) * 18;
    for (let i = 0; i < 16; i++) block(cx + Math.sin(t * 7 + i * 2.3) * 20 * scatter(i, 5), cy + Math.cos(t * 6 + i * 1.7) * 14 * scatter(i, 6), "rgba(236,229,216,0.75)");
  }
}

// And what goes over the frog.
function drawOver(day, t) {
  if (day.water === "clear") for (let i = 0; i < 4; i++) {
    const mx = (scatter(i, 1) * 352 + t * 12 * (i % 2 ? 1 : -1) + 352) % 352, my = 30 + scatter(i, 2) * 90 + Math.sin(t * 3 + i) * 6;
    block(mx, my, "#fff6dc"); if (Math.floor(t * 8 + i) % 2) block(mx, my - 4, "rgba(255,246,220,0.6)");
  }
  if (day.dragonfly) {
    const k = (t % 14) / 14, dx = -40 + k * 440, dy = 40 + Math.sin(k * 9) * 20;
    block(dx, dy, "#3d8fd6", 1); block(dx - 4, dy, "#3d8fd6"); block(dx - 8, dy, "#2b6aa8"); block(dx - 12, dy, "#2b6aa8");
    const up = Math.floor(t * 20) % 2 ? -4 : 4;
    block(dx - 4, dy + up, "rgba(220,240,255,0.7)"); block(dx - 4, dy - up, "rgba(220,240,255,0.7)");
  }
  if (day.rain) for (let i = 0; i < 26; i++) {
    const rx = scatter(i, 7) * 352, ry = (scatter(i, 8) * 272 + t * 160) % 272;
    sx.fillStyle = "rgba(200,230,240,0.45)"; sx.fillRect(Math.round(rx), Math.round(ry), 2, 8);
  }
  if (day.night) {
    sx.fillStyle = "rgba(4,6,18,0.55)"; sx.fillRect(0, 0, 352, 272);
    if (day.moon && !day.rain) {
      MOON.forEach((r, y) => [...r].forEach((ch, x) => { if (ch !== ".") block(292 + x * 4, 16 + y * 4, ch === "a" ? "#f4f1e6" : "#c9cfd6"); }));
      for (let i = 0; i < 6; i++) {
        const w = 28 - i * 3 + Math.round(Math.sin(t * 1.3 + i) * 3);
        sx.fillStyle = "rgba(230,238,246," + (0.32 - i * 0.04) + ")"; sx.fillRect(Math.round(306 - w / 2), 60 + i * 30, w, 3);
      }
    }
    if (!day.cold && !day.rain) for (let i = 0; i < (day.fireflies ? 22 : 7); i++) {
      if (Math.sin(t * 2 + i * 1.9) < 0.2) continue;
      const fx = scatter(i, 9) * 330 + Math.sin(t * 0.5 + i) * 10, fy = 30 + scatter(i, 10) * 160 + Math.cos(t * 0.6 + i) * 8;
      block(fx, fy, "#ffe27a"); sx.fillStyle = "rgba(255,226,122,0.18)"; sx.fillRect(Math.round(fx / 4) * 4 - 4, Math.round(fy / 4) * 4 - 4, 12, 12);
    }
  }
}

// ---------------------------------------------------------------- furniture

function sprite(key, x, y, lit) { drawFurn(sx, key, x, y, 4, lit); }

// On the water, behind the frog.
function drawFurnBack(has, day, t) {
  if (has.f_stones) [[8, 132, 5], [32, 152, 3], [8, 168, 3]].forEach(([x, y, n]) => {
    sx.fillStyle = "#5b6470"; sx.fillRect(x, y, n * 4, 8);
    sx.fillStyle = "#8a939e"; sx.fillRect(x + 4, y - 4, (n - 2) * 4, 4);
    sx.fillStyle = "rgba(127,214,194,0.3)"; sx.fillRect(x - 4, y + 8, (n + 2) * 4, 2);
  });
  if (has.f_lilypad) sprite("f_lilypad", 12, 36 + Math.round(Math.sin(t * 0.8) * 2) * 2, false);
  if (has.f_teacup) sprite("f_teacup", 280, 28 + Math.round(Math.sin(t * 0.7 + 1)) * 4, false);
  if (has.f_reeds) for (let i = 0; i < 5; i++) {
    const x = 324 + i * 6 - (i % 2) * 2, top = 92 + ((i * 37) % 5) * 8, sway = Math.round(Math.sin(t * 0.9 + i) * (1 + (day.wind || 0)));
    sx.fillStyle = "#2f7a3f"; sx.fillRect(x + sway, top, 2, 224 - top);
    sx.fillStyle = "#6b4a2a"; sx.fillRect(x - 1 + sway, top + 6, 4, 14);
    sx.fillStyle = "#3f9a4d"; sx.fillRect(x + sway - 3, top + 40 + i * 6, 3, 18);
  }
}

// On the pad beside the frog, once there's a pad.
function drawFurnPad(has, day, t) {
  if (has.f_toadstool) sprite("f_toadstool", 304, 196, false);
  if (has.f_lantern) sprite("f_lantern", 8, 208, false);
}

// After dark the lantern is lit over the night, and it brings fireflies
// whatever the weather.
function drawFurnNight(has, day, t, onPad) {
  if (!has.f_lantern || !day.night) return;
  if (onPad) {
    const flick = Math.sin(t * 9) > 0.6 ? 0.03 : 0;
    [[40, 0.1], [28, 0.12], [18, 0.16]].forEach(([r, a]) => {
      sx.fillStyle = "rgba(255,226,122," + (a + flick) + ")";
      sx.fillRect(20 - r, 224 - r, r * 2, r * 2);
    });
    sprite("f_lantern", 8, 208, true);
  }
  for (let i = 0; i < 4; i++) {
    if (Math.sin(t * 2.4 + i * 2.1) < 0) continue;
    const fx = 24 + Math.sin(t * 0.7 + i * 1.6) * 20, fy = 176 + Math.cos(t * 0.9 + i) * 18;
    block(fx, fy, "#ffe27a");
  }
}

// ---------------------------------------------------------------- the reeds

// Frogs in the reeds swim across the back of the pond now and then, and
// sleep in the mud on a cold day like the one out front dozes.
const swimSprites = new Map();

function swimmer(f) {
  const key = f.id + f.stage + JSON.stringify(f.mods || null);
  let s = swimSprites.get(key);
  if (!s) {
    const g = bred(genome(f.seed), f.mods), col = colours(g, null);
    s = [false, true].map((kick) => {
      const cv = document.createElement("canvas");
      cv.width = SW * 2; cv.height = SH * 2;
      draw(cv.getContext("2d"), sideCells(g, f.stage, kick), SW, SH, col, 0, 0, 2);
      return cv;
    });
    swimSprites.set(key, s);
  }
  return s;
}

function drawSwimmers(p, day, t) {
  if (day.cold || day.shelter) return;
  p.frogs.filter((f) => !f.front && f.stage !== "egg").slice(0, 7).forEach((f, i) => {
    const period = 30 + scatter(i, 11) * 20, u = reduced ? 0.25 + i * 0.07 : ((t + scatter(i, 12) * period) % period) / period;
    if (u > 0.7) return;
    const dir = i % 2 ? -1 : 1, k = u / 0.7, x = dir > 0 ? -60 + k * 472 : 412 - k * 472;
    const y = 8 + ((i * 17) % 44) + Math.round(Math.sin(t * 1.3 + i) * 2) * 2;
    const spr = swimmer(f)[Math.floor(t * 2 + i) % 3 === 0 ? 1 : 0];
    sx.save();
    sx.translate(Math.round(x / 2) * 2, y);
    if (dir < 0) sx.scale(-1, 1);
    sx.drawImage(spr, dir < 0 ? -SW * 2 : 0, 0);
    sx.restore();
  });
}

function drawScene(now) {
  const p = state.pond, g = state.g;
  if (!p || !g) return;
  const f = p.frog, day = p.day || {}, t = now / 1000, k = greetAt ? Math.min(1, (now - greetAt) / 2100) : 1, moving = k < 1;
  WATER[day.water || ""].forEach((c, i) => { sx.fillStyle = c; sx.fillRect(0, i * 68, 352, 68); });
  const ripples = [6, 10, 14][day.wind || 0], drift = [5, 14, 28][day.wind || 0];
  sx.fillStyle = "rgba(127,214,194,0.22)";
  for (let i = 0; i < ripples; i++) {
    const x = Math.round(((i * 67 + t * drift) % 392) - 20), y = 14 + (i * 97) % 240;
    sx.fillRect(x, y, 20, 2); sx.fillRect(x + 20, y + 2, 12, 2);
  }
  drawBehind(day, t);
  drawSwimmers(p, day, t);
  const has = {};
  (p.furniture || []).forEach((k) => { has[k] = true; });
  drawFurnBack(has, day, t);
  const out = (p.trinkets && p.trinkets.spots) || [];
  if (out[2]) drawIcon(sx, out[2], 160, 12 + Math.round(Math.sin(t * 0.9 + 2)) * 4, 4);
  if (grown(f.stage) && !day.shelter) { drawPad(day); drawFurnPad(has, day, t); }
  const o = { t: moving ? k * 6 : t, hat: f.hat.shape, legs: f.day >= 6 };
  let hop = 0, wig = 0;
  if (grown(f.stage)) {
    // On a cold day it dozes, and wakes for the greeting and a croak.
    const dozing = day.cold && !moving && now >= croakUntil;
    o.blink = dozing || (moving ? k > 0.1 : (t % 4.3) < 0.16);
    o.breathe = Math.sin(t * (dozing ? 0.9 : 2.2)) > 0.4 ? 0.5 : 0;
    if (moving) o.croak = 0.5 + 0.5 * Math.abs(Math.sin(k * 30));
    else if (now < croakUntil) o.croak = 0.55 + 0.45 * Math.abs(Math.sin(t * g.rate * 0.5));
    if (moving) { hop = Math.abs(Math.sin(k * Math.PI * 3.5)) * 50 * (1 - k); wig = Math.sin(k * 44) * 4 * (1 - k); }
  } else if (moving) wig = Math.sin(k * 44) * 6 * (1 - k);
  // Sheltering, it sits low with the pad pulled over it.
  draw(sx, cells(g, f.stage, o), W, H, colours(g, f.hat), Math.round(wig), -Math.round(hop) + (day.shelter ? 40 : 0), 8);
  if (grown(f.stage) && day.shelter) drawPad(day);
  // Trinkets sit in front of the frog at the pad's edge.
  if (grown(f.stage) && !day.shelter) {
    if (out[0]) drawIcon(sx, out[0], 40, 236, 4);
    if (out[1]) drawIcon(sx, out[1], 280, 236, 4);
  }
  if (grown(f.stage) && day.cold && !moving && now >= croakUntil) for (let i = 0; i < 3; i++) {
    const zt = (t * 0.5 + i / 3) % 1, zx = 250 + zt * 30 + i * 4, zy = 70 - zt * 50;
    sx.globalAlpha = 1 - zt; sx.fillStyle = "#fff6dc";
    sx.fillRect(Math.round(zx), Math.round(zy), 12, 3); sx.fillRect(Math.round(zx) + 6, Math.round(zy) + 3, 3, 3);
    sx.fillRect(Math.round(zx) + 3, Math.round(zy) + 6, 3, 3); sx.fillRect(Math.round(zx), Math.round(zy) + 9, 12, 3);
  }
  sx.globalAlpha = 1;
  if (moving) for (let i = 0; i < 5; i++) {
    const ht = k * 1.5 - i * 0.13;
    if (ht <= 0 || ht >= 1) continue;
    const hx = 176 + (i - 2) * 52 + Math.sin(ht * 6 + i) * 8, hy = 96 - ht * 80;
    sx.globalAlpha = 1 - ht; sx.fillStyle = i % 2 ? "#f59ab3" : "#e2567a";
    for (let r = 0; r < 5; r++) for (let c = 0; c < 5; c++) if (HEART[r].charAt(c) === "1") sx.fillRect(Math.round(hx) + c * 4, Math.round(hy) + r * 4, 4, 4);
  }
  sx.globalAlpha = 1;
  drawOver(day, t);
  drawFurnNight(has, day, t, grown(f.stage) && !day.shelter);
}

function loop(now) {
  const greeting = greetAt && now - greetAt < 2100;
  if (view === "pond" && (greeting || now - lastTick > 110)) { lastTick = now; drawScene(now); }
  requestAnimationFrame(loop);
}

$("scene-btn").addEventListener("click", () => {
  if (!state.g) return;
  croakUntil = performance.now() + croak(state.g) * 1000;
  if (reduced) { drawScene(performance.now()); setTimeout(() => drawScene(performance.now()), croakUntil - performance.now() + 50); }
});

const STAGE = { egg: "Egg", tadpole: "Tadpole", froglet: "Froglet", frog: "Frog", elder: "Elder" };
const NEXT_DAY = { egg: 2, tadpole: 9, froglet: 13, frog: 29 };

function renderPond(p) {
  const f = p.frog;
  $("frog-name").textContent = titled(f.name);
  const tier = $("frog-tier");
  tier.textContent = TIERS[f.rarity.tier]; tier.className = "tier t" + f.rarity.tier;
  $("frog-odds").textContent = "1 in " + f.rarity.n.toLocaleString("en-US");
  // An egg doesn't say what colour or back it'll have. That's for hatching.
  const bits = [STAGE[f.stage], "Day " + f.day];
  if (f.stage !== "egg") bits.push(titled(f.morph), titled(f.nature));
  if (NEXT_DAY[f.stage]) bits.push("grows on day " + NEXT_DAY[f.stage]);
  $("frog-what").textContent = bits.join(" · ");

  $("purse").hidden = false;
  $("purse-n").textContent = (p.pebbles - state.held).toLocaleString("en-US");
  $("nudge").hidden = !p.nudge;
  const lines = (p.day && p.day.lines) || [], today = $("today");
  today.textContent = "";
  lines.forEach((row) => {
    const li = document.createElement("li"), b = document.createElement("b");
    b.textContent = row[0];
    li.append(b, " " + row[1]);
    today.append(li);
  });
  $("today-head").hidden = today.hidden = lines.length === 0;
  renderTodo(p);
  renderPlay(p);
}

// The server decides what's worth doing, and the gravel bar and rent count up
// here between fetches, so a chip can appear or grow before the next one.
function renderTodo(p) {
  const live = { gravel: [heldNow(p.bank.gravel), " on the gravel bar"], rent: [heldNow(p.bank.burrows), " in rent"] };
  const counts = Object.values(live).filter(([n]) => n >= 1).map(([n, what]) => ["peb", n + what, "bank"]);
  // A suggestion to save up only stands when nothing else is waiting.
  const rest = p.todo.filter((t) => !live[t.kind] && !(counts.length && t.kind === "save"));
  const todo = counts.concat(rest.map((t) => [t.icon, t.label, t.to]));

  const box = $("todo");
  box.textContent = "";
  todo.forEach(([icon, label, to]) => {
    const a = document.createElement("a");
    a.href = "#" + to; a.className = "chip";
    a.append(iconCanvas(icon, 2), label);
    box.append(a);
  });
  if (!todo.length) {
    const s = document.createElement("p");
    s.className = "dim";
    s.textContent = "That's everything for today. The bank and the garden keep going while you're away.";
    box.append(s);
  }
}
setInterval(() => { if (state.pond && view === "pond" && !document.hidden) renderTodo(state.pond); }, 20000);

// ---------------------------------------------------------------- play

function renderPlay(p) {
  $("play-dive-line").textContent = "Best " + p.dive.best + " · today " + p.dive.paid + "/" + p.dive.cap;
  const w = p.wheel;
  $("play-wheel-line").textContent = w.free > 0 ? w.free + " free spin" + (w.free > 1 ? "s" : "") + " waiting" : "Next free spin soon";
  if (!logTurned) {
    document.querySelectorAll("#logs button").forEach((b) => { b.disabled = !p.log.ready; });
    if (!p.log.ready) $("log-out").textContent = "Turned over for today. Back tomorrow.";
  }
  if (view === "play") drawPlay();
}

function drawLog(cv, open) {
  const x = cv.getContext("2d");
  x.clearRect(0, 0, 64, 40);
  x.fillStyle = "#6b4a2a"; x.fillRect(4, open ? 4 : 12, 56, 20);
  x.fillStyle = "#8a6230"; x.fillRect(4, open ? 8 : 16, 56, 4); x.fillRect(4, open ? 18 : 26, 56, 2);
  x.fillStyle = "#c9974f"; x.fillRect(56, open ? 4 : 12, 6, 20);
  x.fillStyle = "#e3c57a"; x.fillRect(58, open ? 10 : 18, 2, 8);
  x.fillStyle = "#3f9a4d"; x.fillRect(12, open ? 0 : 8, 8, 4); x.fillRect(36, open ? 2 : 10, 4, 4);
  if (open) { x.fillStyle = "#2f2820"; x.fillRect(8, 28, 48, 10); }
}

function drawPlay() {
  const p = state.pond, g = state.g;
  if (!p || !g) return;
  const d = $("play-dive-art").getContext("2d");
  ["#123a3d", "#0e2c30", "#0a1b1f"].forEach((c, i) => { d.fillStyle = c; d.fillRect(0, i * 32, 168, 32); });
  d.fillStyle = "#7fd6c2"; d.fillRect(0, 0, 168, 3);
  d.fillStyle = "#2f7a3f"; d.fillRect(120, 0, 10, 40); d.fillStyle = "#5b6470"; d.fillRect(118, 66, 14, 30);
  d.fillStyle = "#3f9a4d"; d.fillRect(70, 0, 10, 28); d.fillStyle = "#5b6470"; d.fillRect(68, 56, 14, 40);
  draw(d, sideCells(g, p.frog.stage === "egg" ? "tadpole" : p.frog.stage, true), SW, SH, colours(g, null), 22, 40, 2);
  d.fillStyle = "rgba(255,246,220,0.6)"; d.fillRect(12, 30, 4, 4); d.fillRect(8, 22, 2, 2);
  const c = $("play-cross-art").getContext("2d");
  ["#4f9a4a", "#3a3a40", "#3a3a40", "#4f9a4a", "#1b4a5a", "#1d5163", "#2f6b33"].reverse().forEach((col, i) => { c.fillStyle = col; c.fillRect(0, i * 14 - 2, 168, 14); });
  c.fillStyle = "#cfc7a8"; for (let x = 4; x < 168; x += 16) c.fillRect(x, 67, 8, 1);
  c.fillStyle = "#e2412d"; c.fillRect(30, 58, 22, 8); c.fillStyle = "#3d8fd6"; c.fillRect(110, 72, 12, 6);
  c.fillStyle = "#8a5a2b"; c.fillRect(60, 30, 44, 10); c.fillStyle = "#56b45f"; c.fillRect(20, 44, 12, 10); c.fillRect(34, 44, 12, 10);
  c.fillStyle = colours(g, null)[1]; c.fillRect(78, 84, 10, 9);
  const f = $("play-flick-art").getContext("2d");
  f.fillStyle = "#1b3a4a"; f.fillRect(0, 0, 168, 96);
  f.fillStyle = "#56b45f"; f.fillRect(54, 84, 60, 8);
  draw(f, cells(g, ["egg", "tadpole"].includes(p.frog.stage) ? "froglet" : p.frog.stage, {}), W, H, colours(g, null), 62, 52, 1);
  f.fillStyle = "#e2567a"; for (let i = 0; i < 14; i++) f.fillRect(84 + i * 3, 70 - i * 3, 2, 2);
  f.fillStyle = "#3a3f46"; f.fillRect(126, 26, 5, 3); f.fillStyle = "#cfe3ea"; f.fillRect(126, 23, 2, 2); f.fillRect(129, 23, 2, 2);
  f.fillStyle = "#ffe27a"; f.fillRect(30, 30, 4, 3); f.fillRect(140, 60, 4, 3);
  (p.arcade || []).forEach((a) => { $("play-" + a.key + "-line").textContent = "Best " + a.best + " · today " + a.paid + "/" + a.cap; });
  const w = $("play-wheel-art").getContext("2d");
  w.fillStyle = "#0e2c30"; w.fillRect(0, 0, 168, 96);
  for (let y = 0; y < 96; y += 4) for (let x = 0; x < 168; x += 4) {
    const dx = (x + 2 - 84) / 44, dy = (y + 2 - 48) / 44, r = dx * dx + dy * dy;
    if (r > 1) continue;
    const a = Math.atan2(dy, dx), sl = Math.floor(((a + Math.PI) / (Math.PI * 2)) * 16);
    w.fillStyle = r > 0.86 ? "#1f5a2c" : r < 0.06 ? "#ffe27a" : sl % 2 ? "#3f9a4d" : "#56b45f";
    if (sl === 3 && r > 0.06 && r <= 0.86) w.fillStyle = "#d8659a";
    if (sl === 11 && r > 0.06 && r <= 0.86) w.fillStyle = "#e3b23a";
    w.fillRect(x, y, 4, 4);
  }
  document.querySelectorAll("#logs canvas").forEach((cv, i) => drawLog(cv, logTurned && logPick === i));
}

let logTurned = false, logPick = -1;
$("logs").addEventListener("click", (e) => {
  const b = e.target.closest("button");
  if (!b || b.disabled || logTurned) return;
  const all = document.querySelectorAll("#logs button");
  all.forEach((x) => { x.disabled = true; });
  post("/api/log", { pick: +b.dataset.pick }).then((d) => {
    logTurned = true; logPick = d.pick;
    b.setAttribute("aria-pressed", "true");
    $("log-out").textContent = d.finds.map((f, i) => (i === d.pick ? "You: " : "") + f.pebbles).join(" · ");
    setPond(d.pond);
    drawPlay();
    reveal({ kick: "Under the log", art: iconCanvas("peb", 8), name: d.finds[d.pick].pebbles + " pebbles", line: d.finds[d.pick].text });
  }, (err) => { $("log-out").textContent = err.message; setPondAgain(); });
});

function setPondAgain() {
  fetchedAt = Date.now();
  fetch("/api/pond", { credentials: "same-origin" }).then((r) => r.ok ? r.json() : null).then((p) => { if (p) setPond(p); });
}

// Stall sales, the gravel bar and the garden move on the server's clock, so a
// page left open picks them up on its own. Not in a dive or a spin, which
// hand the pond over when they finish.
function freshen() {
  if (document.hidden || view === "dive" || view === "wheel" || GAMES[view] || Date.now() - fetchedAt < 60000) return;
  setPondAgain();
}
setInterval(freshen, 15000);
document.addEventListener("visibilitychange", freshen);

function greet(g) {
  $("greet-line").textContent = g.line;
  const dl = $("greet-missed");
  dl.textContent = "";
  g.missed.forEach((row) => {
    const dt = document.createElement("dt"), dd = document.createElement("dd");
    dt.textContent = row[0]; dd.textContent = row[1];
    dl.append(dt, dd);
  });
  dl.hidden = g.missed.length === 0;
  if (g.present) {
    const z = g.present, name = titled(g.pond.frog.name);
    $("gift").hidden = false;
    drawBox($("gift-box").getContext("2d"), "#e2567a", 6);
    $("gift-line").textContent = name + " has a present for you.";
    $("gift-open").addEventListener("click", () => {
      $("gift").hidden = true;
      state.held = 0;
      setPond(state.pond);
      reveal({
        kick: "A present from " + name,
        art: z.hat ? thingArt({ hat: z.hat }, 8) : thingArt({ kind: z.item || "peb" }, 8),
        name: z.hat ? z.hat_name : z.pebbles ? z.pebbles + " pebbles" : z.text,
        tier: z.hat || (z.item && z.item.startsWith("t_")) ? z.hat_tier : undefined,
        line: z.hat ? (z.spare ? "A spare, Wren will buy it." : "It's in your bag.") : z.text,
      });
    }, { once: true });
  }
  if (!reduced) greetAt = performance.now();
}

// Made up days for the dev clock's panel, to see each thing on the water.
const DAYS = {
  live: null,
  rain: { has_temp: true, temp: 58, cond: "Rain", rain: 90, sunrise: "6:00am", sunset: "11:59pm", has_sp: true, sp: 0.1 },
  night: { has_temp: true, temp: 64, cond: "Clear", sunrise: "11:58pm", sunset: "11:59pm", has_sp: true, sp: 0.8 },
  cold: { has_temp: true, temp: 31, cond: "Clear", sunrise: "6:00am", sunset: "11:59pm", has_sp: true, sp: -1.2, vix: 30 },
  busy: { has_temp: true, temp: 70, cond: "Sunny", sunrise: "6:00am", sunset: "11:59pm", has_sp: true, sp: 1.1, vix: 23, swarm: true, major: true, down: "Blog", front: "CPI today" },
  storm: { has_temp: true, temp: 66, cond: "Thunderstorms", rain: 100, sunrise: "6:00am", sunset: "11:59pm", alert: "Tornado Warning" },
};

$("dev").addEventListener("click", (e) => {
  const h = e.target.dataset && e.target.dataset.hours, d = e.target.dataset && e.target.dataset.day;
  if (h) post("/api/dev/clock", { hours: +h }).then(() => location.reload());
  if (d) post("/api/dev/day", DAYS[d]).then(() => location.reload());
});

// ---------------------------------------------------------------- boot

drawIcon($("purse-icon").getContext("2d"), "peb", 0, 0, 2);
initUI();
watch(renderPond);
initDive();
initWheel();
initReeds();
initBank();
initShops();
initBag();
initCollect();
initTown();
initNeighbours();
initSettings();
show(location.hash.slice(1));

post("/api/greet").then((g) => {
  if (g.present && g.present.pebbles) state.held = g.present.pebbles;
  setPond(g.pond);
  greet(g);
  requestAnimationFrame(loop);
}, (err) => { $("greet-line").textContent = err.message; });

import { $, post, state, setPond, watch, reduced, titled, TIERS } from "./api.js";
import { genome, bred, cells, sideCells, colours, draw, croak, W, H, SW, SH } from "./frog.js";
import { drawIcon, iconCanvas, drawFurn } from "./icons.js";
import { initDive, enterDive, leaveDive } from "./dive.js";
import { initWheel, enterWheel } from "./wheel.js";
import { initHats } from "./hats.js";
import { initReeds } from "./reeds.js";
import { initBank, enterBank, leaveBank } from "./bank.js";
import { initShops } from "./shops.js";
import { initTrinkets } from "./trinkets.js";
import { initRocks } from "./rocks.js";
import { initKeepsakes } from "./keepsakes.js";

const VIEWS = ["pond", "dive", "wheel", "hats", "reeds", "bank", "shops"];
let view = "pond";

function show(next) {
  if (!VIEWS.includes(next)) next = "pond";
  if (view === "dive" && next !== "dive") leaveDive();
  view = next;
  VIEWS.forEach((v) => { $("v-" + v).hidden = v !== next; });
  document.querySelectorAll("#tabs button").forEach((b) => {
    b.setAttribute("aria-pressed", b.dataset.view === next ? "true" : "false");
  });
  if (next === "dive") enterDive();
  if (next === "wheel") enterWheel();
  if (next === "bank") enterBank(); else leaveBank();
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
const NEXT = {
  egg: "A tadpole tomorrow.", tadpole: "A froglet on day 9.", froglet: "A frog on day 13.",
  frog: "An elder on day 29.", elder: "Here for good.",
};
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
function drawBehind(day, t) {
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
    if (!day.cold && !day.rain) for (let i = 0; i < 7; i++) {
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

function renderPond(p) {
  const f = p.frog;
  $("frog-name").textContent = titled(f.name);
  const tier = $("frog-tier");
  tier.textContent = TIERS[f.rarity.tier]; tier.className = "tier t" + f.rarity.tier;
  $("frog-odds").textContent = "1 in " + f.rarity.n.toLocaleString("en-US");
  const stage = f.stage.charAt(0).toUpperCase() + f.stage.slice(1);
  // An egg doesn't say what colour or back it'll have. That's for hatching.
  const look = f.stage === "egg" ? "" : titled(f.morph) + ", " + f.pattern.toLowerCase() + " back, ";
  const what = look + f.nature.toLowerCase() + ".";
  $("frog-what").textContent = stage + ", day " + f.day + ". " + NEXT[f.stage] + " " + what.charAt(0).toUpperCase() + what.slice(1);

  $("purse").hidden = false;
  $("purse-n").textContent = (p.pebbles - state.held).toLocaleString("en-US");
  $("nudge").hidden = !p.nudge;
  const lines = (p.day && p.day.lines) || [], today = $("today");
  today.textContent = "";
  lines.forEach((row) => {
    const dt = document.createElement("dt"), dd = document.createElement("dd");
    dt.textContent = row[0]; dd.textContent = row[1];
    today.append(dt, dd);
  });
  $("today-head").hidden = today.hidden = lines.length === 0;
  $("power-n").textContent = p.share.power.total.toLocaleString("en-US");
  $("power-rank").textContent = p.share.power.rank;
  $("share-link").href = p.share.path;
  $("dev").hidden = !p.dev;

  if (!logTurned) {
    document.querySelectorAll("#logs button").forEach((b) => { b.disabled = !p.log.ready; });
    if (!p.log.ready) $("log-out").textContent = "Turned over for today. It's back tomorrow.";
  }

  const box = $("things");
  box.textContent = "";
  $("things-none").hidden = p.items.length > 0;
  p.items.forEach((it) => {
    const li = document.createElement("li"), name = document.createElement("span");
    name.textContent = it.name + (it.count > 1 ? " x" + it.count : "");
    li.append(iconCanvas(it.kind, 3), name);
    if (it.kind === "geode") {
      const b = document.createElement("button");
      b.type = "button"; b.textContent = "Crack";
      b.addEventListener("click", () => {
        b.disabled = true;
        post("/api/geode").then((d) => { $("thing-out").textContent = d.prize.text; setPond(d.pond); },
          (e) => { $("thing-out").textContent = e.message; b.disabled = false; });
      });
      li.append(b);
    }
    box.append(li);
  });
}

let logTurned = false;
$("logs").addEventListener("click", (e) => {
  const b = e.target.closest("button");
  if (!b || b.disabled || logTurned) return;
  const all = document.querySelectorAll("#logs button");
  all.forEach((x) => { x.disabled = true; });
  post("/api/log", { pick: +b.dataset.pick }).then((d) => {
    logTurned = true;
    all.forEach((x, i) => { x.textContent = d.finds[i].pebbles + " pebbles"; });
    b.setAttribute("aria-pressed", "true");
    $("log-out").textContent = d.finds[d.pick].text;
    setPond(d.pond);
  }, (err) => { $("log-out").textContent = err.message; setPondAgain(); });
});

function setPondAgain() {
  fetch("/api/pond", { credentials: "same-origin" }).then((r) => r.ok ? r.json() : null).then((p) => { if (p) setPond(p); });
}

function hatLine(z) {
  return "A hat. " + z.hat_name + ", " + TIERS[z.hat_tier].toLowerCase() + ", and it's in your hat box" + (z.spare ? " as a spare." : ".");
}

function greet(g) {
  $("greet-line").textContent = g.line;
  const dl = $("greet-missed");
  dl.textContent = "";
  g.missed.forEach((row) => {
    const dt = document.createElement("dt"), dd = document.createElement("dd");
    dt.textContent = row[0]; dd.textContent = row[1];
    dl.append(dt, dd);
  });
  if (g.present) {
    const z = g.present, name = titled(g.pond.frog.name);
    $("gift").hidden = false;
    $("gift-line").textContent = name + " has something for you behind its back.";
    $("gift-open").addEventListener("click", function () {
      this.hidden = true;
      $("gift-line").textContent = z.hat ? hatLine(z) : z.text;
      state.held = 0;
      setPond(state.pond);
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
watch(renderPond);
initDive();
initWheel();
initHats();
initReeds();
initBank();
initShops();
initTrinkets();
initRocks();
initKeepsakes();
show(location.hash.slice(1));

post("/api/greet").then((g) => {
  if (g.present && g.present.pebbles) state.held = g.present.pebbles;
  setPond(g.pond);
  greet(g);
  requestAnimationFrame(loop);
}, (err) => { $("greet-line").textContent = err.message; });

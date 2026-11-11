import { $, post, state, setPond, watch, reduced, titled, heldNow, TIERS } from "./api.js";
import { genome, bred, cells, sideCells, colours, draw, croak, W, H, SW, SH } from "./frog.js";
import { drawIcon, iconCanvas, drawFurn } from "./icons.js";
import { initDive, enterDive, leaveDive } from "./dive.js";
import { arcade } from "./arcade.js";
import { buildPlay, drawDisks, DISKS } from "./play.js";
import { initWheel, enterWheel } from "./wheel.js";
import { initReeds } from "./reeds.js";
import { initJob, enterJob, leaveJob } from "./work.js";
import { initBank, enterBank, leaveBank } from "./bank.js";
import { drawWorks } from "./scenes.js";
import { initShops } from "./shops.js";
import { initUI, openSheet, closeSheet, reveal, thingArt, drawBox } from "./ui.js";
import { sweepArt, shareKey } from "./sweep.js";
import { initBag } from "./bag.js";
import { initCollect } from "./collect.js";
import { initTown, enterTown, leaveTown } from "./town.js";
import { initNeighbours, art as residentArt, presentsFrom } from "./neighbours.js";
import { initSettings } from "./settings.js";
import { initPostcard } from "./postcard.js";
import { drawScenery, drawSceneryNight } from "./scenery.js";
import { initPet, setBox, pose, over as petOver, busy as petBusy, wasRub, tapTrick, arrive, drawBond } from "./pet.js";

buildPlay($("shelves"), $("v-dive"));
const SIDE = DISKS.filter((d) => d.game);
const VIEWS = ["pond", "play", "dive", ...SIDE.map((d) => d.key), "wheel", "bag", "collect", "reeds", "job", "garden", "bank", "town", "settings"];
// Places that open from another light that one's key.
const WORKS = ["reeds", "garden", "bank"];
const TOWN = ["neighbours", "shops", "stall", "wizard"];
// The key in town a waiting thing is behind, by its kind.
const TOWN_KEY = { stall: "stall", favour: "neighbours", gift: "neighbours" };
const TAB_OF = { neighbours: "town", shops: "town", stall: "town", wizard: "town", market: "works", reeds: "works", job: "works", garden: "works", bank: "works", collect: "bag", dive: "play", wheel: "play" };
SIDE.forEach((d) => { TAB_OF[d.key] = "play"; });
let view = "pond", fetchedAt = Date.now(), worksAt = "", waitingAt = new Set();
// What the server last said is open, kept so the keys are right before the
// pond has loaded. A browser with nothing kept is most likely a new egg.
let open = { works: false, market: false }, wanted = "";
try { open = JSON.parse(localStorage.getItem("pond-open")) || open; } catch (e) { /* private window */ }
// A game opened straight off a link has no pond yet when its card is made,
// so the disks are drawn before any game repaints its card off them.
watch(() => { if (view === "dive" || GAMES[view]) drawPlay(); });
const GAMES = Object.fromEntries(SIDE.map((d) => [d.key, arcade(d.game)]));

// The Works key opens the place in it that was open last, and the first time
// one with something waiting.
function placeOf(next) {
  if (next !== "works") return next;
  return worksAt || WORKS.find((w) => waitingAt.has(w)) || WORKS[0];
}

// A key's pip lights for a thing that turned up since the key was last
// opened. What was waiting behind each key then is kept in this browser.
let seen = {}, behind = {};
try { seen = JSON.parse(localStorage.getItem("pond-pips")) || {}; } catch (e) { /* private window */ }

function drawPips() {
  const lit = (key) => (behind[key] || []).some((t) => !(seen[key] || []).includes(t));
  document.querySelectorAll("#tabs button").forEach((b) => b.classList.toggle("wait", lit(b.dataset.view)));
  document.querySelectorAll("#works-tabs button").forEach((b) => b.classList.toggle("wait", lit(b.dataset.go)));
  document.querySelectorAll("#town-tabs button").forEach((b) => b.classList.toggle("wait", lit(b.dataset.town)));
}

// Everything behind the keys that are open now has been seen.
function seeOpen() {
  const keys = [TAB_OF[view] || view];
  if (WORKS.includes(view)) keys.push(view);
  const town = view === "town" && document.querySelector('#town-tabs button[aria-pressed="true"]');
  if (town) keys.push(town.dataset.town);
  keys.forEach((k) => { if (behind[k]) seen[k] = behind[k]; });
  try { localStorage.setItem("pond-pips", JSON.stringify(seen)); } catch (e) { /* private window */ }
  drawPips();
}

// What comes round every day is new each day, and the stall is new again
// each time it sells more. The rest is new until it's been seen once.
const DAILY = ["log", "dive", "arcade", "favour", "gift", "rested"];

function sortPips(p, todo) {
  if (seen.pond !== p.share.path) seen = { pond: p.share.path };
  behind = {};
  const put = (key, t) => { if (key) (behind[key] = behind[key] || []).push(t); };
  todo.forEach((t) => {
    const token = t.kind + ":" + (t.kind === "stall" ? t.label : DAILY.includes(t.kind) ? p.today : ""), tab = TAB_OF[t.to] || t.to;
    put(tab, token);
    put(tab === "works" ? t.to : tab === "town" ? TOWN_KEY[t.kind] : "", token);
  });
  // A thing that went away is new again when it comes back.
  Object.keys(seen).forEach((k) => { if (k !== "pond") seen[k] = [].concat(seen[k]).filter((t) => (behind[k] || []).includes(t)); });
  seeOpen();
}

function isOpen(v) {
  if (v === "market") return open.works && open.market;
  return v === "works" || v === "job" || WORKS.includes(v) ? open.works : true;
}

function bankTab(which) {
  document.querySelectorAll("#bank-tabs button").forEach((x) => x.setAttribute("aria-pressed", x.dataset.bank === which ? "true" : "false"));
  $("bank-works").hidden = which !== "works";
  $("bank-market").hidden = which !== "market";
}

function applyOpen(o) {
  const changed = o.works !== open.works || o.market !== open.market;
  open = { works: o.works, market: o.market };
  document.querySelector('#tabs button[data-view="works"]').hidden = !open.works;
  $("bank-tabs").hidden = !open.market;
  if (!open.market) bankTab("works");
  if (!changed) return;
  try { localStorage.setItem("pond-open", JSON.stringify(open)); } catch (e) { /* private window */ }
  if (!isOpen(view)) show("pond");
  else if (wanted && isOpen(wanted)) { history.replaceState(null, "", "#" + wanted); show(wanted); }
}

function show(next) {
  // A link to a place that isn't open yet lands on the pond, and is kept in
  // case the pond loads and says it's open after all.
  wanted = "";
  if (!isOpen(next)) { wanted = next; next = "pond"; history.replaceState(null, "", location.pathname); }
  const market = next === "market", key = TOWN.includes(next) ? next : "";
  next = placeOf(market ? "bank" : key ? "town" : next);
  if (!VIEWS.includes(next)) next = "pond";
  if (market) bankTab("market");
  if (WORKS.includes(next)) worksAt = next;
  $("works-head").hidden = !WORKS.includes(next);
  document.querySelectorAll("#works-tabs button").forEach((b) => {
    b.setAttribute("aria-pressed", b.dataset.go === next ? "true" : "false");
  });
  if (view === "dive" && next !== "dive") leaveDive();
  if (GAMES[view] && next !== view) GAMES[view].leave();
  view = next;
  VIEWS.forEach((v) => { $("v-" + v).hidden = v !== next; });
  const lit = TAB_OF[next] || next;
  document.querySelectorAll("#tabs button").forEach((b) => {
    b.setAttribute("aria-pressed", b.dataset.view === lit ? "true" : "false");
  });
  $("gear").classList.toggle("lit", next === "settings");
  // A title card copies its art off the Play disks, so they're drawn first.
  if (next === "dive" || GAMES[next]) drawPlay();
  if (next === "dive") enterDive();
  if (GAMES[next]) GAMES[next].enter();
  if (next === "wheel") enterWheel();
  if (next === "play") drawPlay();
  if (next === "garden") enterBank(); else leaveBank();
  if (next === "job") enterJob(); else leaveJob();
  if (next === "town") enterTown(key); else leaveTown();
  seeOpen();
  freshen();
  // A view's name is also the id of its canvas, and the browser scrolls to
  // that when the address changes, a frame after this runs.
  window.scrollTo(0, 0);
  requestAnimationFrame(() => window.scrollTo(0, 0));
}

window.addEventListener("load", () => window.scrollTo(0, 0));

$("tabs").addEventListener("click", (e) => {
  const b = e.target.closest("button"), v = b && placeOf(b.dataset.view);
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

// The pond is never empty: reeds at its edges, a stray pad, a fish now and
// then, and a dragonfly crossing whatever the day.
function drawAmbient(day, t) {
  const sway = Math.round(Math.sin(t * 0.8) * 1);
  [[4, 6], [16, 9], [26, 5], [318, 7], [330, 10], [342, 6]].forEach(([x, h], i) => {
    const top = 272 - h * 12, lean = i % 2 ? sway : -sway;
    sx.fillStyle = "#2f6b33"; sx.fillRect(x, top, 4, h * 12);
    sx.fillStyle = "#3f8a45"; sx.fillRect(x + lean * 4, top - 4, 4, 8);
    if (h > 8) { sx.fillStyle = "#6b4a2a"; sx.fillRect(x + lean * 4, top - 16, 4, 12); }
  });
  const px = 40 + Math.sin(t * 0.07) * 16, py = 36 + Math.cos(t * 0.05) * 6;
  sx.fillStyle = "#1f5a2c"; sx.fillRect(Math.round(px / 4) * 4, Math.round(py / 4) * 4 + 4, 40, 8);
  sx.fillStyle = "#3f9a4d"; sx.fillRect(Math.round(px / 4) * 4 + 4, Math.round(py / 4) * 4, 32, 8);
  sx.fillStyle = WATER[day.water || ""][0]; sx.fillRect(Math.round(px / 4) * 4 + 20, Math.round(py / 4) * 4, 4, 8);
  const ft = t % 9;
  if (ft < 1.6) {
    const n = Math.floor(t / 9), rx = 90 + scatter(n, 11) * 180, ry = 110 + scatter(n, 12) * 80, r = 4 + ft * 18;
    sx.strokeStyle = "rgba(127,214,194," + (0.5 - ft * 0.3) + ")"; sx.lineWidth = 2;
    sx.beginPath(); sx.ellipse(rx, ry, r, r * 0.4, 0, 0, Math.PI * 2); sx.stroke();
  }
}

function dragonfly(t, k) {
  const dx = -40 + k * 440, dy = 40 + Math.sin(k * 9) * 20;
  block(dx, dy, "#3d8fd6", 1); block(dx - 4, dy, "#3d8fd6"); block(dx - 8, dy, "#2b6aa8"); block(dx - 12, dy, "#2b6aa8");
  const up = Math.floor(t * 20) % 2 ? -4 : 4;
  block(dx - 4, dy + up, "rgba(220,240,255,0.7)"); block(dx - 4, dy - up, "rgba(220,240,255,0.7)");
}

// A warm cast on the water at dawn and dusk, by the visitor's own clock.
function tint() {
  const h = new Date().getHours();
  if (h >= 5 && h < 8) return "rgba(255,170,120,0.09)";
  if (h >= 17 && h < 20) return "rgba(255,130,50,0.11)";
  return "";
}

// The mood of the day lays a faint wash over the water.
const MOOD_TINT = {
  bright: "rgba(255,214,120,0.07)",
  gloom1: "rgba(70,80,100,0.10)",
  gloom2: "rgba(60,66,84,0.18)",
  gloom3: "rgba(40,44,60,0.26)",
  jitters: "rgba(180,200,120,0.06)",
  rain: "rgba(90,120,160,0.10)",
  frost: "rgba(220,235,255,0.12)",
  heat: "rgba(255,150,60,0.08)",
  pollen: "rgba(240,220,90,0.10)",
  sombre: "rgba(60,60,64,0.20)",
};

// What the frog is watching or playing today sits beside it on the pad.
function drawHobby(h, t) {
  const x = 262, y = 204;
  if (h.kind === "tv") {
    sx.fillStyle = "#3a2a1c"; sx.fillRect(x, y, 32, 24);
    sx.fillStyle = "#5b4330"; sx.fillRect(x, y, 32, 4);
    const glow = ["#6fb6d8", "#8ad0b0", "#c9a6e0"][Math.floor(t / 1.7) % 3];
    sx.fillStyle = glow; sx.fillRect(x + 4, y + 4, 20, 16);
    sx.fillStyle = "rgba(255,255,255,0.35)"; sx.fillRect(x + 4, y + 4 + (Math.floor(t * 6) % 4) * 4, 20, 2);
    sx.fillStyle = "#c9b48a"; sx.fillRect(x + 26, y + 6, 4, 4); sx.fillRect(x + 26, y + 14, 4, 4);
    sx.fillStyle = "#2a1e14"; sx.fillRect(x + 4, y + 24, 4, 4); sx.fillRect(x + 24, y + 24, 4, 4);
    sx.fillStyle = "#9aa3ad"; sx.fillRect(x + 8, y - 8, 2, 8); sx.fillRect(x + 22, y - 8, 2, 8);
    return;
  }
  sx.fillStyle = "#8a939e"; sx.fillRect(x + 4, y + 4, 24, 20);
  sx.fillStyle = "#5b6470"; sx.fillRect(x + 4, y + 20, 24, 4);
  sx.fillStyle = "#9bbc0f"; sx.fillRect(x + 8, y + 8, 16, 8);
  sx.fillStyle = "#306230"; sx.fillRect(x + 10 + (Math.floor(t * 2) % 3) * 4, y + 12, 4, 2);
  sx.fillStyle = "#2a2a2a"; sx.fillRect(x + 8, y + 18, 4, 2); sx.fillRect(x + 20, y + 18, 4, 2);
}

// And what goes over the frog.
function drawOver(day, t) {
  if (day.water === "clear") for (let i = 0; i < 4; i++) {
    const mx = (scatter(i, 1) * 352 + t * 12 * (i % 2 ? 1 : -1) + 352) % 352, my = 30 + scatter(i, 2) * 90 + Math.sin(t * 3 + i) * 6;
    block(mx, my, "#fff6dc"); if (Math.floor(t * 8 + i) % 2) block(mx, my - 4, "rgba(255,246,220,0.6)");
  }
  if (day.dragonfly) dragonfly(t, (t % 14) / 14);
  else if (t % 45 < 14) dragonfly(t, (t % 45) / 14);
  const warm = !day.night && tint();
  if (warm) { sx.fillStyle = warm; sx.fillRect(0, 0, 352, 272); }
  if (MOOD_TINT[day.mood]) { sx.fillStyle = MOOD_TINT[day.mood]; sx.fillRect(0, 0, 352, 272); }
  if (day.rain) for (let i = 0; i < 26; i++) {
    const rx = scatter(i, 7) * 352, ry = (scatter(i, 8) * 272 + t * 160) % 272;
    sx.fillStyle = "rgba(200,230,240,0.45)"; sx.fillRect(Math.round(rx), Math.round(ry), 2, 8);
  }
  if (day.night) {
    sx.fillStyle = "rgba(4,6,18,0.55)"; sx.fillRect(0, 0, 352, 272);
    if (day.moon && !day.rain) {
      MOON.forEach((r, y) => [...r].forEach((ch, x) => { if (ch !== ".") block(256 + x * 4, 16 + y * 4, ch === "a" ? "#f4f1e6" : "#c9cfd6"); }));
      for (let i = 0; i < 6; i++) {
        const w = 28 - i * 3 + Math.round(Math.sin(t * 1.3 + i) * 3);
        sx.fillStyle = "rgba(230,238,246," + (0.32 - i * 0.04) + ")"; sx.fillRect(Math.round(270 - w / 2), 60 + i * 30, w, 3);
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

// The frog is drawn here first on a frame it's moved, tipped or turned.
const aside = document.createElement("canvas"), fx = aside.getContext("2d");
aside.width = 352; aside.height = 272;
let lastFrame = 0;

// Where the frog is on the scene, so a rub knows its head from its chin.
function frogBox(c, oy) {
  let x0 = W, y0 = H, x1 = 0, y1 = 0;
  for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) if (c[y * W + x]) {
    x0 = Math.min(x0, x); x1 = Math.max(x1, x); y0 = Math.min(y0, y); y1 = Math.max(y1, y);
  }
  return { x0: x0 * 8, y0: y0 * 8 + oy, x1: x1 * 8 + 8, y1: y1 * 8 + 8 + oy };
}

let restedAt = 0;

function drawZs(t, a) {
  for (let i = 0; i < 3; i++) {
    const zt = (t * 0.5 + i / 3) % 1, zx = 250 + zt * 30 + i * 4, zy = 70 - zt * 50;
    sx.globalAlpha = (1 - zt) * a; sx.fillStyle = "#fff6dc";
    sx.fillRect(Math.round(zx), Math.round(zy), 12, 3); sx.fillRect(Math.round(zx) + 6, Math.round(zy) + 3, 3, 3);
    sx.fillRect(Math.round(zx) + 3, Math.round(zy) + 6, 3, 3); sx.fillRect(Math.round(zx), Math.round(zy) + 9, 12, 3);
  }
  sx.globalAlpha = 1;
}

function drawSparkles(t, b) {
  [[b.x0 - 4, b.y0 + 36], [b.x1 - 8, b.y0 + 4], [b.x0 + 36, b.y0 - 16], [b.x1 + 4, b.y0 + 72]].forEach(([x, y], i) => {
    const k = reduced ? (i % 2 ? 1 : 0.5) : (Math.sin(t * 2.4 + i * 1.7) + 1) / 2, c = i % 2 ? "#ffe27a" : "#fff6dc";
    if (k < 0.25) return;
    block(x, y, c);
    if (k > 0.6) [[-4, 0], [4, 0], [0, -4], [0, 4]].forEach(([dx, dy]) => block(x + dx, y + dy, c));
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
  drawAmbient(day, t);
  drawSwimmers(p, day, t);
  const has = {};
  (p.furniture || []).forEach((k) => { has[k] = true; });
  drawFurnBack(has, day, t);
  drawScenery(sx, p.sets, day, t);
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
  const ps = pose(now, Math.min(0.1, (now - lastFrame) / 1000));
  lastFrame = now;
  if (ps.squint) o.blink = true;
  if (ps.open) o.croak = ps.open;
  // Sheltering, it sits low with the pad pulled over it.
  const c = cells(g, f.stage, o), col = colours(g, f.hat), oy = -Math.round(hop) + (day.shelter ? 40 : 0), bx = frogBox(c, oy);
  setBox(bx);
  if (ps.still) draw(sx, c, W, H, col, Math.round(wig), oy, 8);
  else {
    // Drawn aside and laid on whole, since cells drawn turned show seams.
    fx.clearRect(0, 0, 352, 272);
    draw(fx, c, W, H, col, 0, 0, 8);
    const foot = bx.y1 - oy;
    sx.save();
    sx.imageSmoothingEnabled = false;
    sx.translate(176 + ps.x, foot + oy + Math.round(ps.y));
    sx.rotate(ps.tilt); sx.scale(ps.flip, ps.sy);
    sx.drawImage(aside, -176, -foot);
    sx.restore();
  }
  if (grown(f.stage) && day.shelter) drawPad(day);
  // Trinkets sit in front of the frog at the pad's edge.
  if (grown(f.stage) && !day.shelter) {
    if (day.hobby) drawHobby(day.hobby, t);
    if (out[0]) drawIcon(sx, out[0], 40, 236, 4);
    if (out[1]) drawIcon(sx, out[1], 280, 236, 4);
  }
  const dozing = grown(f.stage) && day.cold && !moving && now >= croakUntil;
  if (dozing) drawZs(t, 1);
  if (p.rested && !moving) {
    // Well rested, the last of its Zs drift off and it's left with a sparkle.
    if (!restedAt) restedAt = now;
    const k = (now - restedAt) / 3200;
    if (k < 1 && !dozing && !reduced) drawZs(t, 1 - k);
    drawSparkles(t, bx);
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
  petOver(sx, now, col);
  drawOver(day, t);
  drawFurnNight(has, day, t, grown(f.stage) && !day.shelter);
  drawSceneryNight(sx, p.sets, day);
}

function loop(now) {
  const greeting = greetAt && now - greetAt < 2100;
  if (greetAt && !greeting) { greetAt = 0; arrive(now); }
  if (view === "pond" && (greeting || petBusy(now) || now - lastTick > 110)) { lastTick = now; drawScene(now); }
  requestAnimationFrame(loop);
}

initPet(scene, $("scene-btn"), () => drawScene(performance.now()));
initPostcard(scene, $("snap"), () => drawScene(performance.now()));

$("scene-btn").addEventListener("click", () => {
  if (!state.g || wasRub()) return;
  croakUntil = performance.now() + croak(state.g) * 1000;
  tapTrick(performance.now());
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

  applyOpen(p.open);
  drawBond();
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
  renderPaper(p.gazette);
  renderNext(p);
  renderWorks(p);
  renderPlay(p);
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text) e.textContent = text;
  return e;
}

// The Pond Gazette sits folded on the pond, a strip with the masthead and the
// day's lead, and opens as a sheet.
function renderPaper(g) {
  const fold = $("gazette");
  fold.hidden = !g || !g.lines || g.lines.length === 0;
  if (fold.hidden) return;
  $("gazette-no").textContent = "No. " + g.issue;
  $("gazette-lead").textContent = g.notice || g.lines[0];
}

function paper(g, nb) {
  const page = el("article", "paper");
  const top = el("div", "paper-ear");
  top.append(el("span", "", "Vol. I · No. " + g.issue), el("span", "", "One pebble"));
  const date = el("div", "paper-date");
  date.append(el("span", "", g.date), el("span", "", "Weather: " + g.sky));
  page.append(top, el("h3", "paper-head", "The Pond Gazette"), date);
  if (g.notice) {
    const note = el("p", "paper-biz paper-note");
    note.append(el("b", "", "Opening today"), " " + g.notice);
    page.append(note);
  }
  const [lead, ...rest] = g.lines;
  const who = nb && nb.all[g.photo];
  if (who) {
    const story = el("div", "paper-front"), fig = el("figure", "paper-photo");
    fig.append(residentArt(who, 3), el("figcaption", "", g.pictured ? who.name + ", pictured" : "File photo"));
    story.append(fig, el("p", "paper-lead", lead));
    page.append(story);
  } else page.append(el("p", "paper-lead", lead));
  const cols = el("div", "paper-cols");
  rest.forEach((h) => cols.append(el("p", "paper-story", h)));
  page.append(cols);
  if (g.sweep) page.append(puzzleBox(g.sweep));
  if (g.business) {
    const biz = el("p", "paper-biz");
    biz.append(el("b", "", "Business"), " " + g.business);
    page.append(biz);
  }
  if (g.records && g.records.length) {
    const rec = el("p", "paper-biz");
    rec.append(el("b", "", "Records"), " " + g.records.join(" "));
    page.append(rec);
  }
  return page;
}

// The day's bug sweep is a box in the paper that opens the game, with the
// share key beside it once the day's go is over.
const PUZZLE = { "": "Today's grid is out. Clear the pads and mind the wasps.", play: "Today's grid is part way cleared.", won: "Cleared today. Free goes are open.", lost: "A wasp got there first today. Free goes are open." };

function puzzleBox(n) {
  const box = el("div", "paper-puzzle"), b = el("button", ""), art = el("canvas");
  art.width = 168; art.height = 96;
  sweepArt(art.getContext("2d"));
  b.type = "button";
  b.append(art, el("b", "", "Bug sweep"), el("span", "", PUZZLE[n.state]));
  b.addEventListener("click", () => { closeSheet(); location.hash = "#sweep"; });
  box.append(b);
  if (n.state === "won" || n.state === "lost") box.append(shareKey(n));
  return box;
}

$("gazette").addEventListener("click", () => {
  const p = state.pond;
  openSheet({ bare: true, name: "The Pond Gazette" }, "", (body) => body.append(paper(p.gazette, p.neighbours)));
});

// The picture over Works lights a window for each kind of thing that's ready.
function renderWorks(p) {
  const ready = new Set(p.todo.map((t) => t.kind));
  if (heldNow(p.bank.gravel) >= 1) ready.add("gravel");
  if (heldNow(p.bank.burrows) >= 1) ready.add("rent");
  drawWorks($("works-art"), p, ready);
}

// A tap on the picture opens the place under the finger.
$("works-art").addEventListener("click", (e) => {
  const r = e.target.getBoundingClientRect(), k = (e.clientX - r.left) / r.width;
  location.hash = "#" + (k < 0.2 ? "reeds" : k < 0.41 ? "garden" : "bank");
});

let allRows = false;

function claimRow(st) {
  const b = el("button", "nrow primary", "Claim " + st.prize + " pebbles");
  b.type = "button";
  b.addEventListener("click", () => {
    b.disabled = true;
    post("/api/starter").then((v) => {
      setPond(v);
      reveal({ kick: "First day done", art: thingArt({ kind: "peb" }, 8), name: "+" + st.prize + " pebbles", line: "That's the pond. It keeps going while you're away." });
    }, () => { b.disabled = false; });
  });
  return b;
}

function nextRow(r) {
  const a = el("a", "nrow" + (r.goal ? " goal" : "") + (r.done ? " done" : ""));
  a.href = r.to === "key" ? "/key" : "#" + ((r.to === "town" && TOWN_KEY[r.kind]) || r.to);
  a.append(iconCanvas(r.icon, 2), el("span", "nrow-what", r.label));
  if (r.count) a.append(el("span", "nrow-n", r.count));
  return a;
}

// The Next strip is what's waiting in amber and then goals in green, three
// rows and a key for the rest. The server decides both lists, and the gravel
// bar and rent count up here between fetches.
function renderNext(p) {
  const live = { gravel: [heldNow(p.bank.gravel), " on the gravel bar"], rent: [heldNow(p.bank.burrows), " in rent"] };
  const counts = Object.entries(live).filter(([, [n]]) => n >= 1).map(([kind, [n, what]]) => ({ kind, icon: "peb", label: n + what, to: "bank" }));
  // A suggestion to save up only stands when nothing else is waiting.
  const todo = counts.concat(p.todo.filter((t) => !live[t.kind] && !(counts.length && t.kind === "save")));

  waitingAt = new Set(todo.map((t) => t.to));
  // The gravel bar and the rent always have a little, so they're news when full.
  const full = { gravel: p.bank.gravel, rent: p.bank.burrows };
  sortPips(p, todo.filter((t) => !full[t.kind] || heldNow(full[t.kind]) >= full[t.kind].holds));

  // A goal stands in for the waiting row that says the same thing.
  const goals = p.goals.map((g) => ({ ...g, goal: true })), ready = p.starter && p.starter.ready;
  const said = (t) => goals.some((g) => !g.done && (g.kind === t.kind || g.label === t.label));
  const first = ready ? [] : goals.filter((g) => g.first);
  const rows = first.filter((g) => !g.done).concat(first.filter((g) => g.done), todo.filter((t) => !said(t)), goals.filter((g) => !g.first));

  const box = $("nextup");
  box.textContent = "";
  if (ready) box.append(claimRow(p.starter));
  const room = ready ? 2 : 3, shown = allRows || rows.length <= room ? rows : rows.slice(0, room);
  shown.forEach((r) => box.append(nextRow(r)));
  if (shown.length < rows.length) {
    // The key for the rest sits on the end of the last row, so the strip
    // is never taller than its rows.
    const n = rows.length - shown.length, more = el("button", "nrow more", "+" + n), last = el("div", "nrow-last");
    more.type = "button";
    more.setAttribute("aria-label", "Show " + n + " more");
    more.addEventListener("click", () => { allRows = true; renderNext(state.pond); });
    last.append(box.lastElementChild, more);
    box.append(last);
  }
  if (!rows.length && !ready) box.append(el("p", "dim", "That's everything for today. The pond keeps going while you're away."));
}
setInterval(() => {
  if (!state.pond || document.hidden) return;
  renderNext(state.pond);
  renderWorks(state.pond);
}, 20000);

// ---------------------------------------------------------------- play

function renderPlay(p) {
  payLine("dive", p.dive);
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

// Each game card says whether a run still pays in full today, and lights up
// when it does, so the ones worth playing stand out.
function payLine(key, a) {
  const left = a.paid >= a.cap ? 0 : Math.max(0, a.full - a.runs);
  const tag = document.createElement("span");
  tag.className = "pay " + (left ? "full" : a.paid >= a.cap || a.daily ? "done" : "less");
  tag.textContent = left ? (a.daily ? "Today's grid" : "Full pay · " + left + " left") : a.paid >= a.cap ? "Paid out today" : a.daily ? "Free play" : "Quarter pay";
  $("play-" + key + "-line").replaceChildren(tag, document.createTextNode("Best " + a.best + (a.daily ? " pads" : "") + " · today " + a.paid + "/" + a.cap));
  $("play-" + key).classList.toggle("pays", left > 0);
  // The disk of the day has a sticker stuck on it.
  const floppy = $("play-" + key).querySelector(".floppy"), had = floppy.querySelector(".daytag");
  if (a.day && !had) {
    const tag = document.createElement("span");
    tag.className = "daytag"; tag.textContent = "Disk of the day";
    floppy.append(tag);
  } else if (!a.day && had) had.remove();
}

function drawPlay() {
  const p = state.pond, g = state.g;
  if (!p || !g) return;
  drawDisks(g, p);
  (p.arcade || []).forEach((a) => payLine(a.key, a));
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
        line: z.hat ? (z.spare ? "A spare, Little Tully will buy it." : "It's in your bag.") : z.text,
      });
    }, { once: true });
  }
  // A best friend from before there were mementos hands its one over now.
  if (g.prizes && g.prizes.length) presentsFrom(g);
  if (!reduced) greetAt = performance.now();
  else arrive(performance.now());
}

// Made up days for the dev clock's panel, to see each thing on the water.
const DAYS = {
  live: null,
  rain: { has_temp: true, temp: 58, cond: "Rain", rain: 90, sunrise: "6:00am", sunset: "11:59pm", has_sp: true, sp: 0.1 },
  night: { has_temp: true, temp: 64, cond: "Clear", sunrise: "11:58pm", sunset: "11:59pm", has_sp: true, sp: 0.8 },
  cold: { has_temp: true, temp: 31, cond: "Clear", sunrise: "6:00am", sunset: "11:59pm", has_sp: true, sp: -1.2, vix: 30 },
  busy: { has_temp: true, temp: 70, cond: "Sunny", sunrise: "6:00am", sunset: "11:59pm", has_sp: true, sp: 1.1, vix: 23, swarm: true, major: true, down: "Blog", front: "CPI today" },
  gloom: { has_temp: true, temp: 60, cond: "Cloudy", sunrise: "6:00am", sunset: "11:59pm", has_sp: true, sp: -2.8, has_hl: true, high: 62, low: 45, tomorrow: { high: 58, low: 40, rain: 80, cond: "Rain" } },
  hobby: { has_temp: true, temp: 70, cond: "Clear", sunrise: "6:00am", sunset: "11:59pm", has_sp: true, sp: 0.7, has_hl: true, high: 74, low: 50, tomorrow: { high: 70, low: 31, rain: 0, cond: "Clear" }, shows: ["Bluey", "The Great British Baking Show"], games: ["Stardew Valley", "Unpacking"] },
  moon: { has_temp: true, temp: 62, cond: "Clear", sunrise: "11:58pm", sunset: "11:59pm", moon: true },
  spawn: { has_temp: true, temp: 55, cond: "Rain", rain: 80, sunrise: "6:00am", sunset: "11:59pm", spawn: true },
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
initJob();
initBank();
initShops();
initBag();
initCollect();
document.querySelectorAll(".places button[data-go]").forEach((b) => b.addEventListener("click", () => { location.hash = "#" + b.dataset.go; }));
document.querySelectorAll("#bank-tabs button").forEach((b) => b.addEventListener("click", () => bankTab(b.dataset.bank)));
initTown();
$("town-tabs").addEventListener("click", seeOpen);
initNeighbours();
initSettings();
applyOpen(open);
show(location.hash.slice(1));

post("/api/greet").then((g) => {
  if (g.present && g.present.pebbles) state.held = g.present.pebbles;
  setPond(g.pond);
  greet(g);
  requestAnimationFrame(loop);
}, (err) => { $("greet-line").textContent = err.message; });

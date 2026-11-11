import { post, state, setPond, reduced } from "./api.js";
import { croak } from "./frog.js";
import { sfx } from "./sound.js";

// Rubbing the frog, the hearts it throws and the thing it does at each
// heart. The sprite never changes, the scene draws it moved, turned or
// tipped by pose() and over() draws what goes on top of it.
const HEART = ["0110110", "1111111", "1111111", "0111110", "0011100", "0001000"];
const NOTE = ["0011", "0010", "0010", "1110", "1110"];
const LEN = { wave: 1400, hop: 700, spin: 900, song: 1900, leap: 1100, peek: 1600, stars: 1700, snack: 1300, bubble: 1900, wobble: 600 };
// What a frog does at four hearts, by its nature.
const FOURTH = { SINGER: "song", BOLD: "leap", SHY: "peek", "NIGHT OWL": "stars", GLUTTON: "snack", DRIFTER: "bubble" };

let box = { x0: 96, y0: 80, x1: 256, y1: 250 }, bits = [];
let trick = null, turn = 0, squintUntil = 0, busyUntil = 0;
let press = null, lastPet = 0, lastRub = 0, asking = false, streak = 0;
let follow = null, fx = 0;
let redraw = () => {};

function hearts() { return (state.pond && state.pond.bond.hearts) || 0; }
function stage() { return state.pond ? state.pond.frog.stage : "egg"; }
function limbs() { return ["froglet", "frog", "elder"].includes(stage()); }

// The scene says where the frog is each frame, so a rub knows what it's on.
export function setBox(b) { box = b; }

export function busy(now) { return now < busyUntil || !!follow || Math.abs(fx) > 0.5; }

function keep(now, ms) { busyUntil = Math.max(busyUntil, now + ms); }

function tricks() {
  const n = hearts(), list = [];
  if (n >= 1) list.push("wave");
  if (n >= 2) list.push("hop");
  if (n >= 3) list.push("spin");
  if (n >= 4) list.push(FOURTH[state.pond.frog.nature] || "song");
  return list;
}

export function doTrick(kind, now) {
  if (!limbs()) kind = "wobble";
  trick = { kind, at: now, len: LEN[kind] };
  if (kind === "song" && state.g) trick.len = Math.max(LEN.song, croak(state.g) * 1000 + 500);
  if (kind === "hop" || kind === "leap") sfx("hop");
  keep(now, trick.len + 200);
  redraw();
  if (reduced) setTimeout(redraw, trick.len + 60);
}

// A tap does the newest thing it knows first and then works back round.
export function tapTrick(now) {
  const list = tricks();
  if (!list.length) { if (stage() === "egg") doTrick("wobble", now); return; }
  turn = (turn + list.length - 1) % list.length;
  doTrick(list[turn], now);
}

// It waves when you turn up, once it knows you at all.
export function arrive(now) {
  if (hearts() >= 1 && stage() !== "egg") doTrick("wave", now);
}

function throwHearts(x, y, n, now) {
  for (let i = 0; i < n; i++) {
    bits.push({ x: x + (Math.random() - 0.5) * 60, y: y - Math.random() * 20, vx: (Math.random() - 0.5) * 30, at: now + i * 70, life: 900 + Math.random() * 400, pale: i % 2 });
  }
  keep(now, 1600);
}

function spotAt(x, y) {
  if (stage() === "egg") return "egg";
  const ry = (y - box.y0) / (box.y1 - box.y0), rx = Math.abs(x - (box.x0 + box.x1) / 2) / ((box.x1 - box.x0) / 2);
  return ry < 0.4 ? "head" : rx < 0.4 ? "chin" : "back";
}

function petted(x, y, now) {
  squintUntil = now + 900;
  sfx("kick", ++streak);
  if (stage() === "egg") doTrick("wobble", now);
  else throwHearts(x, box.y0 + 10, 2, now);
  redraw();
  const b = state.pond.bond;
  if (asking || now - lastPet < 450 || (b.left <= 0 && b.hearts < 5) || b.hearts >= 5) return;
  asking = true; lastPet = now;
  post("/api/frogs/pet", { spot: spotAt(x, y) }).then((d) => {
    asking = false;
    const t = performance.now();
    state.pond.bond = d.bond;
    drawBond();
    if (d.fav && stage() !== "egg") throwHearts((box.x0 + box.x1) / 2, box.y0, 5, t);
    if (d.heart) {
      throwHearts((box.x0 + box.x1) / 2, box.y0 + 30, 9, t);
      sfx("land");
      turn = 0;
      tapTrick(t);
      // A heart is worth power, so the rest of the page wants the new pond.
      fetch("/api/pond", { credentials: "same-origin" }).then((r) => r.ok ? r.json() : null).then((p) => { if (p) setPond(p); });
    }
  }, () => { asking = false; });
}

function at(e, scene) {
  const r = scene.getBoundingClientRect();
  return [(e.clientX - r.left) * 352 / r.width, (e.clientY - r.top) * 272 / r.height];
}

function onFrog(x, y, pad) {
  return x > box.x0 - pad + fx && x < box.x1 + pad + fx && y > box.y0 - pad && y < box.y1 + pad;
}

// A stroke pets once for every RUB it travels over the frog, in scene units.
const RUB = 90, TAP = 10;

// A press on the frog is a pet, so a tap counts and a stroke any way pets as
// it goes. One that starts off the frog leaves the page to scroll.
export function initPet(scene, btn, again, tap) {
  redraw = again;
  btn.addEventListener("touchstart", (e) => {
    const t = e.touches[0];
    if (state.pond && e.touches.length === 1 && onFrog(...at(t, scene), 8)) e.preventDefault();
  }, { passive: false });
  btn.addEventListener("contextmenu", (e) => e.preventDefault());
  btn.addEventListener("pointerdown", (e) => {
    if (!state.pond || !e.isPrimary) return;
    const [x, y] = at(e, scene);
    streak = 0;
    press = { x, y, lx: x, ly: y, moved: 0, path: 0, frog: onFrog(x, y, 8) };
    if (!press.frog && hearts() >= 5 && limbs()) follow = { x };
    try { btn.setPointerCapture(e.pointerId); } catch (err) { /* an old browser */ }
  });
  btn.addEventListener("pointermove", (e) => {
    if (!press || !e.isPrimary) return;
    const [x, y] = at(e, scene), now = performance.now(), d = Math.hypot(x - press.lx, y - press.ly);
    press.lx = x; press.ly = y;
    press.moved = Math.max(press.moved, Math.hypot(x - press.x, y - press.y));
    if (follow) { follow.x = x; return; }
    if (!press.frog || !onFrog(x, y, 28)) return;
    press.path += d;
    squintUntil = now + 700; keep(now, 800);
    if (press.path >= RUB && now - lastRub > 300) { press.path = 0; lastRub = now; petted(x, y, now); }
  });
  btn.addEventListener("pointerup", (e) => {
    if (!press || !e.isPrimary) return;
    const p = press, now = performance.now();
    press = null; follow = null;
    keep(now, 900);
    if (p.moved >= TAP) return;
    if (p.frog) petted(p.x, p.y, now);
    tap();
  });
  btn.addEventListener("pointercancel", () => { press = null; follow = null; });
  // Only a key press arrives as a click with no pointer before it.
  btn.addEventListener("click", (e) => { if (e.detail === 0) tap(); });
}

// pose is how the frog is drawn this frame: moved by x and y, tipped by
// tilt, turned by flip and pressed by sy, with its eyes shut or mouth open.
export function pose(now, dt) {
  const p = { x: 0, y: 0, tilt: 0, flip: 1, sy: 1, squint: now < squintUntil, open: 0, still: true };
  if (follow) {
    const want = Math.max(-48, Math.min(48, follow.x - 176));
    fx += (want - fx) * Math.min(1, dt * 6);
    if (!reduced && Math.abs(want - fx) > 6) { p.y = -Math.abs(Math.sin(now / 90)) * 12; p.tilt = Math.sign(want - fx) * 0.05; }
  } else if (fx) {
    fx += (0 - fx) * Math.min(1, dt * 5);
    if (Math.abs(fx) < 0.5) fx = 0;
    else if (!reduced) p.y = -Math.abs(Math.sin(now / 90)) * 8;
  }
  p.x = Math.round(fx / 4) * 4;
  if (trick && now - trick.at >= trick.len) trick = null;
  if (trick) {
    const k = (now - trick.at) / trick.len, kind = trick.kind;
    if (kind === "song") p.open = 0.55 + 0.45 * Math.abs(Math.sin(now / 1000 * state.g.rate * 0.5));
    if (kind === "stars" && k < 0.45) { p.open = 1; p.squint = true; }
    if (kind === "peek" || (kind === "snack" && k > 0.5)) p.squint = true;
    if (reduced) { if (kind === "hop" || kind === "spin" || kind === "leap") p.squint = true; }
    else if (kind === "wave") p.tilt = Math.sin(k * Math.PI * 4) * 0.05 * (1 - k);
    else if (kind === "wobble") p.tilt = Math.sin(k * Math.PI * 6) * 0.09 * (1 - k);
    else if (kind === "hop") { p.y = -Math.sin(Math.min(1, k / 0.8) * Math.PI) * 40; if (k > 0.8) p.sy = 1 - 0.16 * Math.sin((k - 0.8) / 0.2 * Math.PI); }
    else if (kind === "spin") { p.flip = Math.cos(k * Math.PI * 2); p.y = -Math.sin(k * Math.PI) * 16; }
    else if (kind === "leap") { p.y = -Math.sin(Math.min(1, k / 0.85) * Math.PI) * 72; p.flip = Math.cos(Math.min(1, k / 0.85) * Math.PI * 2); if (k > 0.85) p.sy = 1 - 0.2 * Math.sin((k - 0.85) / 0.15 * Math.PI); }
    else if (kind === "peek") p.sy = 1 - 0.22 * Math.sin(Math.min(1, k * 4) * Math.PI / 2) * (k > 0.8 ? (1 - k) / 0.2 : 1);
  }
  p.still = !p.x && !p.y && !p.tilt && p.flip === 1 && p.sy === 1;
  return p;
}

function px(ctx, x, y, c, n) {
  ctx.fillStyle = c;
  ctx.fillRect(Math.round(x / 4) * 4, Math.round(y / 4) * 4, (n || 1) * 4, (n || 1) * 4);
}

function bitmap(ctx, rows, x, y, s, c) {
  ctx.fillStyle = c;
  rows.forEach((row, r) => [...row].forEach((ch, i) => { if (ch === "1") ctx.fillRect(Math.round(x) + i * s, Math.round(y) + r * s, s, s); }));
}

// over is what's drawn on top of the frog, the hearts off a rub and the
// parts of a trick the sprite doesn't have.
export function over(ctx, now, col) {
  const mid = (box.x0 + box.x1) / 2 + Math.round(fx / 4) * 4, mouth = box.y0 + (box.y1 - box.y0) * 0.56;
  if (trick) {
    const k = (now - trick.at) / trick.len, kind = trick.kind, fade = Math.min(1, (1 - k) * 5);
    ctx.globalAlpha = fade;
    if (kind === "wave") {
      // An arm up at one side and a hand going back and forth on the end of it.
      const ax = box.x1 + fx - 28, ay = box.y0 + 96, sway = reduced ? 0 : Math.round(Math.sin(k * Math.PI * 8) * 2) * 4;
      const arm = [0, 1, 2, 3].map((i) => [ax + i * 10 + (sway * i) / 3, ay - i * 18]);
      arm.forEach(([x, y]) => px(ctx, x - 4, y - 4, col[9], 4));
      px(ctx, arm[3][0] - 8, arm[3][1] - 16, col[9], 6);
      [-8, 0, 8].forEach((d) => px(ctx, arm[3][0] + d, arm[3][1] - 20, col[9], 2));
      arm.forEach(([x, y]) => px(ctx, x, y, col[1], 2));
      px(ctx, arm[3][0] - 4, arm[3][1] - 12, col[1], 4);
      [-8, 0, 8].forEach((d) => px(ctx, arm[3][0] + d + 4, arm[3][1] - 16, col[1]));
    } else if (kind === "song") {
      for (let i = 0; i < 5; i++) {
        const nt = reduced ? 0.3 + i * 0.12 : k * 1.6 - i * 0.18;
        if (nt <= 0 || nt >= 1) continue;
        ctx.globalAlpha = fade * (1 - nt);
        bitmap(ctx, NOTE, box.x1 + fx - 52 + i * 12 + Math.sin(nt * 7 + i) * 8, box.y0 + 36 - nt * 80, 4, i % 2 ? "#ffe27a" : "#fff6dc");
      }
    } else if (kind === "peek") {
      px(ctx, mid - 56, mouth - 8, "#f59ab3", 2); px(ctx, mid + 48, mouth - 8, "#f59ab3", 2);
    } else if (kind === "stars") {
      for (let i = 0; i < 6; i++) {
        const a = i * 1.05 + (reduced ? 0 : now / 900), on = reduced || Math.sin(now / 130 + i * 2) > -0.2;
        if (k > 0.4 && on) px(ctx, mid + Math.cos(a) * 84, box.y0 + 8 + Math.sin(a) * 26, i % 2 ? "#ffe27a" : "#fff6dc", 1 + (i % 2));
      }
    } else if (kind === "snack") {
      const bx = mid + 92, by = box.y0 - 6 + (reduced ? 0 : Math.sin(now / 60) * 4);
      if (k < 0.45) { px(ctx, bx, by, "#0a0908", 2); px(ctx, bx - 4, by - 4, "#cfe3ea"); px(ctx, bx + 8, by - 4, "#cfe3ea"); }
      if (k > 0.28 && k < 0.6) {
        const reach = k < 0.45 ? (k - 0.28) / 0.17 : 1 - (k - 0.45) / 0.15;
        for (let i = 0; i <= 12 * reach; i++) px(ctx, mid + (bx - mid) * i / 12, mouth + (by - mouth) * i / 12, "#e2567a");
      }
    } else if (kind === "bubble") {
      const r = k < 0.4 ? 6 + k * 40 : 22, by = k < 0.4 ? mouth : mouth - (k - 0.4) * 190;
      if (k < 0.86) {
        ctx.strokeStyle = "rgba(207,227,234,0.9)"; ctx.lineWidth = 4;
        ctx.beginPath(); ctx.arc(mid + 40, by, r, 0, Math.PI * 2); ctx.stroke();
        px(ctx, mid + 40 - r * 0.4, by - r * 0.4, "#ffffff");
      } else for (let i = 0; i < 6; i++) px(ctx, mid + 40 + Math.cos(i) * (k - 0.86) * 260, by + Math.sin(i) * (k - 0.86) * 260, "#cfe3ea");
    }
    ctx.globalAlpha = 1;
  }
  bits = bits.filter((b) => now - b.at < b.life);
  bits.forEach((b) => {
    const t = (now - b.at) / b.life;
    if (t < 0) return;
    ctx.globalAlpha = 1 - t;
    bitmap(ctx, HEART, b.x + (reduced ? 0 : b.vx * t), b.y - (reduced ? 20 : t * 70), 4, b.pale ? "#f59ab3" : "#e2567a");
  });
  ctx.globalAlpha = 1;
}

// The hearts sit in the corner of the screen, the one being worked on
// filled from the left as far as it's got.
export function drawBond() {
  const cv = document.getElementById("bond"), b = state.pond && state.pond.bond;
  if (!cv || !b) return;
  const ctx = cv.getContext("2d");
  ctx.clearRect(0, 0, cv.width, cv.height);
  for (let i = 0; i < 5; i++) {
    const lit = i < b.hearts ? 7 : i === b.hearts ? Math.round(b.fill * 7 / 100) : 0;
    HEART.forEach((row, r) => [...row].forEach((ch, c) => {
      if (ch !== "1") return;
      ctx.fillStyle = c < lit ? "#e2567a" : "rgba(255,246,220,0.3)";
      ctx.fillRect(i * 8 + c, r, 1, 1);
    }));
  }
  cv.setAttribute("aria-label", b.hearts + " of 5 hearts");
}

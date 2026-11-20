import { $, post, setPond, reduced } from "./api.js";
import { iconCells, iconCanvas } from "./icons.js";
import { px, glow, lantern, rng, trees, hills } from "./paint.js";
import { sfx, chime } from "./sound.js";

// The rain barrel on Play. A stream runs off the bank only while it really
// rains, through a sieve that catches what it carries, and a spout into the
// barrel. Tipping it lifts the sieve's catch out and pours the barrel away.
const SCENE = 150, H = 60, GROUND = 54;
const PENTA = [0, 2, 4, 7, 9, 12, 14, 16, 19, 21, 24, 26, 28, 31, 33];
const SIEVE = { x: 26, y: 20, w: 31 };
const BARREL = { x: 72, y: 24, w: 32, h: 34 };
const SPROUTS = [120, 128, 136, 144];

const art = document.createElement("canvas");
const g = art.getContext("2d");
const stave = document.createElement("canvas");
stave.width = BARREL.w; stave.height = BARREL.h;
const sg = stave.getContext("2d");

// W is the picture's width in its own pixels, the scene kept in the middle
// and the sky and ground carried on to fill the frame.
let W = SCENE, ox = 0;
let pond = null, anim = null, raf = 0, last = 0, haulLine = "", haulDay = "";

function bankTop(x) { return Math.max(4, 12 + x * 0.3); }

// The colour most of an icon is, so a thing in the mesh reads as itself.
const tint = {};
function colourOf(kind) {
  if (!tint[kind]) {
    const n = {};
    iconCells(kind).forEach((c) => { if (c && c !== "#0a0d16") n[c] = (n[c] || 0) + 1; });
    tint[kind] = Object.keys(n).sort((a, b) => n[b] - n[a])[0] || "#9aa6c2";
  }
  return tint[kind];
}

// Where the catch sits in the mesh, a spot each, the same spots every look.
function catchSpots(b) {
  const out = [];
  for (let i = 0; i < Math.min(7, Math.ceil(b.pebbles / 3)); i++) out.push("#b9c3cf");
  b.things.forEach((t) => { for (let i = 0; i < Math.min(3, t.n); i++) out.push(colourOf(t.kind)); });
  return out.slice(0, 12).map((c, i) => ({ c, x: SIEVE.x + 2 + ((i * 9) % (SIEVE.w - 5)), y: SIEVE.y - 1 + ((i * 2) % 3) }));
}

function backdrop(t, wet) {
  const r = rng(7);
  px(g, wet ? "#151c2e" : "#0d1428", 0, 0, W, H);
  if (!wet) for (let i = 0; i < W / 5; i++) px(g, r() < 0.25 ? "#ffffff" : "#5a6aa8", r() * W, r() * 34);
  else for (let i = 0; i < W / 12; i++) px(g, "#1c2438", r() * W, r() * 20, 6 + r() * 10, 2);
  hills(g, W, 40, 2, wet ? "#131a28" : "#0f1730");
  trees(g, W, 44, rng(3), wet ? "#0f1522" : "#0a1128");
  px(g, "#15241a", 0, GROUND, W, H - GROUND);
  px(g, "#203624", 0, GROUND, W, 1);
}

function bank(t) {
  for (let x = -ox; x < 36; x++) {
    const top = Math.round(x < 30 ? bankTop(x) : bankTop(30) + (x - 30) * 6);
    px(g, "#18281c", x, top, 1, H - top);
    px(g, "#2a4a2e", x, top, 1, 1);
    if (x % 7 === 3 && x < 28) px(g, "#2a4a2e", x, top - 2, 1, 2);
  }
  lantern(g, 6, 4, 10, 0.4 + (reduced ? 0 : Math.sin(t * 3.1) * 0.03));
}

// The stream is a groove down the bank, mud when it's dry and running water
// with pale streaks while it rains.
function stream(t, running) {
  for (let x = -ox; x < SIEVE.x + 2; x++) {
    const top = Math.round(bankTop(x)) + 1;
    px(g, running ? "#2f7fa8" : "#2a2018", x, top, 1, 2);
    if (running && Math.floor(x - t * 24) % 7 === 0) px(g, "#a8e0f0", x, top, 1, 1);
    if (!running && (x + 60) % 6 === 2) px(g, "#4a3e30", x, top + 1, 1, 1);
  }
  for (let x = SIEVE.x + SIEVE.w; x < BARREL.x + 4; x++) {
    const y = Math.round(SIEVE.y + 4 + (x - SIEVE.x - SIEVE.w) * 0.12);
    px(g, "#5a3c22", x, y + 1, 1, 1); px(g, "#a87440", x, y + 2, 1, 1);
    if (running) { px(g, "#2f7fa8", x, y, 1, 1); if (Math.floor(x - t * 30) % 5 === 0) px(g, "#a8e0f0", x, y, 1, 1); }
  }
  if (running) for (let y = SIEVE.y + 8; y < BARREL.y + 3; y++) if (Math.floor(y + t * 40) % 3) px(g, "#6fc0e0", BARREL.x + 4, y, 1, 1);
}

function sieve(lift, spots, running, t) {
  const { x, w } = SIEVE, y = SIEVE.y - lift;
  px(g, "#3a2a1a", x + 3, y + 6, 2, GROUND - y - 6); px(g, "#3a2a1a", x + w - 5, y + 6, 2, GROUND - y - 6);
  px(g, "#c48a4a", x, y, w, 1);
  for (let j = 1; j < 5; j++) for (let i = 1; i < w - 1; i++) px(g, (i + j) % 2 ? "#3a4250" : "#8a96a6", x + i, y + j, 1, 1);
  px(g, "#a87440", x, y + 1, 1, 4); px(g, "#a87440", x + w - 1, y + 1, 1, 4);
  px(g, "#6b4a2a", x, y + 5, w, 2);
  if (running) for (let i = 2; i < w - 2; i += 3) if (Math.floor(i + t * 20) % 4 === 0) px(g, "#6fc0e0", x + i, y + 3, 1, 1);
  spots.forEach((s) => { px(g, "#0a0d16", s.x - 1, s.y - lift + 1, 5, 3); px(g, s.c, s.x, s.y - lift, 3, 3); px(g, "#ffffff", s.x, s.y - lift, 1, 1); });
}

// The barrel is drawn on its own small canvas so it can be tipped whole,
// with the water in its mouth as high as it's full.
function drawStave(fill, running, t) {
  const { w, h } = BARREL;
  sg.clearRect(0, 0, w, h);
  for (let x = 0; x < w; x++) {
    const bulge = Math.round(Math.sin((x / (w - 1)) * Math.PI) * 1.4);
    const c = x % 6 === 0 ? "#4a3220" : x < 4 ? "#5a3e24" : x > w - 5 ? "#4a3220" : x % 6 === 2 ? "#a87440" : "#8a6230";
    px(sg, c, x, 4 - bulge, 1, h - 6 + bulge * 2);
  }
  [10, 21, 29].forEach((y) => { px(sg, "#2a2f3a", 0, y, w, 2); px(sg, "#9aa6b8", 1, y, w - 2, 1); });
  px(sg, "#3a3f4a", 4, 15, 2, 2);
  const cy = 4, rx = (w - 1) / 2, ry = 4;
  for (let y = -ry; y <= ry; y++) {
    const half = Math.round(rx * Math.sqrt(Math.max(0, 1 - (y / (ry + 0.5)) ** 2)));
    px(sg, "#c48a4a", Math.round(rx) - half, cy + y, half * 2 + 1, 1);
    const inner = half - 2;
    if (inner > 0 && Math.abs(y) < ry) {
      const water = fill > 0 && y >= ry - 1 - Math.round(fill * 6);
      px(sg, water ? "#2f7fa8" : "#1c140c", Math.round(rx) - inner, cy + y, inner * 2 + 1, 1);
      if (water && fill > 0.95 && y === -ry + 1) px(sg, "#a8e0f0", Math.round(rx) - inner + 4, cy + y, 5, 1);
      if (water && running && Math.floor(t * 9 + y * 3) % 4 === 0) px(sg, "#a8e0f0", Math.round(rx) - inner + ((Math.floor(t * 9) * 5) % (inner * 2)), cy + y, 1, 1);
    }
  }
}

function pivotOf(tilt) { return tilt < 0 ? { x: BARREL.x, y: BARREL.y + BARREL.h } : { x: BARREL.x + BARREL.w, y: BARREL.y + BARREL.h }; }

function barrel(fill, tilt, running, t) {
  drawStave(fill, running, t);
  if (!tilt) { g.drawImage(stave, BARREL.x, BARREL.y); return; }
  const pv = pivotOf(tilt);
  g.save();
  g.imageSmoothingEnabled = false;
  g.translate(pv.x, pv.y);
  g.rotate(tilt);
  g.drawImage(stave, BARREL.x - pv.x, BARREL.y - pv.y);
  g.restore();
}

// Where the lip of the barrel is once it's tipped, for the water to leave.
function mouthAt(tilt) {
  const pv = pivotOf(tilt);
  const mx = (tilt < 0 ? BARREL.x + 1 : BARREL.x + BARREL.w - 1) - pv.x, my = BARREL.y + 4 - pv.y;
  const c = Math.cos(tilt), s = Math.sin(tilt);
  return { x: pv.x + mx * c - my * s, y: pv.y + mx * s + my * c };
}

// The pour is one arc of water out of the lip, thinner as the barrel empties.
function pour(m, dir, fill) {
  const thick = 1 + Math.round(fill * 2);
  for (let i = 0; i < 40; i++) {
    const tt = i / 40 * 0.5, x = m.x + dir * 55 * tt, y = m.y + 40 * tt * tt * 6;
    if (y > GROUND) break;
    px(g, i % 4 ? "#2f7fa8" : "#6fc0e0", x, y, 1, thick);
  }
}

function garden(grow, wet) {
  px(g, wet ? "#3a2614" : "#4a3220", 116, GROUND - 2, 34, 3);
  px(g, "#5a3e24", 116, GROUND - 2, 34, 1);
  SPROUTS.forEach((x, i) => {
    const h = 2 + Math.round(grow * (3 + (i % 2)));
    px(g, grow > 0.5 ? "#6fd08c" : "#3f8a4a", x, GROUND - 2 - h, 1, h);
    px(g, grow > 0.5 ? "#a8e8a0" : "#6fd08c", x - 1, GROUND - 2 - h, 1, 1); px(g, "#6fd08c", x + 1, GROUND - 1 - h, 1, 1);
    if (grow > 0.8) px(g, "#f29bb8", x, GROUND - 3 - h, 1, 1);
  });
}

function rain(t, wet) {
  const r = rng(11), n = Math.round(W / 12 * wet);
  for (let i = 0; i < n; i++) {
    const x0 = r() * (W + 20), sp = 80 + r() * 30, ph = r() * 80;
    const y = ((ph + t * sp) % 80) - 10, x = x0 - y * 0.25;
    if (y < GROUND - 2) px(g, "#7fa6c4", x, y, 1, 3);
    else px(g, "#a8e0f0", x - 1, GROUND - 1, 1, 1), px(g, "#a8e0f0", x + 1, GROUND - 1, 1, 1);
  }
}

function frame(t) {
  const b = pond.barrel, wet = b.raining;
  let fill = Math.min(1, b.level / b.cap), tilt = 0, lift = 0, spots = catchSpots(b), grow = b.watering > 0 ? 1 : 0, pouring = null;
  if (anim) {
    const a = anim, k = t - a.t0;
    const ease = (u) => 1 - (1 - Math.min(1, Math.max(0, u))) ** 3;
    fill = a.fill * (1 - ease((k - 0.7) / 1.1));
    tilt = a.dir * 1.2 * (ease((k - 0.35) / 0.5) - ease((k - 1.9) / 0.5));
    lift = Math.round(6 * (ease(k / 0.3) - ease((k - 2) / 0.4)));
    spots = k < 0.2 ? a.spots : [];
    if (a.how === "garden") grow = Math.max(grow, ease((k - 1.1) / 0.9));
    if (k > 0.6 && k < 1.85 && a.fill > 0) {
      pouring = mouthAt(tilt);
      if (Math.random() < 0.6) a.drops.push({ x: pouring.x, y: pouring.y, vx: a.dir * (20 + Math.random() * 60), vy: -20 + Math.random() * 30, t: 0 });
    }
  }
  g.save();
  backdrop(t, wet);
  g.translate(ox, 0);
  bank(t);
  stream(t, wet > 0);
  garden(grow, wet > 0 || grow > 0);
  if (anim) anim.sheet.forEach((s) => px(g, s.c, s.x, GROUND, 3, 1));
  barrel(fill, tilt, wet > 0, t);
  if (pouring) pour(pouring, anim.dir, fill);
  sieve(lift, spots, wet > 0, t);
  if (anim) anim.drops.forEach((d) => px(g, d.t < 0.15 ? "#a8e0f0" : "#6fc0e0", d.x, d.y, 1, 1));
  g.restore();
  if (wet) rain(t, wet);
}

function step(dt) {
  if (!anim) return;
  anim.drops.forEach((d) => {
    d.t += dt; d.vy += 220 * dt; d.x += d.vx * dt; d.y += d.vy * dt;
    if (d.y >= GROUND) { d.y = GROUND; d.vy = 0; d.vx *= 0.96; if (anim.sheet.length < 90 && Math.random() < 0.3) anim.sheet.push({ x: Math.round(d.x), c: Math.random() < 0.3 ? "#6fc0e0" : "#2f7fa8" }); }
  });
  anim.drops = anim.drops.filter((d) => d.t < 1.1 && d.x > -ox && d.x < W);
}

function paint(t) {
  if (!pond) return;
  const cv = $("barrel-art"), box = cv.parentElement, dpr = window.devicePixelRatio || 1, k = Math.max(1, Math.floor(2 * dpr));
  const want = Math.max(SCENE, Math.floor(box.clientWidth * dpr / k));
  if (want !== W || art.width !== W) {
    W = want; ox = Math.floor((W - SCENE) / 2);
    art.width = W; art.height = H;
  }
  frame(t);
  if (cv.width !== W * k) {
    cv.width = W * k; cv.height = H * k;
    cv.style.width = W * k / dpr + "px"; cv.style.height = H * k / dpr + "px";
  }
  const c = cv.getContext("2d");
  c.imageSmoothingEnabled = false;
  c.drawImage(art, 0, 0, W * k, H * k);
}

function shown() { return !document.hidden && !$("v-play").hidden && !$("play-barrel").hidden; }

// Rain and the stream move a few times a second, a tip every frame, and
// reduced motion gets a still picture and no tip.
function loop(now) {
  raf = 0;
  const t = now / 1000;
  if (!shown()) { anim = null; return; }
  if (anim) {
    step(Math.min(0.05, t - (last || t)));
    last = t;
    paint(t);
    if (t - anim.t0 > 2.6) { const done = anim.done; anim = null; done(); paint(t); }
  } else if (now - last > 110) { last = now; paint(t); }
  if (anim || (pond && pond.barrel.raining && !reduced)) raf = requestAnimationFrame(loop);
}

function kick() { if (!raf) raf = requestAnimationFrame(loop); }

const ONE = { sticks: ["a stick", "sticks"], reeds: ["a reed", "reeds"], clay: ["a lump of clay", "lumps of clay"], snail: ["a snail shell", "snail shells"], bait_worms: ["a tin of worms", "tins of worms"] };

function said(t) {
  if (ONE[t.kind]) return t.n === 1 ? ONE[t.kind][0] : t.n + " " + ONE[t.kind][1];
  const nm = t.name.toLowerCase();
  if (t.n === 1) return (/^[aeiou]/.test(nm) ? "an " : "a ") + nm;
  return t.n + " " + (nm.endsWith("s") ? nm : nm + "s");
}

function listOf(sv) {
  const parts = [];
  if (sv.pebbles) parts.push(sv.pebbles === 1 ? "a pebble" : sv.pebbles + " pebbles");
  sv.things.forEach((t) => parts.push(said(t)));
  if (parts.length > 4) return parts.slice(0, 3).join(", ") + " and " + (parts.length - 3) + " more things";
  return parts.length > 1 ? parts.slice(0, -1).join(", ") + " and " + parts[parts.length - 1] : parts[0] || "";
}

function hm(sec) { return sec >= 3600 ? Math.round(sec / 3600) + "h" : Math.max(1, Math.round(sec / 60)) + "m"; }

export function renderBarrel(p) {
  pond = p;
  const b = p.barrel, row = $("play-barrel");
  row.hidden = !(p.open && p.open.works);
  if (row.hidden) return;
  if (haulDay !== p.today) haulLine = "";
  const inSieve = listOf(b), running = b.raining > 0;
  let line;
  if (b.ready) {
    const lead = running ? "Raining now, the stream's running. " : b.level >= b.cap ? "Full to the brim. " : b.rain === 0 ? "A bit of dew. " : "";
    line = lead + (inSieve ? "In the sieve, " + inSieve + "." : "Nothing in the sieve but water.");
  } else if (b.level === 0 && !haulLine) {
    line = running ? "Raining now, the stream's running." : "Empty for now. The stream runs when it rains.";
  } else {
    line = (haulLine || "Tipped for today.") + (running ? " The stream's running, so it's filling again." : " Back tomorrow.");
  }
  $("barrel-line").textContent = line;
  row.classList.toggle("pays", b.ready);
  if (!anim) {
    $("barrel-tip").disabled = !b.ready;
    $("barrel-tip").className = b.ready ? "primary" : "";
    $("barrel-water").disabled = !b.ready || !!b.water;
  }
  let note = "";
  if (b.watering > 0) note = "The barrel's water is on the garden, growing it twice as fast for " + hm(b.watering) + ".";
  else if (b.ready && b.water) note = b.water + ".";
  else if (b.ready) note = "Watering the garden keeps the sieve too, and doubles it for three hours.";
  $("barrel-note").textContent = note;
  last = 0;
  paint(performance.now() / 1000);
  kick();
}

function pops(sv) {
  const box = $("barrel-pops"), cv = $("barrel-art"), rb = box.getBoundingClientRect(), rc = cv.getBoundingClientRect();
  const sx = rc.width / W, items = [];
  if (sv.pebbles) items.push({ kind: "peb", n: sv.pebbles });
  sv.things.forEach((t) => items.push(t));
  const shownItems = items.slice(0, 7);
  shownItems.forEach((it, i) => {
    setTimeout(() => {
      const d = document.createElement("div");
      d.className = "barrel-pop";
      const ax = ox + Math.max(10, Math.min(SCENE - 10, SIEVE.x + SIEVE.w / 2 + (i - (shownItems.length - 1) / 2) * 14));
      d.style.left = (rc.left - rb.left + ax * sx) + "px";
      d.style.top = (rc.top - rb.top + (SIEVE.y - 2) * sx) + "px";
      const n = document.createElement("span");
      n.textContent = "+" + it.n;
      d.append(iconCanvas(it.kind, 2), n);
      box.append(d);
      chime(PENTA[Math.min(PENTA.length - 1, 3 + i)], 0.03);
      setTimeout(() => d.remove(), 1900);
    }, (reduced ? 0 : 150) + i * 110);
  });
}

function tip(how) {
  if (anim || !pond || !pond.barrel.ready) return;
  $("barrel-tip").disabled = $("barrel-water").disabled = true;
  const b = pond.barrel;
  post("/api/barrel/tip", { how }).then((d) => {
    haulLine = d.sieve.pebbles || d.sieve.things.length ? "You got " + listOf(d.sieve) + "." : "Only water this time.";
    if (how === "garden") haulLine += " The garden's had a drink.";
    haulDay = d.pond.today;
    pops(d.sieve);
    const finish = () => setPond(d.pond);
    if (reduced) { sfx("splash"); finish(); return; }
    sfx("pop");
    setTimeout(() => sfx("splash"), 600);
    setTimeout(() => sfx("thump"), 2300);
    anim = { how, dir: how === "garden" ? 1 : -1, t0: performance.now() / 1000, fill: Math.min(1, b.level / b.cap), spots: catchSpots(b), drops: [], sheet: [], done: finish };
    last = 0;
    kick();
  }, (err) => { $("barrel-line").textContent = err.message; renderBarrel(pond); });
}

export function initBarrel() {
  $("barrel-tip").addEventListener("click", () => tip("sieve"));
  $("barrel-water").addEventListener("click", () => tip("garden"));
  document.addEventListener("visibilitychange", () => { if (pond && shown()) kick(); });
  window.addEventListener("resize", () => { if (pond) paint(performance.now() / 1000); });
}

export function showBarrel() { if (pond) { paint(performance.now() / 1000); kick(); } }

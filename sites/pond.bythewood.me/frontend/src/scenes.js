import { genome, bred, cells, colours, W, H } from "./frog.js";
import { draw } from "./lit.js";
import { drawIcon } from "./icons.js";
import { fullLeft, playOf } from "./api.js";
import { vista, rng, px, mid, look, sky, stars, glow, water, pad, cattail, reeds, hills, lantern, hanging, string, backdrop, LAMP } from "./paint.js";

// The thing each job brings, for an icon.
export const JOB_THING = { garden: "watercress", bugs: "cricket", sticks: "sticks", reeds: "reeds", mud: "clay", pebbles: "peb", craft: "t_basket" };

// The scenes are laid out this wide and centred, with the sky and the water
// carried on past either side on a wider screen.
const SW = 195;

function setup(cv, w, h, s) {
  if (cv.width !== w * s || cv.height !== h * s) { cv.width = w * s; cv.height = h * s; }
  const g = cv.getContext("2d");
  g.setTransform(s, 0, 0, s, 0, 0);
  g.imageSmoothingEnabled = false;
  return g;
}

// The tool each job is done with, small enough to sit by a frog anywhere.
const TOOLS = {
  bugs: [[".aaa..", "a...a.", "a...a.", ".aaa..", "...b..", "....b.", ".....b"], { a: "#fff6dc", b: "#8a5a2b" }],
  reeds: [[".aaa.", "a...a", "....a", "...b.", "..b..", ".b..."], { a: "#cfd3d6", b: "#8a5a2b" }],
  sticks: [["b....", ".b..b", "..bb.", "..bb.", ".b..b", "b...."], { b: "#8a5a2b" }],
  mud: [["..b..", "..b..", "..b..", ".aaa.", ".aaa.", "..a.."], { a: "#cfd3d6", b: "#8a5a2b" }],
  garden: [["a....", ".a.bb", "..bbbb", "..bbbb", "..bbbb"], { a: "#5aa9e6", b: "#3a6a9a" }],
  pebbles: [[".a...", "a.a..", ".a.a.", "...a.", "..a.a", "...a."], { a: "#7fd6c2" }],
  craft: [["aaa.", "aaa.", ".b..", ".b..", ".b.."], { a: "#cfd3d6", b: "#8a5a2b" }],
};

export function tool(g, job, x, y, s) {
  const t = TOOLS[job];
  if (!t) return;
  t[0].forEach((row, r) => [...row].forEach((ch, c) => { if (ch !== ".") px(g, t[1][ch], x + c * s, y + r * s, s, s); }));
}

// ---------------------------------------------------------------- frogs

const genes = new Map();

function geneOf(seed, mods) {
  const k = seed + ":" + (mods ? JSON.stringify(mods) : "");
  if (!genes.has(k)) genes.set(k, bred(genome(seed), mods));
  return genes.get(k);
}

// A pond frog or a resident, lit, with its hat. A resident has no stage and
// is always a grown frog.
function frogAt(g, f, x, y, shadow, s = 1) {
  const ge = geneOf(f.seed, f.mods), hat = f.hat || { shape: 0, colour: 0 };
  if (shadow) { g.fillStyle = "rgba(0,0,0,0.35)"; g.beginPath(); g.ellipse(x + W * s / 2, y + (H - 1.5) * s, W * 0.36 * s, 2.2 * s, 0, 0, 7); g.fill(); }
  draw(g, cells(ge, f.stage || "frog", { hat: hat.shape, legs: f.stage ? f.day >= 6 : true }), W, H, colours(ge, hat), x, y, s);
}

function residents(p) { return (p.neighbours && p.neighbours.all) || []; }
function resident(p, name) { return residents(p).find((n) => n.name === name); }

// Three residents out on the jetty, a different three each day.
function outToday(p) {
  const all = residents(p);
  if (!all.length) return [];
  let h = 7;
  for (const ch of p.today || "") h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return [0, 1, 2].map((i) => all[(h + i * 5) % all.length]);
}

// Fireflies to drift and the rain to fall, for any scene.
function moving(day, off, flies, seed, lamps) {
  const L = look(day);
  return { off, seed, lamps, flies: L.wet ? null : flies, rain: L.wet };
}

// ---------------------------------------------------------------- the reeds

// Every frog on its own pad in two rows, the one out front at the front
// under a lamp, and the pads nobody's on yet bare.
export function drawReeds(cv, p) {
  vista(cv).draw((g, w, h) => {
    const day = p.day, o = mid(w, SW);
    backdrop(g, w, h, 11, { horizon: h - 78, stars: 18, moon: [w - 25, 12, 5] }, day);
    g.translate(o, 0);
    const frogs = [p.frogs.find((f) => f.front)].concat(p.frogs.filter((f) => !f.front));
    const n = Math.max(1, Math.min(8, Math.max(p.pads, frogs.length))), front = Math.ceil(n / 2), back = n - front;
    const sp = Math.min(62, (SW - 20) / front), k = back === front ? sp / 4 : 0;
    const at = (i, c, y, dx) => [Math.round(SW / 2 + (i - (c - 1) / 2) * sp + dx), y];
    const slots = [];
    for (let i = 0; i < front; i++) slots.push(at(i, front, h - 8, -k));
    for (let i = 0; i < back; i++) slots.push(at(i, back, h - 42, k));
    const lamps = [];
    slots.map((s, i) => [s, frogs[i], i]).reverse().forEach(([[x, y], f, i]) => {
      pad(g, x, y, 24, 5, f && i % 3 === 2, !f);
      if (!f) { px(g, LAMP, x - 1, y - 1, 2, 1); return; }
      frogAt(g, f, x - W / 2, y - H + 2, true);
      if (f.work && f.work.job) tool(g, f.work.job, x + 15, y - 12, 1);
      if (f.front) { px(g, "#2a2018", x, y - H - 14, 1, 8); hanging(g, x, y - H - 6); lamps.push([x, y - H - 3, 10]); }
    });
    cattail(g, 4, h, h - 60, 1); cattail(g, 191, h, h - 54, -1);
    return moving(day, o, [6, 10, 185, h - 80, h - 50], 11, lamps);
  });
}

// One frog on its pad for its card in the roster, the water carried to the
// card's edges. A tool sits by it at work and a lamp hangs over the one out front.
export function drawCard(cv, p, f) {
  vista(cv).draw((g, w, h) => {
    const L = look(p.day), r = rng(f.seed % 997 + 3);
    water(g, w, 0, h, L.water[0], L.water[1], r, L.line, L.line2);
    const x = Math.round(w / 2);
    pad(g, x, h - 8, 28, 5, f.seed % 3 === 0);
    frogAt(g, f, x - W / 2, h - 8 - H + 2, true);
    cattail(g, 4, h, h - 30, 1);
    if (f.work && f.work.job) tool(g, f.work.job, x + 18, h - 22, 1);
    if (f.front) { px(g, "#2a2018", w - 14, 0, 1, 6); hanging(g, w - 14, 6); }
    return moving(p.day, 0, null, 5, f.front ? [[w - 14, 9, 10]] : []);
  });
}

// A frog close up on its own page, twice the size, on its pad by the lantern.
export function drawFrogPage(cv, p, f) {
  vista(cv).draw((g, w, h) => {
    const day = p.day, o = mid(w, SW), hor = h - 84;
    const r = backdrop(g, w, h, 13, { horizon: hor, stars: 24, moon: [30, 16, 6] }, day);
    g.translate(o, 0);
    lantern(g, 172, hor + 2, 70, look(day).dark ? 0.4 : 0.25);
    pad(g, 97, h - 14, 50, 9, true);
    frogAt(g, f, 97 - W, h - 14 - 2 * H + 4, true, 2);
    cattail(g, 8, h, hor + 14, 1); cattail(g, 14, h, hor + 24, -1);
    reeds(g, 150, 195, h - 2, r);
    return moving(day, o, [8, 20, 180, hor - 30, h - 40], 13, [[172, hor + 8, 24]]);
  });
}

// The parent on a pad and its frogspawn glowing on the water beside it.
export function drawLaying(cv, p, f) {
  vista(cv).draw((g, w, h) => {
    const day = p.day, o = mid(w, SW), hor = h - 82, L = look(day);
    const r = backdrop(g, w, h, 17, { horizon: hor, stars: 16, moon: [170, 12, 5] }, day);
    g.translate(o, 0);
    pad(g, 58, h - 12, 28, 6);
    frogAt(g, f, 58 - W / 2, h - 12 - H + 2, true);
    glow(g, 140, h - 30, 26, "#9fe6b8", L.dark ? 0.4 : 0.25);
    for (let i = 0; i < 16; i++) {
      const x = 126 + (i % 5) * 7 + (Math.floor(i / 5) % 2) * 3, y = h - 44 + Math.floor(i / 5) * 6;
      g.fillStyle = "#cfe8d0"; g.beginPath(); g.arc(x, y, 3, 0, 7); g.fill();
      px(g, "#1a3a2a", x, y); px(g, "#ffffff", x - 1, y - 2);
    }
    cattail(g, 186, h, hor + 10, -1); cattail(g, 180, h, hor + 20, 1); reeds(g, 0, 30, h - 2, r);
    return moving(day, o, [5, 100, 190, hor, h - 50], 17, []);
  });
}

// ---------------------------------------------------------------- make

// The place a tap on the shore opens, by how far across it lands.
export const MAKE_AT = [[42, "frogs"], [95, "garden"], [Infinity, "bank"]];

// The shore with the reed lodge on stilts, the garden, the gravel bar's
// sluice and wheel, the dye vats and the burrows. Each is dimmed until it's
// built, and a lantern hangs over whatever has something ready.
export function drawMake(cv, p, ready) {
  vista(cv).draw((g, w, h) => {
    const day = p.day || {}, L = look(day), b = p.bank, r = rng(23), o = mid(w, SW);
    sky(g, w, 64, L.sky);
    if (L.dark && !L.wet) stars(g, w, 50, r, 30);
    if (!L.wet) { if (L.dark) { glow(g, o + 160, 22, 18, "#9fb0ff", 0.25); g.fillStyle = "#f4ecd0"; g.beginPath(); g.arc(o + 160, 22, 5, 0, 7); g.fill(); } else { glow(g, o + 40, 58, 30, "#ffd8a0", 0.35); g.fillStyle = "#ffe2a8"; g.beginPath(); g.arc(o + 40, 60, 9, 0, 7); g.fill(); } }
    hills(g, w, 50, 4, L.dark ? "#141d3c" : L.wet ? "#3a3f4e" : "#4a3050", 23);
    const grass = L.dark ? ["#1a2a1a", "#22341f", "#121c14"] : ["#3d5a30", "#4f7038", "#2f3a2a"];
    px(g, grass[2], 0, 62, w, 3); px(g, grass[0], 0, 64, w, 40);
    for (let x = 0; x < w; x += 2) px(g, grass[1], x, 64 + (x * 7 % 3), 1, 2);
    water(g, w, 104, h - 104, L.water[0], L.water[1], r, L.line, L.line2);
    g.translate(o, 0);
    if (!L.dark && !L.wet) for (let y = 106; y < h; y += 2) px(g, "#f2b878", 34 + Math.floor(r() * 12), y, 3, 1);

    // The reed lodge, up on stilts over the water.
    for (const sx of [10, 20, 30]) px(g, "#3a2414", sx, 88, 2, 22);
    for (let y = 0; y < 26; y++) { const half = Math.round(Math.sqrt(Math.max(0, 1 - ((y - 26) / 26) ** 2)) * 17); px(g, y % 3 ? "#c49a5a" : "#9a7038", 21 - half, 62 + y, half * 2, 1); }
    for (let x = 6; x < 37; x += 3) px(g, "#8a6230", x, 72, 1, 14);
    px(g, "#2a1a0e", 18, 78, 6, 10); px(g, LAMP, 19, 79, 4, 3); px(g, "#7a5230", 4, 88, 36, 2);

    // The garden, a bed for each row of plots with what's coming up, and the cloche.
    const plots = b.garden.plots || [];
    for (let k = 0; k < 3; k++) {
      const bx = 48 + k * 15;
      px(g, "#4a2e1a", bx, 86, 12, 7); px(g, "#6a4428", bx, 86, 12, 1);
      for (let s = 0; s < 3; s++) {
        const pl = plots[k * 3 + s];
        if (!pl) continue;
        const tall = 1 + Math.round(Math.min(1, pl.grown) * 2), x = bx + 2 + s * 4;
        px(g, "#6fd08c", x, 86 - tall, 1, tall); px(g, "#3f8a4a", x - 1, 86 - tall + 1);
        if (pl.ready_in === 0) px(g, "#e2687a", x, 85 - tall);
      }
    }
    px(g, "#cfe8f0", 64, 74, 7, 6); px(g, "#ffffff", 65, 74, 2, 3); px(g, "#6fd08c", 66, 77, 3, 3);

    // The gravel bar's sluice and its water wheel.
    px(g, "#7a8696", 98, 92, 22, 6);
    for (let i = 0; i < 18; i++) px(g, r() < 0.5 ? "#b9c3cf" : "#5a6270", 98 + r() * 22, 90 + r() * 7, 2, 1);
    px(g, "#6a4428", 104, 78, 18, 3); px(g, "#4a7aa8", 105, 77, 16, 1);
    g.strokeStyle = "#5a3a22"; g.lineWidth = 1; g.beginPath(); g.arc(126, 84, 7, 0, 7); g.stroke();
    for (let a = 0; a < 6; a++) { const t = a * Math.PI / 3; px(g, "#8a5a32", 126 + Math.cos(t) * 6, 84 + Math.sin(t) * 6, 2, 2); }

    // Three dye vats, steaming their colours while a batch boils.
    [["#6fa8ff", 140], ["#e2687a", 152], [LAMP, 164]].forEach(([c, x], i) => {
      px(g, "#2a1a10", x, 84, 10, 9); px(g, "#6a4428", x - 1, 84, 12, 2); px(g, c, x + 1, 85, 8, 1);
      if (b.dye.busy) for (let k = 0; k < 4; k++) { g.globalAlpha = 0.7 - k * 0.15; px(g, c, x + 3 + ((k + i) % 2) * 2, 80 - k * 4, 3, 3); }
      g.globalAlpha = 1;
    });

    // The burrows, a mound with a lit door for each one dug.
    for (let y = 0; y < 18; y++) { const half = Math.round(Math.sqrt(Math.max(0, 1 - ((y - 18) / 18) ** 2)) * 15); px(g, y < 3 ? "#5fb565" : "#3f8a4a", 187 - half, 76 + y, half * 2, 1); }
    // A finished painting hangs on the mound over the doors.
    const hung = b.burrows.level ? Math.min(4, Math.floor((b.burrows.paint || 0) / 12)) : 0;
    ["#f6c48a", "#8a939e", "#141c2a", "#e6eef6"].slice(0, hung).forEach((c, i) => { px(g, "#e3b23a", 178 + i * 5, 78, 4, 4); px(g, c, 179 + i * 5, 79, 2, 2); });
    const doors = Math.min(3, Math.max(1, b.burrows.level));
    for (let i = 0; i < doors; i++) { const x = 182 - (doors - 1) * 4 + i * 8; px(g, "#2a1a0e", x, 85, 5, 7); px(g, b.burrows.level ? LAMP : "#1a1220", x + 1, 86, 3, 3); }

    [[44, 94, b.garden.level], [94, 136, b.gravel.level], [136, 172, b.dye.level], [172, w - o, b.burrows.level]].forEach(([x0, x1, lv]) => {
      if (!lv) px(g, "rgba(8,10,20,0.62)", x0, 60, x1 - x0, 46);
    });

    const lamps = [];
    [["shift", 21, 50], ["garden", 70, 62], ["gravel", 110, 64], ["dye", 157, 66], ["rent", 187, 62]].forEach(([k, x, y]) => {
      if (!ready.has(k)) return;
      px(g, "#2a1d12", x, y - 8, 1, 8); hanging(g, x, y); lamps.push([x, y + 3, 12]);
    });
    cattail(g, 92, h, 100, 1); cattail(g, 96, h, 106, -1);
    reeds(g, -o, w - o, 104, r, L.dark ? ["#16301e", "#2a4a26"] : ["#2f4a26", "#4f7038"]);
    return moving(day, o, [6, 10, 185, 40, 80], 23, lamps);
  });
}

// ---------------------------------------------------------------- the kitchen

// The kitchen pot over a fire under a lamp, with the two picked things over it.
export function drawPot(cv, picked, night) {
  const PW = 120, PH = 44, g = setup(cv, PW, PH, 3), r = rng(29);
  sky(g, PW, 34, night ? ["#070b1e", "#0c1330", "#141d44"] : ["#5a3a6a", "#a8546a", "#e08a62"]);
  if (night) stars(g, PW, 20, r, 14);
  px(g, "#2a1e14", 0, 34, PW, 10); px(g, "#3a2a1c", 0, 34, PW, 1);
  glow(g, 60, 34, 26, "#ff9a4a", 0.45);
  px(g, "#3a2414", 34, 14, 2, 22); px(g, "#3a2414", 84, 14, 2, 22); px(g, "#5a3a22", 34, 14, 52, 2);
  px(g, "#2b2b30", 46, 20, 28, 12); px(g, "#3a3a42", 44, 19, 32, 2); px(g, "#1f1f24", 48, 31, 24, 2);
  px(g, picked.length === 2 ? "#c9974f" : "#1a1a1e", 46, 21, 28, 2);
  px(g, "#e2412d", 50, 34, 4, 3); px(g, "#ffb000", 55, 33, 6, 4); px(g, "#ffe27a", 57, 34, 2, 2); px(g, "#e2412d", 62, 34, 5, 3); px(g, "#6a4428", 48, 37, 22, 2);
  px(g, "#2a1d12", 104, 0, 1, 6); hanging(g, 104, 6);
  picked.forEach((k, i) => drawIcon(g, k, 46 + i * 18, 4, 1));
  if (picked.length === 2) for (let s = 0; s < 3; s++) px(g, "rgba(255,246,220,0.5)", 52 + s * 7, 14 - (s % 2) * 3, 2, 3);
}

// ---------------------------------------------------------------- a job

const JW = 128, JH = 56;
const frogBuf = document.createElement("canvas");
let frogKey = "";

function frogSprite(f) {
  const key = f.id + ":" + f.stage + ":" + f.hat.shape + ":" + f.hat.colour;
  if (key !== frogKey) {
    frogKey = key;
    frogBuf.width = W; frogBuf.height = H;
    const g = frogBuf.getContext("2d"), ge = geneOf(f.seed, f.mods);
    g.clearRect(0, 0, W, H);
    draw(g, cells(ge, f.stage, { hat: f.hat.shape, legs: f.day >= 6 }), W, H, colours(ge, f.hat), 0, 0, 1);
  }
  return frogBuf;
}

// The ground each job is worked on, drawn behind the frog from x0 to x1.
function ground(g, job, L, x0, x1, r) {
  const span = x1 - x0, wide = (y, hh, c) => px(g, c, x0, y, span, hh);
  switch (job) {
    case "bugs":
      wide(40, 16, L.dark ? "#16301e" : "#2f4a26");
      for (let x = x0; x < x1; x += 2) px(g, x % 4 ? "#2a4a26" : "#1e3a22", x, 34 + ((x * 7) % 8 + 8) % 8, 1, 10);
      break;
    case "reeds":
      wide(30, 13, L.water[0]); wide(43, 13, L.water[1]);
      for (let x = 2; x < JW; x += 9) px(g, L.line2, x, 33 + (x % 4) * 5, 5, 1);
      for (let x = 66; x < JW; x += 4) { px(g, "#16301e", x, 12 + (x * 5) % 9, 1, 40); px(g, "#5a3a1e", x, 9 + (x * 5) % 9, 1, 4); }
      break;
    case "sticks":
      wide(46, 10, "#2a1e14");
      px(g, "#2a1d12", 96, 0, 10, 46); px(g, "#16301e", 82, -10, 40, 18); px(g, "#1e3a22", 86, -6, 30, 8);
      [[70, 50], [80, 52], [62, 53]].forEach(([x, y]) => px(g, "#8a5a2b", x, y, 7, 1));
      break;
    case "mud":
      wide(36, 20, "#3a2a1c");
      px(g, "#2a1d12", 60, 44, 30, 8); px(g, "#a8642e", 64, 46, 6, 3); px(g, "#a8642e", 76, 47, 5, 3);
      px(g, L.water[0], 100, 40, x1 - 100, 16);
      break;
    case "garden":
      wide(36, 20, "#2a1e14");
      for (let k = 0; k < 3; k++) {
        px(g, "#1a120a", 60, 40 + k * 5, 60, 3);
        for (let x = 62; x < 120; x += 6) px(g, "#3f8a4a", x, 38 + k * 5, 1, 2);
      }
      break;
    case "pebbles":
      wide(14, 20, L.water[0]); wide(34, 16, L.water[1]);
      for (let x = 2; x < JW; x += 16) px(g, L.line2, x, 14, 7, 1);
      wide(50, 6, "#4a4438");
      for (let x = 4; x < JW; x += 9) px(g, x % 2 ? "#b9c3cf" : "#7f8a99", x, 50 + (x % 3), 2, 1);
      break;
    default:
      wide(-30, 86, "#1a120a");
      for (let y = -24; y < 44; y += 8) wide(y, 1, "#2a1d12");
      wide(50, 6, "#2a1d12");
      px(g, "#6a4428", 58, 38, 40, 4); px(g, "#4a2e1a", 61, 42, 3, 8); px(g, "#4a2e1a", 92, 42, 3, 8);
  }
}

// A frog at its job, at night or at dusk by the day. k is how far through
// the action it is, pop counts down from 1 as the last one lands, and the
// basket sits on the right with a heap for what's in it.
export function drawJob(cv, o) {
  vista(cv, 3).draw((g, w, h) => {
    const L = look(o.day), r = rng(41), off = mid(w, JW), top = h - JH, job = o.job, f = o.frog;
    sky(g, w, h, L.sky);
    if (L.dark && !L.wet) stars(g, w, top + 20, r, 24);
    g.translate(off, top);
    ground(g, job, L, -off, w - off, r);
    if (job === "craft") hanging(g, 120, -4);
    else lantern(g, 124, -6, 50, L.dark ? 0.35 : 0.2);
    const t = o.still ? 0 : o.t;
    const sway = o.working && !o.still ? Math.round(Math.sin(t * 5)) : 0;
    let fy = JH - H - 6;
    if (job === "pebbles") fy = 12 + (o.working && !o.still ? Math.round(Math.sin(o.k * Math.PI) * 14) : 0);
    const sq = o.still ? 0 : o.pop * 0.18, fh = Math.round(H * (1 - sq));
    if (f.stage !== "egg") {
      g.fillStyle = "rgba(0,0,0,0.35)"; g.beginPath(); g.ellipse(14 + W / 2, JH - 7.5, W * 0.36, 2.2, 0, 0, 7); g.fill();
      g.drawImage(frogSprite(f), 14, fy + (H - fh), W, fh);
    }
    if (o.working) tool(g, job, 52, fy + 12 + sway, 2);
    // What it's after: bugs flit about, the rest wait to be picked up.
    const thing = o.thing;
    if (o.working && thing) {
      if (job === "bugs") {
        for (let i = 0; i < 3; i++) {
          const a = t * (0.7 + i * 0.3) + i * 2;
          drawIcon(g, thing, 70 + Math.round(Math.cos(a) * 10) + i * 8, 8 + Math.round(Math.sin(a * 1.3) * 6) + i * 6, 1);
        }
      } else if (job !== "pebbles" && job !== "craft") drawIcon(g, thing, 68, JH - 20, 1);
      else if (job === "craft") drawIcon(g, thing, 76, 25, 1);
      if (o.pop > 0 && !o.still) drawIcon(g, o.got || thing, 80 + Math.round((1 - o.pop) * 22), 22 - Math.round(Math.sin((1 - o.pop) * Math.PI) * 12), 1);
    }
    const bx = 104, by = JH - 14, heap = Math.round(Math.min(1, o.fill) * 6);
    if (heap && thing) for (let i = 0; i < heap; i++) drawIcon(g, thing, bx + (i % 3) * 5 - 4, by - 8 - Math.floor(i / 3) * 3, 1);
    px(g, "#8a5a2b", bx, by, 18, 10); px(g, "#c9974f", bx, by, 18, 2);
    for (let x = bx + 2; x < bx + 18; x += 4) px(g, "#6b4a2a", x, by + 3, 2, 6);
    if (o.full) { glow(g, bx + 8, by - 10, 8, LAMP, 0.6); px(g, LAMP, bx + 7, by - 12, 3, 3); }
    if (L.wet && !o.still) {
      g.fillStyle = "rgba(190,210,235,0.45)";
      for (let i = 0; i < 40; i++) g.fillRect(-off + Math.floor((i * 37.7 + t * 18) % w), -top + Math.floor((i * 23.3 + t * 90) % h), 1, 3);
    }
  });
}

// ---------------------------------------------------------------- town

function hut(g, x, base, wdt, hgt, roof, wall, lit) {
  for (const sx of [x + 2, x + wdt - 4]) px(g, "#2a1a0e", sx, base, 2, 14);
  px(g, "#3a2414", x - 3, base, wdt + 6, 2);
  px(g, wall, x, base - hgt, wdt, hgt);
  for (let yy = base - hgt; yy < base; yy += 3) px(g, "rgba(0,0,0,0.18)", x, yy, wdt, 1);
  for (let y = 0; y < hgt; y++) { const half = Math.round((y / hgt) * (wdt / 2 + 4)); px(g, y % 2 ? roof[0] : roof[1], x + wdt / 2 - half, base - hgt * 2 + y, half * 2, 1); }
  if (lit) { glow(g, x + wdt / 2, base - hgt / 2, 12, LAMP, 0.5); px(g, LAMP, x + wdt / 2 - 3, base - hgt + 3, 6, 5); px(g, "#3a2414", x + wdt / 2, base - hgt + 3, 1, 5); }
  else px(g, "#1a1220", x + wdt / 2 - 3, base - hgt + 3, 6, 5);
}

// Stilt huts and the mushroom house along the jetty under a string of
// lights, with three of the residents out on it.
export function drawTown(cv, p) {
  vista(cv).draw((g, w, h) => {
    const day = p.day, o = mid(w, SW);
    backdrop(g, w, h, 31, { horizon: h - 50, moon: [o + 176, 14, 6] }, day);
    const base = h - 24;
    px(g, "#5a3a22", 0, base, w, 3); px(g, "#7a5230", 0, base, w, 1);
    g.translate(o, 0);
    for (let x = 4 - o - ((4 - o) % 16 + 16) % 16; x < w - o; x += 16) px(g, "#3a2414", x + 4, base + 3, 2, 20);
    hut(g, 8, base - 12, 22, 12, ["#8a6a3a", "#a8844a"], "#5a3a22", true);
    hut(g, 46, base - 16, 26, 14, ["#7a3a4a", "#9a4a5a"], "#4a3a2a", true);
    g.fillStyle = "#c8433f"; g.beginPath(); g.ellipse(106, base - 34, 16, 9, 0, Math.PI, 0); g.fill();
    px(g, "#f4ecd0", 98, base - 38, 3, 2); px(g, "#f4ecd0", 110, base - 40, 3, 2);
    px(g, "#e8dcc0", 99, base - 34, 14, 18); px(g, LAMP, 103, base - 30, 5, 5); glow(g, 105, base - 28, 12, LAMP, 0.5);
    px(g, "#2a1a0e", 101, base - 16, 2, 16); px(g, "#2a1a0e", 109, base - 16, 2, 16);
    hut(g, 132, base - 14, 24, 12, ["#4a6a3a", "#5a8a4a"], "#3a3a2a", false);
    hut(g, 166, base - 12, 20, 11, ["#5a5a8a", "#6a6aa8"], "#3a2a2a", true);
    const lamps = string(g, -o, w - o, h - 84, 8, 9 + Math.round((w - SW) / 20));
    outToday(p).forEach((n, i) => frogAt(g, n, [24, 86, 150][i] - W / 2, base - H + 3, true));
    return moving(day, o, [8, 10, 185, h - 80, h - 50], 31, lamps);
  });
}

// What each shop has on its shelves and the colours of its awning.
const SHOPS = {
  wren: { keeper: "Little Tully", awning: ["#7a6ab0", "#5a4a90"], hats: [1, 2, 3, 5, 6, 7, 8, 9, 10] },
  brindle: { keeper: "Big Bean", awning: ["#4f8a4a", "#3a6a38"], things: ["watercress", "duckweed", "lotus", "morel", "mayfly", "snail", "marigold", "cap", "firefly"] },
  fenn: { keeper: "Tansy", awning: ["#e2687a", "#b04a5a"], things: ["f_lilypad", "f_teacup", "f_toadstool", "f_lantern", "f_stones", "f_reeds", "f_lilypad", "f_teacup", "f_toadstool"] },
  moss: { keeper: "Moth", awning: ["#3a8a8a", "#2a6a6a"], things: ["t_acorn", "t_marble", "t_button", "t_duck", "t_gnome", "t_globe", "t_pinecone", "t_earring", "t_jar"] },
  sedge: { keeper: "Sedge", awning: ["#4a7aa8", "#3a5a88"], things: ["bait_worms", "bait_lure", "feather", "smallfry", "fillet", "bait_worms", "bait_lure", "feather", "smallfry"] },
  marsh: { keeper: "Marsh", awning: ["#d8642a", "#a84a1a"], frames: ["#f6c48a", "#8a939e", "#6fa8ff", "#e2687a", "#6fd08c", "#ffcf6a", "#a88aff", "#f4ecd0", "#c48a4a"] },
};

// A shopfront with a striped awning and its keeper behind the counter. The
// picture is laid out 112 high and sits on the bottom of a shorter canvas.
export function drawShop(cv, p, shop) {
  const sh = SHOPS[shop];
  vista(cv).draw((g, w, h) => {
    const day = p.day, o = mid(w, SW), dy = h - 112;
    backdrop(g, w, h, 37, { horizon: dy + 40, stars: 18 }, day);
    g.translate(o, dy);
    px(g, "#3a2414", 20, 30, 155, 82); px(g, "#5a3a22", 20, 30, 155, 2);
    for (let i = 0; i < 11; i++) { px(g, i % 2 ? "#f4ecd0" : sh.awning[0], 14 + i * 15, 24, 15, 10); px(g, i % 2 ? "#d8ccb0" : sh.awning[1], 14 + i * 15, 34, 15, 3); }
    for (let s = 0; s < 3; s++) px(g, "#6a4428", 30, 48 + s * 18, 135, 2);
    for (let i = 0; i < 9; i++) {
      const x = 30 + (i % 3) * 40, y = 36 + Math.floor(i / 3) * 18;
      if (sh.hats) {
        g.save(); g.beginPath(); g.rect(x, y, 40, 12); g.clip();
        frogAt(g, { seed: 999 + i, hat: { shape: sh.hats[i], colour: i % 4 } }, x - 2, y - 4);
        g.restore();
      } else if (sh.frames) {
        px(g, "#c48a4a", x + 12, y, 14, 12); px(g, sh.frames[i], x + 14, y + 2, 10, 8); px(g, "#2a1a0e", x + 14, y + 7, 10, 1);
      } else drawIcon(g, sh.things[i], x + 13, y, 1);
    }
    const k = resident(p, sh.keeper);
    if (k) frogAt(g, k, 140, 96 - H + 6);
    px(g, "#8a5a32", -o, 96, w, 16); px(g, "#c48a4a", -o, 96, w, 2);
    lantern(g, 12, 40, 56, 0.4);
    return moving(day, o, null, 37, [[12, 46 + dy, 24]]);
  });
}

// The frog's own stall under string lights, the frog behind the counter and
// what's up for sale on top of it.
export function drawStall(cv, p) {
  vista(cv).draw((g, w, h) => {
    const day = p.day, o = mid(w, SW), r = rng(41), L = look(day), f = p.frog;
    backdrop(g, w, h, 41, { horizon: h - 54, moon: [o + 30, 14, 6] }, day);
    px(g, L.dark ? "#1a2a1a" : "#3d5a30", 0, h - 54, w, 18); px(g, L.dark ? "#121c14" : "#2a3a20", 0, h - 38, w, 2);
    water(g, w, h - 10, 10, L.water[0], L.water[1], r, L.line, L.line2);
    g.translate(o, h - 104);
    for (let i = 0; i < 9; i++) { px(g, i % 2 ? "#f4ecd0" : "#e2687a", 50 + i * 11, 30, 11, 8); px(g, i % 2 ? "#d8ccb0" : "#b04a5a", 50 + i * 11, 38, 11, 3); }
    px(g, "#5a3a22", 52, 41, 2, 30); px(g, "#5a3a22", 145, 41, 2, 30);
    if (f && f.stage !== "egg") frogAt(g, f, 76, 66 - H + 6);
    px(g, "#8a5a32", 46, 66, 104, 4); px(g, "#c48a4a", 46, 66, 104, 1); px(g, "#6a4428", 50, 70, 96, 24);
    for (let i = 0; i < 4; i++) px(g, "#4a2e1a", 50, 74 + i * 5, 96, 1);
    (p.stall ? p.stall.rows : []).forEach((row) => {
      const x = 50 + row.slot * 12;
      drawIcon(g, row.sold ? "peb" : row.hat ? "hat" : row.a ? "bowl" : row.kind, x, 54, 1);
    });
    const a = string(g, -o, 46, 22, 6, 3), b = string(g, 150, w - o, 22, 6, 3);
    return moving(day, o, [6, 0, 195, 40, 80], 41, a.concat(b).map(([x, y, rr, c]) => [x, y + h - 104, rr, c]));
  });
}

// The boardwalk at the night market, the chalk board with how the goods and
// the coin moved, and a lamp post at each end.
export function drawMarket(cv, p) {
  vista(cv).draw((g, w, h) => {
    const day = p.day, o = mid(w, SW), r = rng(43), L = look(day);
    backdrop(g, w, h, 43, { horizon: h - 70, stars: 30, moon: [o + 178, 14, 6] }, day);
    px(g, "#3a2414", 0, h - 32, w, 4); px(g, "#5a3a22", 0, h - 32, w, 1);
    water(g, w, h - 12, 12, L.water[0], L.water[1], r, L.line, L.line2);
    g.translate(o, 0);
    for (let x = 2 - o - ((2 - o) % 18 + 18) % 18; x < w - o; x += 18) px(g, "#2a1a0e", x + 2, h - 28, 2, 16);
    const by = h - 96;
    px(g, "#1a2a1a", 60, by + 2, 76, 50); px(g, "#6a4428", 58, by, 80, 2); px(g, "#6a4428", 58, by + 52, 80, 2); px(g, "#6a4428", 58, by, 2, 54); px(g, "#6a4428", 136, by, 2, 54);
    const firms = (p.market && p.market.firms) || [];
    const rows = firms.filter((f) => f.kind === "good").slice(0, 3).concat(firms.filter((f) => f.kind === "coin"));
    rows.forEach((f, i) => {
      const y = by + 3 + i * 12, up = f.price >= (f.prev || f.price);
      drawIcon(g, f.kind === "good" ? f.item : f.kind, 64, y, 1);
      const c = up ? "#6fd08c" : "#ff8a7a";
      for (let k = 0; k < 3; k++) px(g, c, 98 - k, y + (up ? 3 + k : 8 - k), 1 + k * 2, 1);
      let v = 0;
      for (let x = 0; x < 20; x++) { v += Math.round((r() - (up ? 0.35 : 0.65)) * 2); v = Math.max(-4, Math.min(4, v)); px(g, "#e8e4d0", 108 + x, y + 6 - v, 1, 1); }
    });
    px(g, "#5a3a22", 96, by + 54, 4, h - 32 - by - 54);
    const nb = residents(p);
    [[20, 1], [166, 4]].forEach(([x, i]) => { if (nb[i]) frogAt(g, nb[i], x - W / 2, h - 32 - H + 3, true); });
    const lamps = [];
    [[6, h - 60], [44, h - 56], [150, h - 56], [186, h - 60]].forEach(([x, y]) => {
      px(g, "#2a1d12", x, y, 1, h - 32 - y); px(g, "#1a120a", x - 2, y - 6, 5, 6); px(g, LAMP, x - 1, y - 5, 3, 4); glow(g, x, y - 3, 10, LAMP, 0.5);
      lamps.push([x, y - 3, 10]);
    });
    return moving(day, o, null, 43, lamps.concat(string(g, -o, w - o, h - 108, 6, 10 + Math.round((w - SW) / 20))));
  });
}

// ---------------------------------------------------------------- play

const JARS = ["fish", "dive", "swarm", "lash", "whirl", "sling", "sweep", "putt"];

// A firefly jar with a game's icon in it, lit while the game still pays in
// full and dark once it's spent.
export function jar(g, game, x, y, r, lit = true) {
  if (lit) glow(g, x + 14, y + 18, 14, "#fff2a0", 0.45);
  px(g, "#8a5a32", x + 9, y + 2, 10, 3); px(g, "#c48a4a", x + 9, y + 2, 10, 1);
  px(g, "#b9c3cf", x + 8, y + 5, 12, 1); px(g, "#b9c3cf", x + 6, y + 6, 2, 22); px(g, "#b9c3cf", x + 20, y + 6, 2, 22); px(g, "#b9c3cf", x + 7, y + 28, 14, 2);
  px(g, "rgba(185,195,207,0.15)", x + 8, y + 6, 12, 22); px(g, "#ffffff", x + 8, y + 8, 1, 8);
  drawIcon(g, "g_" + game, x + 8, y + 11, 1);
  if (lit) for (let i = 0; i < 3; i++) px(g, "#fffbd0", x + 9 + r() * 10, y + 8 + r() * 18);
}

// A jetty with a row of firefly jars under a string of lights.
export function drawPlayArt(cv, p) {
  vista(cv).draw((g, w, h) => {
    const day = p && p.day, o = mid(w, SW), r = backdrop(g, w, h, 19, { horizon: h - 60, moon: [o + 160, 16, 6] }, day);
    const jy = h - 32;
    px(g, "#3a2414", 0, jy, w, 4); px(g, "#5a3a22", 0, jy, w, 1);
    g.translate(o, 0);
    for (let x = 6 - o - ((6 - o) % 22 + 22) % 22; x < w - o; x += 22) px(g, "#2a1a0e", x, jy + 4, 3, 30);
    JARS.forEach((k, i) => jar(g, k, 4 + i * 24, jy - 32, r, !p || fullLeft(playOf(p, k)) > 0));
    const lamps = string(g, -o, w - o, h - 96, 10, 8 + Math.round((w - SW) / 24));
    return moving(day, o, [8, 10, 185, h - 80, h - 40], 19, lamps);
  });
}

// ---------------------------------------------------------------- the bag

// The satchel on a table by a lamp, with a few of the pond's own things
// spilled out round it.
export function drawBag(cv, p) {
  vista(cv).draw((g, w, h) => {
    const o = mid(w, SW), L = look(p.day);
    sky(g, w, h, L.dark ? ["#1a1220", "#22182a", "#2a1e30"] : ["#2a1e30", "#3a2838", "#4a3240"]);
    px(g, "#4a2e1a", 0, h - 26, w, 26); px(g, "#6a4428", 0, h - 26, w, 2);
    g.translate(o, h - 96);
    for (let x = -o - (-o % 30 + 30) % 30; x < w - o; x += 30) px(g, "#3a2414", x, 72, 1, 24);
    glow(g, 160, 30, 40, LAMP, 0.35); lantern(g, 160, 14, 56, 0.2);
    g.fillStyle = "#6a4428"; g.beginPath(); g.moveTo(38, 70); g.lineTo(42, 36); g.lineTo(108, 36); g.lineTo(112, 70); g.fill();
    g.fillStyle = "#8a5a32"; g.beginPath(); g.moveTo(40, 70); g.lineTo(44, 40); g.lineTo(106, 40); g.lineTo(110, 70); g.fill();
    for (let x = 46; x < 106; x += 3) px(g, "#a8743a", x, 42, 1, 1);
    g.strokeStyle = "#4a2e1a"; g.lineWidth = 2; g.beginPath(); g.arc(75, 36, 22, Math.PI, 0); g.stroke();
    g.fillStyle = "#a8743a"; g.beginPath(); g.moveTo(42, 38); g.lineTo(108, 38); g.lineTo(100, 56); g.lineTo(50, 56); g.fill();
    px(g, "#c48a4a", 44, 38, 62, 2); for (let x = 52; x < 100; x += 3) px(g, "#6a4428", x, 53, 1, 1);
    px(g, LAMP, 70, 52, 10, 8); px(g, "#c98a2a", 72, 55, 6, 2); px(g, "#4a2e1a", 74, 46, 2, 7);
    const kinds = (p.bag || []).slice().sort((a, b) => b.count - a.count).map((t) => (t.hat ? "hat" : t.a ? "bowl" : t.kind));
    const top = [...new Set(kinds)].slice(0, 10);
    [[52, 26], [84, 24], [66, 28]].forEach(([x, y], i) => { if (top[i]) drawIcon(g, top[i], x, y, 1); });
    [[20, 58], [116, 58], [130, 60], [4, 60], [112, 60], [176, 60], [146, 62]].forEach(([x, y], i) => { if (top[i + 3]) drawIcon(g, top[i + 3], x, y, 1); });
    return { off: o, lamps: [[160, 20 + h - 96, 24]] };
  });
}

// ---------------------------------------------------------------- the stamp book

// The stamp book open on a desk under a lamp, its pages holding the faces of
// the latest stamps, with the ink pad and a rubber stamp beside it.
export function drawStampDesk(cv, p, stamps) {
  vista(cv).draw((g, w, h) => {
    const o = mid(w, SW), L = look(p.day);
    sky(g, w, h, L.dark ? ["#1d1a2a", "#241f33", "#2a2438"] : ["#2a2238", "#3a2c42", "#4a344a"]);
    px(g, "#3a2414", 0, h - 36, w, 36); px(g, "#5a3a22", 0, h - 36, w, 2);
    g.translate(o, h - 96);
    glow(g, 136, 30, 44, LAMP, 0.3);
    px(g, "#2a2018", 136, -40, 1, 52); hanging(g, 136, 12);
    px(g, "#1a120a", 26, 64, 78, 2);
    px(g, "#f3ead2", 28, 16, 74, 48); px(g, "#d8ccb0", 64, 16, 2, 48); px(g, "#e6dcc0", 28, 62, 74, 2);
    const faces = (stamps || []).map((st) => st.stamp);
    [[31, 20], [47, 20], [31, 40], [47, 40], [68, 20], [84, 20], [68, 40], [84, 40]].forEach(([x, y], i) => {
      if (!faces[i]) { px(g, "#e6dcc0", x, y, 15, 15); return; }
      px(g, "#cfc2a2", x, y, 15, 15); px(g, "#123a3d", x + 1, y + 1, 13, 13);
      drawIcon(g, "st_" + faces[i], x + 1, y + 1, 1);
    });
    px(g, "#3a3a4a", 116, 52, 34, 12); px(g, "#5a5a6a", 116, 52, 34, 2); px(g, "#b04a5a", 119, 54, 28, 7); px(g, "#e2687a", 121, 55, 24, 2);
    px(g, "#2a1a10", 160, 54, 16, 6); px(g, "#e2687a", 161, 59, 14, 2); px(g, "#8a5a32", 165, 38, 6, 16); px(g, "#c48a4a", 165, 38, 2, 16);
    g.fillStyle = "#a8743a"; g.beginPath(); g.arc(168, 36, 5, 0, 7); g.fill(); px(g, "#c48a4a", 165, 33, 3, 2);
    for (let i = 0; i < 4; i++) px(g, "rgba(226,104,122,.45)", 118 + i * 18, 72 + (i % 2) * 7, 10, 8);
    return { off: o, lamps: [[136, h - 81, 16]] };
  });
}

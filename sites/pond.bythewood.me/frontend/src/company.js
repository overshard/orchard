import { genome, bred, cells, colours, draw, W, H } from "./frog.js";
import { drawIcon } from "./icons.js";

// Another frog, as the server tells it: a look and a name and nothing else.
export function callerArt(c, s, thing) {
  const cv = document.createElement("canvas"), ctx = cv.getContext("2d"), g = bred(genome(c.seed), c.mods);
  cv.width = W * s; cv.height = H * s;
  draw(ctx, cells(g, c.stage, { hat: c.hat.shape, legs: c.day >= 6 }), W, H, colours(g, c.hat), 0, 0, s);
  if (thing) drawIcon(ctx, thing, W * s - 8 * s, H * s - 8 * s, s);
  return cv;
}

const PRINT = ["x.x.x", ".xxx.", "..x.."];

// The visitor's muddy footprint, on the front edge of the pad.
export function footprint(ctx, x, y) {
  PRINT.forEach((row, r) => [...row].forEach((ch, c) => {
    if (ch === ".") return;
    ctx.fillStyle = r === 0 ? "#8a6230" : "#5a3d1e";
    ctx.fillRect(x + c * 4, y + r * 4, 4, 4);
  }));
}

const FROM = { pond: "From a pond down the way.", resident: "Over from town.", wild: "Just passing through." };

// What the greeting pops up for the visitor.
export function visitorReveal(v) {
  return { kick: "A visitor in the night", art: callerArt(v, 3, v.gift.item), name: v.name, line: FROM[v.from] + " It left " + v.gift.text + " and a muddy footprint.", ok: "Wave" };
}

// The six stamps a frog's page can be pressed with.
export const STAMPS = {
  heart: [["........", ".aa.aa..", "abaaaaa.", "abaaaaa.", ".aaaaa..", "..aaa...", "...a....", "........"], { a: "#e2567a", b: "#ffd0ea" }],
  fly: [["........", ".bb.bb..", "bbbbbbb.", ".bbabb..", "..aaa...", ".aaaaa..", "..aaa...", "........"], { a: "#2a2a30", b: "#cfe3ea" }],
  wave: [["........", ".aa..aa.", "a..aa..a", "........", ".bb..bb.", "b..bb..b", "........", "........"], { a: "#7fd6c2", b: "#3d8fd6" }],
  star: [["...a....", "...a....", "..aba...", "aaabaaa.", ".aabaa..", "..aaa...", ".aa.aa..", ".a...a.."], { a: "#ffe27a", b: "#fff6dc" }],
  leaf: [["......a.", "....aaa.", "..aaaba.", ".aaabaa.", ".aabaaa.", ".abaaa..", "b.aa....", "........"], { a: "#56b45f", b: "#1f5a2c" }],
  pebble: [["........", "..aab...", ".aaaab..", ".aaabbc.", ".abbbcc.", "..bccc..", "........", "........"], { a: "#d5d8dc", b: "#9aa0a6", c: "#6d747a" }],
};

export function stampArt(kind, s) {
  const cv = document.createElement("canvas"), ctx = cv.getContext("2d"), [rows, col] = STAMPS[kind];
  cv.width = cv.height = 8 * s;
  rows.forEach((row, r) => [...row].forEach((ch, c) => {
    if (ch === ".") return;
    ctx.fillStyle = col[ch]; ctx.fillRect(c * s, r * s, s, s);
  }));
  return cv;
}

// A frog kept home stamps as a dim shape with no name.
function shadowArt(s) {
  const cv = callerArt({ seed: 1, mods: null, stage: "frog", day: 20, hat: { shape: 0, colour: 0 } }, s), ctx = cv.getContext("2d");
  ctx.globalCompositeOperation = "source-atop";
  ctx.fillStyle = "#2a241d"; ctx.fillRect(0, 0, cv.width, cv.height);
  return cv;
}

function names(list) {
  return list.length > 1 ? list.slice(0, -1).join(", ") + " and " + list[list.length - 1] : list[0];
}

// What the greeting pops up for stamps left on the frog's page, a frog each
// with its stamp, up to six of them.
export function stampsReveal(stamps) {
  const box = document.createElement("div");
  box.className = "stamped";
  const s = stamps.length > 3 ? 2 : 3;
  stamps.slice(0, 6).forEach((st) => {
    const fig = document.createElement("figure");
    fig.append(st.from ? callerArt(st.from, s) : shadowArt(s), stampArt(st.stamp, s + 1));
    box.append(fig);
  });
  const who = [...new Set(stamps.filter((st) => st.from).map((st) => st.from.name))], quiet = stamps.some((st) => !st.from);
  if (quiet) who.push("a frog that keeps to itself");
  const more = stamps.length > 6 ? " There's " + (stamps.length - 6) + " more besides." : "";
  return { kick: "Stamps on your page", art: box, name: stamps.length === 1 ? "A " + stamps[0].stamp : stamps.length + " stamps", line: "From " + names(who.slice(0, 5)) + (who.length > 5 ? " and others" : "") + "." + more };
}

// ---------------------------------------------------------------- paintings

export const PAINTINGS = ["The pond at dawn", "The pond in the rain", "The pond by lantern light", "The pond in the snow"];
export const PIECES = 12;
const PW = 24, PH = 18;
// The order a painting's twelve squares come in, so it fills from all over.
const ORDER = [5, 0, 10, 3, 7, 1, 11, 8, 2, 6, 9, 4];

function band(g, c, y, h) { g.fillStyle = c; g.fillRect(0, y, PW, h); }
function dot(g, c, x, y, w, h) { g.fillStyle = c; g.fillRect(x, y, w || 1, h || 1); }

// The pad and a little frog on it, the same in every one of the four.
function padFrog(g, pad, frog) {
  dot(g, pad, 7, 14, 10, 2); dot(g, pad, 8, 13, 8, 1);
  dot(g, frog, 10, 11, 4, 2); dot(g, frog, 10, 10, 1, 1); dot(g, frog, 13, 10, 1, 1);
}

const SCENES = [
  (g) => {
    band(g, "#f2a07a", 0, 3); band(g, "#f6c48a", 3, 3); band(g, "#ffe2a8", 6, 3);
    dot(g, "#fff6dc", 15, 6, 5, 3); dot(g, "#fff6dc", 16, 5, 3, 1);
    band(g, "#2f5a3a", 9, 1);
    band(g, "#4a6f7a", 10, 8);
    dot(g, "#f6c48a", 15, 11, 5, 1); dot(g, "#f6c48a", 16, 13, 3, 1); dot(g, "#ffe2a8", 17, 15, 2, 1);
    padFrog(g, "#3f7a45", "#2a4a2c");
  },
  (g) => {
    band(g, "#6d747a", 0, 5); band(g, "#8a939e", 5, 4);
    band(g, "#2f4a3a", 9, 1);
    band(g, "#24414a", 10, 8);
    for (let i = 0; i < 14; i++) dot(g, "#cfe3ea", (i * 7) % PW, (i * 5) % 9, 1, 2);
    [[3, 12], [19, 11], [5, 16], [20, 16]].forEach(([x, y]) => { dot(g, "#7fa8b0", x - 1, y, 3, 1); });
    padFrog(g, "#3f7a45", "#2a4a2c");
  },
  (g) => {
    band(g, "#141c2a", 0, 9);
    [[2, 1], [9, 3], [14, 1], [21, 2], [5, 6], [19, 5]].forEach(([x, y]) => dot(g, "#fff6dc", x, y));
    band(g, "#1f2f2a", 9, 1);
    band(g, "#0c262a", 10, 8);
    dot(g, "rgba(255,226,122,0.35)", 1, 2, 7, 7);
    dot(g, "#6b4a2a", 4, 5, 1, 6); dot(g, "#3a2a1c", 3, 3, 3, 1); dot(g, "#ffe27a", 3, 4, 3, 2);
    dot(g, "#ffe27a", 3, 11, 3, 1); dot(g, "#c9a24a", 4, 13, 2, 1); dot(g, "#8a7330", 4, 15, 1, 1);
    padFrog(g, "#2f5a3a", "#1a2a1c");
  },
  (g) => {
    band(g, "#c9d6e0", 0, 9);
    band(g, "#f4f6f8", 8, 2);
    band(g, "#9fb8c8", 10, 8);
    dot(g, "#e6eef6", 0, 11, 6, 1); dot(g, "#e6eef6", 18, 14, 6, 1);
    for (let i = 0; i < 16; i++) dot(g, "#ffffff", (i * 5 + 2) % PW, (i * 7) % 16);
    padFrog(g, "#3f7a45", "#2a4a2c");
    dot(g, "#ffffff", 7, 13, 2, 1); dot(g, "#ffffff", 14, 13, 2, 1);
  },
];

// A painting in its frame, have of its twelve pieces in. A square not in yet
// is bare canvas, and the frame gilds round from the top left as it fills.
export function paintingArt(i, have, s) {
  const cv = document.createElement("canvas"), ctx = cv.getContext("2d");
  cv.width = (PW + 4) * s; cv.height = (PH + 4) * s;
  ctx.setTransform(s, 0, 0, s, 0, 0);
  ctx.fillStyle = "#4a3318"; ctx.fillRect(0, 0, PW + 4, PH + 4);
  ctx.fillStyle = "#6b4a2a"; ctx.fillRect(1, 1, PW + 2, PH + 2);
  const edge = [];
  for (let x = 0; x < PW + 4; x++) edge.push([x, 0]);
  for (let y = 1; y < PH + 4; y++) edge.push([PW + 3, y]);
  for (let x = PW + 2; x >= 0; x--) edge.push([x, PH + 3]);
  for (let y = PH + 2; y > 0; y--) edge.push([0, y]);
  const lit = Math.round(edge.length * Math.min(have, PIECES) / PIECES);
  ctx.fillStyle = "#e3b23a";
  edge.slice(0, lit).forEach(([x, y]) => ctx.fillRect(x, y, 1, 1));
  const pic = document.createElement("canvas"), pg = pic.getContext("2d");
  pic.width = PW; pic.height = PH;
  SCENES[i](pg);
  ctx.fillStyle = "#e9e1cc"; ctx.fillRect(2, 2, PW, PH);
  ctx.fillStyle = "#d6ccb2";
  for (let y = 4; y < PH + 2; y += 3) ctx.fillRect(3, y, PW - 2, 1);
  ORDER.slice(0, Math.min(have, PIECES)).forEach((n) => {
    const x = (n % 4) * 6, y = Math.floor(n / 4) * 6;
    ctx.drawImage(pic, x, y, 6, 6, 2 + x, 2 + y, 6, 6);
  });
  return cv;
}

export function paintingReveal(i, dug) {
  const hung = dug ? "It's hung in the burrows." : "It goes up in the burrows once there's one dug.";
  return { kick: "A painting's done", art: paintingArt(i, PIECES, 6), name: PAINTINGS[i], line: "Every piece of it came from a visitor or a stamp. " + hung, glow: 3 };
}

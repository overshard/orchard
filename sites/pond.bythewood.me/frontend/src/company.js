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

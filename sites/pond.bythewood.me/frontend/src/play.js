import { cells, sideCells, colours, draw, W, H, SW, SH } from "./frog.js";
import { drawTop } from "./top.js";
import { flick } from "./flick.js";
import { cross } from "./cross.js";
import { raft } from "./raft.js";
import { leap } from "./leap.js";
import { school } from "./school.js";
import { fish } from "./fish.js";
import { stack } from "./stack.js";
import { shell } from "./shell.js";
import { skip } from "./skip.js";
import { drop, dropArt } from "./drop.js";
import { sweep, sweepArt } from "./sweep.js";

// Every disk on Play. A disk is a key, a name, the plastic's colour, one
// line, the picture on its label and the game the frame runs. A new game is
// one line on its shelf, and Dive has no game here since it has its own view.
export const FLAGSHIP = { key: "fish", name: "Fishing", colour: "#1f7a7a", line: "A minute off the jetty. Cast, strike and reel in what you can.", art: fishArt, game: fish };

export const SHELVES = [
  { name: "Arcade", disks: [
    { key: "dive", name: "Dive", colour: "#2b5fa8", line: "Race nine frogs to the bottom.", art: diveArt },
    { key: "cross", name: "Crossing", colour: "#b8392b", line: "Hop the lane and the stream home.", art: crossArt, game: cross },
    { key: "flick", name: "Tongue flick", colour: "#3f8a45", line: "Thirty seconds of bugs.", art: flickArt, game: flick },
    { key: "raft", name: "Raft run", colour: "#d0951c", line: "Steer a leaf down the river.", art: raftArt, game: raft },
    { key: "leap", name: "Lily leap", colour: "#7d52c0", line: "Bounce as high as the pads go.", art: leapArt, game: leap },
    { key: "school", name: "Tadpoles", colour: "#2a2a30", line: "Lead a school round the shallows.", art: schoolArt, game: school },
    { key: "stack", name: "Lily stack", colour: "#c0507a", line: "Stack pads as high as they go.", art: stackArt, game: stack },
    { key: "skip", name: "Pebble skip", colour: "#5b6470", line: "Skip three pebbles across the pond.", art: skipArt, game: skip },
    { key: "drop", name: "Pebble drop", colour: "#d8cfb8", line: "Drop five pebbles through the reeds.", art: dropArt, game: drop },
  ] },
  { name: "Puzzles", disks: [
    { key: "shell", name: "Shell game", colour: "#8a5a2b", line: "Follow the tadpole under the shells.", art: shellArt, game: shell },
    { key: "sweep", name: "Bug sweep", colour: "#8fa832", line: "Clear the pads and mind the wasps.", art: sweepArt, game: sweep },
  ] },
];

export const DISKS = [FLAGSHIP, ...SHELVES.flatMap((s) => s.disks)];

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text) e.textContent = text;
  return e;
}

function disk(d, flag) {
  const a = el("a", flag ? "disk flag" : "disk"), floppy = el("span", "floppy"), sticker = el("span", "sticker"), art = el("canvas");
  a.href = "#" + d.key; a.id = "play-" + d.key;
  a.style.setProperty("--disk", d.colour);
  art.id = "play-" + d.key + "-art"; art.width = 168; art.height = 96;
  art.setAttribute("aria-hidden", "true");
  sticker.append(art, el("b", "", d.name));
  floppy.append(sticker);
  const line = el("span", "dline");
  if (flag) {
    const pay = el("span");
    pay.id = "play-" + d.key + "-line";
    line.append(el("b", "dhead", d.name), el("span", "dsub", d.line), pay);
  } else {
    line.id = "play-" + d.key + "-line";
    line.textContent = d.line;
  }
  a.append(floppy, line);
  return a;
}

// A game's own view, the field in a screen over a strip of readouts and EJECT.
function gameView(d) {
  const v = el("section", "view arcade"), screen = el("div", "screen"), cv = el("canvas"), hud = el("div", "dive-hud"), tiles = el("div", "tiles"), out = el("button", "eject", "Eject");
  v.id = "v-" + d.key; v.hidden = true;
  cv.id = d.key + "-cv"; cv.width = 360; cv.height = 540; cv.tabIndex = 0;
  cv.setAttribute("aria-label", d.name + ". " + d.game.aria);
  screen.append(cv);
  [["Best", "best", "0"], ["Today", "today", "0/0"], ["Pay", "pays", "Full"]].forEach(([k, id, v0]) => {
    const r = el("span", "readout"), val = el("span", "v", v0);
    val.id = d.key + "-" + id;
    r.append(el("span", "k", k), val);
    tiles.append(r);
  });
  out.type = "button"; out.id = d.key + "-exit";
  hud.append(tiles, out);
  v.append(screen, hud);
  return v;
}

// buildPlay puts the disks on their shelves and makes each game's view, and
// has to run before a game's frame goes looking for its canvas.
export function buildPlay(shelves, after) {
  const first = el("div", "disks");
  first.append(disk(FLAGSHIP, true));
  shelves.append(first);
  SHELVES.forEach((s) => {
    const row = el("div", "disks");
    s.disks.forEach((d) => row.append(disk(d)));
    shelves.append(el("h2", "", s.name), row);
  });
  after.after(...DISKS.filter((d) => d.game).map(gameView));
}

// drawDisks paints every label, which a game's title card copies its art off.
export function drawDisks(g, p) {
  DISKS.forEach((d) => d.art(document.getElementById("play-" + d.key + "-art").getContext("2d"), g, p));
}

function grown(p) { return ["egg", "tadpole"].includes(p.frog.stage) ? "froglet" : p.frog.stage; }
function front(g, p) { return cells(g, grown(p), {}); }
function side(g, p, flip) { return sideCells(g, p.frog.stage === "egg" ? "tadpole" : p.frog.stage, flip); }

function diveArt(d, g, p) {
  ["#123a3d", "#0e2c30", "#0a1b1f"].forEach((c, i) => { d.fillStyle = c; d.fillRect(0, i * 32, 168, 32); });
  d.fillStyle = "#7fd6c2"; d.fillRect(0, 0, 168, 3);
  d.fillStyle = "#2f7a3f"; d.fillRect(120, 0, 10, 40); d.fillStyle = "#5b6470"; d.fillRect(118, 66, 14, 30);
  d.fillStyle = "#3f9a4d"; d.fillRect(70, 0, 10, 28); d.fillStyle = "#5b6470"; d.fillRect(68, 56, 14, 40);
  draw(d, side(g, p, true), SW, SH, colours(g, null), 22, 40, 2);
  d.fillStyle = "rgba(255,246,220,0.6)"; d.fillRect(12, 30, 4, 4); d.fillRect(8, 22, 2, 2);
}

function crossArt(c, g) {
  ["#4f9a4a", "#3a3a40", "#3a3a40", "#4f9a4a", "#1b4a5a", "#1d5163", "#2f6b33"].reverse().forEach((col, i) => { c.fillStyle = col; c.fillRect(0, i * 14 - 2, 168, 14); });
  c.fillStyle = "#cfc7a8"; for (let x = 4; x < 168; x += 16) c.fillRect(x, 67, 8, 1);
  c.fillStyle = "#e2412d"; c.fillRect(30, 58, 22, 8); c.fillStyle = "#3d8fd6"; c.fillRect(110, 72, 12, 6);
  c.fillStyle = "#8a5a2b"; c.fillRect(60, 30, 44, 10); c.fillStyle = "#56b45f"; c.fillRect(20, 44, 12, 10); c.fillRect(34, 44, 12, 10);
  c.fillStyle = colours(g, null)[1]; c.fillRect(78, 84, 10, 9);
}

function flickArt(f, g, p) {
  f.fillStyle = "#1b3a4a"; f.fillRect(0, 0, 168, 96);
  f.fillStyle = "#56b45f"; f.fillRect(54, 84, 60, 8);
  draw(f, front(g, p), W, H, colours(g, null), 62, 52, 1);
  f.fillStyle = "#e2567a"; for (let i = 0; i < 14; i++) f.fillRect(84 + i * 3, 70 - i * 3, 2, 2);
  f.fillStyle = "#3a3f46"; f.fillRect(126, 26, 5, 3); f.fillStyle = "#cfe3ea"; f.fillRect(126, 23, 2, 2); f.fillRect(129, 23, 2, 2);
  f.fillStyle = "#ffe27a"; f.fillRect(30, 30, 4, 3); f.fillRect(140, 60, 4, 3);
}

function raftArt(r, g) {
  r.fillStyle = "#3f8a44"; r.fillRect(0, 0, 168, 96);
  for (let y = 0; y < 96; y += 2) { const c = 84 + Math.sin(y / 18) * 14; r.fillStyle = "#1d5163"; r.fillRect(Math.round(c - 46), y, 92, 2); }
  r.fillStyle = "#56b45f"; for (let x = 40; x < 130; x += 3) if (x < 74 || x > 98) r.fillRect(x, 26, 2, 10);
  r.fillStyle = "#8a5a2b"; for (let x = 41; x < 130; x += 6) if (x < 74 || x > 98) r.fillRect(x, 23, 2, 4);
  r.fillStyle = "#8a9099"; r.fillRect(104, 56, 10, 8);
  r.fillStyle = "#56b45f"; r.fillRect(76, 66, 16, 20);
  drawTop(r, colours(g, null), 78, 70, false);
}

function leapArt(l, g, p) {
  l.fillStyle = "#123a3d"; l.fillRect(0, 0, 168, 96);
  [[20, 84], [70, 62], [112, 38], [40, 22]].forEach(([x, y], i) => { l.fillStyle = i === 3 ? "#8a5a2b" : "#56b45f"; l.fillRect(x, y, 32, 5); });
  draw(l, front(g, p), W, H, colours(g, null), 64, 20, 1);
  l.fillStyle = "#3d8fd6"; l.fillRect(118, 30, 14, 3); l.fillStyle = "#cfe3ea"; l.fillRect(121, 26, 4, 3); l.fillRect(126, 26, 4, 3);
}

function schoolArt(sc, g) {
  const fc = colours(g, null);
  sc.fillStyle = "#2f6b33"; sc.fillRect(0, 0, 168, 96);
  sc.fillStyle = "#1d5163"; sc.fillRect(4, 4, 160, 88);
  [[60, 48], [72, 48], [84, 48], [84, 36], [96, 36]].forEach(([x, y], i) => { sc.fillStyle = fc[3]; sc.fillRect(x, y, 8, 8); sc.fillStyle = fc[1]; sc.fillRect(x + 1, y + 1, 6, 6); if (i === 4) { sc.fillStyle = "#0a0908"; sc.fillRect(x + 6, y + 2, 1, 1); sc.fillRect(x + 6, y + 5, 1, 1); } });
  sc.fillStyle = "#e9e3d6"; sc.fillRect(124, 38, 5, 4);
  sc.fillStyle = "#8a9099"; sc.fillRect(30, 20, 10, 9); sc.fillRect(130, 70, 10, 9);
}

function fishArt(f, g, p) {
  ["#2a5163", "#3f6f7a"].forEach((c, i) => { f.fillStyle = c; f.fillRect(0, i * 16, 168, 16); });
  f.fillStyle = "#1f4a30"; for (let i = 0; i < 168; i += 4) f.fillRect(i, 26 - (i * 37) % 7, 4, 8 + (i * 37) % 7);
  ["#1d5163", "#143c4a", "#0c2832"].forEach((c, i) => { f.fillStyle = c; f.fillRect(0, 34 + i * 21, 168, 21); });
  f.fillStyle = "#8a5a2b"; f.fillRect(0, 30, 50, 3);
  f.fillStyle = "#4a3018"; f.fillRect(8, 33, 3, 63); f.fillRect(40, 33, 3, 63);
  draw(f, front(g, p), W, H, colours(g, null), 2, 0, 1);
  f.fillStyle = "#c9974f"; for (let i = 0; i < 34; i++) f.fillRect(34 + i, 22 - Math.round(i * 0.55), 1, 1);
  f.fillStyle = "#e9e3d6"; for (let i = 0; i < 44; i++) f.fillRect(68 + Math.round(i * 0.75), 4 + i * 1.4, 1, 1);
  f.fillStyle = "#c9974f"; f.fillRect(100, 66, 18, 7); f.fillRect(118, 64, 4, 11); f.fillStyle = "#8a5a2b"; f.fillRect(100, 66, 18, 2);
  f.fillStyle = "#0a0908"; f.fillRect(102, 68, 1, 1);
  f.fillStyle = "#ffd23a"; f.fillRect(130, 82, 12, 5); f.fillRect(142, 81, 3, 7); f.fillStyle = "#f07a2a"; f.fillRect(134, 82, 3, 2);
  f.fillStyle = "#e2412d"; f.fillRect(98, 32, 3, 2); f.fillStyle = "#fff6dc"; f.fillRect(98, 34, 3, 2);
}

function stackArt(s, g, p) {
  s.fillStyle = "#123a3d"; s.fillRect(0, 0, 168, 96);
  s.fillStyle = "#1d5163"; s.fillRect(0, 84, 168, 12);
  [[44, 80], [48, 72], [50, 64], [52, 56], [54, 48], [54, 40]].forEach(([x, w], i) => { s.fillStyle = i % 2 ? "#1f5a2c" : "#2a6a35"; s.fillRect(x, 78 - i * 8, w, 8); s.fillStyle = i % 2 ? "#56b45f" : "#62bf6a"; s.fillRect(x, 78 - i * 8, w, 3); });
  s.fillStyle = "#62bf6a"; s.fillRect(96, 22, 40, 8); s.fillStyle = "#2a6a35"; s.fillRect(96, 25, 40, 5);
  draw(s, side(g, p, false), SW, SH, colours(g, null), 64, 20, 1);
}

function shellArt(sh, g) {
  const fc = colours(g, null);
  sh.fillStyle = "#3f6f7a"; sh.fillRect(0, 0, 168, 40); sh.fillStyle = "#1d5163"; sh.fillRect(0, 40, 168, 20);
  sh.fillStyle = "#4a3a28"; sh.fillRect(0, 60, 168, 36);
  [40, 84, 128].forEach((cx, i) => {
    const by = i === 1 ? 64 : 80;
    for (let k = 0; k < 14; k++) { const hw = Math.round(11 * Math.sqrt(1 - (k / 14) * (k / 14))); sh.fillStyle = "#c9974f"; sh.fillRect(cx - hw, by - k - 1, hw * 2, 1); }
    sh.fillStyle = "#8a5a2b"; sh.fillRect(cx - 3, by - 8, 6, 1); sh.fillRect(cx - 1, by - 10, 4, 1); sh.fillRect(cx - 11, by - 1, 22, 1);
  });
  sh.fillStyle = fc[3]; sh.fillRect(80, 74, 8, 5); sh.fillStyle = fc[1]; sh.fillRect(81, 74, 5, 4); sh.fillRect(88, 76, 3, 2);
}

function skipArt(k, g, p) {
  k.fillStyle = "#638e8a"; k.fillRect(0, 0, 168, 56);
  k.fillStyle = "#2f5a3a"; for (let i = 0; i < 168; i += 4) k.fillRect(i, 46 - (i * 37) % 9, 4, 10 + (i * 37) % 9);
  k.fillStyle = "#1d5163"; k.fillRect(0, 56, 168, 40);
  k.fillStyle = "#4a3a28"; k.fillRect(0, 52, 30, 44); k.fillStyle = "#4f9a4a"; k.fillRect(0, 50, 30, 3);
  draw(k, side(g, p, false), SW, SH, colours(g, null), 2, 33, 1);
  k.fillStyle = "#cfe3ea"; [[46, 6], [78, 8], [104, 9], [124, 10], [140, 11]].forEach(([x, s]) => { k.fillRect(x - s, 57, s - 1, 1); k.fillRect(x + 2, 57, s - 1, 1); });
  k.fillStyle = "rgba(255,246,220,0.5)"; [[56, 46], [62, 42], [90, 50], [96, 48], [114, 53]].forEach(([x, y]) => k.fillRect(x, y, 2, 1));
  k.fillStyle = "#8a9099"; k.fillRect(150, 52, 4, 2); k.fillStyle = "#c9ccd0"; k.fillRect(151, 52, 2, 1);
}

// The far bank, a strip of land along the top of the pond's glass that a
// pond buys a patch at a time. Each patch stands one piece on it, behind
// everything else on the water, and past the thirtieth a sign keeps count.

// Each piece is drawn in 2px cells from its own bottom left corner.
const P = (rows, col) => ({ rows, col });
const WOOD = "#6b4a2a", BARK = "#4a3318", LEAF = "#2f7a3f", LEAF2 = "#3f9a4d", DARK = "#1f5a2c", STONE = "#8a939e", STONE2 = "#5b6470", CREAM = "#fff6dc", RED = "#c9412d", ROOF = "#8a5a2b", HAY = "#e3c57a";

const PIECES = [
  P(["...a...", "..aaa..", ".aabaa.", "..aaa..", ".aaaaa.", "aaabaaa", "...w...", "...w..."], { a: LEAF, b: DARK, w: BARK }),
  P(["..aaa..", ".abaaa.", "aaaabaa", "aabaaaa", ".aaaaa."], { a: LEAF2, b: LEAF }),
  P(["..aaaa.", ".aabaaa", "..aaa..", "...w...", "...k...", "...w...", "...w..."], { a: "#8fcf6a", b: LEAF2, w: "#e9e3d6", k: "#2a2a30" }),
  P(["..aab..", ".aaabb.", "aaabbbc"], { a: STONE, b: STONE2, c: "#3a3f46" }),
  P(["w.w.w.w", "wwwwwww", "w.w.w.w", "w.w.w.w"], { w: "#b08a5a" }),
  P(["...r....", "..rrr...", ".rrrrr..", "rrrrrrr.", ".wwwww..", ".wkwdw..", ".wwwdw.."], { r: RED, w: "#e9e3d6", k: "#ffe27a", d: BARK }),
  P(["..hh..", ".hhhh.", "hhhhhh", "hhhhhh"], { h: HAY }),
  P(["s.....s", ".s...s.", "..sws..", ".s.w.s.", "s..w..s", "..www..", "..wkw..", "..www.."], { s: "#e9e3d6", w: "#b08a5a", k: BARK }),
  P(["rr...", "rrr.r", ".w.rr", ".w..w"], { r: RED, w: CREAM }),
  P(["a.a.a", "a.a.a", "aaaaa", ".aaa."], { a: LEAF }),
  P([".aaaaa.", "aabaaba", "a.a.a.a", "a.aaa.a", "..aaa..", "...w...", "...w..."], { a: "#5aa35a", b: LEAF, w: BARK }),
  P(["wwwww", "w...w"], { w: "#b08a5a" }),
  P([".rrr.", "rrrrr", ".sss.", ".sws.", ".sss."], { r: ROOF, s: STONE, w: "#123a3d" }),
  P(["..aaa..", ".aarra.", "aaaaara", "araaaaa", ".aaaaa.", "...w...", "...w..."], { a: LEAF, r: RED, w: BARK }),
  P([".yy.", "yyyy", "ybyy", "yyyy"], { y: "#e3b23a", b: BARK }),
  P(["...t...", "..ttt..", ".ttttt.", "tttkttt"], { t: "#cf9b5a", k: BARK }),
  P(["rrr", "rwr", "rrr", ".w.", ".w.", ".w."], { r: "#5aa9e6", w: WOOD }),
  P(["p.y.p", ".pyp.", "y.p.y", "ggggg"], { p: "#e2567a", y: "#ffe27a", g: LEAF }),
  P(["sssss", "s...s", "s...s"], { s: STONE }),
  P(["w...", "wbbb", "bbbb", "k..k"], { w: WOOD, b: "#9aa0a6", k: "#2a2a30" }),
  P([".oo.", "oooo", ".oo.", "wwww", "wwww"], { o: "#8a6230", w: BARK }),
  P(["a....a", "aa..aa", ".bbbb.", ".bbbb.", ".b..b."], { a: "#8a5a2b", b: "#b08a5a" }),
  P(["a..", "aa.", "aaa", "a.a"], { a: "#cfc7a8" }),
  P(["y", "w", "w", "w", "w"], { y: "#ffe27a", w: "#2a2a30" }),
  P([".rrrr.", "rrrrrr", "wwwwww", "wwddww"], { r: STONE2, w: "#b08a5a", d: BARK }),
  P(["oooo", "oooo", "oooo"], { o: "#8a6230" }),
  P(["frr", "frr", "f..", "f..", "f..", "f.."], { f: "#cfd3d6", r: "#e2567a" }),
  P(["w.....w", "wwwwwww", ".wwwww."], { w: "#c9974f" }),
  P(["..aaaa..", ".aaaaaa.", "aaabaaaa", "aaaaaaba", ".aaaaaa.", "...ww...", "...ww...", "...ww..."], { a: "#3f8a45", b: DARK, w: BARK }),
  P(["...b...", "..bbb..", ".bbbbb.", "bbbbbbb", "...w...", "...w..."], { b: "#2b6aa8", w: WOOD }),
];

// Thirty spots along the bank, the front row first, filled from the middle
// out so a pond with a few patches looks settled. The left and right ends
// are under the hearts and the camera key, so nothing stands there.
const SPOTS = (() => {
  const all = [];
  for (let i = 0; i < 15; i++) all.push({ x: 108 + i * 11, y: 28, back: false });
  for (let i = 0; i < 15; i++) all.push({ x: 113 + i * 11, y: 20, back: true });
  return all.sort((a, b) => Math.abs(a.x - 190) - Math.abs(b.x - 190) || a.back - b.back);
})();

// How far down the water the bank reaches, for anything that keeps clear of it.
export const FAR_DEPTH = 30;

function piece(ctx, p, x, y) {
  const h = p.rows.length;
  p.rows.forEach((row, r) => [...row].forEach((ch, c) => {
    if (ch === ".") return;
    ctx.fillStyle = p.col[ch];
    ctx.fillRect(x + c * 2, y - (h - r) * 2, 2, 2);
  }));
}

export function drawFarBank(ctx, n) {
  if (!n) return;
  ctx.fillStyle = "#1f3a2a"; ctx.fillRect(0, 0, 352, 20);
  for (let x = 0; x < 352; x += 6) { ctx.fillStyle = "#24452e"; ctx.fillRect(x, 4 + ((x * 7) % 9), 2, 2); }
  const shown = Math.min(n, PIECES.length);
  for (let i = 0; i < shown; i++) if (SPOTS[i].back) piece(ctx, PIECES[i], SPOTS[i].x, SPOTS[i].y);
  ctx.fillStyle = "rgba(18,58,61,0.35)"; ctx.fillRect(0, 0, 352, 20);
  ctx.fillStyle = "#335e3a"; ctx.fillRect(0, 20, 352, 8);
  ctx.fillStyle = "#3f7a45"; ctx.fillRect(0, 20, 352, 2);
  ctx.fillStyle = "#4a3a26"; ctx.fillRect(0, 28, 352, 2);
  for (let i = 0; i < shown; i++) if (!SPOTS[i].back) piece(ctx, PIECES[i], SPOTS[i].x, SPOTS[i].y);
  if (n > PIECES.length) sign(ctx, n - PIECES.length);
}

// The sign past the last piece, with how many patches more there are.
function sign(ctx, more) {
  ctx.fillStyle = WOOD; ctx.fillRect(294, 14, 2, 14);
  ctx.fillStyle = "#c9974f"; ctx.fillRect(284, 4, 22, 12);
  ctx.fillStyle = "#1a120b"; ctx.font = "bold 12px 'JetBrains Mono', monospace"; ctx.textAlign = "center"; ctx.textBaseline = "middle";
  ctx.fillText("+" + more, 295, 10.5, 20);
  ctx.textAlign = "start"; ctx.textBaseline = "alphabetic";
}

// The middle of the bank in Works, where the pieces stand, with the water
// under it. With no patches yet it's bare mud waiting.
export function drawFarBankOn(cv, n) {
  const ctx = cv.getContext("2d");
  ctx.setTransform(1, 0, 0, 1, -90, 0);
  ctx.fillStyle = "#123a3d"; ctx.fillRect(0, 0, 352, 36);
  ctx.fillStyle = "#103336"; ctx.fillRect(0, 32, 352, 4);
  if (n) drawFarBank(ctx, n);
  else {
    ctx.fillStyle = "#2a2118"; ctx.fillRect(0, 0, 352, 28);
    ctx.fillStyle = "#4a3a26"; ctx.fillRect(0, 28, 352, 2);
  }
  ctx.setTransform(1, 0, 0, 1, 0, 0);
}

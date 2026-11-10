// Eight by eight icons, each a grid of letters and the colour of each letter.
export const ICONS = {
  peb: [["........", "..aab...", ".aaaab..", ".aaabbc.", ".abbbcc.", "..bccc..", "........", "........"], { a: "#d5d8dc", b: "#9aa0a6", c: "#6d747a" }],
  cricket: [["......c.", ".....c..", "..aaab..", ".aaaabb.", "aabbbbb.", ".c.c.c..", "c..c..c.", "........"], { a: "#c9974f", b: "#8a6230", c: "#4a3318" }],
  agate: [["..aaaa..", ".abbbba.", "abccccba", "abcbbcba", "abccccba", ".abbbba.", "..aaaa..", "........"], { a: "#e8a05a", b: "#f6dcb0", c: "#b8642e" }],
  quartz: [["...a....", "..aab...", "..aab...", ".aaabb..", ".aaabbc.", ".aabbbc.", "..bbcc..", "........"], { a: "#e6e0f2", b: "#a79fc0", c: "#6f6890" }],
  geode: [["..aaaa..", ".aabbaa.", "aabccbaa", "abccccba", "aabccbaa", ".aabbaa.", "..aaaa..", "........"], { a: "#5b534a", b: "#b88ae0", c: "#ecdcff" }],
  hat: [["..a.a...", ".aaaaa..", "..aba...", ".aaaaa..", "..a.a...", "...c....", "...c....", "........"], { a: "#fff6dc", b: "#f2c230", c: "#2f7a3f" }],
  dye_blue: [["..cc....", "..cc....", ".aaaa...", ".abaa...", ".abaa...", ".aaaa...", ".aaaa...", "........"], { a: "#3d8fd6", b: "#b5dcff", c: "#d5d8dc" }],
  snail: [["........", "..aaa...", ".abbba..", ".ababa.c", ".abbba.c", "..aaaccc", ".ccccccc", "........"], { a: "#8a5a2b", b: "#e3c57a", c: "#cfc7a8" }],
  cap: [["........", "..aaaa..", ".abbbba.", "abbccbba", "abbccbba", ".abbbba.", "..aaaa..", "........"], { a: "#6d747a", b: "#d5d8dc", c: "#e2567a" }],
  feather: [["......a.", ".....ab.", "....abb.", "...abba.", "..abba..", ".caba...", ".cc.....", "c......."], { a: "#9aa0a6", b: "#e9e3d6", c: "#6d747a" }],
  amethyst: [["...a....", "..aba...", ".abbba..", ".abcba..", "abbcbba.", "abbbbba.", ".aaaaa..", "........"], { a: "#6f4aa8", b: "#b88ae0", c: "#ecdcff" }],
  calcite: [["........", "..aaa...", ".abbba..", ".abcbba.", ".abbcba.", "..abbba.", "...aaa..", "........"], { a: "#9aa0a6", b: "#e9e3d6", c: "#ffffff" }],
  pyrite: [["........", ".aaaa...", ".abbaaa.", ".abbabba", ".aaaabba", "...aaaa.", "........", "........"], { a: "#a87a12", b: "#ffe27a" }],
  banded: [["..aaaa..", ".bbbbbb.", "aaaaaaaa", "cccccccc", "aaaaaaaa", ".bbbbbb.", "..aaaa..", "........"], { a: "#b8642e", b: "#f6dcb0", c: "#e8a05a" }],
  fossil: [["........", "..aaaa..", ".abbbba.", ".abaaba.", ".ababba.", ".abbaaa.", "..aaaa..", "........"], { a: "#8a7a5a", b: "#e3d6b0" }],
};

// The wheel names its slices in three letters.
export const SLICE_ICON = { P25: "peb", P40: "peb", CRI: "cricket", AGA: "agate", QUA: "quartz", GEO: "geode", HAT: "hat", DYE: "dye_blue" };
export const SLICE_NAME = { P25: "25 pebbles", P40: "40 pebbles", CRI: "Cricket", AGA: "River agate", QUA: "Smoky quartz", GEO: "Geode", HAT: "Daisy hat", DYE: "Blue dye" };

export function drawIcon(ctx, key, x, y, s) {
  const ic = ICONS[key] || ICONS.peb;
  for (let r = 0; r < 8; r++) for (let c = 0; c < 8; c++) {
    const ch = ic[0][r].charAt(c);
    if (ch !== ".") { ctx.fillStyle = ic[1][ch]; ctx.fillRect(x + c * s, y + r * s, s, s); }
  }
}

export function iconCanvas(key, s) {
  const cv = document.createElement("canvas");
  cv.width = cv.height = 8 * s;
  drawIcon(cv.getContext("2d"), key, 0, 0, s);
  return cv;
}

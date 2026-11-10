// A frog from above, by colour class, 12 wide.
export const TOP = [
  "8..........8",
  "88..4114..88",
  ".8.415514.8.",
  "..11111111..",
  ".1113113111.",
  ".1111111111.",
  ".1131111311.",
  "..11222211..",
  "..81111118..",
  ".88.1111.88.",
  "88........88",
  "8..........8",
];

export function drawTop(x, col, ox, oy, flip) {
  TOP.forEach((row, ry) => [...row].forEach((ch, rx) => {
    if (ch === ".") return;
    x.fillStyle = col[ch];
    x.fillRect(ox + rx, oy + (flip ? 11 - ry : ry), 1, 1);
  }));
}

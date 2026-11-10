import { hatCells, hatColours, W } from "./frog.js";

const boxes = {};

// A hat by itself, in its colours when found and as a shadow when not.
export function hatTile(h, have) {
  const cv = document.createElement("canvas"), ctx = cv.getContext("2d");
  cv.width = 76; cv.height = 48;
  const b = boxes[h.shape] || (boxes[h.shape] = hatCells(h.shape)), col = hatColours(h);
  const ox = Math.floor((76 - (b.x1 - b.x0 + 1) * 4) / 2), oy = Math.floor((48 - (b.y1 - b.y0 + 1) * 4) / 2);
  for (let y = b.y0; y <= b.y1; y++) for (let x = b.x0; x <= b.x1; x++) {
    const v = b.c[y * W + x];
    if (v !== 10 && v !== 11) continue;
    ctx.fillStyle = have ? col[v - 10] : (v === 10 ? "#2f2820" : "#3a3228");
    ctx.fillRect(ox + (x - b.x0) * 4, oy + (y - b.y0) * 4, 4, 4);
  }
  return cv;
}

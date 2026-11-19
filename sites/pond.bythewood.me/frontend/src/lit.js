// frog.js's draw lit from the top left, lighter where the cell above or left
// is empty, darker where the one below or right is, and a shine in each eye.
// Other sites copy frog.js as it is, so this stays in pond.
const LIT = new Set([1, 2, 3, 8, 10, 11]);
const tones = new Map();
let probe = null;

function tone(c, k) {
  const key = c + k;
  if (tones.has(key)) return tones.get(key);
  probe = probe || document.createElement("canvas").getContext("2d");
  probe.fillStyle = "#000";
  probe.fillStyle = c;
  const n = parseInt(probe.fillStyle.slice(1), 16);
  const f = (v) => Math.round(k > 0 ? v + (255 - v) * k : v * (1 + k));
  const out = probe.fillStyle.startsWith("#") ? "rgb(" + f(n >> 16) + "," + f((n >> 8) & 255) + "," + f(n & 255) + ")" : c;
  tones.set(key, out);
  return out;
}

export function draw(ctx, c, w, h, col, ox, oy, px) {
  const at = (x, y) => (x < 0 || y < 0 || x >= w || y >= h ? 0 : c[y * w + x]);
  const shone = [false, false];
  for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) {
    const v = at(x, y);
    let fill;
    if (!v) {
      if (!(at(x - 1, y) || at(x + 1, y) || at(x, y - 1) || at(x, y + 1))) continue;
      fill = col[9];
    } else {
      fill = col[v];
      if (LIT.has(v) && fill) {
        if (!at(x, y - 1) || !at(x - 1, y)) fill = tone(fill, 0.28);
        else if (!at(x, y + 1) || !at(x + 1, y)) fill = tone(fill, -0.28);
      } else if (v === 5) {
        const k = x < w / 2 ? 0 : 1;
        if (!shone[k] && at(x - 1, y - 1) !== 5) { shone[k] = true; fill = "#ffffff"; }
      }
    }
    ctx.fillStyle = fill;
    ctx.fillRect(ox + x * px, oy + y * px, px, px);
  }
}

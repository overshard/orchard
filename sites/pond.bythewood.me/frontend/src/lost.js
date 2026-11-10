import { genome, cells, colours, draw, W, H } from "./frog.js";

// A frog off a random seed, so each wrong turn finds somebody else lost.
const cv = document.getElementById("lost"), ctx = cv.getContext("2d");
const g = genome(Math.floor(Math.random() * 2 ** 31));
const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
const col = colours(g, null);
col.q = "#fff6dc";
const Q = [".qqq.", "q...q", "...q.", "..q..", ".....", "..q.."];

function frame(now) {
  const t = reduced ? 0 : now / 1000, left = Math.floor(t / 2.6) % 2 === 1;
  ctx.clearRect(0, 0, cv.width, cv.height);
  const o = { t, legs: true, blink: !reduced && (t % 3.7) < 0.16 };
  ctx.save();
  if (left) { ctx.translate(cv.width, 0); ctx.scale(-1, 1); }
  draw(ctx, cells(g, "frog", o), W, H, col, (cv.width - W * 4) / 2, 36, 4);
  ctx.restore();
  if (Math.floor(t * 1.4) % 3 !== 0 || reduced) {
    ctx.fillStyle = col.q;
    Q.forEach((r, y) => [...r].forEach((ch, x) => { if (ch === "q") ctx.fillRect(left ? 40 + x * 4 : 156 + x * 4, 4 + y * 4, 4, 4); }));
  }
}

let last = 0;
function loop(now) {
  if (now - last > 120) { last = now; frame(now); }
  requestAnimationFrame(loop);
}
frame(0);
if (!reduced) requestAnimationFrame(loop);

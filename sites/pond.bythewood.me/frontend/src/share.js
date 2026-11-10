import qrcode from "qrcode-generator";
import { genome, cells, colours, draw, croak, W, H } from "./frog.js";

const scene = document.getElementById("scene"), sx = scene.getContext("2d"), d = scene.dataset;
const g = genome(+d.seed), stage = d.stage, hat = { shape: +d.hat, colour: +d.colour };
const grown = stage === "froglet" || stage === "frog" || stage === "elder";
const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
let croakUntil = 0;

function frame(now) {
  const t = now / 1000;
  ["#123a3d", "#103336", "#0e2c30", "#0c262a"].forEach((c, i) => { sx.fillStyle = c; sx.fillRect(0, i * 68, 352, 68); });
  if (grown) {
    for (let y = 222; y < 268; y += 4) for (let x = 20; x < 332; x += 4) {
      const a = (x + 2 - 176) / 150, b = (y + 2 - 246) / 20, dd = a * a + b * b;
      if (dd > 1 || (x > 286 && Math.abs(y - 240) < (x - 286) * 0.3)) continue;
      sx.fillStyle = dd > 0.82 ? "#1f5a2c" : (y < 236 ? "#56b45f" : "#3f9a4d");
      sx.fillRect(x, y, 4, 4);
    }
  }
  const o = { t: reduced ? 0 : t, hat: hat.shape, legs: +d.day >= 6 };
  if (grown && !reduced) {
    o.blink = (t % 4.3) < 0.16;
    o.breathe = Math.sin(t * 2.2) > 0.4 ? 0.5 : 0;
    if (now < croakUntil) o.croak = 0.55 + 0.45 * Math.abs(Math.sin(t * g.rate * 0.5));
  }
  draw(sx, cells(g, stage, o), W, H, colours(g, hat), 0, 0, 8);
}

let last = 0;
function loop(now) {
  if (now - last > 110) { last = now; frame(now); }
  requestAnimationFrame(loop);
}
frame(0);
if (!reduced) requestAnimationFrame(loop);

document.getElementById("scene-btn").addEventListener("click", () => {
  croakUntil = performance.now() + croak(g) * 1000;
});

const qr = document.getElementById("qr"), q = qrcode(0, "M");
q.addData(qr.dataset.url); q.make();
const n = q.getModuleCount(), m = Math.floor(qr.width / n), off = Math.floor((qr.width - n * m) / 2), qx = qr.getContext("2d");
qx.fillStyle = "#fff6dc"; qx.fillRect(0, 0, qr.width, qr.height);
qx.fillStyle = "#0a0908";
for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) if (q.isDark(r, c)) qx.fillRect(off + c * m, off + r * m, m, m);

const copy = document.getElementById("share-copy");
copy.addEventListener("click", () => {
  navigator.clipboard.writeText(qr.dataset.url).then(() => { copy.textContent = "Copied"; }, () => { copy.textContent = "Select it above"; });
});

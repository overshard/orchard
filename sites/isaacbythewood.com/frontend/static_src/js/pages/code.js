// The featured pond card draws a frog off a fresh seed each visit, the same way
// pond deals one to every visitor, and a click deals another.
import { W, H, genome, cells, colours, draw } from "../frog.js";

const PX = 6;

export function initCode() {
  const cv = document.querySelector("[data-pond-frog]");
  if (!cv) return;
  const ctx = cv.getContext("2d"), name = document.querySelector("[data-pond-name]");
  const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
  cv.width = W * PX;
  cv.height = H * PX;
  let g, hat, hop, last = 0, blinkAt = 0;

  function deal() {
    g = genome((Math.random() * 2 ** 32) >>> 0);
    const shape = Math.random() < 0.7 ? 1 + Math.floor(Math.random() * 15) : 0;
    hat = { shape, colour: 0 };
    hop = still ? 0 : 1;
    if (name) name.textContent = g.name + ", " + g.morph.name.toLowerCase();
  }

  function frame(ms) {
    const t = ms / 1000, dt = last ? t - last : 0;
    last = t;
    if (!blinkAt) blinkAt = t + 2 + Math.random() * 4;
    const blink = t > blinkAt;
    if (t > blinkAt + 0.14) blinkAt = t + 2 + Math.random() * 5;
    const lift = hop > 0 ? Math.sin(Math.PI * (1 - hop)) * 5 : 0;
    hop = Math.max(0, hop - dt * 1.6);
    const c = cells(g, "frog", { t: still ? 0 : t, hat: hat.shape, blink, breathe: still ? 0 : Math.sin(t * 2.2) * 0.35, legs: true });
    ctx.clearRect(0, 0, cv.width, cv.height);
    draw(ctx, c, W, H, colours(g, hat), 0, -Math.round(lift) * PX, PX);
    if (!still) requestAnimationFrame(frame);
  }

  deal();
  requestAnimationFrame(frame);
  cv.addEventListener("click", () => {
    deal();
    if (still) requestAnimationFrame(frame);
  });
}

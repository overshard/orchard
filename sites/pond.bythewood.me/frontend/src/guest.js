// A frog from the pond on a page that isn't the pond, the visitor's own in a
// footer and a lost one on a 404. It needs only frog.js beside it, so another
// site takes the two files as they are.
import { W, H, genome, bred, cells, colours, draw } from "./frog.js";

const POND = "https://pond.bythewood.me";
const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
const Q = [".qqq.", "q...q", "...q.", "..q..", ".....", "..q.."];

// Eight frames a second is plenty for a blink, and it leaves the page alone.
function run(frame) {
  let last = 0;
  function loop(now) {
    if (now - last > 120) { last = now; frame(now / 1000); }
    requestAnimationFrame(loop);
  }
  frame(0);
  if (!still) requestAnimationFrame(loop);
}

// A frog off a random seed, so each wrong turn finds somebody else lost.
function lost(cv) {
  const ctx = cv.getContext("2d"), g = genome(Math.floor(Math.random() * 2 ** 31));
  const col = colours(g, null);
  col.q = "#fff6dc";
  run((t) => {
    const left = Math.floor(t / 2.6) % 2 === 1;
    ctx.clearRect(0, 0, cv.width, cv.height);
    ctx.save();
    if (left) { ctx.translate(cv.width, 0); ctx.scale(-1, 1); }
    draw(ctx, cells(g, "frog", { t, legs: true, blink: !still && (t % 3.7) < 0.16 }), W, H, col, (cv.width - W * 4) / 2, 36, 4);
    ctx.restore();
    if (Math.floor(t * 1.4) % 3 !== 0 || still) {
      ctx.fillStyle = col.q;
      Q.forEach((r, y) => [...r].forEach((ch, x) => { if (ch === "q") ctx.fillRect(left ? 40 + x * 4 : 156 + x * 4, 4 + y * 4, 4, 4); }));
    }
  });
}

// What a slot shows when the pond can't be reached, so it's never left empty.
const HOUSE = { house: true, frog: { seed: 1207, name: genome(1207).name, stage: "frog", day: 30 } };

// The visitor's frog, in the hat this site hands out on the visit that finds
// it. The canvas keeps the size the page gave it, so nothing moves when it fills.
function foot(el, d) {
  const f = d.frog, g = bred(genome(f.seed), f.mods);
  const hat = d.found || (f.hat && f.hat.shape ? f.hat : null);
  const col = colours(g, hat), cv = el.querySelector("canvas"), ctx = cv.getContext("2d");
  const name = f.name.charAt(0) + f.name.slice(1).toLowerCase();
  el.querySelector("[data-pond-line]").textContent = d.house
    ? "No frog of your own yet. Hatch one at the pond and it finds a hat here."
    : d.found ? name + " found the " + d.found.name + " here, and it's in your bag at the pond."
    : name + " came along from the pond.";
  run((t) => {
    ctx.clearRect(0, 0, cv.width, cv.height);
    const blink = !still && f.stage !== "egg" && (t % 3.7) < 0.16;
    draw(ctx, cells(g, f.stage, { t, hat: hat ? hat.shape : 0, blink, breathe: Math.sin(t * 1.2) * 0.5, legs: f.day >= 6 }), W, H, col, 0, 0, 2);
  });
}

document.querySelectorAll("[data-pond-lost]").forEach(lost);

const el = document.querySelector("[data-pond-foot]");
if (el) {
  fetch(POND + "/api/panel", { credentials: "include" })
    .then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
    .catch(() => HOUSE)
    .then((d) => foot(el, d));
}

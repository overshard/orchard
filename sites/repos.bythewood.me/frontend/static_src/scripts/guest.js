// A frog from the pond on a page that isn't the pond, the visitor's own as a
// mark in a footer bar and a lost one on a 404. It needs only frog.js beside it, so another
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

// The visitor's frog as a mark in one ink, the colour the page gives its
// slot, so it sits in a footer bar like any other glyph there. It wears the
// hat this site hands out on the visit that finds it. The canvas keeps the size
// the page gave it, so nothing moves when it fills.
function foot(el, d) {
  const f = d.frog, g = bred(genome(f.seed), f.mods);
  const hat = d.found || (f.hat && f.hat.shape ? f.hat : null);
  const cv = el.querySelector("canvas"), ctx = cv.getContext("2d");
  const name = f.name.charAt(0) + f.name.slice(1).toLowerCase();
  el.querySelector("[data-pond-line]").textContent = d.house
    ? "Hatch a frog at the pond and it finds a hat here"
    : d.found ? name + " found the " + d.found.name + " here"
    : name + " came along";
  ctx.fillStyle = getComputedStyle(el).color;
  run((t) => {
    ctx.clearRect(0, 0, cv.width, cv.height);
    const blink = !still && f.stage !== "egg" && (t % 3.7) < 0.16;
    const c = cells(g, f.stage, { t, hat: hat ? hat.shape : 0, blink, legs: f.day >= 6 });
    for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
      const v = c[y * W + x];
      if (!v || v === 5 || v === 6 || v === 11) continue;
      ctx.globalAlpha = v === 4 ? 0.45 : v === 2 ? 0.7 : 1;
      ctx.fillRect(x, y, 1, 1);
    }
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

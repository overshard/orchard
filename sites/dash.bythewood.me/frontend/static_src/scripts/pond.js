// Panel 16, the visitor's frog from the pond. The page is the same for everybody
// and can come out of the edge's cache, so the frog is fetched after load. pond
// is same site, so its cookie rides along and a visitor with no frog gets the
// house frog.
import { W, H, genome, bred, cells, croak } from "./frog.js";

const panel = document.querySelector("[data-pond]");
const POND = "https://pond.bythewood.me";

// One bit, near enough. Amber for the frog, the dim amber only where two parts
// would otherwise run together, and the panel's own black for the face.
const AMBER = "#ffb000", DIM = "#b37b00";
const INK = { 1: AMBER, 2: DIM, 4: AMBER, 7: AMBER, 8: DIM, 10: AMBER, 11: DIM };
// In an egg 7 is the jelly and 8 its skin, so the dim and bright swap.
const EGG = Object.assign({}, INK, { 7: DIM, 8: AMBER });

const STAGES = { egg: "EGG", tadpole: "TADPOLE", froglet: "FROGLET", frog: "FROG", elder: "ELDER" };

function paint(ctx, c, px, ink) {
  ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height);
  for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
    const col = ink[c[y * W + x]];
    if (!col) continue;
    ctx.fillStyle = col;
    ctx.fillRect(x * px, y * px, px, px);
  }
}

const SHOWN = 4;

function todo(d) {
  const ul = panel.querySelector("[data-pond-todo]");
  if (!ul || d.house) return;
  ul.textContent = "";
  const item = (el) => { const li = document.createElement("li"); li.append(el); ul.append(li); };
  if (d.found) {
    const s = document.createElement("span");
    s.textContent = "FOUND THE " + d.found.name.toUpperCase() + " HERE, IT'S IN YOUR BAG";
    item(s);
  }
  d.todo.slice(0, SHOWN).forEach((t) => {
    const a = document.createElement("a");
    a.href = POND + "/#" + t.to; a.target = "_blank"; a.rel = "noopener";
    a.textContent = t.label.toUpperCase();
    item(a);
  });
  const more = d.todo.length - SHOWN, s = document.createElement("span");
  if (more > 0) { s.textContent = "+" + more + " MORE AT THE POND"; item(s); }
  if (!d.todo.length) { s.textContent = "NOTHING WAITING TODAY"; item(s); }
  ul.hidden = false;
}

function show(d) {
  const f = d.frog, g = bred(genome(f.seed), f.mods), hat = f.hat && f.hat.shape ? f.hat.shape : 0;
  const cv = panel.querySelector("canvas"), ctx = cv.getContext("2d"), px = cv.width / W;
  const set = (k, v) => { const el = panel.querySelector("[data-pond-" + k + "]"); if (el) el.textContent = v; };

  set("meta", d.house ? "HOUSE FROG" : f.rarity.label.toUpperCase() + " / 1 IN " + f.rarity.n.toLocaleString("en-US"));
  set("name", f.name);
  set("stage", (STAGES[f.stage] || f.stage.toUpperCase()) + " / DAY " + f.day);
  set("pebbles", d.house ? "—" : d.pebbles.toLocaleString("en-US"));
  set("power", d.house ? "—" : d.power.toLocaleString("en-US") + (d.rank ? " / " + d.rank.toUpperCase() : ""));
  set("go", d.house ? "GET A FROG OF YOUR OWN ↗" : "VISIT THE POND ↗");
  todo(d);
  panel.dataset.state = "ready";

  const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
  let hop = still ? 0 : 1, blinkAt = 0, sac = 0, sacFor = 0, last = 0;

  // The egg wobbles and the tadpole swims on t, a frog breathes, hops once as
  // the panel comes in and blinks now and then.
  function frame(ms) {
    const t = ms / 1000, dt = last ? t - last : 0;
    last = t;
    if (!blinkAt) blinkAt = t + 2 + Math.random() * 4;
    const blink = t > blinkAt;
    if (t > blinkAt + 0.14) blinkAt = t + 2 + Math.random() * 5;
    if (sacFor > 0) { sacFor -= dt; sac = Math.min(1, sac + dt * 6); } else sac = Math.max(0, sac - dt * 4);
    const lift = hop > 0 ? Math.sin(Math.PI * (1 - hop)) * 5 : 0;
    hop = Math.max(0, hop - dt * 1.6);
    const c = cells(g, f.stage, { t: still ? 0 : t, hat, blink, breathe: still ? 0 : Math.sin(t * 2.2) * 0.35, croak: sac, legs: f.day >= 6 });
    ctx.save();
    ctx.translate(0, -Math.round(lift) * px);
    paint(ctx, c, px, f.stage === "egg" ? EGG : INK);
    ctx.restore();
    if (!still) requestAnimationFrame(frame);
  }
  requestAnimationFrame(frame);

  // Sound only ever starts from a tap.
  cv.addEventListener("click", () => {
    sacFor = croak(g);
    if (still) requestAnimationFrame(frame);
  });
}

if (panel) {
  fetch(POND + "/api/panel", { credentials: "include" })
    .then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
    .then(show)
    .catch(() => { panel.dataset.state = "down"; });
}

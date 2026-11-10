import { $, post, state, setPond, watch, reduced, titled, TIERS } from "./api.js";
import { genome, cells, colours, draw, croak, W, H } from "./frog.js";
import { drawIcon, iconCanvas } from "./icons.js";
import { initDive, enterDive, leaveDive } from "./dive.js";
import { initWheel, enterWheel } from "./wheel.js";
import { initHats } from "./hats.js";

const VIEWS = ["pond", "dive", "wheel", "hats"];
let view = "pond";

function show(next) {
  if (!VIEWS.includes(next)) next = "pond";
  if (view === "dive" && next !== "dive") leaveDive();
  view = next;
  VIEWS.forEach((v) => { $("v-" + v).hidden = v !== next; });
  document.querySelectorAll("#tabs button").forEach((b) => {
    b.setAttribute("aria-pressed", b.dataset.view === next ? "true" : "false");
  });
  if (next === "dive") enterDive();
  if (next === "wheel") enterWheel();
  // A view's name is also the id of its canvas, and the browser scrolls to
  // that when the address changes, a frame after this runs.
  window.scrollTo(0, 0);
  requestAnimationFrame(() => window.scrollTo(0, 0));
}

window.addEventListener("load", () => window.scrollTo(0, 0));

$("tabs").addEventListener("click", (e) => {
  const b = e.target.closest("button"), v = b && b.dataset.view;
  if (v) { history.replaceState(null, "", v === "pond" ? location.pathname : "#" + v); show(v); }
});

window.addEventListener("hashchange", () => show(location.hash.slice(1)));

// ---------------------------------------------------------------- the pond

const scene = $("scene"), sx = scene.getContext("2d");
const HEART = ["01010", "11111", "11111", "01110", "00100"];
const NEXT = {
  egg: "A tadpole tomorrow.", tadpole: "A froglet on day 9.", froglet: "A frog on day 13.",
  frog: "An elder on day 29.", elder: "Here for good.",
};
let greetAt = 0, croakUntil = 0, lastTick = 0;

function grown(stage) { return stage === "froglet" || stage === "frog" || stage === "elder"; }

function drawScene(now) {
  const p = state.pond, g = state.g;
  if (!p || !g) return;
  const f = p.frog, t = now / 1000, k = greetAt ? Math.min(1, (now - greetAt) / 2100) : 1, moving = k < 1;
  ["#123a3d", "#103336", "#0e2c30", "#0c262a"].forEach((c, i) => { sx.fillStyle = c; sx.fillRect(0, i * 68, 352, 68); });
  sx.fillStyle = "rgba(127,214,194,0.22)";
  for (let i = 0; i < 6; i++) {
    const x = Math.round(((i * 67 + t * 5) % 392) - 20), y = 14 + (i * 97) % 240;
    sx.fillRect(x, y, 20, 2); sx.fillRect(x + 20, y + 2, 12, 2);
  }
  if (grown(f.stage)) {
    // A lily pad in four pixel blocks, with a notch cut out of it.
    for (let y = 222; y < 268; y += 4) for (let x = 20; x < 332; x += 4) {
      const a = (x + 2 - 176) / 150, b = (y + 2 - 246) / 20, d = a * a + b * b;
      if (d > 1 || (x > 286 && Math.abs(y - 240) < (x - 286) * 0.3)) continue;
      sx.fillStyle = d > 0.82 ? "#1f5a2c" : (y < 236 ? "#56b45f" : "#3f9a4d");
      sx.fillRect(x, y, 4, 4);
    }
  }
  const o = { t: moving ? k * 6 : t, hat: f.hat.shape, legs: f.day >= 6 };
  let hop = 0, wig = 0;
  if (grown(f.stage)) {
    o.blink = moving ? k > 0.1 : (t % 4.3) < 0.16;
    o.breathe = Math.sin(t * 2.2) > 0.4 ? 0.5 : 0;
    if (moving) o.croak = 0.5 + 0.5 * Math.abs(Math.sin(k * 30));
    else if (now < croakUntil) o.croak = 0.55 + 0.45 * Math.abs(Math.sin(t * g.rate * 0.5));
    if (moving) { hop = Math.abs(Math.sin(k * Math.PI * 3.5)) * 50 * (1 - k); wig = Math.sin(k * 44) * 4 * (1 - k); }
  } else if (moving) wig = Math.sin(k * 44) * 6 * (1 - k);
  draw(sx, cells(g, f.stage, o), W, H, colours(g, f.hat), Math.round(wig), -Math.round(hop), 8);
  if (moving) for (let i = 0; i < 5; i++) {
    const ht = k * 1.5 - i * 0.13;
    if (ht <= 0 || ht >= 1) continue;
    const hx = 176 + (i - 2) * 52 + Math.sin(ht * 6 + i) * 8, hy = 96 - ht * 80;
    sx.globalAlpha = 1 - ht; sx.fillStyle = i % 2 ? "#f59ab3" : "#e2567a";
    for (let r = 0; r < 5; r++) for (let c = 0; c < 5; c++) if (HEART[r].charAt(c) === "1") sx.fillRect(Math.round(hx) + c * 4, Math.round(hy) + r * 4, 4, 4);
  }
  sx.globalAlpha = 1;
}

function loop(now) {
  const greeting = greetAt && now - greetAt < 2100;
  if (view === "pond" && (greeting || now - lastTick > 110)) { lastTick = now; drawScene(now); }
  requestAnimationFrame(loop);
}

$("scene-btn").addEventListener("click", () => {
  if (!state.g) return;
  croakUntil = performance.now() + croak(state.g) * 1000;
  if (reduced) { drawScene(performance.now()); setTimeout(() => drawScene(performance.now()), croakUntil - performance.now() + 50); }
});

function renderPond(p) {
  const f = p.frog;
  $("frog-name").textContent = titled(f.name);
  const tier = $("frog-tier");
  tier.textContent = TIERS[f.rarity.tier]; tier.className = "tier t" + f.rarity.tier;
  $("frog-odds").textContent = "1 in " + f.rarity.n.toLocaleString("en-US");
  const stage = f.stage.charAt(0).toUpperCase() + f.stage.slice(1);
  $("frog-what").textContent = stage + ", day " + f.day + ". " + NEXT[f.stage] + " " +
    titled(f.morph) + ", " + f.pattern.toLowerCase() + " back, " + f.nature.toLowerCase() + ".";

  $("purse").hidden = false;
  $("purse-n").textContent = (p.pebbles - state.held).toLocaleString("en-US");
  $("nudge").hidden = !p.nudge;
  $("dev").hidden = !p.dev;

  if (!logTurned) {
    document.querySelectorAll("#logs button").forEach((b) => { b.disabled = !p.log.ready; });
    if (!p.log.ready) $("log-out").textContent = "Turned over for today. It's back tomorrow.";
  }

  const box = $("things");
  box.textContent = "";
  $("things-none").hidden = p.items.length > 0;
  p.items.forEach((it) => {
    const li = document.createElement("li"), name = document.createElement("span");
    name.textContent = it.name + (it.count > 1 ? " x" + it.count : "");
    li.append(iconCanvas(it.kind, 3), name);
    if (it.kind === "geode") {
      const b = document.createElement("button");
      b.type = "button"; b.textContent = "Crack";
      b.addEventListener("click", () => {
        b.disabled = true;
        post("/api/geode").then((d) => { $("thing-out").textContent = d.prize.text; setPond(d.pond); },
          (e) => { $("thing-out").textContent = e.message; b.disabled = false; });
      });
      li.append(b);
    }
    box.append(li);
  });
}

let logTurned = false;
$("logs").addEventListener("click", (e) => {
  const b = e.target.closest("button");
  if (!b || b.disabled || logTurned) return;
  const all = document.querySelectorAll("#logs button");
  all.forEach((x) => { x.disabled = true; });
  post("/api/log", { pick: +b.dataset.pick }).then((d) => {
    logTurned = true;
    all.forEach((x, i) => { x.textContent = d.finds[i].pebbles + " pebbles"; });
    b.setAttribute("aria-pressed", "true");
    $("log-out").textContent = d.finds[d.pick].text;
    setPond(d.pond);
  }, (err) => { $("log-out").textContent = err.message; setPondAgain(); });
});

function setPondAgain() {
  fetch("/api/pond", { credentials: "same-origin" }).then((r) => r.ok ? r.json() : null).then((p) => { if (p) setPond(p); });
}

function hatLine(z) {
  return "A hat. " + z.hat_name + ", " + TIERS[z.hat_tier].toLowerCase() + ", and it's in your hat box" + (z.spare ? " as a spare." : ".");
}

function greet(g) {
  $("greet-line").textContent = g.line;
  const dl = $("greet-missed");
  dl.textContent = "";
  g.missed.forEach((row) => {
    const dt = document.createElement("dt"), dd = document.createElement("dd");
    dt.textContent = row[0]; dd.textContent = row[1];
    dl.append(dt, dd);
  });
  if (g.present) {
    const z = g.present, name = titled(g.pond.frog.name);
    $("gift").hidden = false;
    $("gift-line").textContent = name + " has something for you behind its back.";
    $("gift-open").addEventListener("click", function () {
      this.hidden = true;
      $("gift-line").textContent = z.hat ? hatLine(z) : z.text;
      state.held = 0;
      setPond(state.pond);
    }, { once: true });
  }
  if (!reduced) greetAt = performance.now();
}

$("dev").addEventListener("click", (e) => {
  const h = e.target.dataset && e.target.dataset.hours;
  if (h) post("/api/dev/clock", { hours: +h }).then(() => location.reload());
});

// ---------------------------------------------------------------- boot

drawIcon($("purse-icon").getContext("2d"), "peb", 0, 0, 2);
watch(renderPond);
initDive();
initWheel();
initHats();
show(location.hash.slice(1));

post("/api/greet").then((g) => {
  state.g = genome(g.pond.frog.seed);
  if (g.present && g.present.pebbles) state.held = g.present.pebbles;
  setPond(g.pond);
  greet(g);
  requestAnimationFrame(loop);
}, (err) => { $("greet-line").textContent = err.message; });

import { $, TIERS, reduced } from "./api.js";
import { iconCanvas, dishCanvas, drawIcon } from "./icons.js";
import { hatTile } from "./hats.js";

const TIER_GLOW = ["#bcaf99", "#5ddc82", "#6cb8ff", "#cf9bff", "#ffb000"];
export { TIER_GLOW };

// Anything in the bag as a picture, a hat drawn off the frog's own cells.
export function thingArt(t, s) {
  if (t.hat) {
    const cv = hatTile(t.hat, true);
    if (s) cv.style.width = Math.round(76 * s / 4) + "px";
    return cv;
  }
  if (t.a) return dishCanvas(t, s || 4);
  return iconCanvas(t.kind, s || 4);
}

export function tierTag(t) {
  const s = document.createElement("span");
  s.className = "tier t" + t; s.textContent = TIERS[t];
  return s;
}

// A slot is a square with the picture in it, a count in the corner and the
// tier as its border. An unfound one is a dark shape and three question marks.
export function slot(art, opts) {
  const b = document.createElement(opts.onClick ? "button" : "div");
  if (opts.onClick) { b.type = "button"; b.addEventListener("click", opts.onClick); }
  b.className = "slot t" + opts.tier + (opts.found === false ? " unfound" : "") + (opts.on ? " on" : "");
  if (opts.found === false) shade(art);
  b.append(art);
  if (opts.count > 1) {
    const n = document.createElement("i");
    n.textContent = "x" + opts.count;
    b.append(n);
  }
  const name = document.createElement("span");
  name.className = "nm";
  name.textContent = opts.found === false ? "???" : opts.name || "";
  b.append(name);
  if (opts.sub) {
    const sub = document.createElement("span");
    sub.className = "sub"; sub.textContent = opts.sub;
    b.append(sub);
  }
  b.setAttribute("aria-label", (opts.found === false ? "Not found yet, " : (opts.name || "") + ", ") + TIERS[opts.tier] + (opts.count > 1 ? ", " + opts.count + " of them" : ""));
  return b;
}

// What's not found yet is its outline in the dark.
export function shade(cv) {
  const ctx = cv.getContext && cv.getContext("2d");
  if (!ctx) return cv;
  ctx.globalCompositeOperation = "source-atop";
  ctx.fillStyle = "#3a3026"; ctx.fillRect(0, 0, cv.width, cv.height);
  ctx.globalCompositeOperation = "source-over";
  return cv;
}

// ---------------------------------------------------------------- the sheet

export function openSheet(t, what, build) {
  const art = $("sheet-art");
  art.textContent = "";
  art.append(t.art || thingArt(t, 6));
  $("sheet-name").textContent = t.name;
  const tier = $("sheet-tier");
  tier.hidden = t.tier === undefined;
  tier.textContent = TIERS[t.tier || 0]; tier.className = "tier t" + (t.tier || 0);
  $("sheet-what").textContent = what || "";
  $("sheet-out").textContent = "";
  const body = $("sheet-body");
  body.textContent = "";
  build(body);
  $("sheet").hidden = false;
  $("sheet-x").focus();
}

export function closeSheet() {
  $("sheet").hidden = true;
}

export function sheetSay(msg) { $("sheet-out").textContent = msg; }

export function button(label, fn, cls) {
  const b = document.createElement("button");
  b.type = "button"; b.textContent = label;
  if (cls) b.className = cls;
  b.addEventListener("click", () => fn(b));
  return b;
}

// ---------------------------------------------------------------- the reveal

// A prize comes up in the middle of the screen with rays behind it, coloured
// by its tier, and stays until it's tapped away.
export function reveal(r) {
  const card = $("reveal-card");
  card.className = "reveal-card t" + (r.tier || 0) + (r.bad ? " bad" : "");
  $("reveal-kick").textContent = r.kick || "";
  const art = $("reveal-art");
  art.textContent = "";
  if (r.art) art.append(r.art);
  $("reveal-name").textContent = r.name || "";
  const tier = $("reveal-tier");
  tier.hidden = r.tier === undefined;
  tier.textContent = TIERS[r.tier || 0]; tier.className = "tier t" + (r.tier || 0);
  $("reveal-line").textContent = r.line || "";
  $("reveal-ok").textContent = r.ok || "Nice";
  const box = $("reveal");
  box.hidden = false;
  if (!reduced) { card.classList.remove("pop"); void card.offsetWidth; card.classList.add("pop"); }
  $("reveal-ok").focus();
}

function hideReveal() { $("reveal").hidden = true; }

// A wrapped box drawn in a tint, for the gift and the shop boxes.
export function drawBox(ctx, tint, s) {
  const rows = [".b....b.", "..b..b..", "...bb...", "aaabbaaa", "dddbbddd", ".ccbbcc.", ".ccbbcc.", ".ccbbcc."];
  rows.forEach((row, r) => [...row].forEach((ch, c) => {
    if (ch === ".") return;
    ctx.fillStyle = ch === "b" ? "#fff6dc" : tint;
    ctx.fillRect(c * s, r * s, s, s);
    if (ch === "c" || ch === "d") { ctx.fillStyle = ch === "d" ? "rgba(0,0,0,0.35)" : "rgba(0,0,0,0.15)"; ctx.fillRect(c * s, r * s, s, s); }
  }));
}

export function initUI() {
  $("sheet-x").addEventListener("click", closeSheet);
  $("sheet").addEventListener("click", (e) => { if (e.target === $("sheet")) closeSheet(); });
  $("reveal-ok").addEventListener("click", hideReveal);
  $("reveal").addEventListener("click", (e) => { if (e.target === $("reveal")) hideReveal(); });
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    if (!$("reveal").hidden) hideReveal(); else if (!$("sheet").hidden) closeSheet();
  });
  // Explanations stay out of the way behind a question mark on their head.
  document.querySelectorAll("p.help").forEach((p) => {
    const h = p.previousElementSibling;
    if (!h || h.tagName !== "H2") return;
    const q = document.createElement("button");
    q.type = "button"; q.className = "q"; q.textContent = "?";
    q.setAttribute("aria-label", "What's this");
    q.setAttribute("aria-expanded", "false");
    q.addEventListener("click", () => {
      p.hidden = !p.hidden;
      q.setAttribute("aria-expanded", p.hidden ? "false" : "true");
    });
    h.append(q);
  });
  document.querySelectorAll("canvas.boxart").forEach((cv) => drawBox(cv.getContext("2d"), cv.dataset.tint, 6));
  document.querySelectorAll("canvas[data-icon]").forEach((cv) => drawIcon(cv.getContext("2d"), cv.dataset.icon, 0, 0, cv.width / 8));
}

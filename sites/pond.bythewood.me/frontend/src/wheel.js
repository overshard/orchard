import { $, post, state, setPond, watch, reduced, TIERS } from "./api.js";
import { cells, colours, draw as drawCells, hatCells, hatColours, W, H } from "./frog.js";
import { ICONS, SLICE_ICON, SLICE_NAME, drawIcon } from "./icons.js";

// The wheel is a lily pad the frog points at with its tongue. The server
// picks the slice and this only turns the pad round to it.
const OS = 140, OR = 68, CX = 260, CY = 338, SC = 3.3;
const MONO = "'JetBrains Mono', monospace", GROTESK = "'Space Grotesk', sans-serif";
let wc, wx, woff = null, slices = [], NS = 16, DA = Math.PI / 8;
let wrot = -Math.PI / 2 - Math.PI / 20, spinning = false, tongue = 0, lastSlice = -1, wob = 0, freeAt = 0;

// Paints the pad once, pixel by pixel, with each slice's icon toward the rim.
function build() {
  woff = document.createElement("canvas"); woff.width = woff.height = OS;
  const o = woff.getContext("2d"), c = OS / 2;
  for (let y = 0; y < OS; y++) for (let x = 0; x < OS; x++) {
    const dx = x + 0.5 - c, dy = y + 0.5 - c, r = Math.sqrt(dx * dx + dy * dy);
    if (r > OR) continue;
    let a = Math.atan2(dy, dx); if (a < 0) a += Math.PI * 2;
    const i = Math.min(NS - 1, Math.floor(a / DA)), mid = (i + 0.5) * DA, k = slices[i];
    const edge = Math.min(a - i * DA, (i + 1) * DA - a) * r;
    let col;
    if (r > OR - 4) col = (r > OR - 1.2) ? "#123a1c" : (edge < 1.4 ? "#e9fbe4" : "#1f5a2c");
    else if (r < 7) col = r < 3 ? "#ffe27a" : "#e27aa5";
    else if (r < 10) col = "#f3a9c6";
    else {
      col = k === "DYE" ? (edge < 0.6 ? "#a87a12" : "#e3b23a") : k === "HAT" ? (edge < 0.6 ? "#8f3a63" : "#d8659a") : (edge < 0.6 ? "#1f5a2c" : i % 2 ? "#3f9a4d" : "#56b45f");
      if (edge >= 0.6 && r > OR - 7 && (i % 2 ? 0 : 1) && k !== "DYE" && k !== "HAT") col = "#63c26a";
      const u = r - 51, v = (a - mid) * r, ic = ICONS[iconOf(k)], row = Math.floor((8 - u) / 2), cl = Math.floor((v + 8) / 2);
      if (k === "HAT") { const hp = hatAt((v + dayHat.w * dayHat.sc / 2) / dayHat.sc, (dayHat.h * dayHat.sc / 2 - u) / dayHat.sc); if (hp) col = hp; }
      else if (row >= 0 && row < 8 && cl >= 0 && cl < 8) { const ch = ic[0][row].charAt(cl); if (ch !== ".") col = ic[1][ch]; }
    }
    o.fillStyle = col; o.fillRect(x, y, 1, 1);
  }
}

// The hat and the dye change with the day, so their names and the dye's
// colour come from the pond and not from a table here.
function nameOf(k) {
  const w = state.pond.wheel;
  return k === "HAT" ? w.hat.name : k === "DYE" ? w.dye.name : SLICE_NAME[k];
}
function iconOf(k) { return k === "DYE" ? state.pond.wheel.dye.kind : SLICE_ICON[k]; }

// The hat slice shows the hat itself, cut out of a frog wearing it. fx and fy
// are in hat cells from the top left of the hat.
let dayHat = null;
function hatAt(fx, fy) {
  const b = dayHat.box, cx = b.x0 + Math.floor(fx), cy = b.y0 + Math.floor(fy);
  if (cx < b.x0 || cx > b.x1 || cy < b.y0 || cy > b.y1) return null;
  const v = b.c[cy * W + cx];
  return v === 10 || v === 11 ? dayHat.col[v - 10] : null;
}
// Draws the hat as big as fits a square of this size, on whole pixels.
function drawHat(ctx, x, y, size) {
  const s = Math.max(1, Math.floor(Math.min(size / dayHat.w, size / dayHat.h)));
  const ox = x + Math.floor((size - dayHat.w * s) / 2), oy = y + Math.floor((size - dayHat.h * s) / 2);
  for (let r = 0; r < dayHat.h; r++) for (let c = 0; c < dayHat.w; c++) {
    const col = hatAt(c, r);
    if (col) { ctx.fillStyle = col; ctx.fillRect(ox + c * s, oy + r * s, s, s); }
  }
}
function slotIcon(ctx, k, x, y, s) {
  if (k === "HAT") drawHat(ctx, x, y, s * 8); else drawIcon(ctx, iconOf(k), x, y, s);
}

function under() {
  const a = (((-Math.PI / 2 - wrot) % (Math.PI * 2)) + Math.PI * 2) % (Math.PI * 2);
  return Math.floor(a / DA) % NS;
}

function drawWheel(now) {
  if (!woff || !state.g) return;
  wx.setTransform(1, 0, 0, 1, 0, 0); wx.imageSmoothingEnabled = false;
  wx.fillStyle = "#0e2c30"; wx.fillRect(0, 0, 520, 580);
  wx.fillStyle = "#123a3d"; wx.fillRect(0, 60, 520, 520);
  wx.fillStyle = "#7fd6c2"; for (let x = 0; x < 520; x += 32) { wx.fillRect(x, 60, 18, 2); wx.fillRect(x + 18, 62, 14, 2); }
  wx.strokeStyle = "rgba(127,214,194,0.16)"; wx.lineWidth = 3;
  for (let rr = 0; rr < 3; rr++) { wx.beginPath(); wx.arc(CX, CY, OR * SC + 14 + rr * 16 + ((now || 0) / 260) % 16, 0, 6.3); wx.stroke(); }
  wx.save(); wx.translate(CX, CY); wx.rotate(wrot); wx.drawImage(woff, -OS / 2 * SC, -OS / 2 * SC, OS * SC, OS * SC);
  wx.textAlign = "center"; wx.textBaseline = "middle"; wx.font = "700 30px " + GROTESK;
  for (let si = 0; si < NS; si++) {
    const k = slices[si]; if (k !== "P25" && k !== "P40") continue;
    wx.save(); wx.rotate((si + 0.5) * DA); wx.translate(31 * SC, 0); wx.rotate(-wrot - (si + 0.5) * DA);
    wx.fillStyle = "#123a1c"; wx.fillText(k.slice(1), 2, 2); wx.fillStyle = "#fff6dc"; wx.fillText(k.slice(1), 0, 0); wx.restore();
  }
  wx.restore();
  // Name what the tongue is on, so nobody has to read the rim.
  const on = slices[under()];
  wx.textBaseline = "alphabetic"; wx.textAlign = "left"; wx.font = "500 13px " + MONO;
  wx.fillStyle = "#7fd6c2"; wx.fillText("TONGUE'S ON", 16, 24);
  wx.fillStyle = "#fff6dc"; wx.font = "700 18px " + GROTESK; wx.fillText(nameOf(on).toUpperCase(), 16, 47);
  slotIcon(wx, on, 456, 6, 6);
  const g = state.g, f = state.pond.frog, grownUp = f.stage === "frog" || f.stage === "elder" || f.stage === "froglet";
  const c = cells(g, f.stage, { blink: tongue > 0, hat: f.hat.shape, legs: f.day >= 6, t: 0 });
  drawCells(wx, c, W, H, colours(g, f.hat), CX - 44 + Math.round(wob * 3), grownUp ? 6 : -4, 2);
  const ty = 46, tl = 62 + tongue * 60, tx = CX + Math.round(wob * 5);
  wx.fillStyle = "#e2567a"; wx.fillRect(tx - 3, ty, 6, tl); wx.fillRect(tx - 7, ty + tl - 4, 14, 10);
  wx.fillStyle = "#f59ab3"; wx.fillRect(tx - 1, ty, 2, tl);
}

function clock(ms) {
  const h = Math.floor(ms / 3600000), m = Math.floor(ms / 60000) % 60, s = Math.floor(ms / 1000) % 60;
  return h + ":" + ("0" + m).slice(-2) + ":" + ("0" + s).slice(-2);
}

function ui() {
  const p = state.pond; if (!p) return;
  const w = p.wheel, left = freeAt - performance.now();
  const fb = $("spin-free"), pb = $("spin-paid");
  fb.textContent = w.free > 0 ? "Free spin, " + w.free + " saved" : "Free spin";
  fb.disabled = spinning || w.free < 1;
  pb.textContent = "Spin for " + w.paid_cost;
  pb.disabled = spinning || w.paid_left < 1 || p.pebbles < w.paid_cost;
  let note = w.free >= 2 ? "Two free spins is the most the pond holds, so spend one." : "Next free spin in " + clock(Math.max(0, left)) + ".";
  note += " " + (w.paid_left > 0 ? w.paid_left + " bought spins left today." : "No bought spins left today.");
  $("wheel-note").textContent = note;
  // The countdown ran out, so ask the server what the pond holds now.
  if (w.free < 2 && left <= 0 && !spinning && !asking) {
    asking = true;
    fetch("/api/pond", { credentials: "same-origin" }).then((r) => r.ok ? r.json() : null)
      .then((np) => { if (np) setPond(np); }).finally(() => { setTimeout(() => { asking = false; }, 5000); });
  }
}
let asking = false, builtFor = "";

function prizeText(z) {
  if (z.hat) return "A " + z.hat_name.toLowerCase() + " hat, " + TIERS[z.hat_tier].toLowerCase() + (z.spare ? ". You had one, so it's a spare." : ", and it's in your hat box.");
  return z.text;
}

function spin(paid) {
  if (spinning) return;
  spinning = true; ui();
  $("wheel-out").textContent = "Round it goes.";
  post("/api/wheel/spin", { paid }).then((d) => {
    const mid = (d.slice + 0.2 + Math.random() * 0.6) * DA, from = wrot % (Math.PI * 2);
    let to = -Math.PI / 2 - mid;
    while (to < from + Math.PI * 7) to += Math.PI * 2;
    const t0 = performance.now(), dur = reduced ? 1 : 4200;
    function settle() {
      tongue = 0; wob = 0; spinning = false;
      $("wheel-out").textContent = prizeText(d.prize);
      setPond(d.pond);
      drawWheel(performance.now());
    }
    function tick(now) {
      const k = Math.min(1, (now - t0) / dur), e = 1 - Math.pow(1 - k, 4);
      wrot = from + (to - from) * e;
      const sl = under();
      if (sl !== lastSlice) { lastSlice = sl; wob = 1; }
      wob *= 0.8;
      drawWheel(now);
      if (k < 1) { requestAnimationFrame(tick); return; }
      if (reduced) { settle(); return; }
      const t1 = performance.now();
      (function grab(n2) {
        const q = Math.min(1, (n2 - t1) / 420); tongue = q < 0.5 ? q * 2 : 2 - q * 2; wob = 0; drawWheel(n2);
        if (q < 1) requestAnimationFrame(grab); else settle();
      })(t1);
    }
    requestAnimationFrame(tick);
  }, (err) => { spinning = false; $("wheel-out").textContent = err.message; ui(); });
}

function legend() {
  const seen = {}, box = $("wheel-legend");
  box.textContent = "";
  slices.forEach((k) => { seen[k] = (seen[k] || 0) + 1; });
  Object.keys(seen).sort((a, b) => seen[b] - seen[a]).forEach((k) => {
    const li = document.createElement("li"), n = document.createElement("span"), o = document.createElement("span");
    n.textContent = nameOf(k); o.className = "o"; o.textContent = seen[k] + " in " + NS;
    const cv = document.createElement("canvas");
    cv.width = cv.height = 24; slotIcon(cv.getContext("2d"), k, 0, 0, 3);
    li.append(cv, n, o);
    box.append(li);
  });
}

export function initWheel() {
  wc = $("wheel"); wx = wc.getContext("2d");
  watch((p) => {
    const today = p.wheel.hat.name + p.wheel.dye.kind;
    if (today !== builtFor) { builtFor = today; const box = hatCells(p.wheel.hat.shape), hw = box.x1 - box.x0 + 1;
      // Two pad pixels a cell, or less for a hat too wide for its slice.
      dayHat = { box, col: hatColours(p.wheel.hat), w: hw, h: box.y1 - box.y0 + 1, sc: Math.min(2, 18 / hw) }; slices = p.wheel.slices; NS = slices.length; DA = Math.PI * 2 / NS; build(); legend(); }
    freeAt = performance.now() + p.wheel.next_in * 1000;
    ui();
    if (!spinning && !$("v-wheel").hidden) drawWheel(performance.now());
  });
  $("spin-free").addEventListener("click", () => spin(false));
  $("spin-paid").addEventListener("click", () => spin(true));
  setInterval(() => { if (!$("v-wheel").hidden) ui(); }, 1000);
  if (document.fonts && document.fonts.ready) document.fonts.ready.then(() => { if (!spinning && !$("v-wheel").hidden) drawWheel(performance.now()); });
}

export function enterWheel() { ui(); drawWheel(performance.now()); }

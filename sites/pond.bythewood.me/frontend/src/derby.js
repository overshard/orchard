import { sfx, chime, audioClock, croakAt } from "./sound.js";
import { genome, cells, colours, W as FW, H as FH } from "./frog.js";
import { draw as drawFrog } from "./lit.js";
import { feel } from "./feel.js";
import { reduced, post, state, setPond, aimList } from "./api.js";
import { string } from "./paint.js";
import { PENTA } from "./drop.js";

// The snail derby. The server rolls the race when a snail is backed and sends
// where each snail is every quarter second, what it did and the calls, so the
// page only plays it back between ticks.
const W = 200, H = 320, TICK = 4, SLOW = 0.4, LANES = 4;
const X0 = 24, X1 = 184, LOG = 100, LANE = 32, FACE = LOG + LANES * LANE, PANEL = 248;
const SKY = "#0a1128", PANEL_BG = "#0b1326", CREAM = "#fff6dc", LAMP = "#ffcf6a", SURF = "#7fd6c2", INK = "#0a0908";
const BARK = ["#6a4428", "#5e3c23", "#73492b", "#62401f"], GROOVE = "#3a2414", MOSS = ["#3f8a4a", "#6fd08c"];
const FOOT = "#d8cfae", FOOT_DK = "#a89c78";
const FONT = "600 7px Inter, sans-serif", CALL = "600 9px Inter, sans-serif";

const F = feel();
let G = null, pick = -1, raceAt = -1, live = null;

const busyKinds = new Set(["nap", "worn", "leaf", "hide"]);

function d() { return state.pond && state.pond.derby; }

function owners(card) {
  return card.map((r) => {
    const g = genome(r.seed), hat = { shape: r.hat, colour: 0 };
    return { c: cells(g, "frog", { hat: r.hat }), col: colours(g, hat) };
  });
}

function say(line) {
  if (!live) {
    live = document.createElement("p");
    live.className = "derby-live";
    live.setAttribute("aria-live", "polite");
    document.getElementById("v-derby").append(live);
  }
  live.textContent = line;
}

// Where a lane's snail is at a fraction of a tick, from 0 at the start.
function posAt(lane, T) {
  if (!G.s) return 0;
  const p = G.s.pos[lane], i = Math.min(p.length - 1, Math.floor(T)), j = Math.min(p.length - 1, i + 1);
  return p[i] + (p[j] - p[i]) * (T - Math.floor(T));
}

function xOf(pos) { return X0 + (pos / 1000) * (X1 - X0); }
function laneY(i) { return LOG + i * LANE + LANE / 2; }

// What a lane's snail is busy with at T, if anything.
function doing(lane, T) {
  if (!G.s) return null;
  for (const e of G.s.events) if (e.s === lane && T >= e.t && T < e.t + e.d) return e;
  return null;
}

function birdAt(T) {
  if (!G.s) return null;
  return G.s.events.find((e) => e.k === "bird" && T >= e.t - 1 && T < e.t + e.d + 1) || null;
}

function croak(p, v) {
  const ac = audioClock();
  if (ac) croakAt(p, ac.currentTime + 0.02, v || 0.08, 0.3);
}

// Something a snail did is heard the moment the race reaches it.
function heard(e) {
  const y = e.s >= 0 ? laneY(e.s) : LOG;
  const x = e.s >= 0 ? xOf(posAt(e.s, e.t)) : W / 2;
  switch (e.k) {
    case "nap": case "worn": croak(90, 0.06); F.pop(x, y - 14, "zzz", CREAM); break;
    case "leaf": sfx("chomp"); F.burst(x + 6, y, MOSS[1], 4); break;
    case "hide": sfx("thump"); break;
    case "bird": sfx("warn"); F.hit(0.15); break;
    case "dash": sfx("whip"); F.burst(x - 14, y + 4, CREAM, 5); G.hop[e.s] = 0.5; break;
    case "slime": sfx("dew"); F.burst(x - 12, y + 6, "#cfe8ff", 4); break;
  }
}

function lead(T) {
  let best = 0;
  for (let i = 1; i < LANES; i++) if (posAt(i, T) > posAt(best, T)) best = i;
  return best;
}

function finish() {
  const s = G.s, b = G.b, x = X1 + 2, y = laneY(s.win);
  G.phase = "line"; G.lineT = 0;
  G.hop[s.win] = 9;
  if (s.photo) { sfx("zap"); G.flash = reduced ? 0 : 1; }
  F.burst(x, y, LAMP, 14); F.burst(x, y, "#f29bb8", 8);
  [0, 2, 4, 7, 9].forEach((n, i) => setTimeout(() => chime(PENTA[n + 2], 0.035), i * 90));
  if (b.pick === s.win) {
    G.paid = true;
    setTimeout(() => { F.got(x - 10, y - 6, "+" + b.won, { big: true }); }, s.photo ? 700 : 300);
  }
}

export const derby = {
  key: "derby", W, H, title: "THE SNAIL DERBY", feel: F, edge: [SKY, PANEL_BG], full: true, scoreWord: "Slip paid",
  aria: "A snail race on a mossy log. Pick a snail on the board, or leave it and the slip goes on the favourite, then watch the race called as it goes.",
  help: [],
  tiles: [["Race", "race", "1/3"], ["Today", "today", "0"], ["Slip", "pays", "6"]],

  info() {
    const v = d();
    return v ? { best: 0, paid: v.paid, cap: v.cap, aims: v.aims } : null;
  },

  note() {
    const v = d();
    if (!v) return;
    document.getElementById("derby-race").textContent = v.race ? v.race + "/" + v.of : "Done";
    document.getElementById("derby-today").textContent = v.paid + "/" + v.cap;
    document.getElementById("derby-pays").textContent = String(v.slip);
  },

  reset(g, p) {
    const v = p.derby;
    if (v.race !== raceAt) { pick = -1; raceAt = v.race; }
    const card = v.card && v.card.length ? v.card : null;
    G = {
      v, card, own: card ? owners(card) : [], phase: "board", t: 0, gate: 0, T: 0, last: 0, s: null, b: null,
      calls: [], call: null, callAt: 0, lead: -1, hop: [0, 0, 0, 0], ph: [0, 0, 0, 0], was: [0, 0, 0, 0],
      slow: 1, flash: 0, lineT: 0, end: false, failed: "", paid: false, count: -1,
    };
    F.reset();
  },

  begin() {
    const v = G.v;
    G.phase = "gate";
    return post("/api/derby/back", { day: v.day, race: v.race, pick }).then((b) => b, (err) => {
      G.failed = err.message || "That race has been run";
      fetch("/api/pond", { credentials: "same-origin" }).then((r) => (r.ok ? r.json() : null)).then((p) => { if (p) setPond(p); });
      return { run: null };
    });
  },

  dealt(b) {
    if (!b || !b.script) return;
    G.b = b; G.s = b.script; G.calls = b.calls; G.card = b.card;
  },

  end() { return Promise.resolve(G.b); },

  step(dt) {
    G.t += dt;
    G.hop = G.hop.map((h) => Math.max(0, h - dt));
    G.flash = Math.max(0, G.flash - dt * 2.5);
    if (G.failed) { G.end = true; return; }
    if (G.phase === "gate") {
      G.gate += dt;
      const n = Math.min(3, Math.floor(G.gate / 0.8));
      if (n !== G.count && (n < 3 || G.s)) {
        G.count = n;
        if (n < 3) { sfx("pip"); say(3 - n + ""); } else {
          G.phase = "race"; G.T = 0;
          chime(PENTA[4], 0.04); setTimeout(() => chime(PENTA[7], 0.04), 110);
          F.burst(X0, LOG - 2, CREAM, 6);
        }
      }
      return;
    }
    if (G.phase === "race") {
      const s = G.s, end = s.ticks;
      const want = s.photo && G.T > end - 4 ? SLOW : 1;
      G.slow += (want - G.slow) * Math.min(1, dt * 8);
      const was = G.T;
      G.T = Math.min(end, G.T + dt * TICK * G.slow);
      s.events.forEach((e) => { if (e.t > was && e.t <= G.T) heard(e); });
      for (const c of G.calls) if (c.t <= G.T && (!G.call || c.t > G.call.t)) { G.call = c; G.callAt = G.t; say(c.line); }
      const l = lead(G.T);
      if (l !== G.lead && G.T > 1) { if (G.lead >= 0) { sfx("pop"); G.hop[l] = 0.6; } G.lead = l; }
      for (let i = 0; i < LANES; i++) {
        const x = xOf(posAt(i, G.T));
        G.ph[i] += (x - G.was[i]) * 0.9;
        G.was[i] = x;
      }
      if (G.T >= end) finish();
      return;
    }
    if (G.phase === "line") {
      G.slow += (1 - G.slow) * Math.min(1, dt * 4);
      G.lineT += dt;
      if (G.lineT > 2.6) G.end = true;
    }
  },

  over() { return G && G.end; },
  score() { return G && G.b ? G.b.won : 0; },
  why() {
    if (!G) return "";
    if (G.failed) return G.failed;
    if (!G.s) return "";
    const n = G.card[G.s.win].name;
    return n + (G.s.photo ? " wins the photo" : " wins");
  },
  again() { const v = d(); return v && v.race ? "Tap for race " + v.race : "Tap to see the board"; },

  card() {
    if (!G || !G.s) return null;
    const s = G.s, b = G.b, box = el("div", "derbycard"), ol = el("ol", "order");
    s.order.forEach((lane, place) => {
      const r = G.card[lane], li = el("li", lane === b.pick ? "mine" : "");
      li.append(snailArt(r, 1), el("b", "", r.name), el("span", "", r.odds));
      if (lane === b.pick) li.append(el("i", "", "your slip"));
      if (place === 0) li.classList.add("first");
      ol.append(li);
    });
    const mine = G.card[b.pick], at = s.order.indexOf(b.pick);
    const line = b.pick === s.win ? "Your slip on " + mine.name + " came in at " + mine.odds + "."
      : mine.name + " came " + ["first", "second", "third", "last"][at] + ". " + (b.picked ? "" : "The slip went on the favourite. ") + "The slip was free, so no harm done.";
    box.append(ol, el("p", "slipline", line));
    return box;
  },

  // The odds board is the title card, a key a snail and one to send them off.
  board(start) {
    const box = el("div", "derbyboard");
    const fill = () => {
      const v = d();
      if (!v) return;
      if (!v.race) {
        const ul = el("ul", "ran");
        (v.ran || []).forEach((r) => {
          const li = el("li", r.pick === r.win ? "mine" : "");
          li.append(el("b", "", "Race " + r.race), el("span", "", r.win + " won" + (r.pick === r.win ? ", your slip at " + r.odds : ", you had " + r.pick)));
          ul.append(li);
        });
        box.replaceChildren(el("b", "dtitle", "That's the racing for today"), ul, el("p", "going", v.won ? "You picked " + v.won + (v.won > 1 ? " winners" : " winner") + " today." : "The snails are having a lie down."), el("p", "going", "Three more races tomorrow, same log."), aimList(v.aims));
        return;
      }
      const head = el("p", "dhead");
      head.append(el("b", "", "Race " + v.race + " of " + v.of), el("span", "", "free slip " + v.slip));
      const ul = el("ul", "runners");
      v.card.forEach((r, i) => {
        const b = el("button", "runner" + (i === pick ? " picked" : "") + (r.fav ? " fav" : ""));
        b.type = "button";
        b.setAttribute("aria-pressed", i === pick ? "true" : "false");
        const who = el("span", "dwho");
        who.append(el("b", "", r.name), el("span", "", r.owner + "'s, " + r.tip.toLowerCase()));
        const odds = el("span", "dodds");
        odds.append(el("em", "", r.odds), el("i", "", r.fav ? "favourite" : "pays " + r.pays));
        b.append(snailArt(r, 2), who, odds);
        b.addEventListener("pointerdown", (e) => e.stopPropagation());
        b.addEventListener("click", () => { pick = pick === i ? -1 : i; fill(); });
        const li = el("li");
        li.append(b);
        ul.append(li);
      });
      const fav = v.card.find((r) => r.fav), go = el("button", "key", pick < 0 ? "Off, on " + fav.name : "Back " + v.card[pick].name);
      go.type = "button";
      go.addEventListener("pointerdown", (e) => e.stopPropagation());
      go.addEventListener("click", () => start());
      box.replaceChildren(head, el("p", "going", v.said), ul, aimList(v.aims), go);
    };
    fill();
    return box;
  },

  boardKey(code, start) {
    const v = d();
    if (!v || !v.race) return false;
    if (code === "Enter" || code === "Space") { start(); return false; }
    if (code === "ArrowUp" || code === "KeyW") pick = pick <= 0 ? LANES - 1 : pick - 1;
    else if (code === "ArrowDown" || code === "KeyS") pick = (pick + 1) % LANES;
    else return false;
    return true;
  },

  draw(x) {
    scene(x);
    if (!G) return;
    const card = G.card || [];
    const T = G.phase === "race" || G.phase === "line" ? G.T : 0;
    bank(x, card, T);
    const z = reduced ? 0 : (1 - G.slow) / (1 - SLOW);
    x.save();
    if (z > 0.01) {
      const cy = G.s ? (laneY(G.s.order[0]) + laneY(G.s.order[1])) / 2 : LOG + 64;
      x.translate(X1, cy); x.scale(1 + 0.08 * z, 1 + 0.08 * z); x.translate(-X1, -cy);
    }
    log(x);
    card.forEach((r, i) => trail(x, i, T, r));
    finishLine(x);
    card.forEach((r, i) => snail(x, r, i, T));
    bird(x, T);
    x.restore();
    if (G.phase === "gate") countdown(x);
    if (G.phase === "line" && G.s) winner(x);
    panel(x);
    F.draw(x);
    if (G.flash > 0) { x.globalAlpha = G.flash * 0.7; x.fillStyle = "#ffffff"; x.fillRect(0, 0, W, PANEL); x.globalAlpha = 1; }
  },
};

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

// ---------------------------------------------------------------- drawing

function rect(x, c, a, b, w, h) { x.fillStyle = c; x.fillRect(Math.round(a), Math.round(b), w, h); }

// The sky over the bank, a string of lanterns and the log with its lanes.
function scene(x) {
  ["#0a1128", "#0d1630", "#111c38", "#152140"].forEach((c, i) => rect(x, c, 0, i * 16, W, 16));
  [[14, 30], [52, 12], [98, 26], [140, 8], [176, 34], [120, 44], [30, 52]].forEach(([a, b]) => rect(x, "#8a96d0", a, b, 1, 1));
  string(x, -4, W + 4, 4, 10, 6);
  rect(x, "#16261c", 0, 76, W, 24);
  for (let i = 0; i < W; i += 3) rect(x, i % 2 ? "#1f3a26" : "#2a4a26", i, 72 + ((i * 37) % 5), 2, 6);
}

function log(x) {
  for (let i = 0; i < LANES; i++) {
    const y = LOG + i * LANE;
    rect(x, BARK[i], 0, y, W, LANE);
    for (let k = 0; k < 9; k++) rect(x, "#7a5232", (k * 53 + i * 31) % W, y + 6 + ((k * 7 + i * 3) % 18), 10 + (k % 3) * 6, 1);
    rect(x, GROOVE, 0, y + LANE - 1, W, 2);
  }
  rect(x, "#8a6230", 0, LOG, W, 2);
  for (let i = 0; i < W; i += 7) { rect(x, MOSS[0], i, LOG - 2, 5, 3); if (i % 21 === 0) rect(x, MOSS[1], i + 1, LOG - 3, 3, 2); }
  rect(x, "#4a2e1a", 0, FACE, W, 12);
  for (let i = 0; i < W; i += 9) rect(x, "#3a2414", i, FACE + 3 + (i % 4), 5, 2);
  rect(x, "#1d5163", 0, FACE + 12, W, PANEL - FACE - 12);
  rect(x, "#2a6a7a", 12, FACE + 15, 18, 1); rect(x, "#2a6a7a", 120, FACE + 17, 24, 1);
  // The start, chalked on the bark.
  for (let y = LOG + 2; y < FACE - 1; y += 3) rect(x, "rgba(233,227,214,0.75)", X0 + 1, y, 1, 2);
}

function finishLine(x) {
  for (let y = LOG; y < FACE; y += 3) for (let k = 0; k < 2; k++) rect(x, (y / 3 + k) % 2 < 1 ? CREAM : INK, X1 + 1 + k * 3, y, 3, 3);
  rect(x, "#5b6470", X1 + 7, LOG - 26, 1, 26);
  for (let y = 0; y < 3; y++) for (let k = 0; k < 4; k++) rect(x, (y + k) % 2 ? INK : CREAM, X1 + 8 + k * 2, LOG - 26 + y * 2, 2, 2);
}

function trail(x, i, T, r) {
  const p = posAt(i, T);
  if (p < 4) return;
  const y = laneY(i) + 7;
  x.globalAlpha = 0.28; rect(x, "#cfe8ff", X0 - 12, y, xOf(p) - X0, 1); x.globalAlpha = 1;
  const e = doing(i, T) || slick(i, T);
  if (e && e.k === "slime") for (let k = 0; k < 4; k++) if ((Math.floor(G.t * 8) + k) % 3 === 0) rect(x, "#ffffff", xOf(p) - 18 - k * 5, y - 1, 1, 1);
}

function slick(i, T) {
  if (!G.s) return null;
  return G.s.events.find((e) => e.s === i && (e.k === "slime" || e.k === "dash") && T >= e.t && T < e.t + e.d) || null;
}

// A snail at its place on the log, its foot inching as it goes. Busy snails
// draw what they're busy with, a nap, a leaf or a shell with nobody home.
function snail(x, r, i, T) {
  const hx = xOf(posAt(i, T)), base = laneY(i) + 8, e = doing(i, T), go = slick(i, T);
  const sx = hx - 10, sy = base - 7;
  const mine = G.b ? G.b.pick === i : pick === i;
  // The name rides over the shell, lamp yellow on the snail you backed.
  x.font = FONT; x.textAlign = "center";
  const ny = base - (r.look === "hat" ? 21 : 16);
  x.lineWidth = 2; x.strokeStyle = INK; x.strokeText(r.name, sx, ny);
  x.fillStyle = mine ? LAMP : CREAM; x.fillText(r.name, sx, ny);
  if (mine) ticket(x, sx - 14 - x.measureText(r.name).width / 2, ny - 5);
  const tucked = e && (e.k === "hide" || e.k === "nap" || e.k === "worn");
  let shake = 0;
  if (e && e.k === "hide" && !reduced) shake = Math.round(Math.sin(G.t * 40)) * 0.5;
  if (!tucked) {
    const stretch = go || reduced || (e && e.k === "leaf") ? 0 : Math.sin(G.ph[i]) * 1.5;
    rect(x, FOOT_DK, hx - 17 - stretch, base - 1, 18 + stretch, 2);
    rect(x, FOOT, hx - 16 - stretch, base - 4, 17 + stretch, 3);
    rect(x, FOOT, hx - 3, base - 7, 5, 4);
    rect(x, "#fff3d6", hx - 2, base - 7, 2, 1);
    // Eye stalks, swept back when it's going flat out.
    const back = go && go.k === "dash" ? 3 : 0, wob = reduced ? 0 : Math.round(Math.sin(G.t * 3 + i) * 0.6);
    stalk(x, hx, base - 7, hx + 2 - back, base - 13 + wob);
    stalk(x, hx - 2, base - 7, hx - 1 - back, base - 12 - wob);
    if (go && go.k === "dash") for (let k = 0; k < 3; k++) rect(x, "rgba(255,246,220,0.55)", sx - 14 - k * 3 - ((G.t * 30) % 4), base - 10 + k * 3, 6, 1);
  } else {
    rect(x, FOOT_DK, sx - 5, base - 1, 11, 2);
  }
  shell(x, r, sx + shake, sy);
  if (e && (e.k === "nap" || e.k === "worn")) zzz(x, sx + 5, sy - 6, e, T);
  if (e && e.k === "worn") rect(x, "#9fc8ff", sx - 8, sy - 3, 1, 2);
  if (e && e.k === "leaf") leaf(x, hx + 3, base - 6, 1 - (T - e.t) / e.d);
  if (e && e.k === "hide") { rect(x, INK, sx - 2, sy + 2, 2, 1); rect(x, "#ffffff", sx - 2, sy + 2, 1, 1); }
}

function stalk(x, a, b, c, d2) {
  x.strokeStyle = FOOT; x.lineWidth = 1;
  x.beginPath(); x.moveTo(a + 0.5, b); x.lineTo(c + 0.5, d2); x.stroke();
  rect(x, INK, c, d2 - 1, 2, 2);
}

// A round shell in the snail's own colour with a spiral on it, and a hat on
// the one that wears one.
function shell(x, r, cx, cy) {
  const R = 7;
  for (let y = -R; y <= R; y++) {
    const w = Math.round(Math.sqrt(R * R - y * y));
    rect(x, "#1a1208", cx - w - 1, cy + y, w * 2 + 2, 1);
  }
  for (let y = -R + 1; y <= R - 1; y++) {
    const w = Math.round(Math.sqrt((R - 1) * (R - 1) - y * y));
    rect(x, r.shell, cx - w, cy + y, w * 2, 1);
  }
  x.globalAlpha = 0.35; x.fillStyle = "#ffffff"; x.fillRect(cx - 4, cy - 5, 3, 2); x.globalAlpha = 1;
  x.strokeStyle = "rgba(10,9,8,0.5)"; x.lineWidth = 1;
  x.beginPath();
  for (let a = 0; a < Math.PI * 3.4; a += 0.2) { const rr = 0.6 + a * 0.5; x.lineTo(cx + Math.cos(a) * rr, cy + Math.sin(a) * rr); }
  x.stroke();
  if (r.look === "hat") { rect(x, INK, cx - 5, cy - R - 2, 10, 2); rect(x, INK, cx - 3, cy - R - 5, 6, 3); rect(x, "#a8424a", cx - 3, cy - R - 3, 6, 1); }
}

function zzz(x, a, b, e, T) {
  x.font = FONT; x.textAlign = "left"; x.fillStyle = CREAM;
  for (let k = 0; k < 3; k++) {
    const f = ((T - e.t) / 3 + k / 3) % 1, rise = reduced ? k * 4 : f * 12;
    x.globalAlpha = reduced ? 0.8 : 1 - f;
    x.fillText("z", a + k * 3, b - rise);
  }
  x.globalAlpha = 1;
}

function leaf(x, a, b, left) {
  const w = Math.max(3, Math.round(13 * left));
  rect(x, "#3f8a4a", a, b - 4, w, 9);
  rect(x, MOSS[1], a, b - 5, w, 3); rect(x, MOSS[1], a + 1, b + 5, Math.max(1, w - 2), 1);
  rect(x, "#a8e8a0", a, b, w, 1);
  for (let k = 2; k < w; k += 4) rect(x, "#a8e8a0", a + k, b - 3, 1, 3);
}

function ticket(x, a, b) {
  rect(x, LAMP, a, b, 9, 6); rect(x, "#a87a12", a + 1, b + 2, 7, 1);
}

// The blackbird drops onto the log, hops about and goes again.
function bird(x, T) {
  const e = birdAt(T);
  if (!e) return;
  const into = T - (e.t - 1), out = T - (e.t + e.d);
  const bx = W / 2 + 10 * Math.sin(e.t);
  let by = LOG + LANE * 2 - 4, fly = 0;
  if (into < 1) { fly = 1 - into; by -= fly * 60; }
  if (out > 0) { fly = out; by -= out * 60; }
  const hop = fly ? 0 : Math.abs(Math.round(Math.sin(G.t * 9) * 2));
  const ax = bx + (into < 1 ? fly * 40 : out > 0 ? -out * 40 : Math.round(Math.sin(G.t * 1.7) * 4));
  x.globalAlpha = 0.35; rect(x, INK, ax - 9, LOG + LANE * 2 + 2, 18, 3); x.globalAlpha = 1;
  const flap = fly ? Math.round(Math.sin(G.t * 30) * 3) : 0;
  x.save();
  x.translate(Math.round(ax), Math.round(by - hop)); x.scale(1.5, 1.5);
  rect(x, "#141414", -6, -6, 12, 6);
  rect(x, "#141414", 3, -10, 5, 5);
  rect(x, "#f2a43a", 8, -8, 3, 1);
  rect(x, "#f2a43a", 5, -9, 1, 1);
  rect(x, "#2a2a2a", -10, -6, 5, 2);
  rect(x, "#262626", -3, -7 - flap, 6, 2);
  if (!fly) { rect(x, "#f2a43a", -2, 0, 1, 3); rect(x, "#f2a43a", 2, 0, 1, 3); }
  x.restore();
}

// The four owners on the bank, each with a pennant in its snail's colour,
// hopping when their snail does something worth hopping for.
function bank(x, card, T) {
  card.forEach((r, i) => {
    const o = G.own[i];
    if (!o) return;
    const ox = 2 + i * 49, h = G.hop[i], up = reduced ? 0 : h > 0 ? Math.abs(Math.round(Math.sin(h * 14) * 4)) : 0;
    drawFrog(x, o.c, FW, FH, o.col, ox, 44 - up, 1);
    const fx = ox + 40, wave = reduced ? 0 : Math.round(Math.sin(G.t * (h > 0 ? 14 : 2) + i) * (h > 0 ? 2 : 1));
    rect(x, "#8a6230", fx, 50 - up, 1, 26);
    x.font = "600 6px Inter, sans-serif"; x.textAlign = "center"; x.fillStyle = "#9aa6c2";
    x.fillText(r.owner, ox + 22, 94);
    x.fillStyle = r.shell;
    x.beginPath(); x.moveTo(fx + 1, 50 - up); x.lineTo(fx + 9, 53 - up + wave); x.lineTo(fx + 1, 57 - up); x.fill();
  });
  if (!G.s || T <= 0) return;
  const l = lead(T);
  rect(x, LAMP, 2 + l * 49 + 18, 40, 8, 2);
}

function countdown(x) {
  const n = Math.min(3, Math.floor(G.gate / 0.8));
  const word = n < 3 ? String(3 - n) : G.s ? "OFF" : "1";
  const f = (G.gate % 0.8) / 0.8, k = reduced ? 1 : 1 + 0.4 * Math.max(0, 1 - f * 4);
  x.save();
  x.translate(W / 2, LOG + 70); x.scale(k, k);
  x.font = "800 34px Inter, sans-serif"; x.textAlign = "center"; x.textBaseline = "middle";
  x.lineWidth = 5; x.strokeStyle = INK; x.strokeText(word, 0, 0);
  x.fillStyle = LAMP; x.fillText(word, 0, 0);
  x.restore();
  x.textBaseline = "alphabetic";
}

function winner(x) {
  const s = G.s, r = G.card[s.win], k = reduced ? 1 : 1 + 0.25 * Math.max(0, 1 - G.lineT * 5);
  if (s.photo && G.lineT < 0.7) {
    x.font = "800 18px Inter, sans-serif"; x.textAlign = "center";
    x.lineWidth = 4; x.strokeStyle = INK; x.strokeText("PHOTO", W / 2, LOG + 70);
    x.fillStyle = SURF; x.fillText("PHOTO", W / 2, LOG + 70);
    return;
  }
  x.save();
  x.translate(W / 2, LOG + 68); x.scale(k, k);
  x.font = "800 20px Inter, sans-serif"; x.textAlign = "center";
  x.lineWidth = 5; x.strokeStyle = INK;
  const word = r.name.toUpperCase();
  x.strokeText(word, 0, 0); x.fillStyle = LAMP; x.fillText(word, 0, 0);
  x.font = "700 10px Inter, sans-serif"; x.lineWidth = 3;
  const sub = G.b.pick === s.win ? "YOUR SNAIL WINS" : "WINS AT " + r.odds.toUpperCase();
  x.strokeText(sub, 0, 15); x.fillStyle = G.b.pick === s.win ? "#9fe6b8" : CREAM; x.fillText(sub, 0, 15);
  x.restore();
}

// The commentary box under the log, the line typing itself out as it's called.
function panel(x) {
  rect(x, PANEL_BG, 0, PANEL, W, H - PANEL);
  rect(x, "#22345a", 4, PANEL + 4, W - 8, H - PANEL - 8);
  rect(x, PANEL_BG, 5, PANEL + 5, W - 10, H - PANEL - 10);
  const v = G.v;
  x.font = "700 6px Inter, sans-serif"; x.textAlign = "left"; x.fillStyle = SURF;
  x.fillText("THE CALL", 11, PANEL + 15);
  x.textAlign = "right"; x.fillStyle = "#8a96b0";
  const race = G.b ? G.b.race : v.race;
  x.fillText(race ? "RACE " + race + " OF " + v.of + ", " + v.going.toUpperCase() : "BACK TOMORROW", W - 11, PANEL + 15);
  let line = v.said;
  if (G.phase === "gate") line = "They're under starter's orders.";
  if (G.call) line = G.call.line;
  if (G.failed) line = G.failed;
  const typed = reduced || !G.call ? line.length : Math.floor((G.t - G.callAt) * 70);
  x.font = CALL; x.textAlign = "left"; x.fillStyle = CREAM;
  wrap(x, line, W - 22).forEach((l, k, all) => {
    const before = all.slice(0, k).join(" ").length + (k ? 1 : 0);
    const show = l.slice(0, Math.max(0, typed - before));
    if (show) x.fillText(show, 11, PANEL + 29 + k * 11);
  });
  const p = G.b ? G.b.pick : pick, card = G.card || [];
  x.font = "700 7px Inter, sans-serif";
  if (card[p]) { x.fillStyle = LAMP; x.fillText("Your slip: " + card[p].name + ", " + card[p].odds, 11, H - 11); }
  else if (card.length) { x.fillStyle = "#8a96b0"; x.fillText("No pick yet, the slip goes on the favourite", 11, H - 11); }
}

function wrap(x, s, w) {
  const out = [];
  let cur = "";
  for (const word of s.split(" ")) {
    const t = cur ? cur + " " + word : word;
    if (x.measureText(t).width > w && cur) { out.push(cur); cur = word; } else cur = t;
  }
  if (cur) out.push(cur);
  return out.slice(0, 4);
}

// A snail on its own on a little canvas, for the board and the results.
export function snailArt(r, s) {
  const cv = document.createElement("canvas");
  cv.width = 30 * s; cv.height = 20 * s;
  cv.setAttribute("aria-hidden", "true");
  const x = cv.getContext("2d");
  x.scale(s, s);
  rect(x, FOOT_DK, 4, 17, 20, 2);
  rect(x, FOOT, 5, 14, 19, 3);
  rect(x, FOOT, 21, 11, 5, 4);
  stalk(x, 24, 11, 26, 5);
  stalk(x, 22, 11, 23, 6);
  shell(x, r, 13, 10);
  return cv;
}

// The picture on Play and in the card's corner.
export function derbyArt(x) {
  rect(x, SKY, 0, 0, 168, 96);
  rect(x, "#16261c", 0, 22, 168, 8);
  [0, 1, 2].forEach((i) => rect(x, BARK[i], 0, 30 + i * 20, 168, 20));
  [0, 1, 2].forEach((i) => rect(x, GROOVE, 0, 49 + i * 20, 168, 1));
  rect(x, "#4a2e1a", 0, 90, 168, 6);
  for (let y = 30; y < 90; y += 3) for (let k = 0; k < 2; k++) rect(x, (y / 3 + k) % 2 < 1 ? CREAM : INK, 150 + k * 3, y, 3, 3);
  [{ shell: "#d8a25a", at: 120 }, { shell: "#9aa6c2", at: 70 }, { shell: "#f29bb8", at: 96 }].forEach((r, i) => {
    const base = 46 + i * 20;
    rect(x, FOOT, r.at - 16, base - 4, 17, 3); rect(x, FOOT, r.at - 3, base - 7, 5, 4);
    stalk(x, r.at, base - 7, r.at + 2, base - 13);
    shell(x, r, r.at - 10, base - 7);
  });
  x.font = "700 9px Inter, sans-serif"; x.textAlign = "left"; x.fillStyle = CREAM;
  x.fillText("z", 56, 52); x.fillText("z", 60, 47);
}

// derbyLine is the Every day row's line and lamp.
export function derbyLine(v) {
  const row = document.getElementById("play-derby");
  row.classList.toggle("pays", v.race > 0);
  const fav = (v.card || []).find((r) => r.fav);
  document.getElementById("play-derby-line").textContent = v.race > 0 && fav
    ? "Race " + v.race + " of " + v.of + ", " + fav.owner + "'s " + fav.name + " is the favourite"
    : v.won ? "You picked " + v.won + (v.won > 1 ? " winners" : " winner") + " today, more tomorrow" : "That's the racing for today, more tomorrow";
}

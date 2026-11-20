import { sfx, chime } from "./sound.js";
import { colours } from "./frog.js";
import { drawTop } from "./top.js";
import { feel } from "./feel.js";
import { reduced, post, state, setPond } from "./api.js";
import { drawIcon } from "./icons.js";
import { PENTA } from "./drop.js";
import {
  W, H, FX0, FX1, WALL, LIP, GUT, CHUTE, DROPY, TRAY, HOLE, JAR, R, BIG, SFMIN, SFMAX,
  world, fresh, pack, unpack, prizes, aimed, drop, place, run, flight, settle,
} from "./pushworld.js";

// The pebble pusher. The table is the pond's and keeps from one visit to the
// next. The page plays the pile out and saves what happened every ten drops,
// on Eject and when the page is hidden, and the server only takes a save
// whose pebbles all add up.
const SAVE_EVERY = 10, HOLD = 0.5, SLOW = 0.6, ZOOM = 0.06, CHAIN = 0.9, IDLE = 30;
const LAMP = "#ffcf6a", LAMP_DK = "#c98a2a", CREAM = "#fff6dc", INK = "#0a0908", OK = "#6fd08c", NAVY = "#0b1222";
const TONES = [["#8a9099", "#c9ccd0", "#5b6470"], ["#9aa6c2", "#d2d9e6", "#5f6a86"], ["#a89c88", "#d8cfb8", "#6e6252"]];
const FONT = "700 9px Inter, sans-serif", SMALL = "600 7px Inter, sans-serif";

const F = feel();
let G = null, col = null;

// ---------------------------------------------------------------- talking to the server

function bytes(s) {
  const b = atob(s || ""), out = new Uint8Array(b.length);
  for (let i = 0; i < b.length; i++) out[i] = b.charCodeAt(i);
  return out;
}

function b64(u) {
  let s = "";
  for (let i = 0; i < u.length; i++) s += String.fromCharCode(u[i]);
  return btoa(s);
}

// A save as the page hides goes with keepalive so it gets there after the tab
// has gone.
function send(body, keep) {
  if (!keep) return post("/api/pusher/save", body);
  return fetch("/api/pusher/save", {
    method: "POST", credentials: "same-origin", keepalive: true,
    headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  }).then((r) => r.json());
}

function load(tb) {
  const w = world();
  const lay = bytes(tb.layout);
  if (lay.length) unpack(w, lay); else fresh(w, tb.on);
  w.spill = tb.spill; w.floor = tb.floor;
  G.w = w; G.run = tb.run; G.left = tb.drops; G.keep = tb.keep;
  G.dropped0 = tb.dropped; G.base = { dropped: 0, won: 0, spilled: 0, back: 0, claimed: 0 };
  G.prizes = {};
  const on = new Set(prizes(w));
  for (const z of tb.prizes) {
    G.prizes[z.id] = { kind: z.kind, name: z.name, at: tb.dropped + z.ripe };
    if (!on.has(z.id)) { place(w, z.id); G.newPrize = z; }
  }
  G.jar = tb.spill;
  G.lastSave = G.t;
}

function counts() {
  const c = G.w.counts, b = G.base;
  return { dropped: c.dropped - b.dropped, won: c.won - b.won, spilled: c.spilled - b.spilled, back: c.back - b.back, prizes: G.w.claimed.slice(b.claimed) };
}

function changed() { const c = counts(); return c.dropped || c.won || c.spilled || c.back || c.prizes.length; }

// save sends what's happened since the last one. Only one is ever in the
// air, and the counts it took are moved off at once so the next starts clean.
function save(keep) {
  if (!G || !G.w) return Promise.resolve(null);
  if (G.busy) { G.again = true; return G.busy; }
  if (!G.run || !changed()) return Promise.resolve(null);
  const c = counts(), body = Object.assign({ run: G.run, layout: b64(pack(G.w)) }, c), w = G.w;
  G.base = { dropped: w.counts.dropped, won: w.counts.won, spilled: w.counts.spilled, back: w.counts.back, claimed: w.claimed.length };
  G.run = null; G.lastSave = G.t;
  G.busy = send(body, keep).then((d) => {
    G.busy = null;
    if (!d || !d.ok) { restart(); return d; }
    G.run = d.table.run;
    G.paid += d.paid;
    for (const z of d.table.prizes) if (G.prizes[z.id]) G.prizes[z.id].at = d.table.dropped + z.ripe;
    for (const g of d.got || []) G.got.push(g.text || g.item);
    if (d.pond) setPond(d.pond);
    if (G.again) { G.again = false; save(); }
    return d;
  }, () => { G.busy = null; restart(); return null; });
  return G.busy;
}

// A save that didn't add up spent its run, so the table comes back from the
// server as it was at the last good save.
function restart() {
  if (!G) return;
  G.run = null;
  post("/api/pusher/start", {}).then((tb) => { if (G) { load(tb); G.note = "The table was put back to the last save"; G.noteT = 3; } }, () => {});
}

// ---------------------------------------------------------------- playing

function letGo() {
  if (!G || !G.w || G.st !== "play") return false;
  if (G.left <= 0) {
    if (G.t - G.emptyAt > 1.2) { G.emptyAt = G.t; F.pop(G.aim, CHUTE - 6, "No drops left today", "#9aa6c2"); sfx("pip"); }
    return false;
  }
  if (!drop(G.w, G.aim)) return false;
  G.left--; G.drops++;
  sfx("cast"); F.squash(0.6);
  return true;
}

function ripen() {
  const done = G.dropped0 + G.w.counts.dropped;
  for (const id in G.prizes) if (done >= G.prizes[id].at) G.w.ready.add(+id);
}

function plopped(p) {
  G.trayN++;
  sfx("plop");
  if (p.chain > 1 && G.t - G.chimed > 0.025) { G.chimed = G.t; chime(PENTA[Math.min(PENTA.length - 1, p.chain - 1)], 0.03); }
}

function heard(e) {
  switch (e.k) {
    case "land":
      if (e.src === "drop") { sfx(e.top ? "wood" : "stone"); F.burst(e.x, e.y, "#cfd3d6", 2); }
      else if (e.src === "prize") { sfx("thump"); F.burst(e.x, e.y, LAMP, 8); F.hit(0.15); }
      else if (G.t - G.rainAt > 0.03) { G.rainAt = G.t; sfx("kick"); }
      break;
    case "scrape":
      if (G.t - G.scrapeAt > 0.06) { G.scrapeAt = G.t; sfx("pip"); }
      break;
    case "shove":
      sfx("shove");
      break;
    case "won": {
      G.chain = G.t - G.lastWon < CHAIN ? G.chain + 1 : 1;
      G.lastWon = G.t;
      G.falls.push({ x: e.x, y: LIP, vx: e.vx * 0.3, vy: 20, kind: e.kind, chain: G.chain, tone: (e.x * 7) % 3 | 0 });
      if (e.kind) {
        const z = G.prizes[e.kind];
        F.got(e.x, LIP - 6, z ? z.name : "A prize", { big: true, sound: "land", c: LAMP });
        F.burst(e.x, LIP, LAMP, 24); F.burst(e.x, LIP, "#f29bb8", 12); F.hit(0.5); F.freeze(8);
        sfx("perk");
        break;
      }
      G.won++;
      if (G.chain === 1) { F.freeze(2); sfx("stone"); }
      if (G.chain >= 3) F.got(e.x, LIP - 8, G.chain + " OFF", { streak: G.chain - 2, c: G.chain >= 6 ? LAMP : CREAM });
      break;
    }
    case "gutter":
      G.gutters.push({ x: e.right ? FX1 + 6 : FX0 - 6, y: e.y, vy: 30, right: e.right, kind: e.kind });
      if (e.kind) { const z = G.prizes[e.kind]; F.got(e.x, e.y - 8, z ? z.name : "A prize", { big: true, c: LAMP }); }
      break;
    case "hole":
      F.freeze(10); F.hit(0.6);
      F.burst(e.x, e.y, OK, 20); F.burst(e.x, e.y, LAMP, 14);
      sfx("gulp");
      break;
    case "shower":
      G.tip = 1.6;
      if (e.n) {
        F.pop(W / 2, SFMAX + 24, "SHOWER OF " + e.n, LAMP);
        sfx("perk");
        for (let i = 0; i < Math.min(14, e.n); i++) setTimeout(() => chime(PENTA[Math.min(PENTA.length - 1, i)], 0.028), 120 + i * 45);
      } else F.pop(W / 2, SFMAX + 24, "THE JAR'S EMPTY", "#9aa6c2");
      break;
  }
}

// The prize nearest going over, if one is past half way to the lip.
function teeter() {
  let best = null;
  for (const b of G.w.bodies) if (b.kind && !b.top && G.w.ready.has(b.kind) && b.y > LIP - BIG * 1.5 && (!best || b.y > best.y)) best = b;
  return best;
}

export const pusher = {
  key: "pusher", W, H, title: "PEBBLE PUSHER", feel: F, bare: true, edge: [NAVY, NAVY], scoreWord: "Won",
  aria: "A pebble pusher. Drag along the top to aim the chute and let go to drop a pebble, or hold to keep dropping. The shelf shoves the pile toward the front, and what falls over the lip is yours.",
  help: [],
  tiles: [["Drops", "drops", "0"], ["On table", "on", "0"], ["Won", "won", "0"]],

  info() { const v = state.pond && state.pond.pusher; return v ? { best: 0, paid: 0, cap: 0, aims: [] } : null; },

  note() {
    const v = state.pond && state.pond.pusher;
    if (!v) return;
    const live = G && G.w;
    document.getElementById("pusher-drops").textContent = String(live ? G.left : v.drops);
    document.getElementById("pusher-on").textContent = String(live ? G.w.on : v.on);
    document.getElementById("pusher-won").textContent = String(live ? G.won : 0);
  },

  reset(g, p) {
    col = colours(g, p.frog.hat);
    G = {
      st: "play", t: 0, aim: W / 2, carriage: W / 2, dir: 0, armed: false, holdT: 0, held: 0, w: null, run: null,
      left: p.pusher.drops, keep: p.pusher.keep, won: 0, paid: 0, drops: 0, trayN: 0, falls: [], gutters: [], got: [],
      chain: 0, lastWon: -9, chimed: -1, rainAt: -1, scrapeAt: -1, emptyAt: -9, slow: 1, tip: 0, jar: p.pusher.spill,
      busy: null, again: false, lastSave: 0, note: "", noteT: 0, keyHeld: false, close: null,
    };
    // The table as it was last saved, still, until the run's start comes back.
    const w = world(), lay = bytes(p.pusher.layout);
    if (lay.length) unpack(w, lay); else fresh(w, p.pusher.on);
    w.spill = p.pusher.spill;
    G.still = w;
  },

  begin() {
    return post("/api/pusher/start", {}).then((tb) => { load(tb); return { run: tb.run }; }, () => {
      G.note = "The pusher can't reach the pond just now"; G.noteT = 4;
      return { run: null };
    });
  },

  // Leaving saves what's left and says what the visit paid.
  end() {
    const go = () => save().then(() => ({ paid: G ? G.paid : 0, best: false, rested: 0, aims: [], pond: state.pond }));
    return G && G.busy ? G.busy.then(go, go) : go();
  },

  pause() { save(true); },

  demo() { return G && G.w && !G.drops ? { x: 70, y: CHUTE - 6, to: [124, CHUTE - 6] } : null; },

  press() { if (!G) return; G.armed = true; G.holdT = 0; G.held = 0; },
  drag(x) { if (G) G.aim = aimed(x); },
  release() {
    if (!G || !G.armed) return;
    G.armed = false;
    if (!G.held) letGo();
  },

  keys(code, down) {
    if (!G) return;
    if (code === "ArrowLeft" || code === "KeyA") G.dir = down ? -1 : G.dir < 0 ? 0 : G.dir;
    if (code === "ArrowRight" || code === "KeyD") G.dir = down ? 1 : G.dir > 0 ? 0 : G.dir;
    if (code === "Space" || code === "Enter" || code === "ArrowDown") {
      if (down && !G.keyHeld) { G.keyHeld = true; G.holdT = 0; letGo(); }
      if (!down) G.keyHeld = false;
    }
  },

  step(dt) {
    G.t += dt;
    G.noteT = Math.max(0, G.noteT - dt);
    G.tip = Math.max(0, G.tip - dt);
    G.aim = aimed(G.aim + G.dir * 90 * dt);
    // The chute's carriage runs after the finger with a little weight to it.
    G.carriage += (G.aim - G.carriage) * Math.min(1, dt * 14);
    const w = G.w;
    if (!w) return;
    // A held finger or key keeps dropping one every HOLD seconds.
    if (G.armed || G.keyHeld) {
      G.holdT += dt;
      if (G.holdT >= HOLD) { G.holdT -= HOLD; if (letGo()) G.held++; }
    }
    ripen();
    const c = counts();
    w.room = 3 * c.dropped + 12 - c.won;
    const z = teeter();
    G.slow += ((z && !reduced ? SLOW : 1) - G.slow) * Math.min(1, dt * 6);
    G.zx = z ? z.x : G.zx;
    // A shove that ends with a prize still hanging is a near miss.
    const was = G.vsf || 0;
    run(w, dt * G.slow);
    G.vsf = w.vsf;
    if (was > 0 && w.vsf <= 0 && z) F.close(z.x, LIP - 16);
    w.events.forEach(heard);
    w.events.length = 0;
    settle(w);
    G.jar += (w.spill - G.jar) * Math.min(1, dt * 4);
    G.falls.forEach((f) => { f.vy += 600 * dt; f.x += f.vx * dt; f.y += f.vy * dt; });
    G.falls = G.falls.filter((f) => { if (f.y < TRAY - 6) return true; if (!f.kind) plopped(f); return false; });
    G.gutters.forEach((g) => { g.vy += 500 * dt; g.y += g.vy * dt; });
    G.gutters = G.gutters.filter((g) => { if (g.y < JAR.y - 30) return true; sfx("pip"); return false; });
    if (G.got.length && !F.frozen()) { G.note = "In the bag, " + G.got.shift().toLowerCase(); G.noteT = 3.5; }
    // A save every ten drops, when a save's room for wins runs low, when a
    // prize has gone over, and now and then while the pile is still moving.
    if (!G.busy && G.run) {
      if (c.dropped >= SAVE_EVERY || w.room <= 4 || c.prizes.length || (G.t - G.lastSave > IDLE && changed())) save();
    }
    const shown = G.left + "|" + w.on + "|" + G.won;
    if (shown !== G.shown) { G.shown = shown; pusher.note(); }
  },

  over() { return false; },
  score() { return G ? G.won : 0; },
  why() { return ""; },

  draw(x) {
    const w = G && (G.w || G.still);
    table(x, w ? w.sf : SFMIN, G ? G.t : 0);
    if (!w) return;
    const z = reduced || !G ? 0 : (1 - G.slow) / (1 - SLOW);
    x.save();
    if (z > 0.01) { const cx = G.zx || W / 2; x.translate(cx, LIP); x.scale(1 + ZOOM * z, 1 + ZOOM * z); x.translate(-cx, -LIP); }
    pile(x, w, G.t, kindOf);
    x.restore();
    falls(x);
    panel(x, w);
    jar(x, G.jar, G.tip);
    chute(x, w);
    F.draw(x);
    top(x);
  },
};

// A tab closed outright may never say it's hidden.
window.addEventListener("pagehide", () => save(true));

function kindOf(id) { const z = G && G.prizes && G.prizes[id]; return z ? z.kind : "t_marble"; }

// ---------------------------------------------------------------- drawing

const SPR = {};
function sprite(tone) {
  if (SPR[tone]) return SPR[tone];
  const cv = document.createElement("canvas"), [base, light, dark] = TONES[tone];
  cv.width = cv.height = 12;
  const g = cv.getContext("2d");
  disc(g, 6, 6, R, dark);
  disc(g, 6, 5, R - 0.4, base);
  g.fillStyle = light; g.fillRect(3, 3, 4, 2); g.fillRect(4, 2, 3, 1);
  g.fillStyle = CREAM; g.fillRect(4, 3, 2, 1);
  SPR[tone] = cv;
  return cv;
}

function disc(g, cx, cy, r, c) {
  g.fillStyle = c;
  for (let y = -Math.floor(r); y <= Math.floor(r); y++) {
    const hw = Math.round(Math.sqrt(Math.max(0, r * r - y * y)));
    g.fillRect(cx - hw, cy + y, hw * 2, 1);
  }
}

function pebble(x, cx, cy, tone, k) {
  if (k && k !== 1) {
    x.save(); x.translate(cx, cy); x.scale(k, k); x.drawImage(sprite(tone), -6, -6); x.restore();
    return;
  }
  x.drawImage(sprite(tone), Math.round(cx) - 6, Math.round(cy) - 6);
}

// A prize sits on a dark disc so it reads against the pebbles, and glows
// once it's sat long enough to come off.
function prizeAt(x, kind, cx, cy, ripe, t) {
  cx = Math.round(cx); cy = Math.round(cy);
  if (ripe) {
    x.globalAlpha = 0.35 + 0.25 * Math.sin(t * 5);
    disc(x, cx, cy, BIG + 2, LAMP);
    x.globalAlpha = 1;
  }
  disc(x, cx, cy + 1, BIG, "#3a2a1a");
  disc(x, cx, cy, BIG, "#f4ecd0");
  drawIcon(x, kind, cx - 6, cy - 6, 1);
}

// The cabinet, the back wall, the shelf, the field, the ledge and the pipes
// from the gutters down to the spill jar.
function table(x, sf, t) {
  x.fillStyle = NAVY; x.fillRect(0, 0, W, H);
  // The cabinet round it all.
  x.fillStyle = "#2a1a0e"; x.fillRect(4, 20, W - 8, H - 24);
  x.fillStyle = "#4a3018"; x.fillRect(4, 20, W - 8, 2); x.fillRect(4, 20, 2, H - 24); x.fillRect(W - 6, 20, 2, H - 24);
  // The backboard with its row of bulbs.
  x.fillStyle = "#121c33"; x.fillRect(10, 24, W - 20, WALL - 30);
  for (let i = 0; i < 10; i++) {
    const on = Math.floor(t * 3 + i) % 3 === 0, bx = 15 + i * 19;
    if (on) { x.globalAlpha = 0.22; x.fillStyle = LAMP; x.fillRect(bx - 2, 26, 7, 7); x.globalAlpha = 1; }
    x.fillStyle = on ? LAMP : i % 2 ? "#5a3a48" : "#5a4a28"; x.fillRect(bx, 28, 3, 3);
  }
  // Side rails, and the gutters past them where the walls stop.
  x.fillStyle = "#4a3018"; x.fillRect(FX0 - 6, WALL - 6, 6, GUT - WALL + 6); x.fillRect(FX1, WALL - 6, 6, GUT - WALL + 6);
  x.fillStyle = "#6b4a2a"; x.fillRect(FX0 - 6, WALL - 6, 2, GUT - WALL + 6); x.fillRect(FX1 + 4, WALL - 6, 2, GUT - WALL + 6);
  x.fillStyle = "#05080f"; x.fillRect(FX0 - 8, GUT, 8, LIP - GUT + 4); x.fillRect(FX1, GUT, 8, LIP - GUT + 4);
  x.fillStyle = "#22304f"; for (let gy = GUT + 3; gy < LIP; gy += 5) { x.fillRect(FX0 - 7, gy, 6, 1); x.fillRect(FX1 + 1, gy, 6, 1); }
  pipe(x, [[FX0 - 4, LIP + 4], [FX0 - 4, PIPE], [JAR.x - 6, PIPE]]);
  pipe(x, [[FX1 + 4, LIP + 4], [FX1 + 4, PIPE], [JAR.x + 6, PIPE]]);
  // The field, pond water under glass.
  ["#173f4c", "#153a47", "#123542", "#10303c"].forEach((c, i) => { x.fillStyle = c; x.fillRect(FX0, WALL + i * 24, FX1 - FX0, 24); });
  x.fillStyle = "#1b4855"; for (let gy = WALL + 6; gy < LIP; gy += 12) for (let gx = FX0 + 6 + ((gy / 12) % 2) * 6; gx < FX1; gx += 12) x.fillRect(gx, gy, 1, 1);
  // The lily pad hole, lit while the shelf is back off it.
  const open = sf < HOLE.y - 2;
  disc(x, HOLE.x, HOLE.y, 8, open ? "#4f9a4a" : "#2f6b33");
  x.fillStyle = "#10303c"; x.fillRect(HOLE.x + 4, HOLE.y - 1, 5, 2);
  if (open) { x.globalAlpha = 0.3 + 0.2 * Math.sin(t * 9); disc(x, HOLE.x, HOLE.y, 10, OK); x.globalAlpha = 1; }
  disc(x, HOLE.x, HOLE.y, HOLE.r + 0.5, "#04070c");
  // The ledge, and the tray under it.
  x.fillStyle = "#1a0f08"; x.fillRect(FX0, LIP + 3, FX1 - FX0, TRAY - LIP - 3);
  x.fillStyle = "#2a1a0e"; x.fillRect(FX0, LIP + 3, FX1 - FX0, 3);
  x.fillStyle = LAMP_DK; x.fillRect(FX0 - 2, LIP, FX1 - FX0 + 4, 3);
  x.fillStyle = LAMP; x.fillRect(FX0 - 2, LIP, FX1 - FX0 + 4, 1);
  x.fillStyle = "#6b4a2a"; x.fillRect(FX0 - 4, TRAY, FX1 - FX0 + 8, 6);
  x.fillStyle = "#8a6230"; x.fillRect(FX0 - 4, TRAY, FX1 - FX0 + 8, 1);
  shelf(x, sf);
}

const PIPE = 220;

function pipe(x, pts) {
  for (let i = 1; i < pts.length; i++) {
    const [ax, ay] = pts[i - 1], [bx, by] = pts[i], l = Math.min(ax, bx) - 2, t = Math.min(ay, by) - 2;
    x.fillStyle = "#18243f"; x.fillRect(l, t, Math.abs(bx - ax) + 4, Math.abs(by - ay) + 4);
    x.fillStyle = "#2c3c5c"; x.fillRect(l, t, Math.abs(bx - ax) + 4, 1); x.fillRect(l, t, 1, Math.abs(by - ay) + 4);
  }
}

function shelf(x, sf) {
  const f = Math.round(sf);
  x.fillStyle = "#3d4f6e"; x.fillRect(FX0, WALL, FX1 - FX0, f - WALL);
  x.fillStyle = "#4a5e80"; for (let gx = FX0 + 4; gx < FX1; gx += 10) x.fillRect(gx, WALL, 1, f - WALL);
  x.fillStyle = "#4a3018"; x.fillRect(FX0 - 6, WALL - 8, FX1 - FX0 + 12, 8);
  x.fillStyle = "#6b4a2a"; x.fillRect(FX0 - 6, WALL - 8, FX1 - FX0 + 12, 2);
  x.fillStyle = "#1a0f08"; x.fillRect(FX0, WALL, FX1 - FX0, 2);
}

function shelfEdge(x, sf) {
  const f = Math.round(sf);
  x.fillStyle = "#26324a"; x.fillRect(FX0, f - 3, FX1 - FX0, 3);
  x.fillStyle = "#8aa0c4"; x.fillRect(FX0, f - 3, FX1 - FX0, 1);
  x.fillStyle = "rgba(4,7,12,0.5)"; x.fillRect(FX0, f, FX1 - FX0, 2);
}

// Every pebble, back to front, the ones riding the shelf first. One over the
// lip teeters, all the time once it's more than half over and with the
// shove otherwise.
function pile(x, w, t, kind) {
  const tops = [], field = [];
  for (const b of w.bodies) (b.top ? tops : field).push(b);
  tops.sort((a, b) => a.dy - b.dy); field.sort((a, b) => a.dy - b.dy);
  const draw = (b) => {
    const over = b.y + b.r - LIP;
    let j = 0;
    if (over > 0 && !reduced && (over > b.r || w.vsf > 0)) j = Math.sin(t * 40 + b.id) * 0.6;
    const up = b.perch > 0 ? 1 : 0;
    if (b.kind) prizeAt(x, kind(b.kind), b.dx + j, b.dy - up, w.ready.has(b.kind), t);
    else pebble(x, b.dx + j, b.dy - up, b.id % 3);
  };
  tops.forEach(draw);
  shelfEdge(x, w.sf);
  field.forEach(draw);
  // A streak of light on the glass over the field.
  x.fillStyle = "rgba(255,246,220,0.05)";
  for (let i = 0; i < 26; i++) x.fillRect(FX0 + 18 + i, LIP - 8 - i * 3, 3, 3);
  // What's still in the air, with its shadow on the table where it'll land.
  for (const a of w.air) {
    if (a.t < 0) continue;
    const f = flight(a);
    x.globalAlpha = 0.25 + 0.4 * f.u; x.fillStyle = INK;
    x.fillRect(Math.round(a.x) - 3, Math.round(a.y) + 3, 7, 2);
    x.globalAlpha = 1;
    if (a.kind) prizeAt(x, kind(a.kind), f.x, f.y - f.z, false, t);
    else pebble(x, f.x, f.y - f.z, 0, 1 + f.z / 70);
  }
}

// Pebbles that went over the lip falling into the tray, and the tray's heap.
function falls(x) {
  if (!G) return;
  const n = Math.min(G.trayN, 45);
  for (let i = 0; i < n; i++) {
    const row = Math.floor(i / 15), c = i % 15;
    pebble(x, FX0 + 7 + c * 10 + (row % 2) * 5, TRAY - 5 - row * 6, (i * 7) % 3);
  }
  G.falls.forEach((f) => { if (f.kind) prizeAt(x, kindOf(f.kind), f.x, f.y, true, G.t); else pebble(x, f.x, f.y, f.tone); });
  G.gutters.forEach((g) => { if (g.kind) prizeAt(x, kindOf(g.kind), g.x, g.y, true, G.t); else pebble(x, g.x, Math.min(g.y, PIPE), 1, 0.7); });
}

// The spill jar the pipes run into, which tips back over the table.
function jar(x, n, tip) {
  const cx = JAR.x, by = JAR.y + 22, k = tip > 0 && !reduced ? Math.sin(Math.min(1, (1.6 - tip) / 0.6) * Math.PI) : 0;
  x.save();
  x.translate(cx, by - k * 6);
  x.rotate(-k * 0.5);
  x.fillStyle = "rgba(207,227,234,0.12)"; x.fillRect(-12, -40, 24, 40);
  for (let i = 0; i < Math.min(48, Math.round(n)); i++) {
    const r = Math.floor(i / 4);
    x.fillStyle = TONES[i % 3][0]; x.fillRect(-10 + (i % 4) * 5 + (r % 2), -4 - r * 3, 4, 3);
    x.fillStyle = TONES[i % 3][1]; x.fillRect(-10 + (i % 4) * 5 + (r % 2), -4 - r * 3, 2, 1);
  }
  x.fillStyle = "rgba(207,227,234,0.55)"; x.fillRect(-13, -42, 2, 42); x.fillRect(11, -42, 2, 42); x.fillRect(-13, 0, 26, 2);
  x.fillStyle = "#8a9099"; x.fillRect(-14, -45, 28, 3);
  x.fillStyle = "rgba(255,246,220,0.5)"; x.fillRect(-9, -36, 1, 20);
  x.restore();
  x.font = SMALL; x.textAlign = "center"; x.fillStyle = "#9aa6c2";
  x.fillText("SPILL JAR " + Math.round(n), cx - 4, by + 11);
}

// The chute on its rail over the back wall, with the frog riding it and a
// dotted line down to where a pebble lands.
function chute(x, w) {
  const cx = Math.round(G ? G.carriage : W / 2);
  x.fillStyle = "#4a3018"; x.fillRect(FX0 - 4, CHUTE - 6, FX1 - FX0 + 8, 3);
  x.fillStyle = "#8a6230"; x.fillRect(FX0 - 4, CHUTE - 6, FX1 - FX0 + 8, 1);
  const left = G && G.w ? G.left : 0, ready = left > 0 && G && G.w && w.wait <= 0;
  if (G && G.w && left > 0) {
    x.fillStyle = "rgba(255,246,220,0.28)";
    for (let y = CHUTE + 4; y < DROPY - 4; y += 5) x.fillRect(cx, y, 1, 2);
    x.fillStyle = "rgba(255,246,220,0.35)";
    x.fillRect(cx - 4, DROPY, 1, 1); x.fillRect(cx + 4, DROPY, 1, 1); x.fillRect(cx, DROPY - 4, 1, 1); x.fillRect(cx, DROPY + 4, 1, 1);
  }
  x.fillStyle = "#6b4a2a"; x.fillRect(cx - 8, CHUTE - 12, 16, 8); x.fillRect(cx - 5, CHUTE - 4, 10, 4);
  x.fillStyle = "#8a6230"; x.fillRect(cx - 8, CHUTE - 12, 16, 2);
  x.fillStyle = INK; x.fillRect(cx - 3, CHUTE - 2, 6, 2);
  if (ready) pebble(x, cx, CHUTE - 15, 0);
  if (col) F.squashed(x, cx, CHUTE - 18, () => drawTop(x, col, cx - 6, CHUTE - 34, true));
}

// The front of the cabinet under the tray, with its name in lamp letters
// and the prizes on the pile, each with how long it has left to sit.
function panel(x, w) {
  x.fillStyle = "#1a0f08"; x.fillRect(10, PIPE + 6, JAR.x - 30, H - PIPE - 14);
  x.fillStyle = "#3a2414"; x.fillRect(10, PIPE + 6, JAR.x - 30, 1);
  x.font = "800 10px Inter, sans-serif"; x.textAlign = "left";
  x.fillStyle = LAMP_DK; x.fillText("PEBBLE PUSHER", 17, PIPE + 22);
  x.fillStyle = LAMP; x.fillText("PEBBLE PUSHER", 16, PIPE + 21);
  const on = prizes(w);
  x.font = SMALL; x.fillStyle = "#9aa6c2";
  x.fillText(on.length ? "ON THE PILE" : "A PRIZE GOES ON EACH DAY", 16, PIPE + 38);
  const done = G && G.w ? G.dropped0 + G.w.counts.dropped : 0;
  on.forEach((id, i) => {
    const z = G && G.prizes && G.prizes[id], px = 16 + i * 30, py = PIPE + 44;
    if (!z) return;
    const ripe = w.ready.has(id);
    x.fillStyle = "#18243f"; x.fillRect(px, py, 24, 24);
    drawIcon(x, z.kind, px + 6, py + 4, 1);
    const left = Math.max(0, z.at - done), f = ripe ? 1 : 1 - left / 30;
    x.fillStyle = "#22304f"; x.fillRect(px + 2, py + 19, 20, 2);
    x.fillStyle = ripe ? OK : LAMP_DK; x.fillRect(px + 2, py + 19, Math.round(20 * f), 2);
  });
}

// The drops in a dish at the top, what this visit has won, and a line.
function top(x) {
  const left = G && G.w ? G.left : state.pond ? state.pond.pusher.drops : 0;
  x.fillStyle = "#18243f"; x.fillRect(4, 2, 70, 15);
  x.fillStyle = "#22304f"; x.fillRect(4, 2, 70, 1);
  for (let i = 0; i < Math.min(6, left); i++) pebble(x, 11 + i * 6, 10, i % 3, 0.7);
  x.font = FONT; x.textAlign = "right"; x.fillStyle = left ? "#eef0e6" : "#9aa6c2";
  x.fillText(String(left), 70, 13);
  if (G && G.won) {
    x.textAlign = "right"; x.fillStyle = "#9aa6c2"; x.font = SMALL; x.fillText("WON", W - 30, 12);
    x.font = FONT; x.fillStyle = "#eef0e6"; x.fillText(String(G.won), W - 6, 13);
  }
  if (G && G.noteT > 0 && G.note) {
    x.globalAlpha = Math.min(1, G.noteT * 2);
    x.font = FONT; x.textAlign = "center"; x.lineWidth = 3; x.strokeStyle = INK;
    x.strokeText(G.note, W / 2, LIP + 18); x.fillStyle = CREAM; x.fillText(G.note, W / 2, LIP + 18);
    x.globalAlpha = 1;
  }
}

// ---------------------------------------------------------------- Play

// The title card picture, for the frame's label list.
export function pushArt(x, g) {
  x.fillStyle = NAVY; x.fillRect(0, 0, 168, 96);
  x.fillStyle = "#123542"; x.fillRect(14, 30, 140, 50);
  x.fillStyle = "#3d4f6e"; x.fillRect(14, 22, 140, 12);
  x.fillStyle = LAMP; x.fillRect(12, 80, 144, 2);
  for (let i = 0; i < 40; i++) { const px = 18 + (i * 37) % 132, py = 40 + (i * 23) % 38; x.fillStyle = TONES[i % 3][0]; x.fillRect(px, py, 6, 5); x.fillStyle = TONES[i % 3][1]; x.fillRect(px + 1, py, 3, 1); }
  drawTop(x, colours(g, null), 78, 2, true);
}

// The pusher's card on Play draws the pond's own table as it was last saved.
export function pushCard(cv, p) {
  const v = p.pusher, k = Math.max(2, Math.round(2 * (window.devicePixelRatio || 1)));
  const key = v.drops + "|" + v.layout + "|" + k;
  if (cv.dataset.drawn === key) return;
  cv.dataset.drawn = key;
  const w = world(() => 0.5), lay = bytes(v.layout);
  if (lay.length) unpack(w, lay); else fresh(w, v.on);
  const off = document.createElement("canvas"), x0 = FX0 - 10, y0 = WALL - 12, cw = FX1 - FX0 + 20, ch = LIP + 14 - y0;
  off.width = cw * 2; off.height = ch * 2;
  const o = off.getContext("2d");
  o.scale(2, 2); o.translate(-x0, -y0); o.imageSmoothingEnabled = false;
  table(o, SFMIN, 0);
  const kinds = Object.fromEntries(v.prizes.map((z) => [z.id, z.kind]));
  pile(o, w, 0, (id) => kinds[id] || "t_marble");
  cv.width = 60 * k; cv.height = 44 * k;
  const g = cv.getContext("2d");
  g.imageSmoothingEnabled = true;
  g.fillStyle = NAVY; g.fillRect(0, 0, cv.width, cv.height);
  const s = Math.min(cv.width / off.width, cv.height / off.height), dw = off.width * s, dh = off.height * s;
  g.drawImage(off, (cv.width - dw) / 2, (cv.height - dh) / 2, dw, dh);
}

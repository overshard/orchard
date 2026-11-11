import { post, state, titled } from "./api.js";
import { sfx } from "./sound.js";
import { feel } from "./feel.js";
import { drawIcon } from "./icons.js";
import { openPicture, MONTHS } from "./postcard.js";

// Bug sweep. A grid of lily pads with wasps under some, and a number on an
// open pad for how many are next door. The page never holds the wasps. It
// sends each pad it taps and draws what the server says opened.
const W = 200, H = 320, CELL = 24, OX = 4, OY = 42, HOLD = 0.38;
const INK = ["", "#9fd8e8", "#9fe08f", "#ffe27a", "#f59ab3", "#e2567a", "#e2412d", "#e2412d", "#e2412d"];
const MODE = { x: 54, y: 290, w: 92, h: 22 };

let G = null;
const F = feel();

function mid(i) { return [OX + (i % G.cols) * CELL + CELL / 2, OY + Math.floor(i / G.cols) * CELL + CELL / 2]; }

function padAt(x, y) {
  const c = Math.floor((x - OX) / CELL), r = Math.floor((y - OY) / CELL);
  return c < 0 || c >= G.cols || r < 0 || r >= G.rows ? -1 : r * G.cols + c;
}

function clock(secs) { return Math.floor(secs / 60) + ":" + String(secs % 60).padStart(2, "0"); }

function mark(i) {
  if (G.st !== "play" || i < 0 || G.n[i] >= 0) return;
  G.marked[i] = !G.marked[i];
  sfx("click");
  F.burst(...mid(i), "#f59ab3", 3);
}

// A tap waits its turn, since a pad it's after may open under an earlier one.
function tap(i) {
  if (G.st !== "play" || i < 0 || G.n[i] >= 0 || G.marked[i] || G.asked[i]) return;
  const mine = G;
  G.asked[i] = true;
  G.net = G.net.then(() => {
    if (G !== mine || G.st !== "play" || G.n[i] >= 0) return null;
    return post("/api/sweep/tap", { run: G.run, pad: i }).then((d) => { if (G === mine) took(d, i); }, () => { if (G === mine) lost("TRY THAT AGAIN"); });
  });
}

function lost(why) { G.why = why; G.st = "done"; G.st_t = 0; }

function took(d, i) {
  const [x, y] = mid(i);
  G.asked[i] = false; G.taps++;
  d.open.forEach(([p, n], k) => { G.n[p] = n; G.marked[p] = false; G.at[p] = G.t + Math.min(0.5, k * 0.018); });
  G.opened += d.open.length;
  if (d.state === "play") {
    if (d.open.length > 3) { G.runs++; F.got(x, y, "", { streak: G.runs, sound: "land" }); }
    else { sfx("hop", 1 + (G.taps % 6)); F.burst(x, y, "#cfe3ea", 3); }
    return;
  }
  G.res = d; G.hit = d.hit; G.wasps = d.wasps || [];
  if (d.state === "lost") { F.crash(x, y, { s: "WASP" }); lost("A WASP"); return; }
  F.got(W / 2, OY + 40, "ALL CLEAR", { big: true, sound: "land" });
  lost("ALL CLEAR");
}

export const sweep = {
  key: "sweep", W, H, title: "BUG SWEEP", feel: F, full: true, edge: ["#17475a", "#17475a"],
  aria: "Tap a pad to look under it and hold one to mark a wasp, or use the arrow keys, space and M.",
  help: ["TAP A PAD TO LOOK UNDER IT", "A NUMBER IS THE WASPS NEXT DOOR", "HOLD A PAD TO MARK A WASP", "TODAY'S GRID PAYS ONCE", "TAP TO START"],

  reset() {
    G = { t: 0, st: "wait", st_t: 0, cols: 8, rows: 10, wasps: [], left: 12, run: "", daily: true, n: [], marked: [], asked: [], at: [], opened: 0, taps: 0, runs: 0, secs: 0, hit: -1, res: null, why: "", net: Promise.resolve(), down: null, mode: false, cur: 35, kb: false, end: 0 };
    G.n = Array(G.cols * G.rows).fill(-1);
  },

  begin() {
    const mine = G;
    return post("/api/sweep/start", {}).catch(() => { if (G === mine) lost("TRY THAT AGAIN"); return { run: "" }; });
  },

  // The go may be one that was left part way, so it comes with what's open.
  dealt(d) {
    if (!G || !d.run) return;
    G.cols = d.cols; G.rows = d.rows; G.left = d.wasps; G.run = d.run; G.daily = d.daily; G.secs = d.secs || 0;
    G.n = Array(G.cols * G.rows).fill(-1);
    d.open.forEach(([p, n]) => { G.n[p] = n; });
    G.opened = d.open.length; G.taps = d.open.length ? 1 : 0;
    G.st = "play";
  },

  end() {
    const r = G && G.res;
    return Promise.resolve(r ? { paid: r.paid, best: r.best, rested: r.rested, pond: r.pond } : null);
  },

  demo() { return G && G.st !== "done" && !G.opened ? { x: OX + CELL * 3.5, y: OY + CELL * 4.5 } : null; },

  press(x, y) {
    if (!G || G.st !== "play") return;
    if (x >= MODE.x && x < MODE.x + MODE.w && y >= MODE.y - 4 && y < MODE.y + MODE.h + 4) { G.mode = !G.mode; sfx("click"); return; }
    const i = padAt(x, y);
    G.kb = false;
    if (i >= 0) G.down = { i, t: 0, held: false };
  },

  release() {
    if (!G || !G.down) return;
    const d = G.down;
    G.down = null;
    if (d.held) return;
    if (G.mode) mark(d.i); else tap(d.i);
  },

  keys(code, down) {
    if (!G || !down) return;
    const c = G.cur % G.cols, r = Math.floor(G.cur / G.cols);
    if (code === "ArrowLeft" || code === "KeyA") G.cur = r * G.cols + Math.max(0, c - 1);
    if (code === "ArrowRight" || code === "KeyD") G.cur = r * G.cols + Math.min(G.cols - 1, c + 1);
    if (code === "ArrowUp" || code === "KeyW") G.cur = Math.max(0, r - 1) * G.cols + c;
    if (code === "ArrowDown" || code === "KeyS") G.cur = Math.min(G.rows - 1, r + 1) * G.cols + c;
    if (code === "Space" || code === "Enter") tap(G.cur);
    if (code === "KeyM" || code === "KeyF") mark(G.cur);
    G.kb = true;
  },

  step(dt) {
    G.t += dt; G.st_t += dt;
    if (G.st === "play" && G.opened) G.secs += dt;
    // A finger held on a pad marks it, and the lift after that does nothing.
    if (G.down && !G.down.held) {
      G.down.t += dt;
      if (G.down.t >= HOLD) { G.down.held = true; mark(G.down.i); }
    }
    if (G.st === "done" && G.st_t > (G.res && G.res.state === "lost" ? 1.5 : 1)) G.end = 1;
  },

  over() { return G && G.end; },
  score() { return G ? G.opened : 0; },
  why() { return G ? G.why : ""; },

  card() {
    const r = G && G.res, box = document.createElement("div");
    if (!r) return null;
    box.className = "sweepcard";
    const line = document.createElement("p");
    line.className = "dim";
    line.textContent = said(r) + ", " + r.taps + (r.taps === 1 ? " tap" : " taps");
    box.append(line);
    if (!r.daily) {
      const free = document.createElement("p");
      free.className = "dim"; free.textContent = "A free go on a fresh grid";
      box.append(free);
      return box;
    }
    box.append(shareKey({ state: r.state, secs: r.secs, taps: r.taps, pads: r.pads, of: r.of }));
    return box;
  },

  draw(x) {
    x.fillStyle = "#17475a"; x.fillRect(0, 0, W, H);
    if (!G) return;
    x.fillStyle = "#0c2832"; x.fillRect(OX - 2, OY - 2, G.cols * CELL + 4, G.rows * CELL + 4);
    const shown = new Set(G.wasps);
    for (let i = 0; i < G.n.length; i++) {
      const [cx, cy] = mid(i), l = cx - CELL / 2, t = cy - CELL / 2, open = G.n[i] >= 0 && G.t >= (G.at[i] || 0);
      if (open) {
        x.fillStyle = (i + Math.floor(i / G.cols)) % 2 ? "#123a48" : "#143f4e"; x.fillRect(l, t, CELL, CELL);
        if (G.n[i] > 0) {
          x.font = "700 14px 'Space Grotesk', sans-serif"; x.textAlign = "center";
          x.fillStyle = INK[G.n[i]]; x.fillText(String(G.n[i]), cx, cy + 5);
        } else { x.fillStyle = "#1d5163"; x.fillRect(cx - 3, cy, 6, 1); }
        continue;
      }
      x.fillStyle = "#1d5163"; x.fillRect(l, t, CELL, CELL);
      if (shown.has(i)) {
        if (i === G.hit) { x.fillStyle = "#7a2030"; x.fillRect(l + 1, t + 1, CELL - 2, CELL - 2); }
        wasp(x, cx, cy);
        continue;
      }
      pad(x, cx, cy, G.asked[i] || (G.down && G.down.i === i));
      if (G.marked[i]) bud(x, cx, cy);
    }
    if (G.kb && G.st === "play") {
      const [cx, cy] = mid(G.cur);
      x.strokeStyle = "#ffb000"; x.lineWidth = 1; x.strokeRect(cx - CELL / 2 + 0.5, cy - CELL / 2 + 0.5, CELL - 1, CELL - 1);
    }
    F.draw(x);
    wasp(x, 14, 20);
    x.font = "700 14px 'Space Grotesk', sans-serif"; x.textAlign = "left"; x.fillStyle = "#ece5d8";
    x.fillText(String(G.res ? 0 : Math.max(0, G.left - G.marked.filter(Boolean).length)), 26, 25);
    x.textAlign = "right"; x.fillText(clock(Math.floor(G.res ? G.res.secs : G.secs)), W - 8, 25);
    x.font = "500 8px 'JetBrains Mono', monospace"; x.textAlign = "center"; x.fillStyle = "#bcaf99";
    x.fillText(G.st === "wait" ? "" : G.daily ? "TODAY'S GRID" : "FREE GO", W / 2, 23);
    modeKey(x);
  },
};

function said(r) {
  return r.state === "won" ? "Cleared in " + clock(r.secs) : r.pads + " of " + r.of + " pads";
}

// A key that opens the day's result as a picture. The results card takes no
// taps, so this stops its own press from starting the next go.
export function shareKey(note) {
  const b = document.createElement("button");
  b.type = "button"; b.className = "sharekey"; b.textContent = "Share";
  b.addEventListener("pointerdown", (e) => e.stopPropagation());
  b.addEventListener("click", (e) => { e.stopPropagation(); openPicture(sweepCard(note), "bug-sweep-" + state.pond.today); });
  return b;
}

// The share card says how the day's grid went and nothing about where a
// wasp was, so it can be shown to somebody who hasn't played it yet.
function sweepCard(n) {
  const p = state.pond, cv = document.createElement("canvas"), x = cv.getContext("2d");
  cv.width = 800; cv.height = 440;
  x.fillStyle = "#f1e4c8"; x.fillRect(0, 0, 800, 440);
  x.fillStyle = "#2a241d"; x.textBaseline = "alphabetic";
  x.font = "700 52px 'Space Grotesk', sans-serif";
  x.fillText("Bug sweep", 46, 96);
  const [y, m, d] = p.today.split("-").map(Number);
  x.fillStyle = "#6b5d48"; x.font = "22px 'JetBrains Mono', monospace";
  x.fillText(d + " " + MONTHS[m - 1] + " " + y + " · " + titled(p.frog.name), 48, 136);
  x.fillStyle = "#2a241d"; x.fillRect(44, 168, 712, 124);
  x.fillStyle = "#17475a"; x.fillRect(48, 172, 704, 116);
  // Ten pads, one for each tenth of the grid cleared.
  const lit = n.state === "won" ? 10 : Math.floor(10 * n.pads / n.of);
  x.save(); x.translate(78, 194); x.scale(3, 3);
  for (let i = 0; i < 10; i++) {
    const cx = 12 + i * 22;
    if (i < lit) pad(x, cx, 12, false);
    else if (i === lit && n.state === "lost") wasp(x, cx, 12);
    else { x.fillStyle = "#123a48"; x.fillRect(cx - 9, 3, 18, 18); }
  }
  x.restore();
  x.fillStyle = "#2a241d"; x.font = "700 40px 'Space Grotesk', sans-serif";
  x.fillText(said(n), 46, 352);
  x.fillStyle = "#6b5d48"; x.font = "22px 'JetBrains Mono', monospace";
  x.fillText(n.taps + (n.taps === 1 ? " tap" : " taps"), 48, 392);
  x.textAlign = "right"; x.font = "18px 'JetBrains Mono', monospace";
  x.fillText("pond.bythewood.me", 752, 392);
  x.strokeStyle = "#b3a583"; x.lineWidth = 3;
  x.beginPath(); x.arc(706, 86, 34, 0, Math.PI * 2); x.stroke();
  drawIcon(x, "peb", 706 - 20, 86 - 20, 5);
  return cv;
}

// The label on the Play disk.
export function sweepArt(x) {
  x.fillStyle = "#17475a"; x.fillRect(0, 0, 168, 96);
  const open = { 9: 1, 10: 1, 11: 2, 16: 0, 17: 0, 18: 1, 23: 0, 24: 0, 25: 2, 2: 1, 3: 1, 4: 1 };
  for (let i = 0; i < 28; i++) {
    const cx = 12 + (i % 7) * 24, cy = 12 + Math.floor(i / 7) * 24;
    if (!(i in open)) { pad(x, cx, cy, false); if (i === 12 || i === 26) bud(x, cx, cy); continue; }
    x.fillStyle = "#123a48"; x.fillRect(cx - 12, cy - 12, 24, 24);
    if (!open[i]) continue;
    x.font = "700 14px 'Space Grotesk', sans-serif"; x.textAlign = "center";
    x.fillStyle = INK[open[i]]; x.fillText(String(open[i]), cx, cy + 5);
  }
  wasp(x, 132, 84);
}

// A lily pad from above, a disc with a notch cut out of its right side.
function pad(x, cx, cy, lit) {
  cx = Math.round(cx); cy = Math.round(cy);
  for (let dy = -10; dy < 10; dy++) {
    const hw = Math.round(Math.sqrt(100 - (dy + 0.5) * (dy + 0.5))), cut = Math.abs(dy + 0.5) < 3 ? 7 - Math.abs(Math.round(dy + 0.5)) * 2 : 0;
    x.fillStyle = "#1f5a2c"; x.fillRect(cx - hw, cy + dy, hw * 2 - cut, 1);
    if (hw < 3) continue;
    x.fillStyle = lit ? "#7fd08a" : "#3f9a4d"; x.fillRect(cx - hw + 1, cy + dy, hw * 2 - 2 - cut - (cut ? 1 : 0), 1);
  }
  x.fillStyle = lit ? "#b4eab8" : "#62bf6a"; x.fillRect(cx - 6, cy - 6, 4, 1); x.fillRect(cx - 7, cy - 5, 1, 3);
  x.fillStyle = "#2f7a3f"; x.fillRect(cx - 7, cy, 9, 1);
}

// A pink bud set on a pad to say there's a wasp under it.
function bud(x, cx, cy) {
  cx = Math.round(cx); cy = Math.round(cy);
  x.fillStyle = "#e2567a"; x.fillRect(cx - 4, cy - 2, 8, 4); x.fillRect(cx - 2, cy - 4, 4, 8);
  x.fillStyle = "#f59ab3"; x.fillRect(cx - 3, cy - 3, 6, 6);
  x.fillStyle = "#fff6dc"; x.fillRect(cx - 1, cy - 1, 2, 2);
}

function wasp(x, cx, cy) {
  cx = Math.round(cx); cy = Math.round(cy);
  x.fillStyle = "#cfe3ea"; x.fillRect(cx - 4, cy - 7, 4, 4); x.fillRect(cx + 1, cy - 7, 4, 4);
  x.fillStyle = "#ffd23a"; x.fillRect(cx - 6, cy - 3, 12, 7);
  x.fillStyle = "#14110c"; x.fillRect(cx - 3, cy - 3, 2, 7); x.fillRect(cx + 1, cy - 3, 2, 7); x.fillRect(cx + 5, cy - 2, 2, 5);
  x.fillStyle = "#14110c"; x.fillRect(cx - 7, cy - 1, 1, 3);
  x.fillStyle = "#fff6dc"; x.fillRect(cx + 5, cy - 1, 1, 1);
}

// Two halves, LOOK and MARK, with the one a tap does lit.
function modeKey(x) {
  const m = MODE, half = m.w / 2;
  x.fillStyle = "#0a0908"; x.fillRect(m.x - 1, m.y - 1, m.w + 2, m.h + 3);
  [["LOOK", !G.mode], ["MARK", G.mode]].forEach(([word, on], i) => {
    x.fillStyle = on ? "#ffb000" : "#2a241d"; x.fillRect(m.x + i * half, m.y, half - (i ? 0 : 1), m.h);
    x.fillStyle = on ? "#ffd27a" : "#3a322a"; x.fillRect(m.x + i * half, m.y, half - (i ? 0 : 1), 1);
    x.font = "700 8px 'JetBrains Mono', monospace"; x.textAlign = "center";
    x.fillStyle = on ? "#14110c" : "#bcaf99"; x.fillText(word, m.x + i * half + half / 2, m.y + 14);
  });
}

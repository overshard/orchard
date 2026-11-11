import { audioClock, croakAt, rain, sfx, soundOn } from "./sound.js";
import { cells, colours, draw, genome, W as FW, H as FH } from "./frog.js";
import { feel } from "./feel.js";

// Chorus. Four frogs on a log sing a few bars through once, then twice more
// with the player's frog left to tap its own part. The song keeps time off the
// audio clock, and the lamps under the log keep the same time with sound off.
const W = 200, H = 300, STEPS = 8, ROUNDS = 10, LIVES = 3, REGROW = 12;
const PERFECT = 0.05, GOOD = 0.12, NEAR = 0.25, LEAD = 1.2, AHEAD = 0.12;
const LOG = 150, SEATS = [6, 54, 102, 150], ME = 3, PITCH = [131, 196, 262, 392];
const SEEDS = [5021, 77, 31337], GX = 34, GY = 200, CW = 19, RH = 17;
const FONT = "700 13px 'Space Grotesk', sans-serif";

let G = null, ac = null, useAudio = false, frogs = [], singer = false;
const F = feel();

function now() { return useAudio ? ac.currentTime : performance.now() / 1000; }

function pick(from, n) {
  const a = from.slice();
  for (let i = a.length - 1; i > 0; i--) { const j = Math.floor(Math.random() * (i + 1)); [a[i], a[j]] = [a[j], a[i]]; }
  return a.slice(0, n).sort((x, y) => x - y);
}

// One bar's parts, the three on the log and then the player's. The player's
// part starts on the beat and only finds the off beats a few rounds in.
function bar(r) {
  const k = Math.min(5, 2 + Math.floor(r / 2));
  const mine = r < 2 ? pick([0, 2, 4, 6], k) : r < 5 ? pick([0, 2, 4, 6], k - 1).concat(pick([1, 3, 5, 7], 1)).sort((a, b) => a - b) : pick([0, 1, 2, 3, 4, 5, 6, 7], k);
  return [[0, 4], [2, 6], r < 3 ? [7] : pick([1, 3, 5, 7], 2), mine];
}

// The whole song laid out in song seconds. A round is a pattern of one to
// three bars, sung once to listen to and twice for the player to sing.
function song() {
  const bars = [], targets = [], notes = [];
  let t = LEAD;
  for (let r = 0; r < ROUNDS; r++) {
    const step = 30 / (88 + r * 4), n = 1 + Math.floor(r / 4), parts = [];
    for (let b = 0; b < n; b++) parts.push(bar(r));
    ["listen", "play", "play"].forEach((phase) => parts.forEach((p, b) => {
      bars.push({ t, step, r, phase, b, n, p });
      p.forEach((steps, f) => steps.forEach((s) => {
        if (f === ME && phase === "play") targets.push({ t: t + s * step, bar: bars.length - 1, s, got: 0 });
        else notes.push({ t: t + s * step, f });
      }));
      t += STEPS * step;
    }));
  }
  notes.sort((a, b) => a.t - b.t);
  return { bars, targets, notes, end: t + 0.4 };
}

function barAt(h) {
  let i = G.bi;
  while (i + 1 < G.bars.length && G.bars[i + 1].t <= h) i++;
  return h >= G.bars[0].t ? i : -1;
}

function lamp(s, row) { return [GX + s * CW + CW / 2, GY + row * RH + RH / 2]; }

function miss(tg) {
  tg.got = -1;
  G.lives--; G.streak = 0;
  F.hit(0.35);
  const [x, y] = tg.s >= 0 ? lamp(tg.s, ME) : [172, LOG - 20];
  F.burst(x, y, "#8a9099", 5);
  F.pop(x, y - 8, "MISS", "#bcaf99");
  sfx("plop");
  if (G.lives <= 0) end("OUT OF LILIES");
}

function end(why) {
  if (G.st === "done") return;
  G.st = "done"; G.why = why; G.done_t = 0;
  rain(0);
}

// A tap is timed off the clock the moment it lands, not off the frame.
function sing() {
  if (!G || G.st !== "song" || G.hold) return;
  const h = now() - G.t0 - G.lat;
  if (useAudio) croakAt(PITCH[ME], ac.currentTime, 0.2);
  G.puff[ME] = h; F.squash(0.5);
  if (h - G.lastHit < GOOD) return;
  let best = null, near = null;
  for (let i = G.ti; i < G.targets.length && G.targets[i].t < h + NEAR; i++) {
    const tg = G.targets[i], d = Math.abs(tg.t - h);
    if (tg.got) continue;
    if (d <= GOOD && (!best || d < Math.abs(best.t - h))) best = tg;
    if (d <= NEAR && !near) near = tg;
  }
  // A tap that's near nothing only counts against a bar that's the player's.
  const bi = barAt(h);
  if (!best && bi >= 0 && G.bars[bi].phase === "play") miss(near || { s: -1 });
  if (!best) return;
  const err = Math.abs(best.t - h), right = err <= PERFECT;
  G.streak++; G.hits++; G.lastHit = h;
  G.long = Math.max(G.long, G.streak);
  // A good run grows a lily back.
  if (G.streak % REGROW === 0 && G.lives < LIVES) { G.lives++; F.pop(W - 14 - (G.lives - 1) * 14, 24, "+1", "#9fe08f"); }
  if (right) G.right++;
  const pts = (right ? 3 : 2) + (G.streak >= 8 ? 1 : 0);
  G.score += pts; best.got = right ? 2 : 1;
  const [x, y] = lamp(best.s, ME);
  F.burst(x, y, right ? "#ffb000" : "#ffe27a", right ? 6 : 4);
  F.pop(x, y - 10, "+" + pts, right ? "#ffb000" : "#ffe27a");
}

export const chorus = {
  key: "chorus", W, H, title: "CHORUS", feel: F, edge: ["#14303a", "#0a1b1f"],
  aria: "Listen to the bar, then tap or press space on each of your frog's beats as the lamps reach them.",
  get help() {
    const l = ["LISTEN, THEN SING YOUR PART", "TAP ON EACH AMBER LAMP", "A MISS COSTS A LILY"];
    if (singer) l.push("A SINGER SCORES A QUARTER MORE");
    return l.concat("TAP TO START");
  },

  reset(g, p) {
    const stage = ["egg", "tadpole"].includes(p.frog.stage) ? "froglet" : p.frog.stage;
    frogs = SEEDS.map((s) => { const n = genome(s); return { c: cells(n, "frog", {}), col: colours(n, null) }; });
    frogs.push({ c: cells(g, stage, { hat: p.frog.hat.shape }), col: colours(g, p.frog.hat) });
    singer = p.frog.nature === "SINGER";
    G = Object.assign(song(), { st: "song", t: 0, s: 0, t0: null, last: 0, lat: 0, hold: false, bi: 0, ni: 0, vi: 0, ti: 0, sched: 0, rainAt: -1, lives: LIVES, score: 0, streak: 0, long: 0, hits: 0, right: 0, lastHit: -9, puff: [-9, -9, -9, -9], why: "", done_t: 0 });
    useAudio = false;
  },

  // The audio clock is asked for on the run's first frames, since the tap
  // that started it is what lets it run, and it's settled before any note.
  step(dt) {
    G.t += dt;
    if (G.st === "done") { G.done_t += dt; return; }
    if (G.t0 === null) { G.last = now(); G.t0 = G.last; }
    if (!useAudio && G.s < LEAD / 2 && (ac = audioClock())) {
      useAudio = true;
      G.lat = soundOn("sound") ? Math.min(0.2, ac.outputLatency || ac.baseLatency || 0) : 0;
      G.last = now(); G.t0 = G.last - G.s;
    }
    const c = now(), adv = c - G.last;
    G.last = c;
    // A frozen or hidden frame never moves the song on, it waits where it was.
    if (G.hold || adv < 0 || adv > dt + 0.25) { G.t0 = c - G.s; G.hold = false; G.rainAt = -1; }
    G.s = c - G.t0;
    const h = G.s - G.lat;
    if (useAudio) {
      while (G.ni < G.notes.length && G.notes[G.ni].t < G.s + AHEAD) {
        const n = G.notes[G.ni++];
        if (n.t >= G.sched) croakAt(PITCH[n.f], G.t0 + n.t, n.f === ME ? 0.2 : 0.14);
      }
      G.sched = G.s + AHEAD;
    }
    while (G.vi < G.notes.length && G.notes[G.vi].t <= h) G.puff[G.notes[G.vi++].f] = h;
    G.bi = Math.max(0, barAt(h));
    const r = G.bars[G.bi].r;
    if (r !== G.rainAt) { G.rainAt = r; rain(r / (ROUNDS - 1)); }
    while (G.ti < G.targets.length && (G.targets[G.ti].got || h > G.targets[G.ti].t + GOOD)) {
      const tg = G.targets[G.ti++];
      if (!tg.got) miss(tg);
      if (G.st === "done") return;
    }
    if (h > G.end) end("THE SONG IS DONE");
  },

  pause() { if (G) { G.hold = true; rain(0); } },

  demo() { return G && G.st === "song" ? { x: SEATS[ME] + 22, y: LOG - 18 } : null; },

  press() { sing(); },
  keys(code, down) { if (down && ["Space", "Enter", "ArrowUp", "ArrowDown", "KeyW", "KeyS"].includes(code)) sing(); },

  over() { return G && G.st === "done" && G.done_t > 0.9; },
  raw() { return G ? G.score : 0; },
  score() { const s = chorus.raw(); return singer ? s + Math.floor(s / 4) : s; },
  // The server adds the singer's quarter itself, so it's sent the plain score.
  report() { return { score: chorus.raw() }; },
  why() { return G ? G.why : ""; },
  tally() {
    if (!G) return [];
    const l = [G.hits + " of " + G.targets.length + " beats sung", G.right + " right on the beat", "Longest run " + G.long];
    if (singer) l.push("A singer, a quarter more");
    return l;
  },

  draw(x) {
    const t = G ? G.t : 0, h = G ? G.s - G.lat : 0, r = G ? G.bars[G.bi].r : 0;
    scene(x, t, r);
    frogs.forEach((f, i) => {
      const sx = SEATS[i], oy = LOG - 31, sing = G && h - G.puff[i] < 0.14;
      const body = () => {
        draw(x, f.c, FW, FH, f.col, sx, oy - (sing ? 1 : 0), 1);
        if (sing) { x.fillStyle = f.col[7]; x.fillRect(sx + 17, oy + 23, 10, 4); x.fillRect(sx + 18, oy + 22, 8, 6); }
      };
      if (i === ME) F.squashed(x, sx + 22, LOG - 1, body);
      else body();
    });
    x.fillStyle = "#ffb000"; x.fillRect(SEATS[ME] + 20, LOG + 14, 5, 2); x.fillRect(SEATS[ME] + 21, LOG + 16, 3, 1); x.fillRect(SEATS[ME] + 22, LOG + 17, 1, 1);
    if (G) grid(x, h);
    F.draw(x);
    if (!G) return;
    x.font = FONT; x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(String(chorus.score()), 6, 15);
    for (let i = 0; i < LIVES; i++) lily(x, W - 14 - i * 14, 6, i < G.lives);
  },
};

// The lamps, a row a frog and a column a step of the bar being sung. The
// player's row is rings to hit while it's their go.
function grid(x, h) {
  const b = G.bars[G.bi], playing = b.phase === "play" && h >= b.t;
  x.fillStyle = "#0b1f17"; x.fillRect(8, GY - 10, W - 16, RH * 4 + 18);
  x.fillStyle = "#1a3a2c"; x.fillRect(8, GY - 10, W - 16, 1);
  x.fillStyle = "#050d0a"; x.fillRect(8, GY + RH * 4 + 7, W - 16, 1);
  const pos = h >= b.t ? (h - b.t) / b.step : -1;
  frogs.forEach((f, row) => {
    const cy = GY + row * RH;
    x.fillStyle = f.col[1]; x.fillRect(14, cy + 5, 8, 7);
    x.fillStyle = f.col[4]; x.fillRect(15, cy + 4, 2, 2); x.fillRect(19, cy + 4, 2, 2);
    for (let s = 0; s < STEPS; s++) {
      const lx = GX + s * CW + 4, ly = cy + 4, on = b.p[row].includes(s), lit = on && pos >= s && pos < s + 0.7;
      if (row === ME && b.phase === "play") {
        const tg = on && G.targets.find((q) => q.bar === G.bi && q.s === s);
        if (!on) { x.fillStyle = "#13281f"; x.fillRect(lx, ly, 11, 9); continue; }
        x.fillStyle = !tg || !tg.got ? "#3a2a08" : tg.got === 2 ? "#ffb000" : tg.got === 1 ? "#ffe27a" : "#5b6470";
        x.fillRect(lx, ly, 11, 9);
        if (!tg || !tg.got) { x.strokeStyle = "#ffb000"; x.lineWidth = 1; x.strokeRect(lx + 0.5, ly + 0.5, 10, 8); }
        continue;
      }
      x.fillStyle = !on ? "#13281f" : lit ? (row === ME ? "#ffe27a" : "#cfe3ea") : row === ME ? "#7a5208" : "#2f5a48";
      x.fillRect(lx, ly, 11, 9);
    }
  });
  if (pos >= 0 && pos < STEPS) {
    const px = Math.round(GX + pos * CW + 2);
    x.fillStyle = "rgba(255,246,220,0.55)"; x.fillRect(px, GY - 6, 1, RH * 4 + 8);
  }
  x.font = FONT; x.textAlign = "left";
  x.fillStyle = playing ? "#ffb000" : "#cfe3ea";
  x.fillText(h < G.bars[0].t ? "READY" : playing ? "YOUR PART" : "LISTEN", 10, GY + RH * 4 + 26);
  for (let i = 0; i < b.n; i++) { x.fillStyle = i === b.b ? "#ffe27a" : "#2f5a48"; x.fillRect(W - 16 - (b.n - 1 - i) * 9, GY + RH * 4 + 17, 6, 6); }
  x.textAlign = "right"; x.fillStyle = "#7d9a90";
  x.fillText((b.r + 1) + "/" + ROUNDS, W - 46, GY + RH * 4 + 26);
}

function lily(x, cx, cy, on) {
  x.fillStyle = on ? "#3f9a4d" : "#24402c"; x.fillRect(cx - 5, cy + 3, 11, 4); x.fillRect(cx - 4, cy + 2, 9, 6);
  if (!on) return;
  x.fillStyle = "#f59ab3"; x.fillRect(cx - 2, cy - 1, 5, 4); x.fillRect(cx - 1, cy - 2, 3, 6);
  x.fillStyle = "#ffe27a"; x.fillRect(cx, cy + 1, 1, 1);
}

// Night over the pond with the rain coming on harder each round.
function scene(x, t, r) {
  ["#14303a", "#163640", "#183c46", "#1a4048"].forEach((c, i) => { x.fillStyle = c; x.fillRect(0, i * 30, W, 30); });
  x.fillStyle = "#0f2a24"; for (let i = 0; i < W; i += 2) { const hgt = 10 + ((i * 7919) % 13) + Math.round(6 * Math.sin(i / 9)); x.fillRect(i, 120 - hgt, 2, hgt); }
  x.fillStyle = "#123a3d"; x.fillRect(0, 120, W, H - 120);
  ["#123a3d", "#0f3236", "#0d2a2e", "#0b2226", "#0a1b1f"].forEach((c, i) => { x.fillStyle = c; x.fillRect(0, LOG + 10 + i * 30, W, 30); });
  x.fillStyle = "#5a3a1e"; x.fillRect(0, LOG - 1, W, 12);
  x.fillStyle = "#7a5230"; x.fillRect(0, LOG - 1, W, 3);
  x.fillStyle = "#3e2814"; for (let i = 7; i < W; i += 23) x.fillRect(i, LOG + 4, 9, 1);
  x.fillStyle = "#2e1e10"; x.fillRect(0, LOG + 10, W, 2);
  const drops = 14 + r * 10;
  x.fillStyle = "rgba(207,227,234,0.45)";
  for (let i = 0; i < drops; i++) {
    const dx = (i * 71 + 13) % W, sp = 150 + (i * 37) % 60, y = ((t * sp + i * 97) % (H + 40)) - 20;
    x.fillRect((dx - Math.round(y * 0.08) + W) % W, Math.round(y), 1, 5);
  }
  x.fillStyle = "rgba(127,214,194,0.35)";
  for (let i = 0; i < 3 + r; i++) {
    const k = ((t * 0.9 + i * 0.37) % 1), cx = (i * 89 + Math.floor(t * 0.9 + i * 0.37) * 53) % (W - 20) + 10, cy = LOG + 24 + (i * 41) % 26;
    const rw = Math.round(2 + k * 10);
    x.globalAlpha = 1 - k;
    x.fillRect(cx - rw, cy, rw * 2, 1);
    x.globalAlpha = 1;
  }
}

// The label on the Play disk.
export function chorusArt(x, g) {
  x.fillStyle = "#163640"; x.fillRect(0, 0, 168, 96);
  x.fillStyle = "#0f2a24"; for (let i = 0; i < 168; i += 2) { const hgt = 6 + ((i * 7919) % 9) + Math.round(4 * Math.sin(i / 9)); x.fillRect(i, 50 - hgt, 2, hgt); }
  x.fillStyle = "#123a3d"; x.fillRect(0, 50, 168, 46);
  x.fillStyle = "#5a3a1e"; x.fillRect(0, 62, 168, 9); x.fillStyle = "#7a5230"; x.fillRect(0, 62, 168, 2);
  [5021, 77, 31337].forEach((s, i) => { const n = genome(s); draw(x, cells(n, "frog", {}), FW, FH, colours(n, null), 2 + i * 40, 31, 1); });
  draw(x, cells(g, "frog", {}), FW, FH, colours(g, null), 122, 31, 1);
  x.fillStyle = colours(g, null)[7]; x.fillRect(139, 54, 10, 4); x.fillRect(140, 53, 8, 6);
  x.fillStyle = "rgba(207,227,234,0.5)"; for (let i = 0; i < 18; i++) x.fillRect((i * 47) % 168, (i * 29) % 60, 1, 4);
  for (let s = 0; s < 8; s++) { x.fillStyle = [0, 3, 4, 6].includes(s) ? "#ffb000" : "#2f5a48"; x.fillRect(16 + s * 18, 80, 11, 8); }
}

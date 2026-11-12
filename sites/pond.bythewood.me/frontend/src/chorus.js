import { audioClock, croakAt, rain, soundOn } from "./sound.js";
import { cells, colours, draw, genome, W as FW, H as FH } from "./frog.js";
import { feel } from "./feel.js";
import { SONGS, ROUNDS, HOOK, CHOP, PERFECT, GOOD, LATE, lay, points, grade, ovation, hookNote } from "./songs.js";

// Chorus. Four frogs on a log sing one of the songs, the player's frog hears
// its part once and sings it back. Notes rise up the lane under each frog and
// are sung as they meet the log, timed off the audio clock.
const W = 200, H = 300, AHEAD = 0.12;
const LOG = 150, SEATS = [6, 54, 102, 150], ME = HOOK, HY = 174, LB = 296;
const SEEDS = [5021, 77, 31337];
const FONT = "700 13px 'Space Grotesk', sans-serif", SMALL = "700 9px 'Space Grotesk', sans-serif";

let G = null, ac = null, useAudio = false, frogs = [], singer = false, upNext = null;
const F = feel();

function now() { return useAudio ? ac.currentTime : performance.now() / 1000; }
function col(f) { return SEATS[f] + 22; }

// Songs come round in a shuffled order kept in this browser, so all ten are
// heard before one comes back.
function pickSong() {
  let bag = [];
  try { bag = JSON.parse(localStorage.getItem("pond-chorus") || "[]"); } catch (e) { bag = []; }
  bag = Array.isArray(bag) ? bag.filter((i) => Number.isInteger(i) && i >= 0 && i < SONGS.length) : [];
  if (!bag.length) {
    bag = SONGS.map((_, i) => i);
    for (let i = bag.length - 1; i > 0; i--) { const j = Math.floor(Math.random() * (i + 1)); [bag[i], bag[j]] = [bag[j], bag[i]]; }
  }
  const i = bag.shift();
  try { localStorage.setItem("pond-chorus", JSON.stringify(bag)); } catch (e) { /* private window */ }
  return i;
}

function barAt(h) {
  let i = G.bi;
  while (i + 1 < G.bars.length && G.bars[i + 1].t <= h) i++;
  return i;
}

// The lane runs a bar and a half ahead of the log.
function yOf(t, h) { return HY + (t - h) * (LB - HY) / (12 * G.step); }

function miss(tg) {
  tg.got = -1;
  G.run = Math.floor(G.run / 2); G.row = 0;
  F.pop(col(ME), HY - 6, "MISS", "#8a9099");
}

function end() {
  if (G.st === "done") return;
  G.st = "done"; G.done_t = 0;
  G.why = grade(G.right, G.near, G.targets.length);
  upNext = null;
  rain(0);
}

// A tap is timed off the clock the moment it lands, not off the frame, and
// one that's near no note just croaks.
function sing() {
  if (!G || G.st !== "song" || G.hold) return;
  const h = now() - G.t0 - G.lat;
  let best = null;
  for (let i = Math.max(0, G.ti - 2); i < G.targets.length && G.targets[i].t < h + 0.4; i++) {
    const tg = G.targets[i], d = Math.abs(h - tg.t - LATE);
    if (!tg.got && d <= GOOD && (!best || d < Math.abs(h - best.t - LATE))) best = tg;
  }
  const b = G.bars[Math.max(0, barAt(h))], near = best || G.targets[G.ti];
  const p = near && Math.abs(near.t - h) < 0.6 ? near.p : hookNote(b.v, 0);
  if (useAudio) croakAt(p, ac.currentTime, 0.2);
  G.puff[ME] = h; F.squash(0.5);
  if (!best) return;
  const right = Math.abs(h - best.t - LATE) <= PERFECT;
  G.run++; G.row++; G.hits++;
  G.long = Math.max(G.long, G.row);
  if (right) G.right++; else G.near++;
  const pts = points(right, G.run);
  G.score += pts; best.got = right ? 2 : 1;
  F.burst(col(ME), HY, right ? "#ffb000" : "#ffe27a", right ? 6 : 4);
  F.pop(col(ME), HY - 8, "+" + pts, right ? "#ffb000" : "#ffe27a");
}

export const chorus = {
  key: "chorus", W, H, title: "CHORUS", feel: F, edge: ["#14303a", "#0a1b1f"],
  aria: "Listen to your frog's part, then tap or press space as each of its notes rises to the log.",
  get help() {
    const l = [upNext === null ? "A SONG ON THE LOG" : SONGS[upNext].name.toUpperCase(), "LISTEN, THEN SING YOUR PART", "TAP AS EACH NOTE MEETS THE LOG"];
    if (singer) l.push("A SINGER SCORES A QUARTER MORE");
    return l.concat("TAP TO START");
  },

  reset(g, p) {
    const stage = ["egg", "tadpole"].includes(p.frog.stage) ? "froglet" : p.frog.stage;
    frogs = SEEDS.map((s) => { const n = genome(s); return { c: cells(n, "frog", {}), col: colours(n, null) }; });
    frogs.push({ c: cells(g, stage, { hat: p.frog.hat.shape }), col: colours(g, p.frog.hat) });
    singer = p.frog.nature === "SINGER";
    if (upNext === null) upNext = pickSong();
    G = Object.assign(lay(SONGS[upNext]), { song: SONGS[upNext], st: "song", t: 0, s: 0, t0: null, last: 0, lat: 0, hold: false, bi: 0, ni: 0, vi: 0, ti: 0, sched: 0, rainAt: -1, score: 0, run: 0, row: 0, long: 0, hits: 0, right: 0, near: 0, puff: [-9, -9, -9, -9], why: "", done_t: 0 });
    useAudio = false;
  },

  // The audio clock is asked for on the run's first frames, since the tap
  // that started it is what lets it run, and it's settled before any note.
  step(dt) {
    G.t += dt;
    if (G.st === "done") { G.done_t += dt; return; }
    if (G.t0 === null) { G.last = now(); G.t0 = G.last; }
    if (!useAudio && G.s < G.bars[0].t / 2 && (ac = audioClock())) {
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
        if (n.t >= G.sched) croakAt(n.p, G.t0 + n.t, n.v);
      }
      G.sched = G.s + AHEAD;
    }
    while (G.vi < G.notes.length && G.notes[G.vi].t <= h) G.puff[G.notes[G.vi++].f] = h;
    G.bi = barAt(h);
    const r = G.bars[G.bi].r;
    if (r !== G.rainAt) { G.rainAt = r; rain(r / (ROUNDS - 1)); }
    while (G.ti < G.targets.length && (G.targets[G.ti].got || h > G.targets[G.ti].t + LATE + GOOD)) {
      const tg = G.targets[G.ti++];
      if (!tg.got) miss(tg);
    }
    if (h > G.end) end();
  },

  pause() { if (G) { G.hold = true; rain(0); } },

  demo() { return G && G.st === "song" ? { x: col(ME), y: HY } : null; },

  press() { sing(); },
  keys(code, down) { if (down && ["Space", "Enter", "ArrowUp", "ArrowDown", "KeyW", "KeyS"].includes(code)) sing(); },

  over() { return G && G.st === "done" && G.done_t > 0.9; },
  raw() { return G ? G.score : 0; },
  score() { const s = chorus.raw(); return singer ? s + Math.floor(s / 4) : s; },
  // The server adds the singer's quarter itself, so it's sent the plain score.
  report() { return { score: chorus.raw(), counts: { right: G.right, row: G.long, ovation: ovation(G.right, G.near, G.targets.length) ? 1 : 0 } }; },
  why() { return G ? G.why : ""; },
  tally() {
    if (!G) return [];
    const l = [G.song.name, G.hits + " of " + G.targets.length + " notes sung", G.right + " right on the beat", "Longest run " + G.long];
    if (singer) l.push("A singer, a quarter more");
    return l;
  },

  draw(x) {
    const t = G ? G.t : 0, h = G ? G.s - G.lat : 0, r = G ? G.bars[G.bi].r : 0;
    scene(x, t, r);
    if (G) lane(x, h);
    frogs.forEach((f, i) => {
      const sx = SEATS[i], oy = LOG - 31, sing = G && h - G.puff[i] < 0.14;
      const body = () => {
        draw(x, f.c, FW, FH, f.col, sx, oy - (sing ? 1 : 0), 1);
        if (sing) { x.fillStyle = f.col[7]; x.fillRect(sx + 17, oy + 23, 10, 4); x.fillRect(sx + 18, oy + 22, 8, 6); }
      };
      if (i === ME) F.squashed(x, sx + 22, LOG - 1, body);
      else body();
    });
    F.draw(x);
    if (!G) return;
    x.font = FONT; x.textAlign = "left"; x.fillStyle = "#ece5d8"; x.fillText(String(chorus.score()), 6, 15);
    const bonus = points(false, G.run) - 1;
    if (bonus > 0) { x.font = SMALL; x.fillStyle = "#ffb000"; x.fillText("RUN +" + bonus, 6, 27); }
    x.font = FONT; x.textAlign = "right"; x.fillStyle = "#7d9a90";
    x.fillText(Math.max(1, r + 1) + "/" + ROUNDS, W - 6, 15);
    status(x, h);
  },
};

// What the log is up to, over the frogs' heads.
function status(x, h) {
  const b = G.bars[G.bi];
  let s = "", c = "#cfe3ea";
  if (G.st === "done" || b.kind === "outro") { s = grade(G.right, G.near, G.targets.length); c = "#ffe27a"; }
  else if (h < G.bars[0].t || b.kind === "intro") s = G.song.name.toUpperCase();
  else if (b.kind === "listen") s = "LISTEN";
  else if (b.kind === "count") { s = Math.min(4, 1 + Math.floor((h - b.t) / (2 * b.step))); c = "#ffb000"; }
  else { s = b.hide ? "FROM MEMORY" : "SING"; c = "#ffb000"; }
  x.font = FONT; x.textAlign = "center"; x.lineWidth = 3; x.strokeStyle = "#0a1b1f";
  // The count is its digits so far, spaced out one a beat.
  const words = typeof s === "number" ? Array.from({ length: s }, (_, i) => [String(i + 1), W / 2 + (i - 1.5) * 16]) : [[s, W / 2]];
  words.forEach(([w, wx]) => { x.strokeText(w, wx, 44); x.fillStyle = c; x.fillText(w, wx, 44); });
}

// The lane is readout glass under the log, a column a frog. Every note rises
// to the line under its frog, the player's as amber bars to sing and the
// others as small marks so the groove can be seen.
function lane(x, h) {
  const top = 164, span = 12 * G.step, gone = h - 0.2;
  x.fillStyle = "#0b1f17"; x.fillRect(4, top, W - 8, LB - top + 2);
  x.fillStyle = "#050d0a"; x.fillRect(4, top, W - 8, 1);
  x.fillStyle = "#1a3a2c"; x.fillRect(4, LB + 1, W - 8, 1);
  SEATS.forEach((_, f) => {
    const w = f === ME ? 26 : 12;
    x.fillStyle = f === ME ? "#11291d" : "#0e241a"; x.fillRect(col(f) - w / 2, top + 1, w, LB - top);
  });
  x.save(); x.beginPath(); x.rect(4, top + 1, W - 8, LB - top); x.clip();
  for (let i = Math.max(0, G.bi - 1); i < G.bars.length && G.bars[i].t < h + span; i++) {
    const b = G.bars[i];
    for (let k = 0; k < 4; k++) {
      const y = Math.round(yOf(b.t + k * 2 * b.step, h));
      x.fillStyle = k ? "#123024" : b.kind === "count" ? "#3a5a2c" : "#1f4a39";
      x.fillRect(5, y, W - 10, 1);
    }
  }
  for (let i = Math.max(0, G.vi - 16); i < G.notes.length && G.notes[i].t < h + span; i++) {
    const n = G.notes[i];
    if (n.t < gone) continue;
    const y = Math.round(yOf(n.t, h)), past = n.t < h;
    x.globalAlpha = past ? 0.35 : 1;
    if (n.f === ME) {
      x.strokeStyle = "#a8720c"; x.lineWidth = 1; x.strokeRect(col(ME) - 9.5, y - 2.5, 19, 5);
    } else if (n.cue) {
      x.fillStyle = "#fff6dc"; x.fillRect(col(CHOP) - 2, y - 2, 4, 4);
    } else {
      x.fillStyle = frogs[n.f].col[1]; x.fillRect(col(n.f) - 3, y - 1, 6, 3);
    }
  }
  x.globalAlpha = 1;
  for (let i = Math.max(0, G.ti - 6); i < G.targets.length && G.targets[i].t < h + span; i++) {
    const tg = G.targets[i];
    if (tg.got > 0 || tg.t < gone) continue;
    const y = Math.round(yOf(tg.t, h));
    // A part sung from memory fades out on its way up, so it's seen coming.
    x.globalAlpha = tg.got < 0 ? 0.3 : tg.hide ? Math.max(0, Math.min(1, (y - HY - 30) / 40)) : 1;
    x.fillStyle = tg.got < 0 ? "#5b6470" : "#ffb000"; x.fillRect(col(ME) - 10, y - 3, 20, 6);
    x.fillStyle = tg.got < 0 ? "#3a4048" : "#7a5208"; x.fillRect(col(ME) - 8, y - 1, 16, 2);
  }
  x.globalAlpha = 1;
  x.restore();
  x.fillStyle = "rgba(255,246,220,0.6)"; x.fillRect(5, HY, W - 10, 1);
  x.strokeStyle = "#ffb000"; x.lineWidth = 1; x.strokeRect(col(ME) - 13.5, HY - 4.5, 27, 9);
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
  x.fillStyle = "#ffb000"; x.fillRect(SEATS[ME] + 20, LOG + 6, 5, 2); x.fillRect(SEATS[ME] + 21, LOG + 8, 3, 1);
  const drops = 14 + r * 18;
  x.fillStyle = "rgba(207,227,234,0.45)";
  for (let i = 0; i < drops; i++) {
    const dx = (i * 71 + 13) % W, sp = 150 + (i * 37) % 60, y = ((t * sp + i * 97) % (H + 40)) - 20;
    x.fillRect((dx - Math.round(y * 0.08) + W) % W, Math.round(y), 1, 5);
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
  x.fillStyle = "#0b1f17"; x.fillRect(0, 73, 168, 23);
  x.fillStyle = "#fff6dc"; x.fillRect(0, 76, 168, 1);
  [0, 1, 2].forEach((i) => { x.fillStyle = "#2f5a48"; for (let k = 0; k < 3; k++) x.fillRect(21 + i * 40, 80 + k * 6 + (i % 2) * 3, 6, 2); });
  x.fillStyle = "#ffb000"; [79, 88].forEach((y) => x.fillRect(134, y, 22, 4));
}

// Plays raft run through raftworld.js with bun, the frog alone and then a
// player who steers well and catches everything, to set raft's rate and most.
// bun raftsim.js [runs]
import { world, run, score, metres, over } from "./src/raftworld.js";

function rng(a) {
  return () => { a |= 0; a = (a + 0x6d2b79f5) | 0; let t = Math.imul(a ^ (a >>> 15), 1 | a); t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t; return ((t ^ (t >>> 14)) >>> 0) / 4294967296; };
}

const runs = Number(process.argv[2]) || 500, DT = 1 / 60;
const PLAYER = { steer: 150, dodge: 1, read: 1, notice: 1, ride: 1 };

function play(pilot, seed) {
  const w = world({ rand: rng(seed), pilot });
  let t = 0, tips = 0;
  while (!over(w) && t < 1200) {
    run(w, DT);
    for (const e of w.events) if (e.k === "tip") tips++;
    w.events.length = 0;
    t += DT;
  }
  return { score: score(w), t, m: metres(w), tips, flies: w.flies, whirls: w.whirls, weirs: w.weirs };
}

for (const [name, pilot] of [["frog alone", undefined], ["player", PLAYER]]) {
  let sum = 0, best = 0, secs = 0, rate = 0, tips = 0, far = 0, ten = 0, flies = 0;
  const lasts = [];
  for (let i = 0; i < runs; i++) {
    const r = play(pilot, 1000 + i);
    sum += r.score; best = Math.max(best, r.score); secs += r.t; tips += r.tips; flies += r.flies;
    rate = Math.max(rate, r.score / r.t);
    far += r.m >= 300 ? 1 : 0; ten += r.flies >= 10 ? 1 : 0;
    lasts.push(r.t);
  }
  lasts.sort((a, b) => a - b);
  const pct = (n) => Math.round((n * 100) / runs) + "%";
  console.log(name + ": average " + Math.round(sum / runs) + ", best " + best + ", " + (secs / runs).toFixed(1) +
    "s a run, median " + lasts[runs >> 1].toFixed(1) + "s, most " + rate.toFixed(2) + " a second, tipped " + pct(tips) +
    ", 300 metres " + pct(far) + ", ten fireflies " + pct(ten) + ", " + (flies / runs).toFixed(1) + " fireflies a run");
}

// Plays lily pinball through pinworld.js with bun, the frog alone and then a
// player who saves every ball, to set pin's most and rate in arcade.go.
// bun pinsim.js [games]
import { world, run } from "./src/pinworld.js";

function rng(a) {
  return () => { a |= 0; a = (a + 0x6d2b79f5) | 0; let t = Math.imul(a ^ (a >>> 15), 1 | a); t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t; return ((t ^ (t >>> 14)) >>> 0) / 4294967296; };
}

const games = Number(process.argv[2]) || 500, DT = 1 / 60;

function play(save, seed) {
  const w = world(rng(seed), save);
  let t = 0, events = { drain: 0, saved: 0, gulp: 0, set: 0 };
  while (!w.over && t < 1200) {
    run(w, DT);
    for (const e of w.events) if (e.k in events) events[e.k]++;
    w.events.length = 0;
    t += DT;
  }
  return { score: w.score, t, escaped: w.escaped || 0, counts: w.counts, events };
}

for (const [name, save] of [["frog alone", undefined], ["player", 1]]) {
  let sum = 0, best = 0, secs = 0, escaped = 0, rate = 0, mouth = 0, multi = 0, long = 0, lights = 0;
  const scores = [];
  for (let i = 0; i < games; i++) {
    const r = play(save, 1000 + i);
    sum += r.score; best = Math.max(best, r.score); secs += r.t; escaped += r.escaped;
    rate = Math.max(rate, r.score / r.t);
    mouth += r.counts.mouth >= 3 ? 1 : 0; multi += r.counts.multi >= 1 ? 1 : 0; lights += r.counts.lights;
    long += r.counts.long >= 45 ? 1 : 0;
    scores.push(r.score);
  }
  scores.sort((a, b) => a - b);
  const pct = (n) => Math.round((n * 100) / games) + "%";
  console.log(name + ": average " + Math.round(sum / games) + ", median " + scores[games >> 1] + ", best " + best +
    ", " + (secs / games / 3).toFixed(1) + "s a ball, " + (secs / games).toFixed(0) + "s a game, most " + rate.toFixed(1) + " a second, " +
    escaped + " left the table, three mouths " + pct(mouth) + ", a multiball " + pct(multi) + ", a 45s ball " + pct(long) +
    ", sets " + (lights / games).toFixed(2) + " a game");
}

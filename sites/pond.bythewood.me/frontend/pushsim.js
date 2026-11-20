// Drops pebbles through pushworld.js with bun, twenty a day at random spots
// with the table packed and unpacked between days the way the server keeps
// it, and says where they went, how long the prizes took to come off and
// whether a pile left alone holds still.
// bun pushsim.js [drops] [aim]
import { world, fresh, run, drop, place, pack, unpack, settle, FX0, FX1, HOLE } from "./src/pushworld.js";

function rng(a) {
  return () => { a |= 0; a = (a + 0x6d2b79f5) | 0; let t = Math.imul(a ^ (a >>> 15), 1 | a); t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t; return ((t ^ (t >>> 14)) >>> 0) / 4294967296; };
}

const total = Number(process.argv[2]) || 20000, aim = process.argv[3] || "random", DT = 1 / 60, DAY = 20;
const rand = rng(7);
let w = world(rand);
fresh(w, 90);
w.spill = 10;
let front = 0, sides = 0, holes = 0, showers = 0, shower = 0, biggest = 0, dropped = 0, day = 0;
const prizeAt = new Map(), took = [], firstDays = [];
let wonToday = 0;

function step(secs) {
  for (let t = 0; t < secs; t += DT) {
    run(w, DT);
    for (const e of w.events) {
      if (e.k === "won" && !e.kind) { front++; wonToday++; }
      if (e.k === "gutter" && !e.kind) sides++;
      if (e.k === "hole") holes++;
      if (e.k === "shower") { showers++; shower += e.n; biggest = Math.max(biggest, e.n); }
    }
    w.events.length = 0;
    for (const id of w.claimed) { took.push(day - prizeAt.get(id).day); prizeAt.delete(id); w.ready.delete(id); }
    w.claimed.length = 0;
    for (const [id, p] of prizeAt) if (dropped - p.at >= 30) w.ready.add(id);
  }
}

while (dropped < total) {
  // A day's sitting starts from the table as the server kept it.
  const kept = pack(w), spill = w.spill, ready = w.ready;
  w = world(rand); unpack(w, kept); w.spill = spill; w.ready = ready;
  // Prize ids are reused like the server's, the smallest one free.
  if (prizeAt.size < 3) { let id = 1; while (prizeAt.has(id)) id++; prizeAt.set(id, { day, at: dropped }); place(w, id); }
  wonToday = 0;
  for (let i = 0; i < DAY && dropped < total; i++) {
    const x = aim === "hole" && i % 4 === 0 ? HOLE.x + (rand() - 0.5) * 6 : FX0 + 6 + rand() * (FX1 - FX0 - 12);
    if (aim === "hole" && i % 4 === 0) { while (!(w.vsf < 0 && w.sf < 104)) step(DT); }
    drop(w, x); dropped++;
    step(0.5 + rand() * 0.6);
  }
  step(8);
  if (day < 10) firstDays.push(wonToday);
  day++;
}

const on = w.bodies.filter((b) => !b.kind).length + w.air.length;
console.log(`${dropped} drops over ${day} days, aiming ${aim}`);
console.log(`off the front ${front}, off the sides ${sides}, down the hole ${holes}`);
console.log(`showers ${showers}, ${showers ? (shower / showers).toFixed(1) : 0} pebbles a shower, biggest ${biggest}`);
console.log(`on the table ${on}, in the spill jar ${w.spill}, counted ${on + w.spill + front} of ${90 + 10 + dropped}`);
console.log(`won each of the first ten days ${firstDays.join(" ")}`);
took.sort((a, b) => a - b);
console.log(`${took.length} prizes off, days each median ${took[took.length >> 1]}, longest ${took[took.length - 1]}, over a week ${took.filter((d) => d > 7).length}, still on ${prizeAt.size}`);

// A pile left alone for half a minute, then watched for another half.
step(30);
const at = w.bodies.map((b) => [b, b.x, b.y]);
for (let t = 0; t < 30; t += DT) { run(w, DT); w.events.length = 0; settle(w); }
let most = 0;
for (const [b, x, y] of at) if (!b.top) most = Math.max(most, Math.hypot(b.x - x, b.y - y));
console.log(`left alone, the most a field pebble moved in 30s was ${most.toFixed(2)}px`);

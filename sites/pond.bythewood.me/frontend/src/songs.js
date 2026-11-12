// Chorus's songs and how one is laid out in time. Nothing here touches the
// page, so a script can lay every song out with bun and check its timing.

// A bar is eight eighth notes. The frogs on the log are bass, beat and chop,
// left to right, and the player's frog on the right sings the hook.
export const STEPS = 8, BASS = 0, BEAT = 1, CHOP = 2, HOOK = 3;
export const LEAD = 1;
// Every song plays this much quicker than its bpm, which reads as the tempo it was written at.
export const TEMPO = 1.15;

// A part is a bar of steps or a list of bars taken in turn. walk is the bass
// as chord degrees, melody is the hook as chord tones up or down from C5, and
// hook is the player's three parts, each one change from the last.
export const SONGS = [
  {
    name: "Lily Pad Stomp", bpm: 84, swing: 0.5, chords: ["C", "F", "G", "C"],
    bass: "x...x...", walk: ["1", "5"], beat: "..x...x.", chop: ".x.x.x.x",
    hook: ["x...x...", "x...x.x.", "x..xx.x."], melody: [0, 1, 2, 1],
  },
  {
    name: "Tresillo Pond", bpm: 90, swing: 0.5, chords: ["C", "Am", "F", "G"],
    bass: "x..x..x.", walk: ["1", "1", "5"], beat: "..x...x.", chop: ".x...x..",
    hook: ["x...x...", "x...x..x", "x.x.x..x"], melody: [1, 0, -1, 0],
  },
  {
    name: "Swamp Shuffle", bpm: 80, swing: 0.64, chords: ["Am", "Dm", "Am", "E"],
    bass: "x...x..x", walk: ["1", "5", "8"], beat: "..x...x.", chop: ".x.x.x.x",
    hook: ["..x...x.", "..x..xx.", "x.x..xx."], melody: [0, 1, 0, 1],
  },
  {
    name: "Son of a Frog", bpm: 96, swing: 0.5, chords: ["Am", "F"],
    bass: "...x..x.", walk: ["5", "1"], beat: ["x..x..x.", "..x.x..."], chop: ".x.x.x.x",
    hook: ["x.....x.", "x..x..x.", "x..x.xx."], melody: [2, 1, 0, -1],
  },
  {
    name: "Rain Dance", bpm: 100, swing: 0.5, chords: ["Am", "F", "C", "G"],
    bass: "x.x.x.x.", walk: ["1", "8"], beat: "..x...x.", chop: "x..x..x.",
    hook: [".x...x..", ".x.x.x..", ".x.x.x.x"], melody: [0, 1, 2, 1],
  },
  {
    name: "Lazy Log", bpm: 82, swing: 0.58, chords: ["C", "F"],
    bass: "x....xx.", walk: ["1", "5", "1"], beat: "....x...", chop: ".x.x.x.x",
    hook: ["x.....x.", "x.x...x.", "x.x...xx"], melody: [0, -1, 0, 1],
  },
  {
    name: "Call and Croak", bpm: 98, swing: 0.5, chords: ["C", "G", "Am", "F"],
    bass: "x...x...", walk: ["1", "5"], beat: "..x...x.", chop: "x.xx....",
    hook: ["....x.x.", "....x.xx", "....xxxx"], melody: [2, 1, 0, -1],
  },
  {
    name: "Habanera Heron", bpm: 84, swing: 0.5, chords: ["Dm", "Am", "E", "Am"],
    bass: "x..xx.x.", walk: ["1", "5", "1", "5"], beat: "....x...", chop: ".x...x..",
    hook: ["x.x.....", "x.x.x...", "x.x.x.x."], melody: [0, 1, 2, 1],
  },
  {
    name: "Bullfrog Boogie", bpm: 104, swing: 0.66, chords: ["C", "F", "C", "G"],
    bass: "x.x.x.x.", walk: ["1", "3", "5", "6"], beat: "..x...x.", chop: ".x.x.x.x",
    hook: ["x...x...", "x...xx..", "x.x.xx.."], melody: [0, 1, 2, 1],
  },
  {
    name: "Moon Chorus", bpm: 82, swing: 0.62, chords: ["C", "Am", "Dm", "G"],
    bass: "x...x...", walk: ["1", "5"], beat: "..x...x.", chop: ".x.x.x.x",
    hook: ["x..x....", "x..x.x..", "x..x.x.x"], melody: [2, 1, 0, 1],
  },
];

const CHORDS = { C: [0, 0], F: [5, 0], G: [7, 0], Am: [9, 1], Dm: [2, 1], Em: [4, 1], E: [4, 0] };
const DEGREE = { 1: [0, 0], 3: [4, 3], 5: [7, 7], 6: [9, 9], 8: [12, 12] };

function hz(m) { return 440 * Math.pow(2, (m - 69) / 12); }

// A chord's notes in MIDI, the bass's root, the beat and chop's close triad
// round middle C, and every chord tone from C4 up for the hook.
export function voicing(name) {
  const [root, minor] = CHORDS[name], pcs = [0, minor ? 3 : 4, 7].map((d) => (root + d) % 12);
  const low = 45 + ((root - 45) % 12 + 12) % 12, mid = [], high = [];
  for (let m = 55; m < 67; m++) if (pcs.includes(m % 12)) mid.push(m);
  for (let m = 60; m < 92; m++) if (pcs.includes(m % 12)) high.push(m);
  return { root: low, minor, mid, high, anchor: high.findIndex((m) => m >= 72) };
}

function bassNote(v, deg) { return hz(v.root + DEGREE[deg][v.minor]); }
export function hookNote(v, i) { return hz(v.high[Math.max(0, Math.min(v.high.length - 1, v.anchor + i))]); }

export function stepsOf(row) { return [...row].flatMap((c, s) => (c === "x" ? [s] : [])); }
function partAt(p, bar) { return Array.isArray(p) ? p[bar % p.length] : p; }

// A round is heard once and sung twice, or only sung again with a count in
// when the part hasn't changed.
function rounds(s) {
  const [a, b, c] = s.hook;
  return [
    { part: [a], listen: true }, { part: [a] },
    { part: [b], listen: true }, { part: [b] },
    { part: [b, c], listen: true }, { part: [b, c] },
  ];
}

// How many steps each note of a row is held. The first part is all taps, the
// second holds its first note with room after it and the third every one,
// each to a step short of the next note.
export function holds(row, part) {
  const at = stepsOf(row);
  return at.map((st, j) => {
    const gap = (j + 1 < at.length ? at[j + 1] : STEPS) - st;
    if (part === 0 || gap < 3) return 0;
    if (part === 1 && at.slice(0, j).some((q, i) => (i + 1 < at.length ? at[i + 1] : STEPS) - q >= 3)) return 0;
    return gap - 1;
  });
}
export const ROUNDS = 6;

// The whole song laid out in song seconds. Swing pushes every off beat late,
// and a note is sung and judged where it's swung to.
export function lay(s) {
  const step = 30 / (s.bpm * TEMPO), late = (s.swing - 0.5) * 2 * step;
  const bars = [], notes = [], targets = [];
  let t = LEAD;
  const at = (bt, st) => bt + st * step + (st % 2 ? late : 0);
  const add = (kind, r, extra) => {
    const i = bars.length, v = voicing(s.chords[i % s.chords.length]);
    const b = Object.assign({ t, i, kind, r, v, chord: s.chords[i % s.chords.length], step }, extra);
    bars.push(b);
    t += STEPS * step;
    return b;
  };
  const band = (b, count) => {
    const bass = stepsOf(partAt(s.bass, b.i));
    bass.forEach((st, k) => notes.push({ t: at(b.t, st), f: BASS, p: bassNote(b.v, s.walk[k % s.walk.length]), v: 0.17 }));
    const beat = count ? [0, 2, 4, 6] : stepsOf(partAt(s.beat, b.i)), chop = count ? [0, 2, 4, 6] : stepsOf(partAt(s.chop, b.i));
    beat.forEach((st) => notes.push({ t: at(b.t, st), f: BEAT, p: hz(b.v.mid[0]), v: 0.11 }));
    chop.forEach((st) => [1, 2].forEach((k) => notes.push({ t: at(b.t, st), f: CHOP, p: hz(b.v.mid[k]), v: 0.06 })));
  };
  band(add("intro", 0));
  rounds(s).forEach((rd, r) => {
    if (rd.listen) rd.part.forEach((row, k) => {
      const b = add("listen", r, { row, k, n: rd.part.length });
      band(b);
      const hl = holds(row, s.hook.indexOf(row));
      stepsOf(row).forEach((st, j) => notes.push({ t: at(b.t, st), f: HOOK, p: hookNote(b.v, s.melody[j % s.melody.length]), v: 0.17, shown: true, len: hl[j] * step }));
    });
    const count = add("count", r, { n: rd.part.length });
    band(count, true);
    let first = null;
    [0, 1].forEach((pass) => rd.part.forEach((row, k) => {
      const b = add("play", r, { row, k, n: rd.part.length, pass });
      band(b);
      const hl = holds(row, s.hook.indexOf(row));
      stepsOf(row).forEach((st, j) => {
        const tg = { t: at(b.t, st), p: hookNote(b.v, s.melody[j % s.melody.length]), bar: b.i, s: st, got: 0, len: hl[j] * step };
        targets.push(tg);
        if (!first) first = { b, st };
      });
    }));
    // The cue is a croak at the hook's pitch a beat before the player's first note.
    const cs = first.st - 2, cb = cs < 0 ? count : first.b;
    notes.push({ t: at(cb.t, (cs + STEPS) % STEPS), f: CHOP, p: targets.find((q) => q.bar === first.b.i).p, v: 0.13, cue: true });
  });
  const out = add("outro", ROUNDS - 1);
  [[BASS, hz(out.v.root), 0.17], [BEAT, hz(out.v.mid[0]), 0.11], [CHOP, hz(out.v.mid[1]), 0.08], [CHOP, hz(out.v.mid[2]), 0.08], [HOOK, hookNote(out.v, 0), 0.15]]
    .forEach(([f, p, v]) => notes.push({ t: out.t, f, p, v }));
  notes.sort((a, b) => a.t - b.t);
  return { bars, notes, targets, step, end: out.t + 4 * step + 0.4 };
}

// Points for a note sung right or near it, and a run of them adds up to two
// more a note. A miss halves the run, it never takes it to nothing.
export const PERFECT = 0.07, GOOD = 0.16, LATE = 0.03;
export function points(right, run) { return (right ? 2 : 1) + Math.min(2, Math.floor(run / 8)); }

// A held note kept down to its end adds this.
export const HOLD = 2;

// Every note of a song right on time and held, run and all.
export function best(s) {
  const tg = lay(s).targets;
  let sum = tg.filter((t) => t.len).length * HOLD;
  for (let run = 1; run <= tg.length; run++) sum += points(true, run);
  return sum;
}

// How a sung song went, off how near its notes were.
export function grade(right, near, of) {
  const k = of ? (2 * right + near) / (2 * of) : 0;
  return k >= 0.85 ? "A STANDING OVATION" : k >= 0.65 ? "A FINE CHORUS" : k >= 0.4 ? "A GOOD SING" : "SUNG RIGHT THROUGH";
}
export function ovation(right, near, of) { return of > 0 && (2 * right + near) / (2 * of) >= 0.85; }

// Writes testdata/genomes.json from frog.js, which genome_test.go then checks
// the Go port against. Run `bun fixture.js` here after changing genome() and
// commit what it writes.
import { writeFileSync, mkdirSync } from "fs";
import { genome, rng, cells, colours } from "./src/frog.js";

const seeds = [0, 1, 2, 7, 1207, 4242, 99999, 123456789, 2147483648, 4294967295];
const r = rng(20261109);
while (seeds.length < 400) seeds.push(Math.floor(r() * 4294967296));

const rows = seeds.map((seed) => {
  const g = genome(seed);
  return {
    seed, name: g.name, morph: g.morph.name, pattern: g.pattern, pupil: g.pupil,
    nature: g.temper, pitch: g.pitch, rate: g.rate, notes: g.notes,
  };
});
mkdirSync("../testdata", { recursive: true });
writeFileSync("../testdata/genomes.json", JSON.stringify(rows, null, 1) + "\n");
console.log(rows.length + " genomes written");

// The same frogs drawn still at every stage, in a hat, for sprite_test.go.
const STAGES = ["egg", "tadpole", "froglet", "frog", "elder"];
const sprites = [];
seeds.slice(0, 69).forEach((seed, i) => {
  const g = genome(seed), hat = i >= 60 ? { shape: 201 + i - 60, colour: 0 } : i >= 53 ? { shape: 101 + i - 53, colour: 0 } : { shape: i % 16, colour: i % 4 };
  STAGES.forEach((stage, k) => {
    const day = stage === "tadpole" ? 3 + (i % 6) : 1;
    const c = cells(g, stage, { hat: hat.shape, legs: day >= 6 });
    sprites.push({ seed, stage, day, hat, cells: Array.from(c, (v) => v.toString(16)).join(""), colours: k ? undefined : colours(g, hat) });
  });
});
writeFileSync("../testdata/sprites.json", JSON.stringify(sprites) + "\n");
console.log(sprites.length + " sprites written");

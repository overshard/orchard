// Turn frontend/images/ (sources, committed) into frontend/public/images/
// (generated, gitignored), which Vite then copies into dist/.
//
// Go has no next/image, so every width a browser might pick has to exist as a
// real file. This makes them at build time. Nothing here is dynamic: the pages,
// the images, and the sizes they render at are all known, so there is nothing
// an on-demand resizer could work out that this cannot work out first.
//
// Why sharp and not a Go tool: Go can encode AVIF without CGO now
// (gen2brain/avif embeds libavif as WebAssembly), and it was tried. It works,
// and it is about 10% less accurate than libvips at the same file size. The
// deciding factor was not quality but adoption: that package has under a
// hundred stars and one maintainer, against sharp's tens of thousands and
// ~89M downloads a week, and it would sit in the build path for artifacts that
// have to stay reproducible. Go projects generally do not do in-process image
// work at all; they reach for imgproxy or a CDN.
//
// The ladder itself lives in ../../images.json so this and images.go read the
// same numbers.

import { mkdir, readdir, rm, copyFile, stat, readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";

const HERE = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const SRC = join(HERE, "images");
const DST = join(HERE, "public/images");

const spec = JSON.parse(await readFile(resolve(HERE, "..", "images.json"), "utf8"));

// libvips effort, 0 to 9. Higher is slower and slightly smaller; 4 keeps a
// full regeneration to a couple of minutes and the returns above it are thin.
const EFFORT = 4;

const widthsFor = (name) => {
  const w = [...spec.cardWidths, spec.lightboxWidth];
  if (name === spec.hero) w.push(spec.heroWidth);
  return w;
};

const qualityFor = (width) => {
  const q = spec.quality[String(width)];
  if (q === undefined) throw new Error(`images.json has no quality for width ${width}`);
  return q;
};

// One encoder entry point, so adding a format is a case here plus a change in
// images.json rather than edits scattered through the file.
const encode = (pipeline, quality) => {
  switch (spec.format) {
    case "avif":
      return pipeline.avif({ quality, effort: EFFORT });
    case "webp":
      return pipeline.webp({ quality, effort: 6 });
    case "jpeg":
      return pipeline.jpeg({ quality, mozjpeg: true, chromaSubsampling: "4:4:4" });
    default:
      throw new Error(`unsupported format in images.json: ${spec.format}`);
  }
};

const kb = (n) => `${(n / 1000).toFixed(0)}kB`.padStart(7);

const poursSrc = join(SRC, "art/acrylic-pours");
const poursDst = join(DST, "art/acrylic-pours");

// Clear stale output first, so a source that was removed cannot leave an orphan
// behind that a template still happily links to.
await rm(poursDst, { recursive: true, force: true });
await mkdir(poursDst, { recursive: true });

const originals = (await readdir(poursSrc)).filter((f) => f.endsWith(".webp")).sort();
if (originals.length === 0) throw new Error(`no sources in ${poursSrc}`);

const started = Date.now();
let total = 0;
let count = 0;

for (const file of originals) {
  const name = file.replace(/\.webp$/, "");
  let line = `  ${file.padEnd(10)}`;
  for (const width of widthsFor(name)) {
    const dest = join(poursDst, `${name}-${width}.${spec.format}`);
    // withoutEnlargement so a source smaller than the target passes through
    // rather than being upscaled into a bigger, blurrier file.
    const { size } = await encode(
      sharp(join(poursSrc, file)).resize({ width, withoutEnlargement: true }),
      qualityFor(width),
    ).toFile(dest);
    total += size;
    count += 1;
    line += ` ${width}w ${kb(size)}`;
  }
  console.log(line);
}

const { size: avatarSize } = await encode(
  sharp(join(SRC, "avatar.webp")).resize({ width: spec.avatar.width, withoutEnlargement: true }),
  spec.avatar.quality,
).toFile(join(DST, `avatar.${spec.format}`));
total += avatarSize;
count += 1;
console.log(`  ${"avatar".padEnd(10)} ${spec.avatar.width}w ${kb(avatarSize)}`);

// Referenced at 512x512 by the web manifest, so it passes through as PNG.
await copyFile(join(SRC, "favicon.png"), join(DST, "favicon.png"));
total += (await stat(join(DST, "favicon.png"))).size;

console.log(
  `\n${count} variants in ${spec.format}, ${(total / 1e6).toFixed(1)}MB, ` +
    `in ${((Date.now() - started) / 1000).toFixed(1)}s`,
);

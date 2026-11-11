import { genome, bred, cells, colours, draw, W, H } from "./frog.js";
import { drawIcon } from "./icons.js";

// Another frog, as the server tells it: a look and a name and nothing else.
export function callerArt(c, s, thing) {
  const cv = document.createElement("canvas"), ctx = cv.getContext("2d"), g = bred(genome(c.seed), c.mods);
  cv.width = W * s; cv.height = H * s;
  draw(ctx, cells(g, c.stage, { hat: c.hat.shape, legs: c.day >= 6 }), W, H, colours(g, c.hat), 0, 0, s);
  if (thing) drawIcon(ctx, thing, W * s - 8 * s, H * s - 8 * s, s);
  return cv;
}

const PRINT = ["x.x.x", ".xxx.", "..x.."];

// The visitor's muddy footprint, on the front edge of the pad.
export function footprint(ctx, x, y) {
  PRINT.forEach((row, r) => [...row].forEach((ch, c) => {
    if (ch === ".") return;
    ctx.fillStyle = r === 0 ? "#8a6230" : "#5a3d1e";
    ctx.fillRect(x + c * 4, y + r * 4, 4, 4);
  }));
}

const FROM = { pond: "From a pond down the way.", resident: "Over from town.", wild: "Just passing through." };

// What the greeting pops up for the visitor.
export function visitorReveal(v) {
  return { kick: "A visitor in the night", art: callerArt(v, 3, v.gift.item), name: v.name, line: FROM[v.from] + " It left " + v.gift.text + " and a muddy footprint.", ok: "Wave" };
}

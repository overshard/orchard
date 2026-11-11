import { reduced } from "./api.js";
import { sfx } from "./sound.js";

// How a game feels under the thumb, one of these a game. A game calls got on
// a catch and crash on a bonk, and the frame round it runs tick, shakes the
// field by offset and stops the game's step while a freeze lasts.
const SHAKE = 5, DECAY = 1.7, BUFFER = 90, FONT = "700 13px 'Space Grotesk', sans-serif";

export function feel() {
  let trauma = 0, frozen = 0, sq = 0, sqT = 0, t = 0, closeAt = -9, held = null, bits = [], pops = [];

  const F = {
    reset() { trauma = 0; frozen = 0; sq = 0; t = 0; closeAt = -9; held = null; bits = []; pops = []; },

    // tick is once a frame, before the game's step, and false means skip it.
    tick(dt) {
      if (frozen > 0) { frozen -= dt; return false; }
      t += dt; sqT += dt;
      trauma = Math.max(0, trauma - DECAY * dt);
      bits.forEach((b) => { b.vy += 320 * dt; b.x += b.vx * dt; b.y += b.vy * dt; b.t += dt; });
      bits = bits.filter((b) => b.t < b.life);
      pops.forEach((p) => { p.t += dt; p.y -= 20 * dt; });
      pops = pops.filter((p) => p.t < 0.9);
      return true;
    },

    // Trauma runs 0 to 1 and the shake is its square, so a small knock barely shows.
    hit(k) { trauma = Math.min(1, trauma + k); },
    // Frames at sixty a second, whatever the screen runs at.
    freeze(frames) { frozen = Math.max(frozen, frames / 60); },
    frozen() { return frozen > 0; },

    offset() {
      if (reduced || !trauma) return [0, 0];
      const m = trauma * trauma * SHAKE;
      return [Math.round((Math.random() * 2 - 1) * m * 2) / 2, Math.round((Math.random() * 2 - 1) * m * 2) / 2];
    },

    burst(x, y, c, n) {
      for (let i = 0; i < (n || 6); i++) {
        const a = Math.random() * Math.PI * 2, v = 30 + Math.random() * 70;
        bits.push({ x, y, vx: Math.cos(a) * v, vy: Math.sin(a) * v - 40, c: c || "#ffe27a", t: 0, life: 0.3 + Math.random() * 0.3, s: Math.random() < 0.4 ? 3 : 2 });
      }
    },

    pop(x, y, s, c) { pops.push({ x, y, s, c: c || "#ffb000", t: 0 }); },

    squash(k) { if (!reduced) { sq = k || 1; sqT = 0; } },

    // squashed draws fn pressed down onto by, the line under the frog's feet,
    // so a sprite never has to change to be squashed.
    squashed(x, cx, by, fn) {
      const a = sq * Math.exp(-11 * sqT) * Math.cos(20 * sqT);
      if (Math.abs(a) < 0.02) { fn(); return; }
      x.save();
      x.translate(cx, by); x.scale(1 + 0.24 * a, 1 - 0.24 * a); x.translate(-cx, -by);
      fn();
      x.restore();
    },

    // draw is the bits and the popups, moved by whatever camera the game has.
    draw(x, ox, oy) {
      ox = ox || 0; oy = oy || 0;
      bits.forEach((b) => { x.fillStyle = b.c; x.fillRect(Math.round(b.x + ox), Math.round(b.y + oy), b.s, b.s); });
      x.font = FONT; x.textAlign = "center"; x.lineWidth = 3; x.strokeStyle = "#0a0908";
      pops.forEach((p) => {
        const k = 1 + 0.3 * Math.max(0, 1 - p.t / 0.1), px = Math.round(p.x + ox), py = Math.round(p.y + oy);
        x.globalAlpha = Math.min(1, (0.9 - p.t) / 0.25);
        x.save(); x.translate(px, py); x.scale(k, k);
        x.strokeText(p.s, 0, 0); x.fillStyle = p.c; x.fillText(p.s, 0, 0);
        x.restore();
      });
      x.globalAlpha = 1;
    },

    // got is a catch, where o.streak steps the blip up, o.big is a prize worth
    // a knock, o.sound is a blip other than catch and o.c colours the popup.
    got(x, y, s, o) {
      o = o || {};
      sfx(o.sound || "catch", o.streak);
      F.freeze(o.big ? 3 : 2);
      if (o.big) F.hit(0.3);
      F.burst(x, y, o.big ? "#ffb000" : "#ffe27a", o.big ? 10 : 5);
      if (s) F.pop(x, y - 10, s, o.c);
      F.squash(o.big ? 1 : 0.7);
    },

    crash(x, y, o) {
      o = o || {};
      sfx(o.sound || "bonk");
      F.freeze(6); F.hit(0.75);
      F.burst(x, y, "#e2567a", 10);
      if (o.s) F.pop(x, y - 10, o.s, "#e2412d");
    },

    // close is a near miss, said at most twice a second.
    close(x, y) {
      if (t - closeAt < 0.5) return;
      closeAt = t;
      sfx("close");
      F.pop(x, y, "CLOSE", "#7fd6c2");
    },

    // A press that came a touch early is kept this long and taken when it can be used.
    buffer(v) { held = { v, at: performance.now() }; },
    take() {
      const h = held;
      held = null;
      return h && performance.now() - h.at <= BUFFER ? h.v : null;
    },
  };
  return F;
}

// A browser that hasn't opened a game yet gets a ghost finger in place of words.
export function unseen(key) {
  try { return localStorage.getItem("pond-seen-" + key) !== "1"; } catch (e) { return false; }
}

export function sawIt(key) {
  try { localStorage.setItem("pond-seen-" + key, "1"); } catch (e) { /* private window */ }
}

const HAND = [
  "..11....",
  ".1221...",
  ".1221...",
  ".1221...",
  ".122111.",
  ".1221221",
  "11222221",
  "12222221",
  "12222221",
  ".1222221",
  "..122221",
  "..111111",
];

// finger draws the ghost doing d, which is { x, y } for a tap, with hold for
// a press that stays down and to: [x, y] for a drag. t is seconds.
export function finger(x, d, t) {
  const k = reduced ? 0.6 : t % 1.8, down = k > 0.3 && k < (d.hold || d.to ? 1.4 : 0.6);
  let fx = d.x, fy = d.y;
  if (d.to && k > 0.5) { const m = Math.min(1, (k - 0.5) / 0.6), e = m * m * (3 - 2 * m); fx += (d.to[0] - d.x) * e; fy += (d.to[1] - d.y) * e; }
  if (!down) fy -= 5;
  x.globalAlpha = Math.max(0, Math.min(1, k / 0.2, (1.7 - k) / 0.2)) * 0.9;
  if (down) {
    const r = 5 + Math.round(Math.min(1, (k - 0.3) / 0.3) * 4);
    x.fillStyle = "#fff6dc";
    for (let a = 0; a < 12; a++) x.fillRect(Math.round(fx + Math.cos(a * Math.PI / 6) * r), Math.round(fy + Math.sin(a * Math.PI / 6) * r), 1, 1);
  }
  HAND.forEach((row, ry) => [...row].forEach((ch, rx) => {
    if (ch === ".") return;
    x.fillStyle = ch === "1" ? "#0a0908" : "#fff6dc";
    x.fillRect(Math.round(fx) - 5 + rx * 2, Math.round(fy) + ry * 2, 2, 2);
  }));
  x.globalAlpha = 1;
}

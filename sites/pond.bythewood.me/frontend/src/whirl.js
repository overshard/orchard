import { cells, colours, draw, W as FW, H as FH } from "./frog.js";
import { feel } from "./feel.js";
import { reduced } from "./api.js";
import { sfx, slurp } from "./sound.js";
import { W, H, ROUND, LAST, PW, PH, BANDS, KIND, REACH, world, step, steer, view, score, counts } from "./whirlpond.js";

// Whirlpool. A little eddy goes round the pond swallowing anything smaller
// than itself and grows, and the camera backs off as it does.
const FONT = "700 13px 'Space Grotesk', sans-serif", BIG = "700 16px 'Space Grotesk', sans-serif";
const SMALL = "500 8px 'JetBrains Mono', monospace", KEYS = 150;

let G = null, w = null;
const F = feel();

// Every thing is drawn from above on a grid, sized to its s at any zoom, and a
// creature faces the way it's going.
const ART = {
  duckweed: [".gg.", "glgg", "gggl", ".gg."],
  petal: ["..pp", ".pPp", "pPp.", "pp.."],
  bubble: [".cc.", "cw.c", "c..c", ".cc."],
  fluff: ["w.w.w", ".www.", "wwBww", ".www.", "w.w.w"],
  flea: [".tt.", "tkkt", ".tt."],
  midge: ["c.c", ".k.", "c.c"],
  leaf: ["..oo..", ".oYoo.", "ooYYoo", ".ooYo.", "..oo..", "...b.."],
  willow: ["......ll", "....llgl", "..llgl..", "llgl....", "l......."],
  feather: ["......ww", "....wwc.", "..wwcw..", ".wcw....", "s......."],
  acorn: [".dd.", "dbbd", "BttB", ".tt."],
  skater: ["s...s", ".sks.", "..k..", ".sks.", "s...s"],
  tadpole: ["....kk", "kkkkkk", "....kk"],
  clover: [".l.l.", "lglgl", ".lYl.", "lglgl", ".l.l."],
  twig: ["b...........", ".BB......B..", "...BBBBBBB..", ".........BBb", "...........B"],
  cone: [".BBB.", "BdBdB", ".BBB.", "BdBdB", ".BBB.", "..d.."],
  cap: [".sss.", "srrrs", "srwrs", "srrrs", ".sss."],
  snail: [".BBB.", "BbBdB", "BdbBB", ".BBBt", "...tt"],
  dragonfly: [".cc.cc..", "ffffffUU", ".cc.cc.."],
  beetle: ["..kk..", ".kUUk.", "kUkkUk", "kUkkUk", ".kkkk.", "k.kk.k"],
  minnow: ["s...ss..", ".sssssUk", "s...ss.."],
  marble: [".uuu.", "uwumu", "uumuu", "umuuu", ".uuu."],
  pad: ["..GGnGG..", ".GggnggG.", "GgggngggG", "GlggngglG", "GgllgllgG", "GgggggggG", "GggglgggG", ".GgggggG.", "..GGGGG.."],
  lily: ["..GGGGG..", ".GgpwpgG.", "GgpwPwpgG", "GpwPyPwpG", "GwPyYyPwG", "GpwPyPwpG", "GgpwPwpgG", ".GgpwpgG.", "..GGGGG.."],
  boat: ["....c....", "...wcw...", "..wwcww..", "wwwwwwwww", ".wcwcwcw.", "..wwwww.."],
  ball: ["..rrr..", ".rwrrr.", "rwrrrrr", "yyyyyyy", "rrrrrrr", ".rrrrr.", "..rrr.."],
  newt: ["....b...b..", "..bbbbbbbb.", "bbbobbobbbk", "..bbbbbbbb.", "....b...b.."],
  goldfish: ["o....oo..", "oo.ooooo.", ".ooooooYk", "oo.ooooo.", "o....oo.."],
  rubberduck: ["..yyy..", ".yyyyko", ".yyyyy.", "yyyyyy.", ".yyyyy.", "..yyy.."],
  bottle: ["b..uuuuuuu.", "bSuuwwwwuuu", "bSuuwwwwuuu", "b..uuuuuuu."],
  reeds: ["G...g...G", ".g.GgG.g.", "..gGbGg..", "gGgbBbgGg", ".GbBgBbG.", "gGgbBbgGg", "..gGbGg..", ".g.GgG.g.", "G...g...G"],
  bucket: ["..SSSS..", ".SUUUUS.", "SUuuuuUS", "SUkkkkUS", "SUkkkkUS", "SUuuuuUS", ".SUUUUS.", "..SSSS.."],
  welly: ["GGGG......", "GggG......", "GggG......", "GggGGGGGG.", "GgggggggGG", "dddddddddd"],
  sailboat: ["....w....", "....ww...", "...wwwr..", "..wwwwrr.", ".wwwwwrrr", "....b....", "bbbbbbbbb", ".BBBBBBB.", "..BBBBB.."],
  beachball: ["..rrww..", ".rrrwwy.", "rrrrwyyy", "uuuuwyyy", "uuuuwggg", "uuuwgggg", ".uuwggg.", "..uwgg.."],
  carp: ["tt.....BB....", ".tBBBBBBBBB..", "..BtBBtBBBBBk", ".tBBBBBBBBB..", "tt.....BB...."],
  turtle: ["..t....t...", "...GGGGG...", "..GgGgGgG..", ".GgGGgGGgtt", ".GGgGgGgGtk", ".GgGGgGGgtt", "..GgGgGgG..", "...GGGGG...", "..t....t..."],
  koi: ["Y....ww..", "ww.wYwww.", ".wYwwwYwk", "ww.wwYww.", "Y....ww.."],
  duck: ["....ttttt....", "..ttBBBBBt...", "wtBBbbbBBBGG.", "wtBbbbbbBBGgY", "wtBBbbbBBBGG.", "..ttBBBBBt...", "....ttttt...."],
  goose: ["....sssss....", "..ssSSSSSs...", "wsSSssSSSSkk.", "wsSsssssSSkwY", "wsSSssSSSSkk.", "..ssSSSSSs...", "....sssss...."],
  swan: ["....ccccc.....", "..ccwwwwwc....", "cwwwccwwwwc...", "cwwcccwwwwwwko", "cwwwccwwwwc...", "..ccwwwwwc....", "....ccccc....."],
  buoy: ["..rrww..", ".rrrwww.", "rr....ww", "rr....ww", "ww....rr", "ww....rr", ".wwwrrr.", "..wwrr.."],
  crate: ["BBBBBBBB", "BdBBBBdB", "BBdBBdBB", "BBBddBBB", "BBBddBBB", "BBdBBdBB", "BdBBBBdB", "BBBBBBBB"],
  driftwood: ["......tt.....", "ttttttttttt..", "..tcttttcttts", ".t....tt.....", "........t...."],
  kite: ["...r...", "..rYr..", ".rYrYr.", "rYrYrYr", ".rYrYr.", "..rYr..", "...r...", "....k..", "...k...", "....k.."],
  heron: ["...SS.........", "..SsssS.......", ".SssssssS.....", "SsssssssscckYY", ".SssssssS.....", "..SsssS.......", "...SS........."],
  otter: ["bb............", ".bbbbbbbbbbb..", "..bBBBBBBBBbbk", ".bbbbbbbbbbb..", "bb..b...b....."],
  canoe: ["...oooooooooo...", ".ooddddddddddoo.", "oodtdtdtdtdtddoo", ".ooddddddddddoo.", "...oooooooooo..."],
  padisle: ["..GG....GG..", ".GggG..GggG.", ".GggG..GgpG.", "..GG.GG.GG..", "....GggG....", ".GG.GgyG.GG.", "GggG.GG.GggG", "GggG....GggG", ".GG..GG..GG.", "....GggG....", "....GggG....", ".....GG....."],
  pedalo: ["..wwwwwwww..", ".wrrrrrrrrw.", "wrrSSrrSSrrw", "wrrSSrrSSrrw", ".wrrrrrrrrw.", "..wwwwwwww.."],
  branch: ["b.........b...", ".bB......bB...", "..bBBBBBBBBBBB", ".......BB...lg", "......B....gl.", "....lg........"],
  houseboat: ["dddddddddddddddd", "dBBBBBBBBBBBBBBd", "dBrrrrrrrrrrrrBd", "dBrkrrkrrkrrkrBd", "dBrrrrrrrrrrrrBd", "dBBBBBBBBBBBBBBd", "dddddddddddddddd"],
  tyre: ["...kkkkk...", ".kkSSSSSkk.", "kkS.....Skk", "kS.......Sk", "kS.......Sk", "kS.......Sk", "kkS.....Skk", ".kkSSSSSkk.", "...kkkkk..."],
  log: ["dbbbbbbbbbbbbbbd", "tBbBbBbGGbBbBbBt", "tbBbBbBGbBbBbBbt", "tBbBbBbBbBbGbBbt", "dbbbbbbbbbbbbbbd"],
  chest: ["dbbbbbbd", "bBYBBYBb", "dddddddd", "bBBYYBBb", "bBBYYBBb", "dbbbbbbd"],
  rowboat: ["....bbbbbbb....", "..bbBBBBBBBbb..", "sbBBdBBdBBdBBbs", "sbBBdBBdBBdBBbs", "..bbBBBBBBBbb..", "....bbbbbbb...."],
  punt: ["dddddddddddddddddd", "dBBBBBBBBBBBBBBBBd", "dBtBBBBBBBBBBBBtBd", "dBBBBBBBBBBBBBBBBd", "dddddddddddddddddd"],
  jetty: ["dtttttd", "dBBBBBd", "dtttttd", "dBBBBBd", "dtttttd", "dBBBBBd", "dtttttd", "dBBBBBd", "dtttttd", "dBBBBBd", "dtttttd", "dBBBBBd", "dtttttd", "dBBBBBd", "dtttttd", "d..d..d"],
  isle: ["....tttttt....", "..ttggggggtt..", ".tgglllllllgt.", "tgglgglgglgggt", "tglgllllllglgt", "tgllgllbllgllt", "tglllgbbbllglt", "tgllgllbllgllt", "tglgllllllglgt", "tgglgglgglgggt", ".tgglllllllgt.", "..ttggggggtt..", "....tttttt...."],
};
const PAL = {
  g: "#4fa043", G: "#2f6a2c", l: "#8fd06a", y: "#ffe27a", Y: "#ffb000", o: "#e07a2a", r: "#e2412d", p: "#f2a0c0", P: "#e2567a",
  w: "#fff6dc", c: "#cfe3ea", k: "#1a1a20", b: "#6b4a2a", B: "#8a6a3a", d: "#3e2a18", s: "#8a8f96", S: "#5b6470", u: "#7fc8e8",
  U: "#2b5fa8", t: "#d9c9a8", m: "#7d52c0", f: "#2a8a7a", n: "#1d4f5c",
};
// The colour most of a thing is, for when it's too small to draw.
const MAIN = Object.fromEntries(Object.entries(ART).map(([k, rows]) => {
  const n = {};
  rows.join("").split("").forEach((ch) => { if (ch !== ".") n[ch] = (n[ch] || 0) + 1; });
  return [k, PAL[Object.keys(n).sort((a, b) => n[b] - n[a])[0]]];
}));

function px(v) { return Math.round(v * 2) / 2; }

// sprite draws a thing about 2s across at a zoom, centred on cx, cy.
function sprite(x, k, cx, cy, s, z, face) {
  const rows = ART[k], h = rows.length, wd = rows[0].length, cell = 2 * s / Math.max(h, wd) * z;
  if (cell * Math.max(h, wd) < 1.5) { x.fillStyle = MAIN[k]; x.fillRect(px(cx), px(cy), 1, 1); return; }
  const c = Math.max(0.5, px(cell)), ox = px(cx - wd * c / 2), oy = px(cy - h * c / 2);
  for (let ry = 0; ry < h; ry++) {
    const row = rows[ry];
    for (let rx = 0; rx < wd; rx++) {
      const ch = row[face < 0 ? wd - 1 - rx : rx];
      if (ch === ".") continue;
      x.fillStyle = PAL[ch]; x.fillRect(ox + rx * c, oy + ry * c, c, c);
    }
  }
}

function sx(v) { return (v - w.cx) * zoom() + W / 2; }
function sy(v) { return (v - w.cy) * zoom() + H / 2; }
// A band crossed gives the camera a little push in before it backs off.
function zoom() { return w.z * (reduced ? 1 : 1 + 0.07 * Math.sin(w.kick * Math.PI)); }

function events() {
  w.events.forEach((e) => {
    if (e.e === "eat") {
      const k = KIND[e.k], x = sx(e.x), y = sy(e.y);
      G.twist = Math.min(3, G.twist + 0.25 + k.s / w.r * 0.5);
      if (G.real - G.slurpAt > 0.035) { G.slurpAt = G.real; slurp(e.s); }
      if (G.tick[0] && G.tick[0].k === e.k) { G.tick[0].n++; G.tick[0].t = 0; }
      else { G.tick.unshift({ k: e.k, t: 0, n: 1, rare: e.rare }); G.tick.length = Math.min(G.tick.length, 3); }
      if (e.rare) {
        F.got(x, y, "RARE", { sound: "band", big: true, c: "#ffb000" });
        G.flash = 0.5;
      } else if (k.fixed) F.got(x, y, "+" + k.pts, { sound: "plop", big: true, c: "#ffe27a" });
      else if (k.s > w.r * 0.5 && G.real - G.popAt > 0.3) { G.popAt = G.real; F.pop(x, y - 6, "+" + k.pts, "#ffe27a"); F.burst(x, y, "#cfe3ea", 4); }
    } else if (e.e === "band") {
      F.pop(W / 2, H / 2 - 40, BANDS[e.b].name, "#ffb000");
      F.burst(sx(w.x), sy(w.y), "#9fdcf2", 12);
      sfx("band");
      G.ring = 0;
    } else if (e.e === "bump") {
      F.crash(sx(e.x), sy(e.y), { s: e.lost.length ? "BUMP" : "", sound: KIND[e.k].bump ? "boing" : "bonk" });
    } else if (e.e === "end") {
      G.spout = Object.keys(w.took).sort((a, b) => KIND[b].s - KIND[a].s).slice(0, 40).map((k, i) => ({ k, t: -i * 0.04, a: -Math.PI / 2 + (Math.random() - 0.5) * 2.2, v: 70 + Math.random() * 70, rot: Math.random() * 6 }));
      sfx("band");
    }
  });
  w.events.length = 0;
}

export const whirl = {
  key: "whirl", W, H, title: "WHIRLPOOL", feel: F, timed: true, edge: ["#2e5a30", "#2e5a30"],
  aria: "Drag anywhere to steer a little eddy round the pond. It swallows anything smaller than itself and grows, so leaves come first and the jetty last. Bump into something bigger and a few things pop back out.",
  help: ["DRAG ANYWHERE TO STEER THE EDDY", "IT SWALLOWS ANYTHING SMALLER", "BIG THINGS KNOCK A FEW BACK OUT", "TAP TO START"],

  reset() {
    w = world();
    G = { real: 0, anchor: null, keys: {}, twist: 0, slurpAt: -9, popAt: -9, tick: [], spout: [], flash: 0, ring: 9, last: false };
  },

  demo() { return { x: 100, y: 230, to: [146, 196] }; },

  // A drag moves the eddy by as much as the finger moved, so the thumb can be
  // anywhere and never sits on it.
  drag(x, y) {
    if (!w || w.st !== "play") return;
    const a = G.anchor;
    G.anchor = { x, y };
    if (a) steer(w, (x - a.x) / w.z, (y - a.y) / w.z);
  },

  release() { if (G) G.anchor = null; },

  keys(code, down) {
    if (!G) return;
    const dir = { ArrowUp: "up", KeyW: "up", ArrowDown: "down", KeyS: "down", ArrowLeft: "left", KeyA: "left", ArrowRight: "right", KeyD: "right" }[code];
    if (dir) G.keys[dir] = down;
  },

  step(dt) {
    G.real += dt;
    const kx = (G.keys.right ? 1 : 0) - (G.keys.left ? 1 : 0), ky = (G.keys.down ? 1 : 0) - (G.keys.up ? 1 : 0);
    if (kx || ky) { const l = Math.hypot(kx, ky); steer(w, kx / l * KEYS * dt / w.z, ky / l * KEYS * dt / w.z); }
    step(w, dt);
    G.twist = Math.max(0, G.twist - dt * 1.5);
    G.flash = Math.max(0, G.flash - dt);
    G.ring += dt;
    G.tick.forEach((t) => { t.t += dt; });
    G.spout.forEach((p) => { p.t += dt; });
    if (!G.last && w.t >= ROUND - LAST && w.st === "play") { G.last = true; F.pop(W / 2, 40, "LAST SWIRL", "#9fdcf2"); sfx("warn"); }
    events();
  },

  pause() { if (G) { G.anchor = null; G.keys = {}; } },
  over() { return w && w.st === "done" && w.done_t > 3; },
  score() { return w ? score(w) : 0; },
  why() { return w ? "BIG ENOUGH FOR " + BANDS[w.band].line : ""; },
  report() { return { counts: counts(w) }; },

  // What went down, the biggest first, a box a kind with how many, and the
  // rare ones edged in amber.
  card(quick) {
    if (!w) return null;
    const ks = Object.keys(w.took).sort((a, b) => (KIND[b].rare - KIND[a].rare) || KIND[b].s - KIND[a].s).slice(0, quick ? 8 : 40);
    if (!ks.length) return null;
    const box = document.createElement("div"), c = document.createElement("canvas"), x = c.getContext("2d");
    box.className = "whirlcard";
    const per = 8, cw = 64, ch = 62, rows = Math.ceil(ks.length / per);
    c.width = cw * Math.min(per, ks.length); c.height = ch * rows;
    c.style.width = (c.width / 2) + "px";
    x.imageSmoothingEnabled = false;
    ks.forEach((k, i) => {
      const kind = KIND[k], l = (i % per) * cw, t = Math.floor(i / per) * ch;
      x.fillStyle = "#0b1f17"; x.fillRect(l + 2, t + 2, cw - 4, ch - 4);
      if (kind.rare) { x.fillStyle = "#ffb000"; x.fillRect(l + 2, t + 2, cw - 4, 2); x.fillRect(l + 2, t + ch - 4, cw - 4, 2); x.fillRect(l + 2, t + 2, 2, ch - 4); x.fillRect(l + cw - 4, t + 2, 2, ch - 4); }
      const rs = ART[k], cell = Math.max(2, Math.floor(36 / Math.max(rs.length, rs[0].length)));
      sprite(x, k, l + cw / 2, t + 24, cell * Math.max(rs.length, rs[0].length) / 2, 1, 1);
      x.font = "700 18px 'JetBrains Mono', monospace"; x.textAlign = "center";
      x.fillStyle = kind.rare ? "#ffb000" : "#bcaf99";
      x.fillText(w.took[k] > 1 ? "x" + w.took[k] : kind.rare ? "RARE" : "", l + cw / 2, t + ch - 9);
    });
    if (!quick) {
      const top = ks.filter((k) => !KIND[k].rare)[0], p = document.createElement("p"), q = document.createElement("p");
      p.className = "dim"; q.className = "dim";
      p.textContent = w.things_n + " things and " + across(w.r).toLowerCase() + " across";
      q.textContent = top ? "The biggest was " + KIND[top].name.toLowerCase() : "";
      box.append(p, q);
    }
    box.append(c);
    return box;
  },

  draw(x) {
    if (!w) { whirl.reset(); }
    pond(x);
    const z = zoom(), v = view(w), hw = v.w / 2 * 1.15 + 140, hh = v.h / 2 * 1.15 + 140;
    eddy(x, z);
    const shown = w.things.filter((o) => Math.abs(o.x - w.cx) < hw && Math.abs(o.y - w.cy) < hh).sort((a, b) => b.kind.s - a.kind.s);
    shown.forEach((o) => thing(x, o, z));
    spins(x, z);
    if (w.st === "done") spout(x, z);
    F.draw(x);
    hud(x);
  },
};

function pond(x) {
  const z = zoom();
  x.fillStyle = "#2e5a30"; x.fillRect(0, 0, W, H);
  const l = sx(0), t = sy(0), r = sx(PW), b = sy(PH), m = Math.max(1, px(5 * z));
  x.fillStyle = "#4a3a26"; x.fillRect(px(l - m), px(t - m), px(r - l + 2 * m), px(b - t + 2 * m));
  x.fillStyle = "#1d4f5c"; x.fillRect(px(l), px(t), px(r - l), px(b - t));
  // Ripples sit on a grid in the pond, so they slide by as the eddy moves.
  const step = Math.pow(2, Math.ceil(Math.log2(30 / z))), v = view(w);
  const x0 = Math.floor((w.cx - v.w / 2) / step) - 1, x1 = Math.ceil((w.cx + v.w / 2) / step) + 1;
  const y0 = Math.floor((w.cy - v.h / 2) / step) - 1, y1 = Math.ceil((w.cy + v.h / 2) / step) + 1;
  for (let gy = y0; gy <= y1; gy++) for (let gx = x0; gx <= x1; gx++) {
    const hsh = Math.abs(Math.sin(gx * 12.9898 + gy * 78.233) * 43758.5453) % 1, wx = (gx + hsh) * step, wy = (gy + (hsh * 7) % 1) * step;
    if (wx < 4 || wy < 4 || wx > PW - 4 || wy > PH - 4) {
      if (wx > -60 && wy > -60 && wx < PW + 60 && wy < PH + 60 && (wx < 0 || wy < 0 || wx > PW || wy > PH)) tuft(x, sx(wx), sy(wy), hsh);
      continue;
    }
    const k = (G.real * 0.25 + hsh) % 1, len = Math.round(3 + k * 6);
    x.fillStyle = "rgba(207,240,246,0.13)";
    x.globalAlpha = Math.sin(k * Math.PI);
    x.fillRect(px(sx(wx)) - len, px(sy(wy)), len * 2, 1);
    x.globalAlpha = 1;
  }
}

function tuft(x, cx, cy, h) {
  x.fillStyle = h > 0.5 ? "#3f8a38" : "#4fa043";
  x.fillRect(px(cx), px(cy) - 4, 1, 5); x.fillRect(px(cx) + 2, px(cy) - 6, 1, 7); x.fillRect(px(cx) - 2, px(cy) - 3, 1, 4);
  if (h > 0.8) { x.fillStyle = "#6b4a2a"; x.fillRect(px(cx) + 1.5, px(cy) - 8, 2, 3); }
}

// The eddy is a dark eye with pale arms going round it, and the arms wind
// tighter for a moment after every gulp.
function eddy(x, z) {
  const ex = sx(w.x), ey = sy(w.y), R = Math.max(3, w.r * z), spin = w.spin;
  const pull = R * (w.t > ROUND - LAST ? 3.8 : 2.8) + 8;
  x.fillStyle = w.t > ROUND - LAST && w.st === "play" ? "rgba(159,220,242,0.35)" : "rgba(207,240,246,0.12)";
  const n = Math.min(90, Math.round(pull * 0.6));
  for (let i = 0; i < n; i++) {
    const a = i / n * Math.PI * 2 + spin * 0.3;
    x.fillRect(px(ex + Math.cos(a) * pull), px(ey + Math.sin(a) * pull), 1, 1);
  }
  disc(x, ex, ey, R * 1.05, "#17434e");
  disc(x, ex, ey, R * 0.75, "#123844");
  disc(x, ex, ey, R * 0.42, "#0a2530");
  const arms = 3, twist = 2.4 + G.twist + (w.st === "done" ? 2 : 0), dot = Math.max(1, px(R / 12));
  for (let i = 0; i < arms; i++) {
    const steps = Math.max(10, Math.round(R * 1.4));
    for (let s = 0; s <= steps; s++) {
      const u = s / steps, rr = R * (0.2 + 1.1 * u), a = spin + i * Math.PI * 2 / arms + twist * (1 - u);
      x.fillStyle = u > 0.75 ? "rgba(207,240,246,0.45)" : u > 0.4 ? "#9fdcf2" : "#cfe3ea";
      x.fillRect(px(ex + Math.cos(a) * rr) - dot / 2, px(ey + Math.sin(a) * rr * 0.92) - dot / 2, dot, dot);
    }
  }
  if (G.ring < 0.6) {
    const rr = R * (1.2 + G.ring * 4);
    x.fillStyle = "rgba(255,226,122," + (1 - G.ring / 0.6).toFixed(2) + ")";
    for (let i = 0; i < 48; i++) { const a = i / 48 * Math.PI * 2; x.fillRect(px(ex + Math.cos(a) * rr), px(ey + Math.sin(a) * rr), 2, 2); }
  }
}

function disc(x, cx, cy, r, c) {
  x.fillStyle = c;
  for (let y = -r; y < r; y += 1) {
    const hw = Math.sqrt(Math.max(0, r * r - (y + 0.5) * (y + 0.5)));
    x.fillRect(Math.round(cx - hw), Math.round(cy + y), Math.round(hw * 2), 1);
  }
}

function thing(x, o, z) {
  const k = o.kind, cx = sx(o.x), cy = sy(o.y);
  const b = o.born < 1 ? o.born : 1, s = k.s * (0.3 + 0.7 * b);
  if (b < 1) {
    x.fillStyle = "rgba(207,240,246," + (0.5 * (1 - b)).toFixed(2) + ")";
    const rr = k.s * z * (1 + b);
    for (let i = 0; i < 16; i++) { const a = i / 16 * Math.PI * 2; x.fillRect(px(cx + Math.cos(a) * rr), px(cy + Math.sin(a) * rr), 1, 1); }
  }
  // A bob on the water, never a turn, since pixel art breaks up at an angle.
  const bob = k.act === "bob" || k.act === "float" ? Math.sin(o.t * 2 + o.id) * 0.4 : 0;
  if (o.flee > 0 && k.act !== "fly") {
    x.fillStyle = "rgba(207,240,246,0.5)";
    const d = Math.hypot(o.vx, o.vy) || 1, l = k.s * z;
    x.fillRect(px(cx - o.vx / d * l * 1.3), px(cy - o.vy / d * l * 1.3), 1, 1);
    x.fillRect(px(cx - o.vx / d * l * 1.8 + 1), px(cy - o.vy / d * l * 1.8), 1, 1);
  }
  sprite(x, k.k, cx, cy + bob, s, z, o.face);
  if (k.rare && w.st === "play") twinkle(x, cx + k.s * z * 0.7, cy - k.s * z * 0.7, o.t);
}

function twinkle(x, cx, cy, t) {
  const on = reduced || Math.floor(t * 4) % 2;
  x.fillStyle = on ? "#fff6dc" : "#ffb000";
  const r = on ? 3 : 2;
  x.fillRect(px(cx) - r, px(cy), r * 2 + 1, 1); x.fillRect(px(cx), px(cy) - r, 1, r * 2 + 1);
}

// Things go round and down the middle, and a creature only goes round for a
// spin before it's flung clear and paddles off.
function spins(x, z) {
  const ex = sx(w.x), ey = sy(w.y), R = w.r * z;
  w.spins.forEach((s) => {
    const p = Math.min(1, s.t / 0.6);
    let d = s.d * z * Math.pow(1 - p, 1.4), a = s.a + p * 5, sc = 1 - p * 0.75;
    if (s.out) {
      d = Math.max(d, R * 0.4); sc = Math.max(sc, 0.6);
      if (s.t > 0.6) { const q = s.t - 0.6; d = R * 0.4 + q * q * 260 + q * 60; a = s.a + 5 + q * 2; sc = Math.min(1, 0.6 + q); }
    }
    const cx = ex + Math.cos(a) * d, cy = ey + Math.sin(a) * d, r = s.out && s.t > 0.6 ? 0 : s.rot + p * 9;
    x.save();
    x.translate(px(cx), px(cy)); x.rotate(r); x.scale(sc, sc);
    sprite(x, s.kind.k, 0, 0, s.kind.s, z, s.face);
    x.restore();
    if (s.out && s.t > 0.6) {
      // A dizzy ring of little stars over whatever just went round.
      x.fillStyle = "#ffe27a";
      for (let i = 0; i < 3; i++) { const b = s.t * 8 + i * 2.1, l = s.kind.s * z * 0.6 + 2; x.fillRect(px(cx + Math.cos(b) * l), px(cy - l * 0.8 + Math.sin(b) * l * 0.35), 1, 1); }
    }
  });
}

// At the end the pond throws up a spout of everything that went down, with a
// rainbow in the spray.
function spout(x, z) {
  const ex = sx(w.x), ey = sy(w.y), R = Math.max(4, w.r * z), k = Math.min(1, w.done_t / 1.2);
  const cols = ["#e2412d", "#e07a2a", "#ffe27a", "#4fa043", "#7fc8e8", "#7d52c0"];
  x.globalAlpha = 0.55 * k;
  cols.forEach((c, i) => {
    x.fillStyle = c;
    const rr = R * 2.2 + 14 - i * 2;
    for (let s = 0; s <= 60; s++) { const a = Math.PI + s / 60 * Math.PI; x.fillRect(px(ex + Math.cos(a) * rr), px(ey + Math.sin(a) * rr * 0.9), 2, 2); }
  });
  x.globalAlpha = 1;
  G.spout.forEach((p) => {
    if (p.t < 0) return;
    const t = Math.min(p.t, 2.4), cx = ex + Math.cos(p.a) * p.v * t, cy = ey + Math.sin(p.a) * p.v * t + 90 * t * t;
    if (cy > H + 20) return;
    x.save();
    x.translate(px(cx), px(cy)); x.rotate(reduced ? 0 : p.rot + t * 4);
    const rs = ART[p.k];
    sprite(x, p.k, 0, 0, Math.max(rs.length, rs[0].length) * 0.75, 1, 1);
    x.restore();
  });
}

function across(r) { return r < 50 ? Math.round(r * 2) + " CM" : (r / 50).toFixed(1) + " M"; }

function hud(x) {
  const next = w.band + 1 < BANDS.length ? REACH[w.band + 1] : null, from = REACH[w.band];
  x.fillStyle = "#0a0908"; x.fillRect(0, 0, W, 3);
  x.fillStyle = "#9fdcf2"; x.fillRect(0, 0, Math.round(W * (next ? Math.min(1, (w.r - from) / (next - from)) : 1)), 3);
  x.font = FONT; x.textAlign = "left"; x.lineWidth = 3; x.strokeStyle = "#0a0908";
  const sc = String(score(w));
  x.strokeText(sc, 6, 18); x.fillStyle = G.flash > 0 ? "#ffb000" : "#ece5d8"; x.fillText(sc, 6, 18);
  const cm = across(w.r);
  x.textAlign = "right"; x.strokeText(cm, W - 6, 18); x.fillStyle = "#ece5d8"; x.fillText(cm, W - 6, 18);
  x.font = SMALL; x.lineWidth = 2;
  const nm = BANDS[w.band].name;
  x.strokeText(nm, W - 6, 28); x.fillStyle = "#9fdcf2"; x.fillText(nm, W - 6, 28);
  G.tick.forEach((t, i) => {
    const a = Math.min(1, (3 - t.t) / 0.6);
    if (a <= 0) return;
    const y = H - 12 - i * 12, nm2 = KIND[t.k].name.toUpperCase() + (t.n > 1 ? " X" + t.n : "");
    x.globalAlpha = a * (i ? 0.6 : 1);
    x.fillStyle = "rgba(10,9,8,0.55)"; x.fillRect(2, y - 7, x.measureText(nm2).width + 18, 10);
    const rs = ART[t.k];
    sprite(x, t.k, 9, y - 2, Math.max(rs.length, rs[0].length) * 0.5 * Math.min(1, 8 / Math.max(rs.length, rs[0].length)), 1, 1);
    x.textAlign = "left"; x.fillStyle = t.rare ? "#ffb000" : "#ece5d8"; x.fillText(nm2, 16, y);
    x.globalAlpha = 1;
  });
  x.fillStyle = "#0a0908"; x.fillRect(0, H - 3, W, 3);
  x.fillStyle = "#ffb000"; x.fillRect(0, H - 3, Math.round(W * Math.max(0, 1 - w.t / ROUND)), 3);
}

// The label on the Play disk.
export function whirlArt(x, g) {
  x.fillStyle = "#1d4f5c"; x.fillRect(0, 0, 168, 96);
  x.fillStyle = "rgba(207,240,246,0.13)"; for (let i = 0; i < 14; i++) x.fillRect((i * 53) % 168, (i * 37) % 96, 6, 1);
  x.fillStyle = "#2e5a30"; x.fillRect(0, 0, 168, 8);
  x.fillStyle = "#4a3a26"; x.fillRect(0, 8, 168, 2);
  disc(x, 84, 54, 22, "#17434e"); disc(x, 84, 54, 15, "#123844"); disc(x, 84, 54, 8, "#0a2530");
  for (let i = 0; i < 3; i++) for (let s = 0; s <= 30; s++) {
    const u = s / 30, rr = 22 * (0.2 + 1.1 * u), a = 0.4 + i * Math.PI * 2 / 3 + 2.6 * (1 - u);
    x.fillStyle = u > 0.4 ? "#9fdcf2" : "#cfe3ea"; x.fillRect(Math.round(84 + Math.cos(a) * rr) - 1, Math.round(54 + Math.sin(a) * rr * 0.92) - 1, 2, 2);
  }
  x.save(); x.translate(98, 46); x.rotate(0.9); sprite(x, "leaf", 0, 0, 5, 1, 1); x.restore();
  x.save(); x.translate(72, 64); x.rotate(-0.6); sprite(x, "twig", 0, 0, 7, 1, 1); x.restore();
  sprite(x, "duck", 142, 32, 17, 1, -1);
  sprite(x, "boat", 26, 78, 7, 1, 1);
  sprite(x, "duckweed", 120, 76, 2.5, 1, 1); sprite(x, "petal", 44, 40, 2.5, 1, 1); sprite(x, "duckweed", 58, 22, 2.5, 1, 1);
  sprite(x, "pad", 22, 40, 14, 1, 1);
  draw(x, cells(g, "frog", {}), FW, FH, colours(g, null), 0, 18, 1);
}

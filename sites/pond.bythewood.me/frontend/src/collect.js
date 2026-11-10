import { $, state, watch, TIERS } from "./api.js";
import { genome, bred, cells, colours, draw, W, H, MORPHS } from "./frog.js";
import { iconCanvas, dishCanvas, furnCanvas } from "./icons.js";
import { hatTile } from "./hats.js";
import { slot, openSheet, shade } from "./ui.js";

const BACKS = ["PLAIN", "SPOTS", "BANDS", "SPECKLED", "MASK", "BACK STRIPE"];
const PUPILS = ["BAR", "ROUND", "SLIT"];
const MORPH_TIER = { GREEN: 0, OLIVE: 0, "WOOD BROWN": 0, GREY: 1, GOLDEN: 2, "RED EYE": 2, STRAWBERRY: 3, BLUE: 3, ALBINO: 4 };
const BACK_TIER = { PLAIN: 0, SPOTS: 0, BANDS: 1, SPECKLED: 1, MASK: 1, "BACK STRIPE": 2 };
const PUPIL_TIER = { BAR: 0, ROUND: 1, SLIT: 1 };
let cat = "hats";

function cap(s) { return s.charAt(0) + s.slice(1).toLowerCase(); }

// A model frog in one colour, back or pupil, for the register.
const model = genome(1207);
function frogArt(change) {
  const m = MORPHS.find((x) => x.name === (change.morph || "GREEN"));
  const g = Object.assign(bred(model, {
    morph: m.name, pattern: change.pattern || "PLAIN",
    hue: (m.h[0] + m.h[1]) / 2, sat: (m.s[0] + m.s[1]) / 2, lit: (m.l[0] + m.l[1]) / 2,
    bw: model.bw, bh: model.bh, eye: model.eye,
  }), change.pupil ? { pupil: change.pupil } : {});
  const cv = document.createElement("canvas");
  cv.width = W * 2; cv.height = H * 2;
  draw(cv.getContext("2d"), cells(g, "frog", { legs: true }), W, H, colours(g, null), 0, 0, 2);
  return cv;
}

function register(p) {
  const seen = { morph: new Set(), pattern: new Set(), pupil: new Set() };
  p.frogs.filter((f) => f.stage !== "egg").forEach((f) => {
    const g = bred(genome(f.seed), f.mods);
    seen.morph.add(g.morph.name); seen.pattern.add(g.pattern); seen.pupil.add(g.pupil);
  });
  return [
    ...MORPHS.map((m) => ({ art: () => frogArt({ morph: m.name }), name: cap(m.name), tier: MORPH_TIER[m.name], found: seen.morph.has(m.name), what: "A frog colour." })),
    ...BACKS.map((b) => ({ art: () => frogArt({ pattern: b }), name: cap(b) + " back", tier: BACK_TIER[b], found: seen.pattern.has(b), what: "A frog's back." })),
    ...PUPILS.map((u) => ({ art: () => frogArt({ pupil: u }), name: cap(u) + " pupils", tier: PUPIL_TIER[u], found: seen.pupil.has(u), what: "A frog's eyes." })),
  ];
}

function cookbook(p) {
  const k = p.kitchen, left = k.tiers.slice();
  const out = k.cookbook.map((d) => {
    const i = left.indexOf(d.tier);
    if (i >= 0) left.splice(i, 1);
    return { art: () => dishCanvas(d, 4), name: d.name, tier: d.tier, found: true, what: "A dish." };
  });
  left.sort((a, b) => a - b).forEach((t) => out.push({ art: () => dishCanvas({ a: "duckweed", b: "duckweed", name: "", tier: 0 }, 4), name: "", tier: t, found: false, what: "Two things from the garden make it." }));
  return out;
}

// Every collection, each a list of what there is, found or not.
function cats(p) {
  const furn = (p.shops.fenn && p.shops.fenn.lines) || [];
  return [
    { key: "hats", name: "Hats", icon: "hat", all: p.hats.map((h) => ({ art: () => hatTile(h, true), name: h.name, tier: h.tier, found: h.count > 0, what: "Boxes at Wren's, the wheel and presents." })) },
    { key: "trinkets", name: "Trinkets", icon: "t_duck", all: p.trinkets.all.map((t) => ({ art: () => iconCanvas(t.kind, 4), name: t.name, tier: t.tier, found: t.found, what: "Boxes at Old Moss's and presents." })) },
    { key: "rocks", name: "Rocks", icon: "agate", all: p.rocks.map((r) => ({ art: () => iconCanvas(r.kind, 4), name: r.name, tier: r.tier, found: r.found, what: "The wheel, the reeds and Old Moss." })) },
    { key: "keepsakes", name: "Keepsakes", icon: "k_frost", all: p.keepsakes.map((k) => ({ art: () => iconCanvas(k.kind, 4), name: k.name, tier: k.tier, found: k.days > 0, what: k.hint, sub: k.days ? k.first : "" })) },
    { key: "frogs", name: "Frog looks", icon: "i_pond", all: register(p) },
    { key: "dishes", name: "Cookbook", icon: "lotus", all: cookbook(p) },
    { key: "furniture", name: "Furniture", icon: "f_reeds", all: furn.map((l) => ({ art: () => furnCanvas(l.kind), name: l.name, tier: 0, found: p.furniture.includes(l.kind), what: "Fenn's Pad and Reed, in town." })) },
  ];
}

function render(p) {
  const all = cats(p);
  let have = 0, of = 0;
  const box = $("col-cats");
  box.textContent = "";
  all.forEach((c) => {
    const n = c.all.filter((x) => x.found).length;
    have += n; of += c.all.length;
    const b = document.createElement("button"), name = document.createElement("b"), count = document.createElement("span"), bar = document.createElement("span"), fill = document.createElement("i");
    b.type = "button"; b.className = "cat" + (n === c.all.length ? " done" : "");
    b.setAttribute("aria-pressed", c.key === cat ? "true" : "false");
    name.textContent = c.name;
    count.textContent = n + "/" + c.all.length;
    bar.className = "meter"; fill.style.transform = "scaleX(" + (c.all.length ? n / c.all.length : 0) + ")";
    bar.append(fill);
    b.append(iconCanvas(c.icon, 3), name, count, bar);
    b.addEventListener("click", () => { cat = c.key; render(state.pond); });
    box.append(b);
  });
  const pct = of ? Math.floor(100 * have / of) : 0;
  $("col-pct").textContent = pct + "%";
  $("col-bar").style.transform = "scaleX(" + (of ? have / of : 0) + ")";
  $("col-n").textContent = have + " of " + of + " found";

  const c = all.find((x) => x.key === cat) || all[0];
  $("col-head").textContent = c.name;
  const grid = $("col-grid");
  grid.textContent = "";
  c.all.forEach((x) => {
    const art = x.art();
    grid.append(slot(art, {
      tier: x.tier, name: x.name, found: x.found, sub: x.found ? x.sub : TIERS[x.tier],
      onClick: () => {
        const big = x.art();
        big.classList.add("big");
        openSheet({ art: x.found ? big : shade(big), name: x.found ? x.name : "???", tier: x.tier },
          x.found ? (x.sub ? "First kept " + x.sub + "." : "Found.") : x.what, () => {});
      },
    }));
  });
}

export function initCollect() { watch(render); }

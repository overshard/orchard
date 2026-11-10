import { $, post, setPond, watch, titled, TIERS } from "./api.js";
import { genome, bred, cells, colours, draw, W, H, MORPHS } from "./frog.js";

let dye = "", stamp = "", drop = "", frontID = 0;

function days(sec) {
  const d = Math.ceil(sec / 86400);
  return d === 1 ? "tomorrow" : "in " + d + " days";
}

function say(id, msg) { $(id).textContent = msg; }

function card(f) {
  const div = document.createElement("div"), cv = document.createElement("canvas"), g = bred(genome(f.seed), f.mods);
  div.className = "reed" + (f.front ? " front" : "");
  cv.width = W * 3; cv.height = H * 3;
  cv.setAttribute("role", "img"); cv.setAttribute("aria-label", titled(f.name));
  draw(cv.getContext("2d"), cells(g, f.stage, { hat: f.hat.shape, legs: f.day >= 6 }), W, H, colours(g, f.hat), 0, 0, 3);
  const name = document.createElement("b"), what = document.createElement("span"), tier = document.createElement("span");
  name.textContent = titled(f.name);
  what.textContent = f.stage.charAt(0).toUpperCase() + f.stage.slice(1) + ", day " + f.day + (f.stage === "egg" ? "" : ", " + f.nature.toLowerCase());
  tier.className = "tier t" + f.rarity.tier; tier.textContent = TIERS[f.rarity.tier];
  div.append(cv, name, what, tier);
  if (f.front) {
    const on = document.createElement("span");
    on.className = "on"; on.textContent = "OUT FRONT";
    div.append(on);
  } else {
    const b = document.createElement("button");
    b.type = "button"; b.textContent = "Out front";
    b.addEventListener("click", () => {
      b.disabled = true;
      post("/api/frogs/front", { frog: f.id }).then((p) => {
        setPond(p);
        say("reeds-out", titled(f.name) + " is out front now. It greets you, wears its own hat and dives.");
      }, (e) => { say("reeds-out", e.message); b.disabled = false; });
    });
    div.append(b);
  }
  return div;
}

function render(p) {
  const front = p.frogs.find((f) => f.front), name = titled(front.name);
  frontID = front.id;
  const box = $("reeds");
  box.textContent = "";
  [front].concat(p.frogs.filter((f) => !f.front)).forEach((f) => box.append(card(f)));
  say("reeds-line", p.frogs.length === 1
    ? name + " has the whole pond to itself for now. Once it's grown it can lay an egg, and the egg grows up here beside it."
    : p.frogs.length + " frogs live here. One is out front and the rest are in the reeds, and swim past now and then. Any of them can swap. Once a day the ones old enough go foraging, and what they find depends on their nature.");
  $("forage-row").hidden = !p.forage;
  $("forage").textContent = "See what " + (p.forage === 1 ? "it" : "they") + " brought in";

  // The bench lays from whoever is out front.
  const full = p.frogs.length >= p.pads, lay = $("lay");
  let line;
  if (front.lay_in < 0) line = name + " is too young to lay. It can once it's a frog, on day 13.";
  else if (front.lay_in > 0) line = name + " laid not long ago and can lay again " + days(front.lay_in) + ".";
  else if (full) line = name + " is ready to lay, but every pad has a frog on it.";
  else line = name + " is ready to lay an egg.";
  say("bench-line", line);
  lay.disabled = front.lay_in !== 0 || full;

  const dyes = p.items.filter((it) => it.kind.startsWith("dye_"));
  if (dye && !dyes.some((d) => d.kind === dye)) dye = "";
  const seg = $("bench-dyes");
  seg.textContent = "";
  [{ kind: "", name: "No dye", count: 0 }].concat(dyes).forEach((d) => {
    const b = document.createElement("button");
    b.type = "button"; b.dataset.dye = d.kind;
    b.textContent = d.name + (d.count > 1 ? " x" + d.count : "");
    b.setAttribute("aria-pressed", d.kind === dye ? "true" : "false");
    seg.append(b);
  });
  seg.hidden = dyes.length === 0;

  const stamps = p.items.filter((it) => it.kind.startsWith("stamp_"));
  if (stamp && !stamps.some((d) => d.kind === stamp)) stamp = "";
  const sseg = $("bench-stamps");
  sseg.textContent = "";
  [{ kind: "", name: "No stamp", count: 0 }].concat(stamps).forEach((d) => {
    const b = document.createElement("button");
    b.type = "button"; b.dataset.stamp = d.kind;
    b.textContent = d.name + (d.count > 1 ? " x" + d.count : "");
    b.setAttribute("aria-pressed", d.kind === stamp ? "true" : "false");
    sseg.append(b);
  });
  sseg.hidden = stamps.length === 0;

  const drops = p.items.filter((it) => it.kind.startsWith("drops_"));
  if (drop && !drops.some((d) => d.kind === drop)) drop = "";
  const dseg = $("bench-drops");
  dseg.textContent = "";
  [{ kind: "", name: "No drops", count: 0 }].concat(drops).forEach((d) => {
    const b = document.createElement("button");
    b.type = "button"; b.dataset.drop = d.kind;
    b.textContent = d.name + (d.count > 1 ? " x" + d.count : "");
    b.setAttribute("aria-pressed", d.kind === drop ? "true" : "false");
    dseg.append(b);
  });
  dseg.hidden = drops.length === 0;

  say("pads-line", p.frogs.length + " of " + p.pads + " pads have a frog on them." + (p.pad_cost ? "" : " That's as many pads as the pond holds."));
  const buy = $("pad-buy");
  buy.hidden = !p.pad_cost;
  buy.textContent = "Another pad, " + (p.pad_cost || 0).toLocaleString("en-US");
  buy.disabled = p.pebbles < p.pad_cost;
}

const BACKS = ["PLAIN", "SPOTS", "BANDS", "SPECKLED", "MASK", "BACK STRIPE"];
const PUPILS = ["BAR", "ROUND", "SLIT"];

function renderRegister(p) {
  const seen = { colour: new Set(), back: new Set(), pupil: new Set() };
  p.frogs.filter((f) => f.stage !== "egg").forEach((f) => {
    const g = bred(genome(f.seed), f.mods);
    seen.colour.add(g.morph.name); seen.back.add(g.pattern); seen.pupil.add(g.pupil);
  });
  const rows = [["Colours", "colour", MORPHS.map((m) => m.name)], ["Backs", "back", BACKS], ["Pupils", "pupil", PUPILS]];
  const box = $("register");
  box.textContent = "";
  let have = 0, all = 0;
  rows.forEach(([label, key, names]) => {
    const n = names.filter((x) => seen[key].has(x)).length, head = document.createElement("p"), list = document.createElement("p");
    have += n; all += names.length;
    head.className = "label"; head.textContent = label + ", " + n + " of " + names.length;
    list.className = "register";
    names.forEach((x) => {
      const c = document.createElement("span");
      c.className = seen[key].has(x) ? "on" : "";
      c.textContent = x;
      list.append(c);
    });
    box.append(head, list);
  });
  $("reg-count").textContent = have + " OF " + all;
}

export function initReeds() {
  watch(renderRegister);
  watch(render);
  $("bench-dyes").addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    dye = b.dataset.dye;
    document.querySelectorAll("#bench-dyes button").forEach((x) => x.setAttribute("aria-pressed", x === b ? "true" : "false"));
  });
  $("bench-stamps").addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    stamp = b.dataset.stamp;
    document.querySelectorAll("#bench-stamps button").forEach((x) => x.setAttribute("aria-pressed", x === b ? "true" : "false"));
  });
  $("bench-drops").addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    drop = b.dataset.drop;
    document.querySelectorAll("#bench-drops button").forEach((x) => x.setAttribute("aria-pressed", x === b ? "true" : "false"));
  });
  $("lay").addEventListener("click", () => {
    const lay = $("lay");
    lay.disabled = true;
    post("/api/frogs/lay", { frog: frontID, dye, stamp, drops: drop }).then((d) => {
      const took = d.frog.mods && d.frog.mods.took ? d.frog.mods.took : [];
      let msg = "An egg, and it's on a pad in the reeds. It'll be " + titled(d.frog.name) + ".";
      if (took.includes("dye")) msg += " The dye took.";
      else if (dye) msg += " The dye didn't take this time.";
      if (took.includes("stamp")) msg += " The stamp took.";
      else if (stamp) msg += " The stamp didn't take this time.";
      if (took.includes("drops")) msg += " The drops took.";
      else if (drop) msg += " The drops didn't take this time.";
      if (took.includes("throwback")) msg += " Something about it looks like nobody in the family.";
      say("bench-out", msg);
      setPond(d.pond);
    }, (e) => { say("bench-out", e.message); lay.disabled = false; });
  });
  $("forage").addEventListener("click", function () {
    this.disabled = true;
    post("/api/reeds/forage").then((d) => {
      const out = $("forage-out");
      out.textContent = "";
      d.finds.forEach((x) => {
        const dt = document.createElement("dt"), dd = document.createElement("dd");
        dt.textContent = x.frog;
        dd.textContent = x.prize.text;
        out.append(dt, dd);
      });
      out.hidden = false;
      this.disabled = false;
      setPond(d.pond);
    }, (e) => { say("reeds-out", e.message); this.disabled = false; });
  });
  $("pad-buy").addEventListener("click", () => {
    post("/api/pads/buy").then(setPond, (e) => say("pads-line", e.message));
  });
}

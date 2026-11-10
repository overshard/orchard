import { $, post, state, setPond, watch, titled, TIERS } from "./api.js";
import { genome, bred, cells, colours, draw, W, H } from "./frog.js";
import { iconCanvas } from "./icons.js";
import { openSheet, closeSheet, sheetSay, button, tierTag, reveal } from "./ui.js";
import { hatTile } from "./hats.js";

const JOB_ICON = { garden: "watercress", bugs: "cricket", sticks: "sticks", reeds: "reeds", mud: "clay", pebbles: "peb", craft: "t_basket" };

let dye = "", stamp = "", drop = "", frontID = 0;

function days(sec) {
  const d = Math.ceil(sec / 86400);
  return d === 1 ? "tomorrow" : "in " + d + " days";
}

function say(id, msg) { $(id).textContent = msg; }

function plural(n) { return n === 1 ? "One frog" : n + " frogs"; }

function hours(h) {
  if (h < 1) return Math.max(1, Math.round(h * 60)) + "m";
  return (Math.round(h * 10) / 10) + "h";
}

function frogArt(f, s) {
  const cv = document.createElement("canvas"), g = bred(genome(f.seed), f.mods);
  cv.width = W * s; cv.height = H * s;
  draw(cv.getContext("2d"), cells(g, f.stage, { hat: f.hat.shape, legs: f.day >= 6 }), W, H, colours(g, f.hat), 0, 0, s);
  return cv;
}

function levelOf(f, key) { return f.work.levels.find((l) => l.job === key); }

function jobName(key) {
  const d = state.pond.jobs.defs.find((x) => x.key === key);
  return d ? d.name : "";
}

function setJob(f, job, recipe) {
  return (b) => {
    b.disabled = true;
    post("/api/jobs/set", { frog: f.id, job, recipe: recipe || "" }).then((d) => {
      setPond(d.pond);
      if (d.done.length) showDone(d.done);
      closeSheet();
      say("reeds-out", job ? titled(f.name) + " is " + state.pond.jobs.defs.find((x) => x.key === job).verb + " now." : titled(f.name) + " is taking it easy.");
    }, (e) => { sheetSay(e.message); b.disabled = false; });
  };
}

// The job sheet lists every job with the frog's level and how far to the next.
function jobSheet(f) {
  const p = state.pond;
  openSheet({ art: frogArt(f, 4), name: titled(f.name) }, "Total level " + f.work.total + ". Pick what it does all day.", (body) => {
    p.jobs.defs.forEach((d) => {
      const l = levelOf(f, d.key), b = document.createElement("button");
      b.type = "button"; b.className = "jobrow" + (f.work.job === d.key ? " on" : "");
      const name = document.createElement("b"), lv = document.createElement("span"), bar = document.createElement("span"), fill = document.createElement("i");
      name.textContent = d.name + (d.nature && f.nature === d.nature ? " ★" : "");
      lv.className = "lv"; lv.textContent = l.level;
      bar.className = "meter";
      fill.style.transform = "scaleX(" + (l.level >= 99 ? 1 : (l.xp - l.from) / Math.max(1, l.to - l.from)) + ")";
      bar.append(fill);
      b.append(iconCanvas(JOB_ICON[d.key], 3), name, lv, bar);
      b.addEventListener("click", d.key === "craft" ? () => craftSheet(f) : () => { if (f.work.job !== d.key) setJob(f, d.key)(b); });
      body.append(b);
    });
    if (f.work.job) {
      const r = document.createElement("div"); r.className = "row";
      r.append(button("No job", setJob(f, "")));
      body.append(r);
    }
  });
}

function craftSheet(f) {
  const p = state.pond, lvl = levelOf(f, "craft").level, held = {};
  p.bag.forEach((t) => { held[t.kind] = t.count; });
  openSheet({ art: iconCanvas("t_basket", 6), name: "Craft bench" }, titled(f.name) + " is level " + lvl + " at crafting. What should it make?", (body) => {
    p.jobs.recipes.forEach((r) => {
      const b = document.createElement("button"), name = document.createElement("b"), need = document.createElement("span");
      b.type = "button"; b.className = "jobrow" + (f.work.recipe === r.kind ? " on" : "");
      name.textContent = r.name;
      need.className = "need";
      need.textContent = lvl < r.level ? "Level " + r.level
        : Object.entries(r.in).map(([k, n]) => n + " " + k + ((held[k] || 0) < n ? " (" + (held[k] || 0) + ")" : "")).join(", ") + ", " + hours(r.hours / (1 + (lvl - 1) / 98));
      b.append(iconCanvas(r.kind, 3), name, tierTag(r.tier), need);
      b.disabled = lvl < r.level;
      b.addEventListener("click", () => setJob(f, "craft", r.kind)(b));
      body.append(b);
    });
  });
}

function showDone(done) {
  const out = $("jobs-out");
  out.textContent = "";
  done.forEach((w) => {
    const dt = document.createElement("dt"), dd = document.createElement("dd");
    const got = Object.entries(w.got).map(([k, n]) => n + " " + (n > 1 ? many(nameOf(k)) : nameOf(k)));
    if (w.pebbles) got.unshift(w.pebbles + " pebbles");
    let line = got.length ? got.join(", ") + "." : w.short ? "Ran out of materials." : "Nothing this time.";
    if (w.up) line += " " + w.job + " is " + w.level + " now!";
    dt.textContent = w.frog;
    dd.textContent = line;
    out.append(dt, dd);
  });
  out.hidden = false;
  big(done);
}

// A pet or a hood is rare enough to pop up like a prize.
function big(done) {
  const p = state.pond;
  for (const w of done) {
    const pet = Object.keys(w.got).map((k) => p.trinkets.all.find((t) => t.kind === k && t.pet)).find(Boolean);
    if (pet) {
      reveal({ kick: "A pet!", art: iconCanvas(pet.kind, 8), name: pet.name, tier: pet.tier, line: w.frog + " found it while " + p.jobs.defs.find((d) => d.name === w.job).verb + ". It's in your bag." });
      return;
    }
    const hood = w.up && w.level === 99 && p.hoods.find((h) => h.name.startsWith(w.job));
    if (hood) {
      reveal({ kick: "Level 99!", art: hatTile(hood, true), name: hood.name, tier: 4, line: w.frog + " maxed " + w.job.toLowerCase() + ". Any frog can wear the hood, it's in your bag." });
      return;
    }
  }
}

function many(name) { return name.endsWith("s") ? name : name + "s"; }

function nameOf(kind) {
  const p = state.pond, t = p.bag.find((x) => x.kind === kind);
  if (t) return t.name.toLowerCase();
  const r = p.jobs.recipes.find((x) => x.kind === kind);
  return r ? r.name.toLowerCase() : kind;
}

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
  if (f.fed) {
    const ate = document.createElement("span");
    ate.className = "ate"; ate.textContent = "Full for " + hours(f.fed.left / 3600);
    div.append(ate);
  }
  if (f.work) {
    const job = document.createElement("button");
    job.type = "button"; job.className = "job";
    if (f.work.job) {
      const l = levelOf(f, f.work.job);
      job.textContent = jobName(f.work.job) + " " + l.level + (f.work.hours >= 1 / 60 ? " · " + hours(f.work.hours) : "");
      job.classList.toggle("full", f.work.hours >= state.pond.jobs.hold);
    } else job.textContent = "Give it a job";
    job.addEventListener("click", () => jobSheet(f));
    div.append(job);
  }
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
    : p.frogs.length + " frogs live here. One is out front and the rest are in the reeds, and swim past now and then. Any of them can swap.");

  const working = p.frogs.filter((f) => f.work && f.work.job).length;
  say("jobs-line", working ? plural(working) + " working. Pond total level " + p.jobs.total + "." : "Nobody's working. Tap a frog's job button to give it one. Pond total level " + p.jobs.total + ".");
  $("jobs-row").hidden = p.jobs.waiting < 1 / 60;
  $("jobs-collect").textContent = "Collect " + hours(p.jobs.waiting) + " of work";

  // The bench lays from whoever is out front.
  const full = p.frogs.length >= p.pads, lay = $("lay");
  let line;
  if (front.lay_in < 0) line = name + " is too young to lay. It can once it's a frog, on day 13.";
  else if (front.lay_in > 0) line = name + " laid not long ago and can lay again " + days(front.lay_in) + ".";
  else if (full) line = name + " is ready to lay, but every pad has a frog on it.";
  else line = name + " is ready to lay an egg.";
  if (front.lay_in >= 0) {
    const m = front.fed;
    if (!m) line += " Feed it a dish from your bag first and the egg comes out better.";
    else {
      line += " It's full of " + m.name.toLowerCase() + " for " + hours(m.left / 3600) + ", so dyes, stamps and drops take " + (m.takes >= 1 ? "every time" : Math.round(m.takes * 100) + "% of the time");
      line += m.eyes ? " and about 1 egg in " + Math.round(1 / m.eyes) + " gets odd eyes." : ".";
    }
  }
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

export function initReeds() {
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
      if (took.includes("meal")) msg += " It has odd eyes from the meal.";
      if (took.includes("throwback")) msg += " Something about it looks like nobody in the family.";
      say("bench-out", msg);
      setPond(d.pond);
    }, (e) => { say("bench-out", e.message); lay.disabled = false; });
  });
  $("jobs-collect").addEventListener("click", function () {
    this.disabled = true;
    post("/api/jobs/collect").then((d) => {
      setPond(d.pond);
      showDone(d.done);
      this.disabled = false;
    }, (e) => { say("reeds-out", e.message); this.disabled = false; });
  });
  $("pad-buy").addEventListener("click", () => {
    post("/api/pads/buy").then(setPond, (e) => say("pads-line", e.message));
  });
}

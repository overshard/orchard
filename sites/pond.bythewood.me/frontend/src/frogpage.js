import { $, post, state, setPond, watch, titled, workNow, TIERS } from "./api.js";
import { genome, bred, cells, colours, MORPHS, W, H } from "./frog.js";
import { draw } from "./lit.js";
import { iconCanvas } from "./icons.js";
import { drawFrogPage, drawLaying, JOB_THING } from "./scenes.js";
import { openSheet, closeSheet, sheetSay, reveal, slot, thingArt } from "./ui.js";
import { openJob, jobSheet, collect } from "./work.js";
import { span, jobName } from "./reeds.js";
import { rng, water, look } from "./paint.js";

// A frog's own page at #frog-<id>, and laying an egg from it at #lay-<id>.
let frogId = 0, on = "", drawn = "", diaryFor = 0;

const STAGE = { egg: "Egg", tadpole: "Tadpole", froglet: "Froglet", frog: "Frog", elder: "Elder" };
const cap = (s) => s.charAt(0) + s.slice(1).toLowerCase();

function frogOf(p) { return p.frogs.find((f) => f.id === frogId); }

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function tile(label, value, lit) {
  const d = el("div", "fstat" + (lit ? " lit" : ""));
  d.append(el("span", "", label), el("b", "", value));
  return d;
}

function canLay(p, f) { return f.lay_in === 0 && p.frogs.length < p.pads; }

// ---------------------------------------------------------------- the page

const TRICKS = [["wave", "Waves"], ["hop", "Hops"], ["spin", "Spins"], ["four", ""], ["come", "Comes"]];
const FOURTH = { SINGER: "Sings", BOLD: "Leaps", SHY: "Peeks", "NIGHT OWL": "Stars", GLUTTON: "Snacks", DRIFTER: "Bubbles" };

function renderFrog(p) {
  const f = frogOf(p);
  if (!f) { location.hash = "#frogs"; return; }
  const key = [f.id, f.stage, f.day, f.hat.shape, f.hat.colour, JSON.stringify(f.mods), p.day && p.day.night, p.day && p.day.rain].join();
  if (key !== drawn) { drawn = key; drawFrogPage($("frog-art"), p, f); }
  $("fp-name").textContent = titled(f.name);
  $("fp-tier").textContent = TIERS[f.rarity.tier]; $("fp-tier").className = "tier t" + f.rarity.tier;
  $("fp-odds").textContent = "1 in " + f.rarity.n.toLocaleString("en-US");
  const bits = [STAGE[f.stage], "day " + f.day];
  if (f.stage !== "egg") bits.push(f.morph.toLowerCase(), f.nature.toLowerCase(), f.pattern.toLowerCase());
  $("fp-what").textContent = bits.join(" · ");

  const n = f.work && f.work.act ? workNow(f.work) : null, l = f.work && f.work.job && f.work.levels.find((x) => x.job === f.work.job);
  const egg = f.lay_in < 0 ? (f.stage === "egg" ? "Not yet" : "Day 13") : f.lay_in > 0 ? "In " + span(f.lay_in) : p.frogs.length < p.pads ? "Now" : "Pads full";
  $("fp-stats").replaceChildren(
    tile("Level", l ? String(l.level) : "None"),
    tile("Basket", n ? n.n + "/" + f.work.cap : "Empty", !!(n && n.stop === "full")),
    tile("Full", f.fed ? span(f.fed.left) : "No"),
    tile("Egg", egg, canLay(p, f)),
  );
  $("fp-lay").hidden = !canLay(p, f);
  const feed = $("fp-feed"), front = $("fp-front");
  feed.disabled = f.stage === "egg" || !!f.fed || !p.bag.some((t) => t.sort === "dish");
  feed.textContent = f.fed ? "Full" : "Feed";
  front.disabled = f.front;
  front.textContent = "Out front";

  // Work: the level and a bar to the next, the basket and its keys.
  $("fp-job-box").hidden = !f.work;
  if (f.work) {
    $("fp-job-head").textContent = l ? jobName(p, f.work.job) + " " + l.level : "No job";
    const bar = el("span", "meter"), fill = document.createElement("i"), k = l ? (l.level >= 99 ? 1 : (l.xp - l.from) / Math.max(1, l.to - l.from)) : 0;
    fill.style.transform = "scaleX(" + k + ")";
    bar.append(fill);
    $("fp-job").replaceChildren(iconCanvas(l ? JOB_THING[f.work.job] : "basket", 2), bar, el("span", "fjob-n", Math.floor(k * 100) + "%"));
    $("fp-job").hidden = !l;
    const has = n && n.n > 0;
    $("fp-basket").hidden = !l;
    $("fp-basket").classList.toggle("ready", !!(n && n.stop === "full"));
    $("fp-basket-head").textContent = !n ? "Basket" : n.stop === "full" ? "Basket full" : n.n + " of " + f.work.cap + " in the basket";
    $("fp-basket-what").textContent = has ? n.basket.map((b) => b.n + " " + b.name.toLowerCase()).join(", ") : "Nothing in it yet";
    $("fp-take").hidden = !has;
    $("fp-work").hidden = !l;
    $("fp-change").textContent = l ? "Change job" : "Give it a job";
  }

  // Bond is the pond's, so only the frog out front has hearts to show.
  $("fp-bond-box").hidden = !f.front || f.stage === "egg";
  if (f.front) {
    const b = p.bond;
    $("fp-bond-n").textContent = b.hearts + " of 5";
    $("fp-bond").replaceChildren(...TRICKS.map(([t, word], i) => {
      const d = el("div", "fstat trick" + (b.hearts > i ? " lit" : ""));
      d.append(iconCanvas("heart", 2), el("b", "", String(i + 1)), el("span", "", t === "four" ? FOURTH[f.nature] || "Sings" : word));
      return d;
    }));
  }

  $("fp-favs-box").hidden = !f.fav;
  if (f.fav) {
    $("fp-favs").replaceChildren(
      slot(thingArt(f.fav.dish, 4), { tier: f.fav.dish.tier, name: f.fav.dish.name, sub: "dish" }),
      slot(thingArt({ hat: f.fav.hat }, 4), { tier: f.fav.hat_tier, name: f.fav.hat_name, sub: "hat" }),
    );
  }
  if (diaryFor !== f.id) {
    diaryFor = f.id;
    $("fp-diary-day").textContent = "";
    $("fp-diary-line").textContent = "";
    fetch("/api/diary?frog=" + f.id, { credentials: "same-origin" }).then((r) => r.ok ? r.json() : null).then((d) => {
      const row = d && d.lines && d.lines[0];
      $("fp-diary-day").textContent = row ? dayWord(row.day, p.today) : "Nothing yet";
      $("fp-diary-line").textContent = row ? row.line : "It writes a line a day about the day before, starting tomorrow.";
    });
  }
}

function dayWord(day, today) {
  const d = (Date.parse(today) - Date.parse(day)) / 86400000;
  if (d === 1) return "Yesterday";
  const [, m, n] = day.split("-").map(Number);
  return n + " " + ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"][m - 1];
}

function say(msg) { $("fp-out").textContent = msg; }

function feedSheet(f) {
  const dishes = state.pond.bag.filter((t) => t.sort === "dish");
  openSheet({ bare: true, name: "Feed " + titled(f.name) }, "A full frog lays a better egg and works better.", (body) => {
    dishes.forEach((t) => {
      const fav = f.fav && f.fav.dish.kind === t.kind;
      const b = el("button", "pick");
      b.type = "button";
      b.append(thingArt(t, 3), el("span", "pick-n", t.name + (fav ? ", its favourite" : "")), el("span", "pick-c", "x" + t.count));
      b.addEventListener("click", () => {
        b.disabled = true;
        post("/api/frogs/feed", { frog: f.id, dish: t.kind }).then((v) => { setPond(v); closeSheet(); say(titled(f.name) + " ate it up."); }, (e) => { sheetSay(e.message); b.disabled = false; });
      });
      body.append(b);
    });
  });
}

// ---------------------------------------------------------------- laying

const pick = { dye: "", pattern: "", drops: "" };
const SLOT = { dye: ["Colour", "dye_"], pattern: ["Pattern", "stamp_"], drops: ["Eyes", "drops_"] };
let asked = "", odds = null, layDrawn = "";

function held(p, prefix) { return p.items.filter((it) => it.kind.startsWith(prefix)); }

function renderLay(p) {
  const f = frogOf(p);
  if (!f) { location.hash = "#frogs"; return; }
  $("lay-back").href = "#frog-" + f.id;
  const key = [f.id, f.stage, f.hat.shape, JSON.stringify(f.mods), p.day && p.day.night].join();
  if (key !== layDrawn) { layDrawn = key; drawLaying($("lay-art"), p, f); }
  $("lay-from").textContent = "From " + titled(f.name);
  Object.keys(pick).forEach((k) => { if (pick[k] && !held(p, SLOT[k][1]).some((it) => it.kind === pick[k])) pick[k] = ""; });
  document.querySelectorAll("#lay-slots button").forEach((b) => {
    const k = b.dataset.slot, it = p.items.find((x) => x.kind === pick[k]), any = held(p, SLOT[k][1]).length > 0;
    b.className = "layslot" + (it ? " on" : "");
    b.disabled = !any && !it;
    const label = el("span", "layslot-k", SLOT[k][0]);
    if (it) b.replaceChildren(label, iconCanvas(it.kind, 3), el("span", "layslot-n", it.name.replace(/ (pattern|dye|eye drops)$/i, "") + " · x" + it.count));
    else b.replaceChildren(label, el("b", "", "+"), el("span", "layslot-n", any ? "Add one" : "None in the bag"));
  });
  const can = canLay(p, f);
  $("lay-go").disabled = !can;
  $("lay-go").textContent = can ? "Lay the egg" : f.lay_in > 0 ? "Lays again in " + span(f.lay_in) : f.lay_in < 0 ? "Too young to lay" : "Every pad has a frog on it";
  askOdds(f);
}

function askOdds(f) {
  const q = "frog=" + f.id + "&dye=" + pick.dye + "&pattern=" + pick.pattern + "&drops=" + pick.drops + "&fed=" + (f.fed ? f.fed.kind : "");
  if (q === asked) return;
  asked = q;
  fetch("/api/lay/odds?" + q, { credentials: "same-origin" }).then((r) => r.ok ? r.json() : null).then((o) => {
    if (!o || q !== asked) return;
    odds = o;
    drawOdds(f, o);
  });
}

function pct(x) {
  if (x >= 0.995) return "100%";
  if (x < 0.01) return "under 1%";
  return Math.round(x * 100) + "%";
}

// An outcome is the parent's own frog with the colour and back it would hatch with.
function lookOf(f, morph, pattern) {
  const base = bred(genome(f.seed), f.mods);
  if (morph === base.morph.name && pattern === base.pattern) return base;
  const m = MORPHS.find((x) => x.name === morph);
  const same = morph === base.morph.name;
  return bred(genome(f.seed), {
    morph, pattern, bw: base.bw, bh: base.bh, eye: base.eye,
    hue: same ? base.hue : (m.h[0] + m.h[1]) / 2, sat: same ? base.sat : (m.s[0] + m.s[1]) / 2, lit: same ? base.lit : (m.l[0] + m.l[1]) / 2,
  });
}

function outArt(g, seed) {
  const s = Math.max(1, Math.round(2 * (window.devicePixelRatio || 1))), cv = document.createElement("canvas");
  cv.width = 56 * s; cv.height = 44 * s;
  const x = cv.getContext("2d"), L = look(state.pond.day);
  x.imageSmoothingEnabled = false;
  x.scale(s, s);
  water(x, 56, 0, 44, L.water[0], L.water[1], rng(seed), L.line, L.line2);
  x.fillStyle = "rgba(0,0,0,0.3)"; x.beginPath(); x.ellipse(28, 40, W * 0.36, 2.2, 0, 0, 7); x.fill();
  draw(x, cells(g, "frog", { legs: true }), W, H, colours(g, null), 6, 42 - H + 1, 1);
  cv.setAttribute("aria-hidden", "true");
  return cv;
}

function drawOdds(f, o) {
  $("lay-take").textContent = (o.meal ? "Full of " + o.meal.toLowerCase() + ", so each takes " : "Not fed, so each takes ") + pct(o.takes) + "." + (o.meal ? "" : " A dish first makes them take more.");
  const hint = !pick.drops || o.eyes > 0;
  const box = $("lay-outs"), shown = o.outcomes.filter((x) => x.chance >= 0.02).slice(0, hint ? 4 : 5);
  const rest = o.outcomes.length - shown.length ? 1 - shown.reduce((n, x) => n + x.chance, 0) : 0;
  const cards = shown.map((x, i) => {
    const d = el("div", "hatchling");
    d.append(outArt(lookOf(f, x.morph, x.pattern), i + 7));
    const t = el("div", "hatchling-t");
    t.append(el("b", "", pct(x.chance)), el("span", "", x.morph.toLowerCase() + ", " + x.pattern.toLowerCase()));
    d.append(t);
    if (x.new) d.append(el("i", "newtag", "New"));
    return d;
  });
  if (rest > 0.0005) {
    const d = el("div", "hatchling"), g = genome(271828182 ^ f.seed), t = el("div", "hatchling-t");
    d.append(outArt(g, 3));
    d.classList.add("any");
    t.append(el("b", "", pct(rest)), el("span", "", "anything else"));
    d.append(t);
    cards.push(d);
  }
  const eyes = el("div", "hatchling hint");
  eyes.textContent = pick.drops ? "Odd eyes " + pct(o.eyes) : o.drops > 0 ? "Add eye drops for odd eyes, " + pct(o.drops) + (o.eyes > 0 ? " instead of " + pct(o.eyes) : "") : "";
  if (hint) cards.push(eyes);
  box.replaceChildren(...cards);
}

function slotSheet(k) {
  const p = state.pond, list = held(p, SLOT[k][1]);
  openSheet({ bare: true, name: SLOT[k][0] }, "Used up whether it takes or not.", (body) => {
    const none = el("button", "pick" + (pick[k] ? "" : " on"));
    none.type = "button";
    none.append(el("span", "pick-n", "None"));
    none.addEventListener("click", () => { pick[k] = ""; closeSheet(); renderLay(state.pond); });
    body.append(none);
    list.forEach((it) => {
      const b = el("button", "pick" + (pick[k] === it.kind ? " on" : ""));
      b.type = "button";
      b.append(iconCanvas(it.kind, 2), el("span", "pick-n", it.name), el("span", "pick-c", "x" + it.count));
      b.addEventListener("click", () => { pick[k] = it.kind; closeSheet(); renderLay(state.pond); });
      body.append(b);
    });
  });
}

function eggArt(f) {
  const g = bred(genome(f.seed), f.mods), cv = document.createElement("canvas");
  cv.width = W * 4; cv.height = H * 4;
  draw(cv.getContext("2d"), cells(g, "egg", {}), W, H, colours(g, null), 0, 0, 4);
  return cv;
}

function lay(b) {
  const f = frogOf(state.pond);
  if (!f) return;
  b.disabled = true;
  const chose = { ...pick };
  post("/api/frogs/lay", { frog: f.id, dye: chose.dye, stamp: chose.pattern, drops: chose.drops }).then((d) => {
    const took = (d.frog.mods && d.frog.mods.took) || [];
    const bits = [];
    if (chose.dye) bits.push(took.includes("dye") ? "The dye took." : "The dye didn't take this time.");
    if (chose.pattern) bits.push(took.includes("stamp") ? "The pattern took." : "The pattern didn't take this time.");
    if (chose.drops) bits.push(took.includes("drops") ? "The drops took." : "The drops didn't take this time.");
    if (took.includes("meal")) bits.push("It has odd eyes from the meal.");
    if (took.includes("throwback")) bits.push("Something about it looks like nobody in the family.");
    pick.dye = pick.pattern = pick.drops = "";
    asked = "";
    setPond(d.pond);
    location.hash = "#frogs";
    reveal({ kick: "An egg!", art: eggArt(d.frog), name: titled(d.frog.name), tier: d.frog.rarity.tier, line: ["It's on a pad in the reeds and hatches tomorrow."].concat(bits).join(" "), ok: "Lovely" });
  }, (e) => { $("lay-out").textContent = e.message; b.disabled = false; });
}

// ---------------------------------------------------------------- routing

export function enterFrog(id) { frogId = id; on = "frog"; if (state.pond) renderFrog(state.pond); }
export function enterLay(id) {
  if (id !== frogId) { pick.dye = pick.pattern = pick.drops = ""; }
  frogId = id; on = "lay"; asked = ""; $("lay-out").textContent = "";
  if (state.pond) renderLay(state.pond);
}
export function leaveFrog() { on = ""; }

export function initFrogPage() {
  watch((p) => { if (on === "frog") renderFrog(p); else if (on === "lay") renderLay(p); });
  $("fp-lay").addEventListener("click", () => { location.hash = "#lay-" + frogId; });
  $("fp-feed").addEventListener("click", () => { const f = frogOf(state.pond); if (f) feedSheet(f); });
  $("fp-front").addEventListener("click", (e) => {
    const b = e.currentTarget, f = frogOf(state.pond);
    if (!f) return;
    b.disabled = true;
    post("/api/frogs/front", { frog: f.id }).then((p) => { setPond(p); say(titled(f.name) + " is out front now. It greets you, wears its own hat and dives."); }, (err) => { say(err.message); b.disabled = false; });
  });
  $("fp-take").addEventListener("click", function () { collect(this).then(() => { this.disabled = false; }); });
  $("fp-work").addEventListener("click", () => { const f = frogOf(state.pond); if (f) openJob(f); });
  $("fp-change").addEventListener("click", () => { const f = frogOf(state.pond); if (f) jobSheet(f); });
  $("lay-slots").addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (b && !b.disabled) slotSheet(b.dataset.slot);
  });
  $("lay-go").addEventListener("click", (e) => lay(e.currentTarget));
  setInterval(() => { if (on === "frog" && state.pond && !document.hidden) renderFrog(state.pond); }, 20000);
}

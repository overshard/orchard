import { $, post, state, setPond, watch, reduced, TIERS } from "./api.js";
import { genome, cells, colours, draw, W, H } from "./frog.js";
import { dishCanvas, iconCanvas } from "./icons.js";
import { openSheet, sheetSay, button, reveal, thingArt } from "./ui.js";
import { cookFor } from "./bank.js";

export function art(n, s) {
  const cv = document.createElement("canvas"), g = genome(n.seed);
  cv.width = W * s; cv.height = H * s;
  draw(cv.getContext("2d"), cells(g, "frog", { hat: n.hat.shape }), W, H, colours(g, n.hat), 0, 0, s);
  return cv;
}

function hearts(level) { return "♥".repeat(level) + "♡".repeat(5 - level); }

function nameOf(kind) {
  const i = state.pond.kitchen.ingredients.find((x) => x.kind === kind);
  return i ? i.name.toLowerCase() : kind;
}

// Each present pops up in turn after the last is closed.
export function presents(n, zs) {
  if (!zs.length) return;
  const [z, ...rest] = zs;
  const m = z.item && state.pond.mementos.find((x) => x.kind === z.item);
  if (m) reveal({ kick: "A memento from " + n.name, art: thingArt({ kind: m.kind }, 8), name: m.name, tier: m.tier, line: z.text, ok: "Keep it" });
  else reveal({
    kick: "A present from " + n.name,
    art: z.hat ? thingArt({ hat: z.hat }, 8) : thingArt({ kind: z.item || "peb" }, 8),
    name: z.hat ? z.hat_name : z.pebbles ? z.pebbles + " pebbles" : z.text,
    tier: z.hat || (z.item && z.item.startsWith("t_")) ? z.hat_tier : undefined,
    line: z.hat ? (z.spare ? "A spare, Little Tully will buy it." : "It's in your bag.") : z.text,
  });
  if (rest.length) $("reveal-ok").addEventListener("click", () => presents(n, rest), { once: true });
}

// Presents from several neighbours at once, a rent or a stall collect, come one
// neighbour at a time. d.from says who each of d.prizes is from.
export function presentsFrom(d) {
  const by = [];
  d.from.forEach((who, i) => {
    const last = by[by.length - 1];
    if (last && last[0] === who) last[1].push(d.prizes[i]);
    else by.push([who, [d.prizes[i]]]);
  });
  const next = () => {
    const step = by.shift();
    if (!step) return;
    presents(d.pond.neighbours.all[step[0]], step[1]);
    if (by.length) $("reveal-ok").addEventListener("click", next, { once: true });
  };
  next();
}

// A neighbour two hearts in can be asked into a free burrow, and asked out.
function lodging(p, n, body) {
  const bw = p.bank.burrows;
  let label = "";
  if (n.lodger) label = "Move out of the burrow";
  else if (n.level >= bw.lodge_at && bw.lodgers.length < bw.level) label = "Ask it to move into a burrow";
  if (!label) return;
  const r = document.createElement("div");
  r.className = "row";
  r.append(button(label, (b) => {
    b.disabled = true;
    post("/api/burrows/lodge", { who: n.id, in: !n.lodger }).then((v) => {
      setPond(v);
      sheetSay(n.lodger ? n.name + " has gone home. Any rent it owed is in your purse." : n.name + " has moved in. Rent's 4 pebbles an hour, in the bank.");
    }, (e) => { sheetSay(e.message); b.disabled = false; });
  }));
  body.append(r);
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text) e.textContent = text;
  return e;
}

// A labelled box on a neighbour's sheet, with a picture when there is one.
function fact(label, text, pic, sub) {
  const box = el("div", "fact"), words = el("div");
  words.append(el("span", "k", label), el("p", "", text));
  if (sub) words.append(el("p", "dim", sub));
  if (pic) box.append(pic);
  box.append(words);
  return box;
}

// ---------------------------------------------------------------- the sums

function held(kind) {
  const i = state.pond.kitchen.ingredients.find((x) => x.kind === kind);
  return i ? i.count : 0;
}

// The recipe is known once it's in the cookbook or the neighbour has said.
function recipe(n) {
  if (n.fav_a) return [n.fav_a, n.fav_b];
  const c = state.pond.kitchen.cookbook.find((d) => d.kind === n.fav_kind);
  return c ? [c.a, c.b] : null;
}

// The server's sum, a dish's tier in points, three times for the favourite
// and twice for the fish dish a few of them like.
function worth(n, d) {
  const g = state.pond.neighbours.gift_points[d.tier];
  return d.kind === n.fav_kind ? g * 3 : d.kind === n.also_kind ? g * 2 : g;
}

function favWorth(n) { return state.pond.neighbours.gift_points[n.fav_tier] * 3; }

function favState(n) {
  if (state.pond.kitchen.dishes.some((d) => d.kind === n.fav_kind)) return { s: "bag" };
  const r = recipe(n);
  if (!r) return { s: "secret" };
  const need = {};
  r.forEach((x) => { need[x] = (need[x] || 0) + 1; });
  const short = Object.keys(need).filter((x) => held(x) < need[x]);
  return short.length ? { s: "need", short, r } : { s: "cook", r };
}

function dishesFor(n) {
  return state.pond.kitchen.dishes.slice().sort((a, b) => worth(n, b) - worth(n, a));
}

function stateTag(st) {
  if (st.s === "bag") return el("span", "nb-tag bag", "In bag");
  if (st.s === "cook") return el("span", "nb-tag cook", "Can cook");
  if (st.s === "secret") return el("span", "nb-tag", "Recipe at ♥♥");
  return el("span", "nb-tag", "Need " + st.short.map(nameOf).join(" and "));
}

function frac(n, points) {
  if (n.level >= 5 || n.to <= n.from) return 1;
  return Math.max(0, Math.min(1, (points - n.from) / (n.to - n.from)));
}

function rail(f) {
  const r = el("span", "rail"), i = el("i");
  i.style.transform = "scaleX(" + f + ")";
  r.append(i);
  return r;
}

function toGo(n) { return n.level >= 5 ? "Best of friends" : n.to - n.points + " to " + "♥".repeat(n.level + 1); }

// ---------------------------------------------------------------- giving

function cook(n) {
  const st = favState(n);
  cookFor(...(st.s === "cook" ? st.r : []));
  window.addEventListener("hashchange", () => requestAnimationFrame(() => requestAnimationFrame(() => {
    $("pot-art").parentElement.previousElementSibling.scrollIntoView({ block: "start" });
  })), { once: true });
  location.hash = "#garden";
}

// After a gift or a favour the neighbour's sheet opens on what it did, and
// any present pops over it.
function after(id, d, kind) {
  setPond(d.pond);
  const n = d.pond.neighbours.all[id];
  show(n, d, kind);
  if (d.prizes.length) presents(n, d.prizes);
}

function give(n, dish, b, sayErr) {
  b.disabled = true;
  post("/api/neighbours/gift", { who: n.id, dish: dish.kind }).then((d) => after(n.id, d, dish.kind),
    (e) => { sayErr(e.message); b.disabled = false; });
}

// The reply says the points, moves the rail and says what the next heart
// brings, so a one point gift still shows.
function reply(n, d, kind) {
  const box = el("div", "nb-reply"), m = el("div", "nb-meter"), r = el("span", "rail"), now = el("i", "now"), was = el("i", "was");
  box.setAttribute("role", "status");
  const up = n.level > 0 && d.total - d.points < n.from;
  const from = up ? 0 : frac(n, d.total - d.points), to = frac(n, d.total);
  was.style.transform = "scaleX(" + from + ")";
  now.style.transform = "scaleX(" + (reduced ? to : from) + ")";
  r.append(now, was);
  m.append(el("b", "nb-plus", "+" + d.points), r, el("span", "dim", toGo(n)));
  box.append(el("p", "", d.line), m);
  const next = state.pond.neighbours.hearts[n.level];
  let line = up ? "A new heart, " + n.name + " is " + n.title.toLowerCase() + " now. " : "";
  if (next) line += "The next heart brings " + next.brings.charAt(0).toLowerCase() + next.brings.slice(1) + ".";
  if (line) box.append(el("p", "dim", line));
  if (kind && kind !== n.fav_kind) box.append(el("p", "hint", n.fav + " is its favourite, worth " + favWorth(n) + "."));
  if (!reduced) requestAnimationFrame(() => requestAnimationFrame(() => { now.style.transform = "scaleX(" + to + ")"; }));
  return box;
}

// ---------------------------------------------------------------- the sheet

function ladder(n) {
  const box = el("ol", "hladder");
  state.pond.neighbours.hearts.forEach((h, i) => {
    const l = i + 1, li = el("li", n.level >= l ? "got" : n.level + 1 === l ? "next" : "");
    li.append(el("span", "h", "♥".repeat(l)), el("span", "brings", h.name + " · " + h.brings), el("span", "pts", String(h.at)));
    box.append(li);
  });
  return box;
}

function dishRows(n, body) {
  if (n.gave) {
    body.append(el("p", "dim", "It's had a dish from you today. Come back tomorrow."));
    return;
  }
  const ds = dishesFor(n);
  if (!ds.length) {
    const r = el("div", "row");
    r.append(el("span", "dim", "Nothing cooked in the bag."), button("Cook", () => cook(n)));
    body.append(r);
    return;
  }
  ds.forEach((d) => {
    const row = el("div", "nb-dish"), what = el("span", "what"), pts = worth(n, d);
    const why = d.kind === n.fav_kind ? "Its favourite, " : d.kind === n.also_kind ? "Its fish favourite, " : "";
    what.append(el("b", "", d.name), el("span", "", why + TIERS[d.tier].toLowerCase() + ", " + d.count + " in your bag"));
    row.append(dishCanvas(d, 3), what, el("b", "nb-plus", "+" + pts),
      button("Give", (b) => give(n, d, b, sheetSay), d.kind === n.fav_kind ? "primary" : ""));
    body.append(row);
  });
}

function show(n, d, kind) {
  const p = state.pond;
  openSheet({ art: art(n, 4), name: n.name }, n.title + " · " + n.points + " points" + (n.lodger ? " · lodger" : ""), (body) => {
    if (d) body.append(reply(n, d, kind));
    if (n.now.paper) body.append(el("span", "press", "In today's gazette"));
    if (n.now.thinks) body.append(el("p", "thought", n.now.thinks));
    if (n.now.says || n.talk) body.append(el("p", "bubble", n.now.says || n.talk));
    body.append(el("h3", "nb-k", "Hearts"));
    const fr = el("div", "friend");
    fr.append(el("b", "hearts", hearts(n.level)), rail(frac(n, n.points)), el("span", "dim", toGo(n)));
    body.append(fr, ladder(n));
    body.append(el("h3", "nb-k", "Give a dish"));
    dishRows(n, body);
    const facts = el("div", "facts"), r = recipe(n), st = favState(n);
    facts.append(fact("Right now", n.now.doing));
    const said = st.s === "bag" ? ", and there's one in your bag" : st.s === "cook" ? ", and you can cook it" : st.s === "need" ? ", short of " + st.short.map(nameOf).join(" and ") : "";
    facts.append(fact("Favourite dish", n.fav + ", " + favWorth(n) + " points", r ? dishCanvas({ a: r[0], b: r[1], name: n.fav }, 4) : null,
      r ? nameOf(r[0]) + " and " + nameOf(r[1]) + said : "It'll say how to cook it at two hearts"));
    if (n.also) facts.append(fact("Also likes", n.also + ", " + state.pond.neighbours.gift_points[n.also_tier] * 2 + " points", n.also_a ? dishCanvas({ a: n.also_a, b: n.also_b, name: n.also }, 4) : null,
      n.also_a ? nameOf(n.also_a) + " and " + nameOf(n.also_b) + ", from fish you've kept" : "A fish dish, and it'll say which fish at two hearts"));
    const m = p.mementos[n.id];
    facts.append(fact("Memento", m.day ? m.name : "Something of its own, for a best friend", m.day ? iconCanvas(m.kind, 4) : null, m.day ? "Yours since " + m.day : ""));
    if (n.shop) facts.append(fact("Keeps shop", n.shop));
    if (n.gossip) facts.append(fact("Heard", n.gossip));
    facts.append(fact("About", n.bio));
    body.append(facts);
    if (n.now.says && n.talk) body.append(el("p", "dim", n.talk));
    lodging(p, n, body);
  });
}

// ---------------------------------------------------------------- the list

let only = "unfed";

const SORTS = [
  ["unfed", "Not fed", (n) => !n.gave],
  ["fave", "Can feed fave", (n, st) => !n.gave && (st.s === "bag" || st.s === "cook")],
  ["all", "All", () => true],
];

const RANK = { bag: 0, cook: 1, need: 2, secret: 3 };

function nbRow(n, st) {
  const row = el("div", "nb" + (n.gave ? " fed" : "")), who = el("button", "nb-who"), txt = el("span", "nb-txt");
  who.type = "button";
  const top = el("span", "nb-top"), hs = el("span", "nb-hearts"), fav = el("span", "nb-fav");
  top.append(el("b", "", n.name), el("span", "nb-title", n.title + (n.lodger ? " · lodger" : "")));
  const h = el("i", "", hearts(n.level));
  h.setAttribute("aria-label", n.level + " of 5 hearts");
  hs.append(h, rail(frac(n, n.points)), el("span", "", n.level >= 5 ? "Best" : n.to - n.points + " to go"));
  fav.append(stateTag(st), el("span", "nm", n.fav), el("span", "pts", favWorth(n) + " pts"));
  txt.append(top, hs, fav);
  who.append(art(n, 1), txt);
  who.addEventListener("click", () => show(n));
  row.append(who);
  if (n.gave) { row.append(el("span", "nb-fed", "Fed ✓")); return row; }
  const best = dishesFor(n)[0];
  if (best) {
    const b = button("Give +" + worth(n, best), (b) => give(n, best, b, (m) => { $("neigh-out").textContent = m; }), "nb-key" + (best.kind === n.fav_kind ? " primary" : ""));
    b.setAttribute("aria-label", "Give " + best.name.toLowerCase() + " to " + n.name + ", " + worth(n, best) + " points");
    row.append(b);
  } else row.append(button("Cook", () => cook(n), "nb-key"));
  return row;
}

function render(p) {
  const nb = p.neighbours, f = nb.favour, who = nb.all[f.who];
  const fv = $("favour");
  fv.textContent = "";
  fv.append(art(who, 2));
  const txt = el("span"), b = el("b", "", who.name + " needs " + f.n + " " + f.what.toLowerCase());
  txt.append(b, el("span", "dim", f.done ? "Done for today. Thank you!" : "Pays " + f.pay + " pebbles and " + f.points + " points toward a heart."));
  const have = (p.bag.find((t) => t.kind === f.kind) || { count: 0 }).count;
  const key = button(f.done ? "Done" : "Give " + f.n + (have < f.n ? " (" + have + ")" : ""), () => {
    key.disabled = true;
    post("/api/neighbours/favour").then((d) => after(f.who, d, ""),
      (e) => { $("neigh-out").textContent = e.message; key.disabled = false; });
  }, "primary");
  key.disabled = f.done || have < f.n;
  fv.append(txt, key);

  const rows = nb.all.map((n) => ({ n, st: favState(n) }));
  const sorts = $("neigh-sorts");
  sorts.textContent = "";
  SORTS.forEach(([k, name, test]) => {
    const c = button(name + " " + rows.filter((x) => test(x.n, x.st)).length, () => { only = k; render(state.pond); });
    c.setAttribute("aria-pressed", k === only ? "true" : "false");
    sorts.append(c);
  });
  const test = SORTS.find((x) => x[0] === only)[2];
  const grid = $("neigh-grid");
  grid.textContent = "";
  rows.filter((x) => test(x.n, x.st))
    .sort((a, b) => (a.n.gave ? 9 : RANK[a.st.s]) - (b.n.gave ? 9 : RANK[b.st.s]) || a.n.id - b.n.id)
    .forEach((x) => grid.append(nbRow(x.n, x.st)));
  if (!grid.children.length) grid.append(el("p", "dim", only === "unfed" ? "Everybody's had a dish from you today." : "No favourite in the bag or in reach of the larder yet."));
}

export function initNeighbours() { watch(render); }

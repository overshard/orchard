import { $, post, state, setPond, watch } from "./api.js";
import { genome, cells, colours, draw, W, H } from "./frog.js";
import { openSheet, closeSheet, sheetSay, button, reveal, thingArt } from "./ui.js";

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
  reveal({
    kick: "A present from " + n.name,
    art: z.hat ? thingArt({ hat: z.hat }, 8) : thingArt({ kind: z.item || "peb" }, 8),
    name: z.hat ? z.hat_name : z.pebbles ? z.pebbles + " pebbles" : z.text,
    tier: z.hat || (z.item && z.item.startsWith("t_")) ? z.hat_tier : undefined,
    line: z.hat ? (z.spare ? "A spare, Wren will buy it." : "It's in your bag.") : z.text,
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

function done(id, d, sayIn) {
  setPond(d.pond);
  const n = d.pond.neighbours.all[id];
  sayIn(d.line);
  if (d.prizes.length) { closeSheet(); presents(n, d.prizes); }
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

function show(n) {
  const p = state.pond;
  let what = n.title + ", " + hearts(n.level) + ". Its favourite is " + n.fav.toLowerCase() + ".";
  if (n.fav_a) what += " It told you how, " + nameOf(n.fav_a) + " and " + nameOf(n.fav_b) + ".";
  openSheet({ art: art(n, 4), name: n.name }, what, (body) => {
    if (n.gossip) {
      const g = document.createElement("p");
      g.className = "gossip";
      g.textContent = n.gossip;
      body.append(g);
    }
    lodging(p, n, body);
    if (n.gave) {
      const s = document.createElement("p");
      s.className = "dim"; s.textContent = "It's had something from you today. Come back tomorrow.";
      body.append(s);
      return;
    }
    const dishes = p.bag.filter((t) => t.sort === "dish");
    if (!dishes.length) {
      const s = document.createElement("p");
      s.className = "dim"; s.textContent = "Cook something in the garden to bring it.";
      body.append(s);
      return;
    }
    const r = document.createElement("div");
    r.className = "row";
    dishes.forEach((t) => r.append(button("Give " + t.name.toLowerCase(), (b) => {
      b.disabled = true;
      post("/api/neighbours/gift", { who: n.id, dish: t.kind }).then((d) => done(n.id, d, sheetSay),
        (e) => { sheetSay(e.message); b.disabled = false; });
    }, t.name === n.fav ? "primary" : "")));
    body.append(r);
  });
}

function render(p) {
  const nb = p.neighbours, f = nb.favour, who = nb.all[f.who];
  const fv = $("favour");
  fv.textContent = "";
  fv.append(art(who, 2));
  const txt = document.createElement("span"), b = document.createElement("b"), d = document.createElement("span");
  b.textContent = who.name + " needs " + f.n + " " + f.what.toLowerCase();
  d.className = "dim";
  d.textContent = f.done ? "Done for today. Thank you!" : "Pays " + f.pay + " pebbles and makes a friend.";
  txt.append(b, d);
  const have = (p.bag.find((t) => t.kind === f.kind) || { count: 0 }).count;
  const go = document.createElement("button");
  go.type = "button"; go.className = "primary"; go.textContent = f.done ? "Done" : "Give " + f.n + (have < f.n ? " (" + have + ")" : "");
  go.disabled = f.done || have < f.n;
  go.addEventListener("click", () => {
    go.disabled = true;
    post("/api/neighbours/favour").then((d) => done(f.who, d, (m) => { $("neigh-out").textContent = m; }),
      (e) => { $("neigh-out").textContent = e.message; go.disabled = false; });
  });
  fv.append(txt, go);

  const grid = $("neigh-grid");
  grid.textContent = "";
  nb.all.forEach((n) => {
    const c = document.createElement("button"), name = document.createElement("b"), h = document.createElement("span"), fav = document.createElement("span");
    c.type = "button"; c.className = "reed";
    name.textContent = n.name;
    h.className = "hearts"; h.textContent = hearts(n.level) + (n.lodger ? " · lodger" : n.gave ? " · fed today" : "");
    fav.textContent = n.talk || "Loves " + n.fav.toLowerCase();
    c.append(art(n, 3), name, h, fav);
    c.addEventListener("click", () => show(n));
    grid.append(c);
  });
}

export function initNeighbours() { watch(render); }

import { $, post, setPond, watch, reduced } from "./api.js";
import { openSheet, sheetSay, button, reveal } from "./ui.js";
import { iconCanvas } from "./icons.js";
import { renderBlack, showBlack } from "./black.js";

// Each business has its own awning colour and a mark on its sign.
const LOOK = {
  sp500: ["#3f9a4d", "L"],
  dow: ["#8a5a2b", "B"],
  nasdaq: ["#3d8fd6", "R"],
  russell: ["#2fa59a", "T"],
  gold: ["#e0b030", "G"],
  oil: ["#4a3a2a", "O"],
  vix: ["#7a8798", "S"],
  guild: ["#b8e05a", "B"],
  reedcut: ["#6b9a3f", "R"],
  claypit: ["#a8642e", "C"],
  gatherers: ["#8a5a2b", "S"],
  divers: ["#63a9c9", "D"],
  fishery: ["#9fb7c0", "F"],
  gardens: ["#58b85a", "G"],
  tackle: ["#2f6b6b", "S"],
  frames: ["#b03a8c", "M"],
};

const GLYPH = {
  L: ["1..", "1..", "1..", "111"], B: ["11.", "111", "1.1", "111"], R: ["11.", "1.1", "11.", "1.1"],
  T: ["111", ".1.", ".1.", ".1."], G: [".11", "1..", "1.1", ".11"], O: [".1.", "1.1", "1.1", ".1."],
  C: [".11", "1..", "1..", ".11"], S: [".11", ".1.", "..1", "11."], D: ["11.", "1.1", "1.1", "11."],
  F: ["111", "1..", "11.", "1.."], M: ["1.1", "111", "1.1", "1.1"],
};

const BUCKETS = [["share", "Shares"], ["good", "Goods"], ["coin", "Coin"], ["black", "Back door"]];
let bucket = "share";

function canvas(w, h, s) {
  const cv = document.createElement("canvas");
  cv.width = w * s; cv.height = h * s;
  cv.setAttribute("aria-hidden", "true");
  const x = cv.getContext("2d");
  return [cv, (c, r, ww, hh, f) => { x.fillStyle = f; x.fillRect(c * s, r * s, ww * s, hh * s); }];
}

// A shopfront in pixels, an awning in its colour over a door and its sign.
function front(key, s) {
  const [col, g] = LOOK[key] || ["#888", "?"];
  const [cv, px] = canvas(19, 12, s);
  px(1, 3, 17, 9, "#c9b48a");
  for (let i = 0; i < 19; i += 2) { px(i, 1, 1, 3, col); px(i + 1, 1, 1, 3, "#f4f1e6"); }
  px(0, 0, 19, 1, col);
  px(3, 6, 4, 6, "#3a2a1c"); px(6, 9, 1, 1, "#e0b030");
  px(9, 5, 8, 6, "#1b1814");
  (GLYPH[g] || []).forEach((row, r) => [...row].forEach((ch, c) => { if (ch === "1") px(11 + c, 6 + r, 1, 1, col); }));
  return cv;
}

// Glowworm Coin, a gold coin with a glowworm curled on its face.
const COIN = [
  "...aaaaaa...", "..abbbbbba..", ".abbcccccba.", "abbcbbbbbcba", "abcbbddbbcba", "abcbdeedbcba",
  "abcbdeedbcba", "abcbbdbbbcba", "abbcbbbbbcba", ".abbcccccba.", "..abbbbbba..", "...aaaaaa...",
];
const COIN_INK = { a: "#6b4a10", b: "#e0b030", c: "#ffe27a", d: "#7fd67f", e: "#d8ffb0" };

function coinArt(s) {
  const [cv, px] = canvas(12, 12, s);
  COIN.forEach((row, r) => [...row].forEach((ch, c) => { if (COIN_INK[ch]) px(c, r, 1, 1, COIN_INK[ch]); }));
  return cv;
}

function art(f, s) {
  if (f.kind === "coin") return coinArt(s);
  if (f.kind === "good") { const cv = iconCanvas(f.item, s); cv.setAttribute("aria-hidden", "true"); return cv; }
  return front(f.key, s);
}

// The closes so far and then today's prices as a stepped pixel line in
// readout glass, with a faint line where the holder paid.
function chart(f, h) {
  const pts = f.days.concat(f.ticks.length ? f.ticks : [f.price]), W = 320;
  if (pts[pts.length - 1] !== f.price) pts.push(f.price);
  const cv = document.createElement("canvas");
  cv.width = W; cv.height = h; cv.className = "chart";
  const x = cv.getContext("2d");
  x.fillStyle = "#0b1006"; x.fillRect(0, 0, W, h);
  x.fillStyle = "rgba(125,141,98,0.18)";
  for (let y = 12; y < h; y += 20) x.fillRect(0, y, W, 1);
  const paid = f.held && f.kind !== "good" ? f.cost / f.held : 0;
  const lo = Math.min(...pts, paid || Infinity), hi = Math.max(...pts, paid), span = Math.max(4, hi - lo), step = (W - 8) / Math.max(1, pts.length - 1);
  const yOf = (v) => h - 10 - Math.round(((v - lo) / span) * (h - 24) / 4) * 4;
  if (paid) { x.fillStyle = "rgba(236,229,216,0.35)"; for (let xx = 0; xx < W; xx += 8) x.fillRect(xx, yOf(paid) + 1, 4, 1); }
  x.fillStyle = pts[pts.length - 1] >= pts[0] ? "#ffb000" : "#e2567a";
  for (let i = 0; i < pts.length; i++) {
    const x0 = Math.round(i * step / 4) * 4 + 2, y0 = yOf(pts[i]);
    x.fillRect(x0, y0, 4, 4);
    if (i > 0) {
      const xp = Math.round((i - 1) * step / 4) * 4 + 2, yp = yOf(pts[i - 1]);
      for (let xx = xp; xx < x0; xx += 4) x.fillRect(xx, yp, 4, 4);
      for (let yy = Math.min(yp, y0); yy <= Math.max(yp, y0); yy += 4) x.fillRect(x0, yy, 4, 4);
    }
  }
  x.fillStyle = "#7d8d62"; x.font = "10px 'JetBrains Mono', monospace";
  x.fillText(String(hi), 4, 10); x.fillText(String(lo), 4, h - 2);
  return cv;
}

function arrow(f) { return f.price > f.prev ? "▲" : f.price < f.prev ? "▼" : "·"; }
function signed(n, unit) { return (n > 0 ? "+" : "") + n + (unit || ""); }
function dayPct(f) { return f.prev ? Math.round((f.price - f.prev) / f.prev * 1000) / 10 : 0; }
function gainPct(worth, cost) { return cost ? Math.round((worth - cost) / cost * 1000) / 10 : 0; }
function lots(f) { return Math.floor(f.held / state.lot); }

const state = { lot: 10, fees: {} };

function tile(k, v, cls) {
  const box = document.createElement("span"), kk = document.createElement("span"), vv = document.createElement("span");
  box.className = "readout " + (cls || ""); kk.className = "k"; vv.className = "v";
  kk.textContent = k; vv.textContent = v;
  box.append(kk, vv);
  return box;
}

function tiles(...ts) {
  const t = document.createElement("div");
  t.className = "tiles";
  t.append(...ts);
  return t;
}

function trade(f, n, sell, b) {
  b.disabled = true;
  post("/api/market", { key: f.key, n, sell }).then((v) => {
    setPond(v);
    show(v.market.firms.find((x) => x.key === f.key));
    const what = f.kind === "good" ? (n === 1 ? "a lot" : n + " lots") : String(n);
    sheetSay((sell ? "Sold " : "Bought ") + what + ".");
  }, (e) => { sheetSay(e.message); b.disabled = false; });
}

function keys(...btns) {
  const r = document.createElement("div");
  r.className = "trade";
  r.append(...btns);
  return r;
}

// Selling under what was paid asks first, with waiting as the big key.
function sell(f, n, b) {
  const back = f.sell * n, paid = Math.round(f.cost * n / f.held);
  if (back >= paid) return trade(f, n, true, b);
  openSheet({ art: art(f, 4), name: f.name }, "That's " + back + " back for what cost " + paid + ", " + gainPct(back, paid) + "%. Prices come back as often as not.", (body) => {
    body.append(keys(button("Wait", () => show(f), "primary"), button("Sell anyway", (bb) => trade(f, n, true, bb))));
  });
}

function holding(f) {
  const g = f.worth - f.cost;
  return tiles(tile(f.kind === "coin" ? "Coins" : "Held", f.held), tile("Paid", f.cost), tile("Worth now", f.worth),
    tile("If sold", signed(g) + " · " + signed(gainPct(f.worth, f.cost), "%"), g < 0 ? "down" : "up"));
}

let story = null;

function sayOf(f) {
  const today = signed(dayPct(f), "%") + " today." + (story && story.key === f.key ? " In the paper: " + story.line + "." : "");
  if (f.kind === "good") return "The more " + f.moves + " every pond brings in, the cheaper it gets. A lot of " + state.lot + " is " + f.price + ", " + today;
  if (f.kind === "coin") return "Swings far more than anything else on the board. A coin is " + f.price + ", " + today;
  return "Moves with " + f.moves + " across every pond. A share is " + f.price + ", " + today;
}

function show(f) {
  openSheet({ art: art(f, 4), name: f.name }, sayOf(f), (body) => {
    body.append(chart(f, 96));
    let btns;
    if (f.kind === "good") {
      body.append(tiles(tile("In the bag", f.held), tile("Lots", lots(f)), tile("Lot to buy", f.buy), tile("Sells for", f.sell)));
      btns = [button("Buy a lot for " + f.buy, (b) => trade(f, 1, false, b), "primary"), button("Buy 5 for " + f.buy * 5, (b) => trade(f, 5, false, b))];
      if (lots(f)) btns.push(button("Sell a lot for " + f.sell, (b) => trade(f, 1, true, b)));
      if (lots(f) > 1) btns.push(button("Sell " + Math.min(lots(f), 100) + " lots", (b) => trade(f, Math.min(lots(f), 100), true, b)));
    } else {
      if (f.kind === "share") body.append(tiles(tile("A share pays", f.div.toFixed(2)), tile("A day held", (f.div * f.held).toFixed(1))));
      if (f.held) body.append(holding(f));
      btns = [button("Buy 1 for " + f.buy, (b) => trade(f, 1, false, b), "primary"), button("Buy 5 for " + f.buy * 5, (b) => trade(f, 5, false, b))];
      if (f.held) btns.push(button("Sell 1 for " + f.sell, (b) => sell(f, 1, b)));
      if (f.held > 1) btns.push(button("Sell all for " + f.worth, (b) => sell(f, Math.min(f.held, 100), b)));
    }
    body.append(keys(...btns));
  });
}

function collect(b) {
  b.disabled = true;
  post("/api/market", { collect: true }).then((r) => {
    setPond(r.pond);
    $("mkt-out").textContent = r.pebbles ? "Collected " + r.pebbles + " in dividends." : "";
  }, (e) => { $("mkt-out").textContent = e.message; b.disabled = false; });
}

function card(f) {
  const li = document.createElement("li"), head = document.createElement("div"), what = document.createElement("span"), name = document.createElement("b"), sub = document.createElement("span");
  head.className = "firm";
  what.className = "what";
  name.textContent = f.name;
  sub.className = "dim";
  sub.textContent = f.kind === "good" ? "a lot of " + state.lot + " · " + f.held + " in the bag" : f.kind === "coin" ? "swings hard" : "moves with " + f.moves;
  what.append(name, sub);
  if (story && story.key === f.key) {
    const tag = document.createElement("span");
    tag.className = "newstag " + (story.up ? "up" : "down");
    tag.textContent = "IN THE PAPER";
    what.append(tag);
  }
  const price = document.createElement("span"), dir = f.price > f.prev ? "up" : f.price < f.prev ? "down" : "";
  price.className = "readout px " + dir;
  price.innerHTML = '<span class="v"></span><span class="k"></span>';
  price.firstChild.textContent = f.price + " " + arrow(f);
  price.lastChild.textContent = signed(dayPct(f), "%") + " today";
  head.append(art(f, f.kind === "share" ? 2 : 4), what, price);
  li.append(head, chart(f, f.kind === "coin" ? 120 : 56));
  if (f.held && f.kind !== "good") {
    const own = document.createElement("p"), gain = f.worth - f.cost;
    own.className = "own " + (gain < 0 ? "down" : "up");
    own.textContent = f.held + " held · paid " + f.cost + " · worth " + f.worth + " · " + signed(gain) + " (" + signed(gainPct(f.worth, f.cost), "%") + ")";
    if (f.kind === "share" && f.div) own.textContent += " · pays " + (f.div * f.held).toFixed(1) + " a day";
    li.append(own);
  }
  li.tabIndex = 0;
  li.setAttribute("role", "button");
  li.addEventListener("click", () => show(f));
  li.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); show(f); } });
  return li;
}

const LINE = {
  share: () => "Shares in the town's businesses. Each pays a little every evening it's held.",
  good: (m) => "Goods by the lot of " + m.lot + ", into the bag and out of it. The market pays " + m.room + " more for goods today.",
  coin: () => "Glowworm Coin. Big days both ways, and no telling which.",
  black: () => "",
};

// The tape runs the day's story and then the biggest movers either way.
function tape(m) {
  const el = $("mkt-tape"), bits = [];
  if (m.news) bits.push(["IN THE PAPER  " + m.news.line.toUpperCase(), m.news.up ? "up" : "down"]);
  m.firms.filter((f) => f.prev).sort((a, b) => Math.abs(dayPct(b)) - Math.abs(dayPct(a))).slice(0, 6)
    .forEach((f) => bits.push([f.name.toUpperCase() + " " + f.price + " " + arrow(f) + " " + signed(dayPct(f), "%"), f.price > f.prev ? "up" : f.price < f.prev ? "down" : ""]));
  el.replaceChildren();
  for (let twice = 0; twice < 2; twice++) bits.forEach(([text, cls]) => {
    const b = document.createElement("span");
    b.className = cls; b.textContent = text + "   ·   ";
    el.append(b);
  });
  el.classList.toggle("still", reduced);
}

// A held thing that has a very good day pops up once that day.
function bigDay(p) {
  let seen = {};
  try { seen = JSON.parse(localStorage.getItem("pond-bigday")) || {}; } catch (e) { /* private window */ }
  if (seen.day !== p.today) seen = { day: p.today, keys: [] };
  const big = p.market.firms.filter((f) => f.held && f.kind !== "good" && dayPct(f) >= (f.kind === "coin" ? 12 : 4) && !seen.keys.includes(f.key));
  big.forEach((f) => {
    seen.keys.push(f.key);
    const more = f.held * (f.price - f.prev);
    reveal({ kick: "A big day", art: art(f, 6), name: f.name, glow: 4,
      line: "Up " + dayPct(f) + "% today, so your " + f.held + " are worth about " + more + " more than last night.", ok: "Nice" });
  });
  if (big.length) try { localStorage.setItem("pond-bigday", JSON.stringify(seen)); } catch (e) { /* private window */ }
}

// The portfolio is everything held, what it's made and the best of it.
function portfolio(m) {
  const held = m.firms.filter((f) => f.held && f.kind !== "good");
  const best = held.slice().sort((a, b) => gainPct(b.worth, b.cost) - gainPct(a.worth, a.cost))[0];
  const g = m.worth - m.in;
  openSheet({ bare: true, name: "Your holdings" }, "", (body) => {
    body.append(tiles(tile("Worth now", m.worth), tile("Gain", signed(g), g < 0 ? "down" : "up"),
      tile("On paid", signed(gainPct(m.worth, m.in), "%"), g < 0 ? "down" : "up"), tile("Dividends", m.earned)));
    const say = document.createElement("p");
    say.className = "line";
    say.textContent = best ? "The best of it is " + best.name + ", " + signed(gainPct(best.worth, best.cost), "%") + " on what you paid." : "Nothing held yet. Buy a share or a coin and it shows here.";
    body.append(say);
    const ul = document.createElement("ul");
    ul.className = "shelf";
    held.forEach((f) => {
      const li = document.createElement("li"), what = document.createElement("span"), b = document.createElement("b"), sub = document.createElement("span"), gain = f.worth - f.cost;
      what.className = "what"; sub.className = "sub";
      b.textContent = f.name;
      sub.textContent = f.held + " held · paid " + f.cost + " · worth " + f.worth;
      what.append(b, sub);
      const glass = tile(f === best ? "Best" : "Gain", signed(gain), gain < 0 ? "down" : "up");
      li.append(art(f, f.kind === "share" ? 2 : 3), what, glass);
      li.addEventListener("click", () => show(f));
      ul.append(li);
    });
    if (held.length) body.append(ul);
  });
}

let latest = null;

function render(p) {
  latest = p;
  const m = p.market;
  if (!m) return;
  state.lot = m.lot; state.fees = m.fees;
  story = m.news || null;
  const g = m.worth - m.in;
  $("mkt-tiles").replaceChildren(tile("Worth now", m.worth), tile("Paid", m.in), tile("Gain", m.in ? signed(g) + " · " + signed(gainPct(m.worth, m.in), "%") : "None", g < 0 ? "down" : "up"),
    tile("Room", Math.max(0, m.cap - m.in)));
  const due = $("mkt-div");
  due.hidden = !m.due && !m.daily;
  if (!due.hidden) {
    const say = document.createElement("span"), key = button("Collect", collect, "primary");
    say.className = "what";
    say.textContent = m.due ? m.due + " in dividends" : "Dividends come each evening";
    key.disabled = !m.due;
    due.replaceChildren(iconCanvas("peb", 3), say, tile("A day", m.daily.toFixed(1)), key);
  }
  $("mkt-help").textContent = "Shares are the businesses round the pond, and each one moves with what every pond did in its line of work that day, plus a little drift nobody can see coming. " +
    "A share pays a dividend every evening off how its business did, collected here. Goods are bought and sold by the lot of " + m.lot + ", straight into the bag and out of it, and the more of a thing ponds bring in the cheaper it gets. " +
    "Glowworm Coin swings far more than anything else and is worth about what it costs over time, so it's a gamble. " +
    "Buying and selling cost " + m.fees.share + " in a hundred on shares, " + m.fees.good + " on goods and " + m.fees.coin + " on the coin. " +
    "Tap the readouts at the top for everything you hold. How much you can hold in shares and coin grows with the bank and with frog power. This and the back door are the places pebbles can be lost to chance.";
  const sorts = $("mkt-sorts");
  sorts.replaceChildren(...BUCKETS.map(([k, label]) => {
    const b = button(label, () => { bucket = k; render(p); });
    b.setAttribute("aria-pressed", k === bucket ? "true" : "false");
    return b;
  }));
  $("mkt-open").textContent = LINE[bucket](m);
  tape(m);
  bigDay(p);
  $("mkt-list").replaceChildren(...m.firms.filter((f) => f.kind === bucket).map(card));
  const back = bucket === "black";
  $("mkt-open").hidden = back;
  $("mkt-black").hidden = !back;
  if (back) renderBlack(p, () => { bucket = "coin"; render(p); });
  showBlack(back);
}

export function initMarket() {
  watch(render);
  const t = $("mkt-tiles");
  t.addEventListener("click", () => { const p = latest; if (p) portfolio(p.market); });
}

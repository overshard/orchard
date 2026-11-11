import { $, post, setPond, watch, reduced } from "./api.js";
import { openSheet, sheetSay, button } from "./ui.js";

// Each business has its own awning colour and a mark on its sign.
const LOOK = {
  sp500: ["#3f9a4d", "L"],
  dow: ["#8a5a2b", "B"],
  nasdaq: ["#3d8fd6", "R"],
  russell: ["#2fa59a", "T"],
  gold: ["#e0b030", "G"],
  oil: ["#4a3a2a", "O"],
  bitcoin: ["#b8e05a", "C"],
  vix: ["#7a8798", "S"],
};

const GLYPH = {
  L: ["1..", "1..", "1..", "111"], B: ["11.", "111", "1.1", "111"], R: ["11.", "1.1", "11.", "1.1"],
  T: ["111", ".1.", ".1.", ".1."], G: [".11", "1..", "1.1", ".11"], O: [".1.", "1.1", "1.1", ".1."],
  C: [".11", "1..", "1..", ".11"], S: [".11", ".1.", "..1", "11."],
};

// A shopfront in pixels, an awning in its colour over a door and its sign.
function front(key, s) {
  const [col, g] = LOOK[key] || ["#888", "?"];
  const cv = document.createElement("canvas");
  cv.width = 19 * s; cv.height = 12 * s;
  const x = cv.getContext("2d"), px = (c, r, w, h, f) => { x.fillStyle = f; x.fillRect(c * s, r * s, w * s, h * s); };
  px(1, 3, 17, 9, "#c9b48a");
  for (let i = 0; i < 19; i += 2) { px(i, 1, 1, 3, col); px(i + 1, 1, 1, 3, "#f4f1e6"); }
  px(0, 0, 19, 1, col);
  px(3, 6, 4, 6, "#3a2a1c"); px(6, 9, 1, 1, "#e0b030");
  px(9, 5, 8, 6, "#1b1814");
  (GLYPH[g] || []).forEach((row, r) => [...row].forEach((ch, c) => { if (ch === "1") px(11 + c, 6 + r, 1, 1, col); }));
  cv.setAttribute("aria-hidden", "true");
  return cv;
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
  const paid = f.held ? f.cost / f.held : 0;
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

function tile(k, v, cls) {
  const box = document.createElement("span"), kk = document.createElement("span"), vv = document.createElement("span");
  box.className = "readout " + (cls || ""); kk.className = "k"; vv.className = "v";
  kk.textContent = k; vv.textContent = v;
  box.append(kk, vv);
  return box;
}

function trade(f, n, sell, b) {
  b.disabled = true;
  post("/api/market", { key: f.key, n, sell }).then((v) => {
    setPond(v);
    const now = v.market.firms.find((x) => x.key === f.key);
    show(now);
    sheetSay(sell ? "Sold " + n + "." : "Bought " + n + ".");
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
  openSheet({ art: front(f.key, 4), name: f.name }, "That's " + back + " back for shares that cost " + paid + ", " + gainPct(back, paid) + "%. Prices come back as often as not.", (body) => {
    body.append(keys(button("Wait", () => show(f), "primary"), button("Sell anyway", (bb) => trade(f, n, true, bb))));
  });
}

function holding(f) {
  const t = document.createElement("div"), g = f.worth - f.cost;
  t.className = "tiles";
  t.append(tile("Held", f.held), tile("Paid", f.cost), tile("Worth now", f.worth), tile("If sold", signed(g) + " · " + signed(gainPct(f.worth, f.cost), "%"), g < 0 ? "down" : "up"));
  return t;
}

function show(f) {
  const what = "Follows " + f.follows + ". A share is " + f.price + ", " + signed(dayPct(f), "%") + " today.";
  openSheet({ art: front(f.key, 4), name: f.name }, what, (body) => {
    body.append(chart(f, 96));
    if (f.held) body.append(holding(f));
    const btns = [button("Buy 1 for " + f.buy, (b) => trade(f, 1, false, b), "primary"), button("Buy 5 for " + f.buy * 5, (b) => trade(f, 5, false, b))];
    if (f.held) btns.push(button("Sell 1 for " + f.sell, (b) => sell(f, 1, b)));
    if (f.held > 1) btns.push(button("Sell all for " + f.worth, (b) => sell(f, f.held, b)));
    body.append(keys(...btns));
  });
}

function render(p) {
  const m = p.market;
  if (!m) return;
  const g = m.worth - m.in, tiles = $("mkt-tiles");
  tiles.replaceChildren(tile("Worth now", m.worth), tile("Paid", m.in), tile("Gain", m.in ? signed(g) + " · " + signed(gainPct(m.worth, m.in), "%") : "None", g < 0 ? "down" : "up"),
    tile("Room", Math.max(0, m.cap - m.in)));
  $("mkt-open").textContent = m.open ? "The board is open." : "The board is shut, only Glowworm Coin moves.";
  $("mkt-help").textContent = "Eight businesses round the pond, each following something real: " +
    m.firms.map((f) => f.name + " follows " + f.follows).join(", ") +
    ". A share moves three times the real day and the close is kept each evening. Glowworm Coin trades every day and the rest on weekdays. " +
    "Buying and selling each cost " + m.fee + " in a hundred, and how much you can hold grows with the bank. Prices can fall, and so can what you put in.";
  const tape = $("mkt-tape");
  const bits = m.firms.map((f) => f.name.toUpperCase() + " " + f.price + " " + arrow(f));
  tape.textContent = bits.join("   ·   ") + "   ·   " + bits.join("   ·   ");
  tape.classList.toggle("still", reduced);
  const list = $("mkt-list");
  list.textContent = "";
  m.firms.forEach((f) => {
    const li = document.createElement("li"), head = document.createElement("div"), what = document.createElement("span"), name = document.createElement("b"), sub = document.createElement("span");
    head.className = "firm";
    what.className = "what";
    name.textContent = f.name;
    sub.className = "dim";
    sub.textContent = "follows " + f.follows;
    what.append(name, sub);
    const price = document.createElement("span"), dir = f.price > f.prev ? "up" : f.price < f.prev ? "down" : "";
    price.className = "readout px " + dir;
    price.innerHTML = '<span class="v"></span><span class="k"></span>';
    price.firstChild.textContent = f.price + " " + arrow(f);
    price.lastChild.textContent = signed(dayPct(f), "%") + " today";
    head.append(front(f.key, 2), what, price);
    li.append(head, chart(f, 56));
    if (f.held) {
      const own = document.createElement("p"), gain = f.worth - f.cost;
      own.className = "own " + (gain < 0 ? "down" : "up");
      own.textContent = f.held + " held · paid " + f.cost + " · worth " + f.worth + " · " + signed(gain) + " (" + signed(gainPct(f.worth, f.cost), "%") + ")";
      li.append(own);
    }
    li.tabIndex = 0;
    li.setAttribute("role", "button");
    li.addEventListener("click", () => show(f));
    li.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); show(f); } });
    list.append(li);
  });
}

export function initMarket() { watch(render); }

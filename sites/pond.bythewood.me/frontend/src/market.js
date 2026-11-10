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

// Thirty days of closes as a stepped pixel line in readout glass.
function chart(days) {
  const cv = document.createElement("canvas");
  cv.width = 240; cv.height = 96; cv.className = "chart";
  const x = cv.getContext("2d");
  x.fillStyle = "#0b1006"; x.fillRect(0, 0, 240, 96);
  x.fillStyle = "rgba(125,141,98,0.18)";
  for (let y = 16; y < 96; y += 24) x.fillRect(0, y, 240, 1);
  if (days.length < 2) {
    x.fillStyle = "#7d8d62"; x.font = "12px 'JetBrains Mono', monospace";
    x.fillText("A chart after two days", 44, 52);
    return cv;
  }
  const lo = Math.min(...days), hi = Math.max(...days), span = Math.max(1, hi - lo), step = 240 / (days.length - 1);
  const yOf = (v) => 84 - Math.round(((v - lo) / span) * 72 / 4) * 4;
  x.fillStyle = days[days.length - 1] >= days[0] ? "#ffb000" : "#e2567a";
  for (let i = 0; i < days.length; i++) {
    const x0 = Math.round(i * step / 4) * 4, y0 = yOf(days[i]);
    x.fillRect(x0 - 2, y0, 4, 4);
    if (i > 0) {
      const xp = Math.round((i - 1) * step / 4) * 4, yp = yOf(days[i - 1]);
      for (let xx = xp; xx < x0; xx += 4) x.fillRect(xx, yp, 4, 4);
      for (let yy = Math.min(yp, y0); yy <= Math.max(yp, y0); yy += 4) x.fillRect(x0 - 2, yy, 4, 4);
    }
  }
  return cv;
}

function arrow(f) { return f.price > f.prev ? "▲" : f.price < f.prev ? "▼" : "·"; }

function trade(f, n, sell, b) {
  b.disabled = true;
  post("/api/market", { key: f.key, n, sell }).then((v) => {
    setPond(v);
    const now = v.market.firms.find((x) => x.key === f.key);
    show(now);
    sheetSay(sell ? "Sold " + n + "." : "Bought " + n + ".");
  }, (e) => { sheetSay(e.message); b.disabled = false; });
}

// Selling under what was paid asks first, with waiting as the big key.
function sell(f, n, b) {
  const back = f.sell * n, paid = Math.round(f.cost * n / f.held);
  if (back >= paid) return trade(f, n, true, b);
  openSheet({ art: front(f.key, 4), name: f.name }, "That's " + back + " back for shares that cost " + paid + ". Prices come back as often as not.", (body) => {
    body.append(button("Wait", () => show(f), "primary"), button("Sell anyway", (bb) => trade(f, n, true, bb)));
  });
}

function show(f) {
  const what = f.held
    ? "You hold " + f.held + ", paid " + f.cost + ", worth " + f.worth + " if sold now."
    : "Follows " + f.follows + ". A share is " + f.price + " and costs " + f.buy + " with the fee.";
  openSheet({ art: front(f.key, 4), name: f.name }, what, (body) => {
    body.append(chart(f.days));
    body.append(button("Buy 1 for " + f.buy, (b) => trade(f, 1, false, b), "primary"));
    body.append(button("Buy 5", (b) => trade(f, 5, false, b)));
    if (f.held) body.append(button("Sell 1 for " + f.sell, (b) => sell(f, 1, b)));
    if (f.held > 1) body.append(button("Sell all " + f.held, (b) => sell(f, f.held, b)));
  });
}

function render(p) {
  const m = p.market;
  if (!m) return;
  $("mkt-worth").textContent = m.worth;
  $("mkt-room").textContent = Math.max(0, m.cap - m.in);
  $("mkt-open").textContent = m.open ? "Open" : "Shut";
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
    const li = document.createElement("li"), what = document.createElement("span"), name = document.createElement("b"), sub = document.createElement("span");
    what.className = "what";
    name.textContent = f.name;
    sub.className = "dim";
    sub.textContent = f.held ? f.held + " held, worth " + f.worth : "follows " + f.follows;
    what.append(name, sub);
    const price = document.createElement("span");
    price.className = "readout px " + (f.price > f.prev ? "up" : f.price < f.prev ? "down" : "");
    price.innerHTML = '<span class="v"></span>';
    price.firstChild.textContent = f.price + " " + arrow(f);
    li.append(front(f.key, 2), what, price);
    li.tabIndex = 0;
    li.setAttribute("role", "button");
    li.addEventListener("click", () => show(f));
    li.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); show(f); } });
    list.append(li);
  });
}

export function initMarket() { watch(render); }

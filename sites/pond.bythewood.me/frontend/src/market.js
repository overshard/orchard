import { $, post, setPond, watch } from "./api.js";
import { openSheet, sheetSay, button, reveal } from "./ui.js";
import { iconCanvas } from "./icons.js";
import { art as folkArt } from "./neighbours.js";
import { renderBlack, showBlack } from "./black.js";

const SVG = "http://www.w3.org/2000/svg";

// The icon for a thing on the board, a good by its item.
function iconOf(f) { return f.kind === "good" ? f.item : f.kind; }

function art(f, s) {
  const cv = iconCanvas(iconOf(f), s);
  cv.setAttribute("aria-hidden", "true");
  return cv;
}

function arrow(f) { return f.price > f.prev ? "▲" : f.price < f.prev ? "▼" : "·"; }
function signed(n, unit) { return (n > 0 ? "+" : "") + n + (unit || ""); }
function dayPct(f) { return f.prev ? Math.round((f.price - f.prev) / f.prev * 1000) / 10 : 0; }
function gainPct(worth, cost) { return cost ? Math.round((worth - cost) / cost * 1000) / 10 : 0; }
function dir(f) { return f.price > f.prev ? "up" : f.price < f.prev ? "down" : ""; }
function lots(f) { return Math.floor(f.held / state.lot); }
function points(f) { return f.days.slice(-14).concat(f.ticks.length ? f.ticks : [f.price]); }

const state = { lot: 10, melon: null };

// The closes and today's prices as a line, green up and red down.
function spark(f) {
  const pts = points(f), w = 70, h = 26, lo = Math.min(...pts), hi = Math.max(...pts), span = Math.max(1, hi - lo);
  const svg = document.createElementNS(SVG, "svg"), line = document.createElementNS(SVG, "polyline");
  svg.setAttribute("viewBox", "0 0 " + w + " " + h);
  svg.setAttribute("aria-hidden", "true");
  line.setAttribute("points", pts.map((v, i) => (i * (w - 2) / Math.max(1, pts.length - 1) + 1).toFixed(1) + "," + (h - 2 - (v - lo) / span * (h - 4)).toFixed(1)).join(" "));
  line.setAttribute("class", dir(f) || "flat");
  svg.append(line);
  return svg;
}

// A bigger chart for the sheet, stepped in pixels, with a dashed line where
// the holder paid.
function chart(f, h) {
  const pts = f.days.concat(f.ticks.length ? f.ticks : [f.price]), W = 320;
  if (pts[pts.length - 1] !== f.price) pts.push(f.price);
  const cv = document.createElement("canvas");
  cv.width = W; cv.height = h; cv.className = "chart";
  const x = cv.getContext("2d");
  x.fillStyle = "#0a1020"; x.fillRect(0, 0, W, h);
  x.fillStyle = "#22304f";
  for (let y = 12; y < h; y += 20) x.fillRect(0, y, W, 1);
  const paid = f.held && f.kind !== "good" ? f.cost / f.held : 0;
  const lo = Math.min(...pts, paid || Infinity), hi = Math.max(...pts, paid), span = Math.max(4, hi - lo), step = (W - 8) / Math.max(1, pts.length - 1);
  const yOf = (v) => h - 10 - Math.round(((v - lo) / span) * (h - 24) / 4) * 4;
  if (paid) { x.fillStyle = "rgba(238,240,230,0.35)"; for (let xx = 0; xx < W; xx += 8) x.fillRect(xx, yOf(paid) + 1, 4, 1); }
  x.fillStyle = pts[pts.length - 1] >= pts[0] ? "#6fd08c" : "#ff8a7a";
  for (let i = 0; i < pts.length; i++) {
    const x0 = Math.round(i * step / 4) * 4 + 2, y0 = yOf(pts[i]);
    x.fillRect(x0, y0, 4, 4);
    if (i > 0) {
      const xp = Math.round((i - 1) * step / 4) * 4 + 2, yp = yOf(pts[i - 1]);
      for (let xx = xp; xx < x0; xx += 4) x.fillRect(xx, yp, 4, 4);
      for (let yy = Math.min(yp, y0); yy <= Math.max(yp, y0); yy += 4) x.fillRect(x0, yy, 4, 4);
    }
  }
  x.fillStyle = "#9aa6c2"; x.font = "600 10px Inter, sans-serif";
  x.fillText(String(hi), 4, 10); x.fillText(String(lo), 4, h - 2);
  return cv;
}

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

function keys(...btns) {
  const r = document.createElement("div");
  r.className = "trade";
  r.append(...btns);
  return r;
}

function trade(f, n, sell, b) {
  b.disabled = true;
  post("/api/market", { key: f.key, n, sell }).then((v) => {
    setPond(v);
    show(v.market.firms.find((x) => x.key === f.key));
    const what = f.kind === "good" ? (n === 1 ? "a lot" : n + " lots") : f.kind === "melon" ? (n === 1 ? "a melon" : n + " melons") : String(n);
    sheetSay((sell ? "Sold " : "Bought ") + what + ".");
  }, (e) => { sheetSay(e.message); b.disabled = false; });
}

// Selling under what was paid asks first, with waiting as the big key.
function sell(f, n, b) {
  const back = f.sell * n, paid = Math.round(f.cost * n / f.held);
  if (back >= paid || f.kind === "melon" && state.melon.days === 1) return trade(f, n, true, b);
  openSheet({ art: art(f, 4), name: f.name }, "That's " + back + " back for what cost " + paid + ", " + gainPct(back, paid) + "%. Prices come back as often as not.", (body) => {
    body.append(keys(button("Wait", () => show(f), "primary"), button("Sell anyway", (bb) => trade(f, n, true, bb))));
  });
}

function holding(f) {
  const g = f.worth - f.cost;
  return tiles(tile("Held", f.held), tile("Paid", f.cost), tile("Worth now", f.worth),
    tile("If sold", signed(g) + " · " + signed(gainPct(f.worth, f.cost), "%"), g < 0 ? "down" : "up"));
}

function days(n) { return n === 1 ? "today" : n + " days"; }

function sayOf(f) {
  const today = signed(dayPct(f), "%") + " today. " + f.why + ".";
  if (f.kind === "good") return "The more " + f.moves + " the ponds bring in the cheaper it gets, and the neighbours and the weather push it too. A lot of " + state.lot + " is " + f.price + ", " + today;
  if (f.kind === "melon") {
    const m = state.melon;
    return "Moonmelons come in on Sunday and are only bought then, up to " + m.max + " a week. They sell all week, and any left once Saturday is over spoil. One is " + f.price + ", " + signed(dayPct(f), "%") + " today, with " + days(m.days) + " left to sell.";
  }
  return "Swings far more than anything else on the board, and is worth about what it costs over time. A coin is " + f.price + ", " + today;
}

function show(f) {
  openSheet({ art: art(f, 4), name: f.name }, sayOf(f), (body) => {
    body.append(chart(f, 96));
    let btns = [];
    if (f.kind === "good") {
      body.append(tiles(tile("In the bag", f.held), tile("Lots", lots(f)), tile("Lot to buy", f.buy), tile("Sells for", f.sell)));
      btns = [button("Buy a lot for " + f.buy, (b) => trade(f, 1, false, b), "primary"), button("Buy 5 for " + f.buy * 5, (b) => trade(f, 5, false, b))];
      if (lots(f)) btns.push(button("Sell a lot for " + f.sell, (b) => trade(f, 1, true, b)));
      if (lots(f) > 1) btns.push(button("Sell " + Math.min(lots(f), 100) + " lots", (b) => trade(f, Math.min(lots(f), 100), true, b)));
    } else {
      if (f.held) body.append(holding(f));
      const left = f.kind === "melon" ? state.melon.left : 100;
      if (left >= 1) btns.push(button("Buy 1 for " + f.buy, (b) => trade(f, 1, false, b), "primary"));
      if (left >= 5) btns.push(button("Buy 5 for " + f.buy * 5, (b) => trade(f, 5, false, b)));
      if (f.held) btns.push(button("Sell 1 for " + f.sell, (b) => sell(f, 1, b), left ? "" : "primary"));
      if (f.held > 1) btns.push(button("Sell all for " + f.worth, (b) => sell(f, Math.min(f.held, 100), b)));
    }
    if (btns.length) body.append(keys(...btns));
  });
}

function tick(f) {
  const li = document.createElement("li"), t = document.createElement("span"), name = document.createElement("b"), why = document.createElement("span");
  const pr = document.createElement("span"), price = document.createElement("b"), ch = document.createElement("span");
  t.className = "t"; pr.className = "pr";
  name.textContent = f.name;
  why.textContent = f.why;
  t.append(name, why);
  price.textContent = f.price;
  ch.className = dir(f) || "flat";
  ch.textContent = arrow(f) + " " + Math.abs(dayPct(f)) + "%";
  pr.append(price, ch);
  li.append(art(f, 2), t, spark(f), pr);
  li.tabIndex = 0;
  li.setAttribute("role", "button");
  li.setAttribute("aria-label", f.name + ", " + f.price + (f.kind === "good" ? " a lot" : "") + ", " + signed(dayPct(f), "%") + " today");
  li.addEventListener("click", () => show(f));
  li.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); show(f); } });
  return li;
}

const faces = new Map();

function face(n) {
  if (!faces.has(n.seed)) faces.set(n.seed, folkArt(n, 1));
  const src = faces.get(n.seed), cv = document.createElement("canvas");
  cv.width = src.width; cv.height = src.height;
  cv.getContext("2d").drawImage(src, 0, 0);
  cv.setAttribute("aria-hidden", "true");
  return cv;
}

// The tape is the neighbours' latest trades, newest first.
function tape(p) {
  const all = (p.neighbours && p.neighbours.all) || [];
  $("mkt-tape").replaceChildren(...p.market.tape.map((r) => {
    const li = document.createElement("li"), say = document.createElement("span"), who = document.createElement("b"), at = document.createElement("small");
    who.textContent = r.name;
    say.append(who, " " + r.line);
    at.textContent = r.at;
    li.append(all[r.who] ? face(all[r.who]) : document.createElement("span"), say, at);
    return li;
  }));
  $("mkt-tape").hidden = !p.market.tape.length;
}

// The melon cart's row, lit on a Sunday while there are melons to buy.
function melonRow(m, f) {
  const row = $("mkt-melon"), key = $("mkt-melon-go");
  let head = "Moonmelons", line = "Back on Sunday, when the cart comes in", go = "";
  if (m.open && m.left) {
    head = "Moonmelons are in"; line = "Buy today only, they spoil Saturday night"; go = "Buy";
  } else if (f.held) {
    head = f.held + " moonmelon" + (f.held === 1 ? "" : "s");
    line = "Worth " + f.worth + ", " + (m.days === 1 ? "they spoil tonight" : days(m.days) + " left to sell"); go = "Sell";
  }
  row.classList.toggle("pays", go === "Buy" || go === "Sell" && m.days === 1);
  $("mkt-melon-head").textContent = head;
  $("mkt-melon-line").textContent = line;
  key.textContent = go || "Look";
  key.classList.toggle("primary", go === "Buy" || go === "Sell" && m.days === 1);
}

// A held thing that has a very good day pops up once that day.
function bigDay(p) {
  let seen = {};
  try { seen = JSON.parse(localStorage.getItem("pond-bigday")) || {}; } catch (e) { /* private window */ }
  if (seen.day !== p.today) seen = { day: p.today, keys: [] };
  const big = p.market.firms.filter((f) => f.held && f.kind !== "good" && dayPct(f) >= (f.kind === "coin" ? 12 : 8) && !seen.keys.includes(f.key));
  big.forEach((f) => {
    seen.keys.push(f.key);
    const more = f.held * (f.price - f.prev);
    reveal({ kick: "A big day", art: art(f, 6), name: f.name, glow: 4,
      line: "Up " + dayPct(f) + "% today, so your " + f.held + " are worth about " + more + " more than last night.", ok: "Nice" });
  });
  if (big.length) try { localStorage.setItem("pond-bigday", JSON.stringify(seen)); } catch (e) { /* private window */ }
}

// What's held is the coin and the melons, what it's made and the best of it.
function portfolio(m) {
  const held = m.firms.filter((f) => f.held && f.kind !== "good");
  const g = m.worth - m.in;
  openSheet({ bare: true, name: "What you hold" }, "", (body) => {
    body.append(tiles(tile("Worth now", m.worth), tile("Paid", m.in), tile("Gain", signed(g), g < 0 ? "down" : "up"),
      tile("Room", Math.max(0, m.cap - m.in))));
    const say = document.createElement("p");
    say.className = "line";
    say.textContent = held.length ? "Tap one to trade it." : "Nothing held yet. Glow coin and moonmelons show here once bought, and goods go in the bag.";
    body.append(say);
    const ul = document.createElement("ul");
    ul.className = "shelf";
    held.forEach((f) => {
      const li = document.createElement("li"), what = document.createElement("span"), b = document.createElement("b"), sub = document.createElement("span"), gain = f.worth - f.cost;
      what.className = "what"; sub.className = "sub";
      b.textContent = f.name;
      sub.textContent = f.held + " held · paid " + f.cost + " · worth " + f.worth;
      what.append(b, sub);
      li.append(art(f, 3), what, tile("Gain", signed(gain), gain < 0 ? "down" : "up"));
      li.addEventListener("click", () => show(f));
      ul.append(li);
    });
    if (held.length) body.append(ul);
  });
}

let latest = null, back = false;

function render(p) {
  latest = p;
  const m = p.market;
  if (!m) return;
  state.lot = m.lot; state.melon = m.melon;
  const melon = m.firms.find((f) => f.kind === "melon");
  melonRow(m.melon, melon);
  const g = m.worth - m.in;
  $("mkt-tiles").replaceChildren(tile("Held", m.worth), tile("Paid", m.in), tile("Gain", m.in ? signed(g) : "None", g < 0 ? "down" : "up"),
    tile("Room", Math.max(0, m.cap - m.in)));
  $("mkt-help").textContent = "Reeds, clay, fish and sticks are bought and sold by the lot of " + m.lot + ", straight into the bag and out of it. The more of a thing the ponds bring in the cheaper it gets, and the neighbours trade every hour, so a price moves even on a quiet night. " +
    "Rain, cold, the season and the paper push goods too, and the line under each says why. The market pays " + m.room + " more for goods today. " +
    "Glow coin swings far more than anything else and is worth about what it costs over time. Moonmelons are only bought on Sunday and spoil once Saturday is over, so sell them before then. " +
    "Buying and selling cost " + m.fees.good + " in a hundred on goods, " + m.fees.coin + " on the coin and " + m.fees.melon + " on melons. " +
    "How much coin and melon you can hold grows with the bank and with frog power. The board and the back door are the places pebbles can be lost to chance.";
  $("mkt-board").replaceChildren(...m.firms.map(tick));
  tape(p);
  bigDay(p);
  const b = p.black;
  $("mkt-back").classList.toggle("pays", b.open);
  $("mkt-back-line").textContent = b.open ? "Open now, deals for glow coin" : "Shut till " + b.back;
  $("mkt-back-go").classList.toggle("primary", b.open && !back);
  $("mkt-back-go").textContent = back ? "Close" : b.open ? "Knock" : "Look";
  $("mkt-black").hidden = !back;
  if (back) renderBlack(p, () => show(m.firms.find((f) => f.kind === "coin")));
  showBlack(back);
}

export function initMarket() {
  watch(render);
  $("mkt-tiles").addEventListener("click", () => { if (latest) portfolio(latest.market); });
  $("mkt-melon-go").addEventListener("click", () => { if (latest) show(latest.market.firms.find((f) => f.kind === "melon")); });
  $("mkt-back-go").addEventListener("click", () => { back = !back; if (latest) render(latest); });
}

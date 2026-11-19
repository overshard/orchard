import { $, post, state, setPond, watch, reduced, TIERS } from "./api.js";
import { genome, cells, colours, W, H } from "./frog.js";
import { draw } from "./lit.js";
import { presentsFrom } from "./neighbours.js";
import { initMarket } from "./market.js";
import { drawTown, drawShop, drawStall, drawMarket } from "./scenes.js";
import { slot, thingArt, button, reveal, ghost, taperMeter, TIER_GLOW } from "./ui.js";

let tab = "neighbours";

function showTab(next) {
  tab = next;
  ["shops", "neighbours", "stall", "market"].forEach((k) => { $("town-" + k).hidden = k !== next; });
  document.querySelectorAll("#town-tabs button").forEach((b) => b.setAttribute("aria-pressed", b.dataset.town === next ? "true" : "false"));
  drawTownArt();
}

export function enterTown(which) {
  // The market key is hidden until the market opens, and the place behind it with it.
  if (!which && document.querySelector('#town-tabs button[data-town="' + tab + '"]').hidden) tab = "neighbours";
  showTab(which || tab);
}
export function leaveTown() {}
export function enterWizard() { wizOn = true; cancelAnimationFrame(wizRaf); wizRaf = requestAnimationFrame(wizTick); }
export function leaveWizard() { wizOn = false; }

// ---------------------------------------------------------------- boxes

function boxBuy(id, path, shop) {
  $(id).addEventListener("click", function () {
    this.disabled = true;
    post(path).then((d) => {
      const z = d.prize, hat = z.hat;
      setPond(d.pond);
      const kind = hat ? null : z.item;
      reveal({
        kick: z.spare ? "A spare" : "New!",
        art: thingArt(hat ? { hat } : { kind }, 8),
        name: hat ? z.hat_name : z.text,
        tier: z.hat_tier,
        line: z.spare ? "You had one already, so " + shop + " will buy this one." : hat ? "It's in your bag. Tap it there to wear it." : "It's in your bag. Tap it there to set it out.",
      });
    }, (e) => { $("shop-out-" + (shop === "Little Tully" ? "wren" : "moss")).textContent = e.message; this.disabled = false; });
  });
}

// A meter fills a notch a box with no rare in it, and a full one makes the
// next box a rare or better.
function pity(id, m) {
  $(id).style.transform = "scaleX(" + m.count / m.at + ")";
  $(id + "-n").textContent = m.in <= 1 ? "The next box is a sure rare or better." : "Sure rare or better in " + m.in + " boxes.";
}

function renderBoxes(p) {
  const hb = $("box-open"), tb = $("trinket-box");
  hb.textContent = p.box.cost; hb.disabled = p.pebbles < p.box.cost;
  tb.textContent = p.trinkets.box_cost; tb.disabled = p.pebbles < p.trinkets.box_cost;
  $("box-odds").textContent = p.box.odds.map((o, t) => o + "% " + TIERS[t].toLowerCase()).slice(2).join(", ")
    + ". A rare or better every " + (p.box.pity.at + 1) + " boxes at worst.";
  pity("box-pity", p.box.pity);
  pity("trinket-pity", p.trinkets.pity);
}

// ---------------------------------------------------------------- the stall

function pct(c) { return c >= 0.095 ? Math.round(c * 100) + "%" : c >= 0.01 ? (c * 100).toFixed(1) + "%" : "under 1%"; }
function num(n) { return n.toLocaleString("en-US"); }

const KEYS = { quick: "Quick", fair: "Fair", greedy: "Greedy" };

// How soon a price sells, off the chance of each hourly look over its day.
// A Greedy price held under a shop's can sell as soon as Fair.
function soon(c) { return c >= 0.95 ? "1h" : 1 - Math.pow(1 - c, 24) >= 0.99 ? "today" : "maybe"; }

// A word on why a thing goes for what it does today.
function noteOf(t) {
  const d = t.rate / t.worth, have = t.sort === "hat" || t.sort === "trinket" ? t.free + " spare" : "×" + t.free;
  const why = t.listed >= 6 ? t.listed + " up on stalls lately" : t.wanted ? "wanted for something"
    : d >= 1.5 ? "in demand today" : d >= 1.15 ? "wanted today" : d <= 0.9 ? "slow today" : "";
  return why ? have + " · " + why : have;
}

// The three price keys for a thing, each what it asks and how soon it goes.
// done gets the pond and a line once it's up.
export function priceKeys(t, done, fail) {
  const p = state.pond, box = document.createElement("div"), full = p.stall.rows.length >= p.stall.slots;
  box.className = "pricekeys";
  t.keys.forEach((k) => {
    const b = document.createElement("button"), what = document.createElement("small"), price = document.createElement("span");
    b.type = "button";
    if (k.key === "fair") b.className = "primary";
    what.textContent = KEYS[k.key] + " · " + soon(k.chance);
    price.textContent = num(k.price);
    b.append(what, price);
    if (k.fee) { const fee = document.createElement("small"); fee.textContent = "fee " + num(k.fee); b.append(fee); }
    b.disabled = full || k.fee > p.pebbles;
    b.setAttribute("aria-label", KEYS[k.key] + ", sells " + soon(k.chance) + ", " + k.price + " pebbles" + (k.fee ? ", fee " + k.fee : ""));
    b.addEventListener("click", () => {
      box.querySelectorAll("button").forEach((x) => { x.disabled = true; });
      post("/api/stall/list", { kind: t.kind, key: k.key }).then((v) => done(v, t.name + " is up for " + num(k.price) + "."), (e) => {
        fail(e.message);
        box.querySelectorAll("button").forEach((x) => { x.disabled = false; });
      });
    });
    box.append(b);
  });
  return box;
}

let said = null, collecting = false;

function renderSell(p) {
  const t = p.taper;
  $("sell-taper").replaceChildren(taperMeter(t));
  $("sell-now").textContent = t.sold < t.full ? "What sells now pays in full, up to " + t.full + " today."
    : t.sold < t.half ? "What sells now pays half, until " + t.half + " is sold today."
    : "What sells now pays 1 pebble in " + t.tail + " until midnight.";
  const list = $("sell-list"), can = p.bag.filter((t) => t.free > 0 && t.keys);
  list.textContent = "";
  $("sell-none").hidden = can.length > 0;
  can.sort((a, b) => b.trend - a.trend || b.rate - a.rate);
  can.forEach((t) => {
    const item = document.createElement("div"), what = document.createElement("span"), name = document.createElement("b"), note = document.createElement("span"), trend = document.createElement("span");
    item.className = "sellitem";
    name.textContent = t.name;
    note.textContent = noteOf(t);
    if (said && said.kind === t.kind) { note.textContent = said.line; note.className = "said"; }
    what.className = "what";
    what.append(name, note);
    trend.className = "trend" + (t.trend > 0 ? " rise" : t.trend < 0 ? " fall" : "");
    trend.textContent = t.trend > 0 ? "▲ " + t.trend + "%" : t.trend < 0 ? "▼ " + -t.trend + "%" : "Steady";
    item.append(thingArt(t, 3), what, trend, priceKeys(t, (v, line) => {
      said = { kind: t.kind, line };
      setPond(v);
      if (!v.bag.some((x) => x.kind === t.kind && x.free > 0)) $("stall-out").textContent = line;
    }, (msg) => { $("stall-out").textContent = msg; }));
    list.append(item);
  });
}

function renderStall(p) {
  const st = p.stall, box = $("stall-rows");
  // Nothing waits on the stall, so a sale the page sees is collected there and then.
  if (st.takings && !collecting) {
    collecting = true;
    post("/api/stall/collect").then((d) => {
      collecting = false;
      $("stall-out").textContent = d.line;
      setPond(d.pond);
      presentsFrom(d);
    }, () => { collecting = false; });
  }
  renderSell(p);
  box.textContent = "";
  const now = Math.floor(Date.now() / 1000);
  $("stall-n").textContent = st.rows.length + " of " + st.slots + " up";
  st.rows.forEach((r) => {
    const row = document.createElement("div");
    row.className = "stallrow" + (r.sold ? " sold" : r.over ? " over" : "");
    const what = document.createElement("span"), name = document.createElement("b"), note = document.createElement("span"), price = document.createElement("span");
    name.textContent = r.name;
    note.className = "dim";
    note.textContent = r.sold ? "Sold to " + r.buyer + "!" : r.over ? "No takers at that price"
      : pct(r.chance) + " an hour · " + Math.max(1, Math.ceil((r.ends - now) / 3600)) + "h left";
    what.append(name, note);
    price.className = "price"; price.textContent = num(r.price);
    row.append(thingArt(r, 4), what, price);
    if (!r.sold) {
      row.append(button("Take back", (b) => {
        b.disabled = true;
        const before = state.pond.pebbles;
        post("/api/stall/take", { slot: r.slot }).then((v) => {
          setPond(v);
          // It may have sold since the page last heard.
          $("stall-out").textContent = v.pebbles > before ? "Sold! +" + (v.pebbles - before) + " pebbles." : r.name + " is back in your bag.";
        }, (e) => { $("stall-out").textContent = e.message; b.disabled = false; });
      }));
    }
    box.append(row);
  });
  drawTownArt();
}

// The scene over town is the jetty, a shopfront, the stall or the market by
// the tab, and every shop below the first has its own front over it.
function drawTownArt() {
  const p = state.pond;
  if (!p) return;
  const cv = $("town-art");
  if (tab === "shops") {
    drawShop(cv, p, "wren");
    document.querySelectorAll(".shopfront").forEach((c) => drawShop(c, p, c.dataset.shop));
  } else if (tab === "stall") drawStall(cv, p);
  else if (tab === "market") drawMarket(cv, p);
  else drawTown(cv, p);
}

// ---------------------------------------------------------------- the wizard

let wizOn = false, wizRaf = 0, pick = "", casting = 0, castTier = 0;
const WIZ = 50311;
let wizG = null;

export function toWizard(kind) {
  pick = kind;
  location.hash = "wizard";
  renderWizard(state.pond);
}

function wizTick(now) {
  if (!wizOn) return;
  drawWizard(now);
  wizRaf = requestAnimationFrame(wizTick);
}

function drawWizard(now) {
  const cv = $("wiz-art"), x = cv.getContext("2d"), t = now / 1000;
  const k = casting ? Math.min(1, (now - casting) / 1700) : 0;
  x.fillStyle = "#1a1226"; x.fillRect(0, 0, 352, 232);
  for (let i = 0; i < 24; i++) {
    const sx = (i * 97) % 352, sy = (i * 53) % 120, tw = Math.sin(t * 2 + i) > 0.3;
    x.fillStyle = tw ? "#fff6dc" : "#6a5a8a"; x.fillRect(sx, sy, 2, 2);
  }
  x.fillStyle = "#2a2036"; x.fillRect(0, 196, 352, 36);
  x.fillStyle = "#3a2c4a"; for (let i = 0; i < 352; i += 32) x.fillRect(i + ((i / 32) % 2) * 16, 196, 2, 36);
  if (!wizG) wizG = genome(WIZ);
  const lift = casting && k < 1 ? Math.round(Math.abs(Math.sin(k * Math.PI * 4)) * 8) : 0;
  draw(x, cells(wizG, "elder", { hat: 8, legs: true, blink: (t % 3.7) < 0.15, croak: casting ? 0.8 : 0 }), W, H, colours(wizG, { shape: 8, colour: 2 }), 4, 52 - lift, 4);
  // The cauldron, bubbling, and glowing the colour of what comes out.
  const glow = casting ? TIER_GLOW[castTier] : "#7fd6c2";
  for (let yy = 0; yy < 12; yy++) for (let xx = 0; xx < 26; xx++) {
    const a = (xx - 12.5) / 13, b = (yy - 1) / 11;
    if (a * a + b * b > 1 && yy > 1) continue;
    x.fillStyle = yy < 2 ? glow : "#2e2e33";
    if (yy >= 2 && xx > 4 && xx < 9 && yy < 6) x.fillStyle = "#4a4a52";
    x.fillRect(216 + xx * 4, 148 + yy * 4, 4, 4);
  }
  x.fillStyle = "#1f1f24"; x.fillRect(224, 192, 8, 6); x.fillRect(304, 192, 8, 6);
  x.fillStyle = "#e86a1a"; for (let i = 0; i < 5; i++) if (Math.sin(t * 9 + i * 2) > -0.2) x.fillRect(236 + i * 14, 196 + (i % 2) * 2, 6, 4);
  for (let i = 0; i < 5; i++) {
    const bt = (t * 0.6 + i / 5) % 1, bx = 228 + ((i * 37) % 80), by = 146 - bt * (casting ? 120 : 40);
    x.globalAlpha = 1 - bt; x.fillStyle = glow; x.fillRect(Math.round(bx), Math.round(by), 6, 6);
  }
  x.globalAlpha = 1;
  if (casting) {
    for (let i = 0; i < 18; i++) {
      const a = i / 18 * Math.PI * 2 + t * 5, r = 20 + k * 80;
      x.fillStyle = i % 3 ? glow : "#fff6dc";
      x.fillRect(Math.round(268 + Math.cos(a) * r), Math.round(130 + Math.sin(a) * r * 0.6), 4, 4);
    }
    if (pick) {
      const a = thingArt(state.pond.bag.find((b) => b.kind === pick) || { kind: pick }, 4);
      x.globalAlpha = Math.max(0, 1 - k * 1.4);
      x.drawImage(a, 268 - a.width / 2, 40 + k * 90);
      x.globalAlpha = 1;
    }
  } else {
    // A spark off the wand end of the hat now and then.
    if (Math.sin(t * 1.7) > 0.8) { x.fillStyle = "#ffe27a"; x.fillRect(150, 40 + Math.round(Math.sin(t * 9) * 4), 4, 4); }
  }
}

const LINES = {
  up: ["It worked! Even I'm surprised.", "Ha! Better than it went in.", "Now that's a spell."],
  same: ["Something different, much the same.", "A sideways sort of spell.", "Not better, not worse. Different."],
  down: ["Oh dear. That one fizzled a bit.", "Hm. Smaller than I hoped.", "Magic is mostly this, to be honest."],
  dust: ["Poof. Just dust, and a couple of pebbles in it.", "Gone. Sorry about that."],
};

function cast() {
  const p = state.pond, t = p.bag.find((b) => b.kind === pick);
  if (!t || casting) return;
  $("wiz-cast").disabled = true;
  post("/api/wizard", { kind: pick }).then((d) => {
    castTier = d.went === "dust" ? 0 : d.tier;
    casting = performance.now();
    $("wiz-line").textContent = "Bubble, bubble...";
    setTimeout(() => {
      casting = 0;
      const lines = LINES[d.went];
      $("wiz-line").textContent = lines[Math.floor(Math.random() * lines.length)];
      setPond(d.pond);
      if (!state.pond.bag.some((b) => b.kind === pick && b.free >= p.wizard.takes)) pick = "";
      renderWizard(state.pond);
      if (d.went === "dust") reveal({ kick: "Poof", art: thingArt({ kind: "peb" }, 8), name: "+" + d.dust + " pebbles", line: "Three " + t.name.toLowerCase() + " went up in smoke.", bad: true, ok: "Oh well" });
      else reveal({
        kick: d.went === "up" ? "Upgraded!" : d.went === "same" ? "Transformed" : "Downgraded",
        art: thingArt(d, 8), name: d.name, tier: d.tier, bad: d.went === "down",
        line: "From three " + t.name.toLowerCase() + "." + (d.spare ? " You had one already." : ""),
        ok: d.went === "down" ? "Oh well" : "Nice",
      });
    }, reduced ? 100 : 1700);
  }, (e) => { $("wiz-line").textContent = e.message; renderWizard(state.pond); });
}

function renderWizard(p) {
  const wz = p.wizard, can = p.bag.filter((t) => t.free >= wz.takes && t.sort !== "material");
  if (pick && !can.some((t) => t.kind === pick)) pick = "";
  const odds = $("wiz-odds");
  odds.textContent = "";
  [["Better", wz.up, "up"], ["Same tier", wz.same, "same"], ["Worse", 100 - wz.up - wz.same, "down"]].forEach(([l, n, c]) => {
    const s = document.createElement("span");
    s.className = "odd " + c; s.innerHTML = "<b>" + n + "%</b>" + l;
    odds.append(s);
  });
  const grid = $("wiz-grid");
  grid.textContent = "";
  can.forEach((t) => grid.append(slot(thingArt(t, 4), { tier: t.tier, name: t.name, count: t.free, on: t.kind === pick, onClick: () => { pick = t.kind; renderWizard(state.pond); } })));
  $("wiz-none").hidden = can.length > 0;
  const c = $("wiz-cast"), sel = can.find((t) => t.kind === pick);
  c.disabled = !sel || !!casting;
  c.textContent = sel ? "Cast on three " + sel.name.toLowerCase() : "Pick three of something";
}

export function initTown() {
  $("wiz-none").prepend(ghost("i_star"));
  watch(renderBoxes);
  watch(renderStall);
  watch(renderWizard);
  initMarket();
  boxBuy("box-open", "/api/hats/box", "Little Tully");
  boxBuy("trinket-box", "/api/trinkets/box", "Moth");
  $("town-tabs").addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (b) showTab(b.dataset.town);
  });
  $("wiz-cast").addEventListener("click", cast);
}

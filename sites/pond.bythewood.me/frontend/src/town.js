import { $, post, state, setPond, watch, reduced, TIERS } from "./api.js";
import { genome, cells, colours, draw, W, H } from "./frog.js";
import { drawIcon } from "./icons.js";
import { presentsFrom } from "./neighbours.js";
import { initMarket } from "./market.js";
import { slot, thingArt, openSheet, closeSheet, sheetSay, button, reveal, ghost, TIER_GLOW } from "./ui.js";

let tab = "neighbours";

function showTab(next) {
  tab = next;
  ["shops", "neighbours", "stall", "wizard"].forEach((k) => { $("town-" + k).hidden = k !== next; });
  document.querySelectorAll("#town-tabs button").forEach((b) => b.setAttribute("aria-pressed", b.dataset.town === next ? "true" : "false"));
  if (next === "wizard") { wizOn = true; cancelAnimationFrame(wizRaf); wizRaf = requestAnimationFrame(wizTick); } else wizOn = false;
  if (next === "stall") drawStall();
}

export function enterTown(which) { showTab(which || tab); }
export function leaveTown() { wizOn = false; }

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

// The same sums the server does when a thing goes up.
function stallChance(price, rate) { return Math.min(0.95, Math.max(0.0005, 0.9 / Math.pow(price / rate, 3))); }
function stallFee(price, rate) { return price <= rate ? 0 : Math.floor((price - rate + 9) / 10); }
function pct(c) { return c >= 0.095 ? Math.round(c * 100) + "%" : c >= 0.01 ? (c * 100).toFixed(1) + "%" : "under 1%"; }

// How wanted a thing is today, as a word for its tile.
function demand(t) {
  const d = t.rate / t.worth;
  return d >= 1.5 ? "In demand" : d >= 1.15 ? "Wanted" : d <= 0.7 ? "Flooded" : d <= 0.9 ? "Slow" : "";
}

export function pricePicker(body, t) {
  const lo = Math.max(1, Math.ceil(t.worth * 0.5)), hi = t.max;
  // The slider steps finely near the rate and coarsely toward ten times.
  const steps = [];
  for (let m = 0.5; m <= 10.001; m += m < 2 ? 0.05 : m < 5 ? 0.25 : 0.5) {
    const v = Math.max(lo, Math.min(hi, Math.round(t.worth * m)));
    if (steps[steps.length - 1] !== v) steps.push(v);
  }
  if (!steps.includes(t.rate) && t.rate >= lo && t.rate <= hi) { steps.push(t.rate); steps.sort((a, b) => a - b); }
  const near = steps.reduce((a, v, i) => (Math.abs(v - t.rate) < Math.abs(steps[a] - t.rate) ? i : a), 0);
  const wrap = document.createElement("div"), price = document.createElement("b"), range = document.createElement("input"), odds = document.createElement("div");
  wrap.className = "pricer";
  odds.className = "tiles";
  range.type = "range"; range.min = 0; range.max = steps.length - 1; range.value = near; range.step = 1;
  range.setAttribute("aria-label", "Price");
  const tile = (k) => {
    const box = document.createElement("span"), kk = document.createElement("span"), v = document.createElement("span");
    box.className = "readout"; kk.className = "k"; v.className = "v"; kk.textContent = k;
    box.append(kk, v); odds.append(box);
    return v;
  };
  const hour = tile("Sells within the hour"), day = tile("Sells by tomorrow"), fee = tile("Fee"), word = document.createElement("span");
  let go = null;
  const upd = () => {
    const v = steps[+range.value], c = stallChance(v, t.rate), f = stallFee(v, t.rate), r = v / t.rate;
    price.textContent = v.toLocaleString("en-US");
    hour.textContent = pct(c); day.textContent = pct(1 - Math.pow(1 - c, 24)); fee.textContent = f || "None";
    const want = demand(t);
    word.textContent = (want ? want + " today. " : "") + "Goes for about " + t.rate.toLocaleString("en-US") + ". "
      + (r < 0.95 ? "A bargain." : r < 1.05 ? "The going rate." : r < 2 ? "A bit dear." : r < 4 ? "A long shot." : "Somebody might be daft enough.")
      + (f ? " The fee isn't given back." : "");
    if (go) go.disabled = f > state.pond.pebbles;
  };
  range.addEventListener("input", upd);
  wrap.append(price, range, odds, word);
  const put = button("Put it up", (b) => {
    b.disabled = true;
    post("/api/stall/list", { kind: t.kind, price: steps[+range.value] }).then((v) => {
      setPond(v);
      closeSheet();
      location.hash = "town";
      showTab("stall");
      $("stall-out").textContent = t.name + " is on your stall.";
    }, (e) => { sheetSay(e.message); b.disabled = false; });
  }, "primary");
  go = put;
  upd();
  body.append(wrap, put);
}

function pickForStall() {
  const p = state.pond, can = p.bag.filter((t) => t.free > 0);
  openSheet({ kind: "i_town", name: "Your stall" }, can.length ? "Pick something to sell." : "Nothing to sell yet. A hat or trinket has to be a spare.", (body) => {
    const grid = document.createElement("div");
    grid.className = "slots";
    can.sort((a, b) => b.rate / b.worth - a.rate / a.worth);
    can.forEach((t) => grid.append(slot(thingArt(t, 4), { tier: t.tier, name: demand(t) ? t.name + " · " + demand(t) : t.name, count: t.free, onClick: () => {
      body.textContent = "";
      $("sheet-name").textContent = t.name;
      $("sheet-art").replaceChildren(thingArt(t, 6));
      $("sheet-what").textContent = "";
      pricePicker(body, t);
    } })));
    body.append(grid);
  });
}

function renderStall(p) {
  const st = p.stall, box = $("stall-rows");
  box.textContent = "";
  const free = st.slots - st.rows.length, now = Math.floor(Date.now() / 1000);
  $("stall-n").textContent = st.rows.length + " of " + st.slots + " up";
  for (let i = 0; i < st.slots; i++) {
    const r = st.rows.find((x) => x.slot === i), row = document.createElement("div");
    if (!r) continue;
    row.className = "stallrow" + (r.sold ? " sold" : r.over ? " over" : "");
    const what = document.createElement("span"), name = document.createElement("b"), note = document.createElement("span"), price = document.createElement("span");
    name.textContent = r.name;
    note.className = "dim";
    note.textContent = r.sold ? "Sold to " + r.buyer + "!" : r.over ? "No takers at that price"
      : r.ends ? pct(r.chance) + " to sell within the hour · " + Math.max(1, Math.ceil((r.ends - now) / 3600)) + "h left" : "Up for sale";
    what.append(name, note);
    price.className = "price"; price.textContent = r.price.toLocaleString("en-US");
    row.append(thingArt(r, 3), what, price);
    row.append(button(r.sold ? "Collect" : "Take back", (b) => {
      b.disabled = true;
      const before = state.pond.pebbles;
      post("/api/stall/take", { slot: r.slot }).then((v) => {
        setPond(v);
        // It may have sold since the page last heard.
        $("stall-out").textContent = v.pebbles > before ? "Sold! +" + (v.pebbles - before) + " pebbles." : r.name + " is back in your bag.";
      }, (e) => { $("stall-out").textContent = e.message; b.disabled = false; });
    }, r.sold ? "primary" : ""));
    box.append(row);
  }
  if (free > 0) {
    const row = document.createElement("div"), b = button("+ Put something up", pickForStall);
    row.className = "stallrow empty";
    b.prepend(ghost("i_bag"));
    row.append(b);
    box.append(row);
  }
  const can = Math.min(st.takings, st.room);
  $("stall-collect").hidden = !st.takings;
  $("stall-collect").disabled = !can;
  $("stall-collect").textContent = can ? "Collect " + can.toLocaleString("en-US") : "Paid out for today";
  $("stall-room").hidden = can === st.takings;
  $("stall-room").textContent = "The neighbours pay up to " + p.sell_room + " a day between them, so the rest waits on the stall until tomorrow.";
  drawStall();
}

// A little stall with a striped awning, the frog behind the counter and the
// things for sale on top of it.
function drawStall() {
  const p = state.pond, cv = $("stall-art");
  if (!p || !state.g || $("town-stall").hidden) return;
  const x = cv.getContext("2d");
  x.fillStyle = "#123a3d"; x.fillRect(0, 0, 352, 128);
  x.fillStyle = "#56b45f"; x.fillRect(0, 112, 352, 16);
  x.fillStyle = "#3f9a4d"; for (let i = 0; i < 352; i += 12) x.fillRect(i, 108, 4, 4);
  const f = p.frog;
  draw(x, cells(state.g, f.stage, { hat: f.hat.shape, legs: true }), W, H, colours(state.g, f.hat), 132, 4, 2);
  for (let i = 0; i < 9; i++) { x.fillStyle = i % 2 ? "#fff6dc" : "#e2567a"; x.fillRect(64 + i * 25, 8, 25, 18); x.fillRect(64 + i * 25 + 4, 26, 17, 6); }
  x.fillStyle = "#8a5a2b"; x.fillRect(70, 26, 6, 86); x.fillRect(276, 26, 6, 86);
  x.fillStyle = "#c9974f"; x.fillRect(60, 74, 232, 12);
  x.fillStyle = "#8a5a2b"; x.fillRect(64, 86, 224, 26);
  x.fillStyle = "#6b4a2a"; for (let i = 0; i < 4; i++) x.fillRect(64, 92 + i * 6, 224, 2);
  p.stall.rows.forEach((r) => {
    const sx = 92 + r.slot * 64;
    if (r.sold) {
      drawIcon(x, "peb", sx + 4, 46, 3);
      x.fillStyle = "#ffe27a"; x.font = "bold 10px 'JetBrains Mono', monospace"; x.fillText("SOLD", sx + 4, 44);
    } else {
      const a = thingArt(r, 3);
      x.drawImage(a, sx + 16 - a.width / 2, 74 - a.height);
    }
  });
}

// ---------------------------------------------------------------- the wizard

let wizOn = false, wizRaf = 0, pick = "", casting = 0, castTier = 0;
const WIZ = 50311;
let wizG = null;

export function toWizard(kind) {
  pick = kind;
  location.hash = "town";
  showTab("wizard");
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
  $("stall-collect").addEventListener("click", function () {
    this.disabled = true;
    post("/api/stall/collect").then((d) => { setPond(d.pond); $("stall-out").textContent = d.line; this.disabled = false; presentsFrom(d); },
      (e) => { $("stall-out").textContent = e.message; this.disabled = false; });
  });
  $("wiz-cast").addEventListener("click", cast);
}

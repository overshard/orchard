import { $, post, state, setPond, watch, reduced, TIERS } from "./api.js";
import { genome, cells, colours, draw, W, H } from "./frog.js";
import { drawIcon } from "./icons.js";
import { presentsFrom } from "./neighbours.js";
import { slot, thingArt, openSheet, closeSheet, sheetSay, button, reveal, TIER_GLOW } from "./ui.js";

let tab = "shops";

function showTab(next) {
  tab = next;
  ["shops", "neighbours", "stall", "wizard"].forEach((k) => { $("town-" + k).hidden = k !== next; });
  document.querySelectorAll("#town-tabs button").forEach((b) => b.setAttribute("aria-pressed", b.dataset.town === next ? "true" : "false"));
  if (next === "wizard") { wizOn = true; cancelAnimationFrame(wizRaf); wizRaf = requestAnimationFrame(wizTick); } else wizOn = false;
  if (next === "stall") drawStall();
}

export function enterTown() { showTab(tab); }
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
    }, (e) => { $("shop-out-" + (shop === "Wren" ? "wren" : "moss")).textContent = e.message; this.disabled = false; });
  });
}

function renderBoxes(p) {
  const hb = $("box-open"), tb = $("trinket-box");
  hb.textContent = p.box.cost; hb.disabled = p.pebbles < p.box.cost;
  tb.textContent = p.trinkets.box_cost; tb.disabled = p.pebbles < p.trinkets.box_cost;
  $("box-odds").textContent = p.box.odds.map((o, t) => o + "% " + TIERS[t].toLowerCase()).slice(2).join(", ");
}

// ---------------------------------------------------------------- the stall

function hoursLike(h) {
  if (h < 1.5) return "about an hour";
  if (h < 20) return "about " + Math.round(h) + " hours";
  if (h < 36) return "about a day";
  if (h < 24 * 10) return "about " + Math.round(h / 24) + " days";
  return "a few weeks";
}

export function pricePicker(body, t) {
  const lo = Math.max(1, Math.ceil(t.worth * 0.5)), hi = t.max;
  const wrap = document.createElement("div"), price = document.createElement("b"), when = document.createElement("span"), range = document.createElement("input");
  wrap.className = "pricer";
  range.type = "range"; range.min = lo; range.max = hi; range.value = t.worth; range.step = 1;
  range.setAttribute("aria-label", "Price");
  const upd = () => {
    const v = +range.value, r = v / t.worth;
    price.textContent = v.toLocaleString("en-US");
    when.textContent = "Sells in " + hoursLike(24 * Math.pow(r, 2.6)) + (Math.abs(r - 1) < 0.05 ? ", the going rate" : r < 1 ? ", a bargain" : "");
  };
  range.addEventListener("input", upd);
  upd();
  wrap.append(price, range, when);
  const go = button("Put it up", (b) => {
    b.disabled = true;
    post("/api/stall/list", { kind: t.kind, price: +range.value }).then((v) => {
      setPond(v);
      closeSheet();
      location.hash = "town";
      showTab("stall");
      $("stall-out").textContent = t.name + " is on your stall.";
    }, (e) => { sheetSay(e.message); b.disabled = false; });
  }, "primary");
  body.append(wrap, go);
}

function pickForStall() {
  const p = state.pond, can = p.bag.filter((t) => t.free > 0);
  openSheet({ kind: "i_town", name: "Your stall" }, can.length ? "Pick something to sell." : "Nothing to sell yet. A hat or trinket has to be a spare.", (body) => {
    const grid = document.createElement("div");
    grid.className = "slots";
    can.forEach((t) => grid.append(slot(thingArt(t, 4), { tier: t.tier, name: t.name, count: t.free, onClick: () => {
      body.textContent = "";
      $("sheet-name").textContent = t.name;
      pricePicker(body, t);
    } })));
    body.append(grid);
  });
}

function ago(sec) {
  if (sec < 3600) return "just now";
  if (sec < 86400) return Math.round(sec / 3600) + "h ago";
  return Math.round(sec / 86400) + "d ago";
}

function renderStall(p) {
  const st = p.stall, box = $("stall-rows");
  box.textContent = "";
  for (let i = 0; i < st.slots; i++) {
    const r = st.rows.find((x) => x.slot === i), row = document.createElement("div");
    row.className = "stallrow" + (r && r.sold ? " sold" : "");
    if (!r) {
      row.classList.add("empty");
      row.append(button("Empty spot, put something up", pickForStall));
      box.append(row);
      continue;
    }
    const what = document.createElement("span"), name = document.createElement("b"), note = document.createElement("span"), price = document.createElement("span");
    name.textContent = r.name;
    note.className = "dim";
    note.textContent = r.sold ? "Sold to " + r.buyer + "!" : "Up " + ago(Math.floor(Date.now() / 1000) - r.listed);
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
  $("stall-collect").hidden = !st.takings;
  $("stall-collect").textContent = "Collect " + st.takings.toLocaleString("en-US");
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
  watch(renderBoxes);
  watch(renderStall);
  watch(renderWizard);
  boxBuy("box-open", "/api/hats/box", "Wren");
  boxBuy("trinket-box", "/api/trinkets/box", "Old Moss");
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

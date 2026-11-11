import { drawPot } from "./scenes.js";
import { $, post, state, setPond, watch, reduced, heldNow, canBuild, offer, TIERS } from "./api.js";
import { iconCanvas, dishCanvas } from "./icons.js";
import { slot } from "./ui.js";
import { genome, cells, colours, draw, W, H } from "./frog.js";
import { presentsFrom } from "./neighbours.js";

const gx = $("garden").getContext("2d");
const GW = 176, S = 3;
let picked = [], growing = [], seenAt = 0, raf = 0, on = false;

function say(id, msg) { $(id).textContent = msg; }

export function beanLine(p) {
  const left = p.room.brindle;
  if (!left) return "Big Bean has bought all the dishes he can today. He buys again tomorrow.";
  return "Big Bean spends up to " + p.sell_room + " a day on dishes, and has " + left + " left today.";
}

function hours(sec) {
  if (sec < 3600) return Math.max(1, Math.round(sec / 60)) + " min";
  const h = sec / 3600;
  return (h < 10 ? h.toFixed(1).replace(/\.0$/, "") : Math.round(h)) + " h";
}

function build(label, level, cost, mats) {
  if (!cost) return label + ", built all the way";
  const more = Object.entries(mats || {}).map(([k, n]) => " + " + n + " " + k).join("");
  return (level ? "Level " + (level + 1) : "Build it") + ", " + cost.toLocaleString("en-US") + more;
}


// ---------------------------------------------------------------- the garden

// The server says how far along each plot was when it answered, and the page
// carries it on at the same rate so a plot fills while you watch.
function plotsNow() {
  const dt = (performance.now() - seenAt) / 1000;
  return growing.map((p) => {
    if (p.ready_in <= 0) return Math.min(1, p.grown);
    return Math.min(1, p.grown + (1 - p.grown) * dt / p.ready_in);
  });
}

function drawGarden(now) {
  const p = state.pond, why = p.bank.garden.why, wet = why === "rain";
  gx.setTransform(S, 0, 0, S, 0, 0);
  gx.imageSmoothingEnabled = false;
  gx.fillStyle = wet ? "#27323c" : why === "frost" ? "#3a4654" : p.day.night ? "#141c2a" : "#3b5a6a";
  gx.fillRect(0, 0, GW, 34);
  if (!p.day.night) {
    gx.fillStyle = why ? "#9fb0bd" : "#ffd98a";
    gx.fillRect(150, 6, 10, 10); gx.fillRect(148, 8, 14, 6); gx.fillRect(152, 4, 6, 14);
  }
  if (why) {
    gx.fillStyle = wet ? "#55626e" : "#c9d3dc";
    gx.fillRect(20, 6, 40, 7); gx.fillRect(28, 3, 22, 4); gx.fillRect(96, 9, 34, 6); gx.fillRect(104, 6, 16, 4);
  }
  gx.fillStyle = "#2f5a3a";
  for (let r = 0; r < GW; r += 7) gx.fillRect(r, 30 - ((r * 13) % 5), 2, 6 + ((r * 13) % 5));
  gx.fillStyle = "#5a4630"; gx.fillRect(0, 34, GW, 26);
  gx.fillStyle = "#6c5538";
  for (let d = 3; d < GW; d += 11) gx.fillRect(d, 37 + (d % 3) * 6, 3, 1);
  gx.fillStyle = "#123a3d"; gx.fillRect(0, 60, GW, 12);
  gx.fillStyle = "#7fd6c2";
  for (let w = (Math.floor(now / 400) % 2) * 4; w < GW; w += 16) gx.fillRect(w, 61, 7, 1);

  const g = plotsNow(), n = Math.max(g.length, 1), span = Math.floor((GW - 8) / n), pw = Math.min(34, span - 6);
  g.forEach((k, i) => {
    const x = 4 + i * span + Math.floor((span - pw) / 2), cx = x + Math.floor(pw / 2);
    const damp = wet || growing[i].watered;
    gx.fillStyle = damp ? "#3a2c1c" : "#4a3824"; gx.fillRect(x, 46, pw, 9);
    gx.fillStyle = damp ? "#2e2216" : "#3d2e1d";
    for (let f = 2; f < pw; f += 6) gx.fillRect(x + f, 48, 3, 1);
    if (why === "frost") { gx.fillStyle = "#dfe9f2"; for (let q = 1; q < pw; q += 5) gx.fillRect(x + q, 46, 2, 1); }
    if (k < 0.3) { gx.fillStyle = "#c9b25a"; gx.fillRect(cx - 1, 49, 2, 2); }
    else if (k < 0.65) { gx.fillStyle = "#58b85a"; gx.fillRect(cx, 42, 1, 5); gx.fillRect(cx - 2, 42, 2, 1); gx.fillRect(cx + 1, 43, 2, 1); }
    else if (k < 1) { gx.fillStyle = "#3f9a4d"; gx.fillRect(cx, 36, 1, 11); gx.fillStyle = "#58b85a"; gx.fillRect(cx - 3, 39, 3, 2); gx.fillRect(cx + 1, 37, 3, 2); gx.fillRect(cx - 2, 43, 2, 1); }
    else {
      // Ready, and what it is stays a surprise until it's picked.
      gx.fillStyle = "#3f9a4d"; gx.fillRect(cx, 40, 1, 7);
      gx.fillStyle = "#ffe27a"; const bob = reduced ? 0 : Math.floor(now / 300 + i) % 2;
      gx.fillRect(cx - 2, 33 - bob, 5, 5); gx.fillStyle = "#fff6dc"; gx.fillRect(cx - 1, 34 - bob, 2, 2);
    }
    if (!damp && !why && k < 1) { gx.fillStyle = "#7eaab8"; gx.fillRect(x + pw - 5, 40, 2, 3); gx.fillRect(x + pw - 6, 42, 4, 2); }
  });
  if (!growing.length) {
    gx.fillStyle = "#3d2e1d";
    for (let i = 0; i < 4; i++) gx.fillRect(10 + i * 42, 52, 34, 1);
  }
  if (wet && !reduced) {
    gx.fillStyle = "#8fb6c9";
    for (let i = 0; i < 40; i++) gx.fillRect((i * 37 + Math.floor(now / 60) * 3) % GW, (i * 23 + Math.floor(now / 60) * 5) % 58, 1, 3);
  }
  if (why === "frost" && !reduced) {
    gx.fillStyle = "#eef4f9";
    for (let i = 0; i < 14; i++) gx.fillRect((i * 41 + Math.floor(now / 500)) % GW, (i * 17 + Math.floor(now / 250)) % 46, 1, 1);
  }
}

function tick(now) {
  if (!on) return;
  if (state.pond) {
    drawGarden(now);
    const gv = state.pond.bank.gravel, bw = state.pond.bank.burrows;
    if (gv.level) {
      const n = heldNow(gv);
      $("gravel-held").textContent = n + "/" + gv.holds;
      $("gravel-collect").disabled = n < 1;
    }
    if (bw.rate) {
      const n = heldNow(bw);
      $("burrow-held").textContent = n + "/" + bw.holds;
      $("burrow-collect").disabled = n < 1;
    }
    $("garden-pick").disabled = !plotsNow().some((k) => k >= 1);
  }
  raf = requestAnimationFrame(tick);
}

// ---------------------------------------------------------------- render

function render(p) {
  const b = p.bank, gv = b.gravel, gd = b.garden;

  const built = (gv.level ? 1 : 0) + (gd.level ? 1 : 0);
  say("bank-line", built ? "" : "Build the gravel bar first. It makes pebbles while you're away.");

  $("gravel-level").textContent = gv.level ? gv.level + "/5" : "0";
  $("gravel-rate").textContent = gv.level ? gv.rate + "/h" : "—";
  $("gravel-held").textContent = gv.level ? gv.held + "/" + gv.holds : "—";
  say("gravel-note", !gv.level
    ? "Not built yet."
    : gv.held >= gv.holds ? "Full. It stops making more until it's collected."
      : "The next pebble in " + hours(gv.next_in) + ". It holds eight hours' worth.");
  $("gravel-collect").hidden = !gv.level;
  $("gravel-collect").disabled = gv.held < 1;
  $("gravel-build").textContent = build("Gravel bar", gv.level, gv.cost, gv.mats);
  $("gravel-build").disabled = !canBuild(p, gv);

  growing = gd.plots;
  seenAt = performance.now();
  let note;
  if (!gd.level) note = "No plots yet. They grow things for the kitchen in real hours.";
  else if (gd.why === "frost") note = "Frost. Nothing grows and nothing is lost, the plots wait for it to lift.";
  else if (gd.why === "rain") note = "Rain, so the plots are watered and growing twice as fast.";
  else {
    const next = gd.plots.filter((x) => x.ready_in > 0).map((x) => x.ready_in).sort((a, c) => a - c)[0];
    note = gd.plots.length + " plots." + (next ? " The next is ready in " + hours(next) + "." : " Everything's ready.");
  }
  if (gd.level && gd.gardener && gd.speed > 1.01) note += " " + gd.gardener + " is tending them, so they grow " + gd.speed.toFixed(1).replace(/\.0$/, "") + " times as fast.";
  say("garden-note", note);
  $("garden-pick").hidden = !gd.level;
  $("garden-pick").disabled = !gd.plots.some((x) => x.ready_in === 0);
  $("garden-water").hidden = !gd.level || gd.rate !== 1;
  $("garden-water").disabled = !gd.plots.some((x) => !x.watered && x.grown < 1);
  $("garden-build").textContent = build("Garden", gd.level, gd.cost, gd.mats);
  $("garden-build").disabled = !canBuild(p, gd);

  renderDye(p);
  renderBurrows(p);

  const k = p.kitchen, larder = $("larder");
  picked = picked.filter((kind) => k.ingredients.some((i) => i.kind === kind && i.count > 0));
  if (picked.length === 2 && picked[0] === picked[1] && k.ingredients.find((i) => i.kind === picked[0]).count < 2) picked.pop();
  larder.textContent = "";
  k.ingredients.filter((it) => (!it.trinket && !it.fish) || it.count > 0).forEach((it) => {
    const btn = document.createElement("button"), name = document.createElement("span"), n = document.createElement("span");
    btn.type = "button"; btn.dataset.kind = it.kind;
    const times = picked.filter((x) => x === it.kind).length;
    btn.setAttribute("aria-pressed", times ? "true" : "false");
    btn.disabled = it.count === 0;
    name.textContent = it.name + (times === 2 ? " x2" : "");
    n.className = "held";
    n.textContent = it.count || "";
    n.hidden = !it.count;
    const dot = document.createElement("i");
    dot.className = "season" + (it.season ? " on" : "");
    dot.title = it.season ? "In season" : "Not in season";
    dot.setAttribute("aria-label", dot.title);
    dot.hidden = !!it.fish;
    btn.append(iconCanvas(it.kind, 2), name, dot, n);
    larder.append(btn);
  });
  $("cook").disabled = picked.length !== 2;
  const rec = $("recipe"), known = picked.length === 2 && k.cookbook.find((d) => (d.a === picked[0] && d.b === picked[1]) || (d.a === picked[1] && d.b === picked[0]));
  rec.hidden = picked.length !== 2;
  rec.classList.toggle("known", !!known);
  rec.textContent = "";
  if (known) {
    const words = document.createElement("span"), name = document.createElement("b");
    name.textContent = known.name;
    words.append("In the cookbook", name, TIERS[known.tier] + ", Big Bean pays " + known.worth);
    rec.append(dishCanvas(known, 3), words);
  } else if (picked.length === 2) rec.textContent = "Not in the cookbook yet. Cook it to see what it makes.";
  drawPot($("pot-art"), picked, p.day && p.day.night);

  const dishes = $("dishes");
  dishes.textContent = "";
  k.dishes.forEach((d) => {
    const li = document.createElement("li"), name = document.createElement("span"), sell = document.createElement("button");
    name.textContent = d.name + (d.count > 1 ? " x" + d.count : "");
    const pays = offer("brindle", d.worth);
    sell.type = "button"; sell.textContent = "Sell " + pays; sell.disabled = !pays;
    sell.addEventListener("click", () => {
      sell.disabled = true;
      const before = state.pond.pebbles;
      post("/api/kitchen/sell", { dish: d.kind }).then((v) => {
        say("cook-out", "Big Bean took the " + d.name.toLowerCase() + " for " + (v.pebbles - before) + " pebbles.");
        setPond(v);
      }, (e) => { say("cook-out", e.message); sell.disabled = false; });
    });
    li.append(dishCanvas(d, 3), name, sell);
    dishes.append(li);
  });
  $("bean-room").hidden = !k.dishes.length;
  say("bean-room", beanLine(p));
}

// ---------------------------------------------------------------- dye works

let rocks = [];

const DYE_NAME = { dye_olive: "Olive", dye_brown: "Wood brown", dye_grey: "Grey", dye_gold: "Golden", dye_berry: "Strawberry", dye_blue: "Blue" };

// The same sum as the server's, so the odds shown are the odds.
function dyeOdds(d) {
  const w = {}, top = d.level === 5;
  d.pool.forEach((k, i) => { w[k] = top && i >= Math.floor(d.pool.length / 2) ? 2 : 1; });
  rocks.forEach((r) => Object.entries(d.leans[r] || {}).forEach(([k, n]) => { if (k in w) w[k] += n * 2; }));
  const sum = d.pool.reduce((a, k) => a + w[k], 0);
  return d.pool.map((k) => [k, w[k] / sum]).sort((a, b) => b[1] - a[1]);
}

// A fourth pick starts the three over.
function pickRock(k, have) {
  if (rocks.length >= 3) rocks = [];
  if (rocks.filter((x) => x === k).length < have) rocks.push(k);
  renderDye(state.pond);
}

function renderDye(p) {
  const d = p.bank.dye;
  $("dye-level").textContent = d.level ? d.level + "/5" : "0";
  $("dye-hours").textContent = d.level ? d.hours + " h" : "—";
  $("dye-state").textContent = !d.busy ? (d.level ? "Empty" : "—") : d.ready_in > 0 ? hours(d.ready_in) : "Done";
  let note;
  if (!d.level) note = "Not built yet.";
  else if (d.busy && d.ready_in > 0) note = "Boiling. It'll be ready in " + hours(d.ready_in) + ", and what colour stays a surprise till it's bottled.";
  else if (d.busy) note = "Done, and ready to bottle.";
  else note = "Pick three rocks. Each one leans the pot toward its own colours. It can make " + d.pool.map((k) => DYE_NAME[k].toLowerCase()).join(", ") + ".";
  say("dye-note", note);

  const have = {};
  d.rocks.forEach((r) => { have[r.kind] = r.count; });
  rocks = rocks.filter((k, i) => rocks.slice(0, i + 1).filter((x) => x === k).length <= (have[k] || 0));
  // Held rocks are slots to pick from, and the ones the pond has none of are
  // a line under them.
  const box = $("dye-rocks"), tier = {};
  p.rocks.forEach((r) => { tier[r.kind] = r.tier; });
  box.textContent = "";
  box.hidden = !d.level || d.busy;
  d.rocks.filter((r) => r.count > 0).forEach((r) => {
    const times = rocks.filter((x) => x === r.kind).length;
    const b = slot(iconCanvas(r.kind, 4), {
      tier: tier[r.kind] || 0, name: r.name, count: r.count, on: times > 0,
      sub: times ? "In x" + times : "", onClick: () => pickRock(r.kind, r.count),
    });
    b.setAttribute("aria-pressed", times ? "true" : "false");
    box.append(b);
  });
  const none = d.rocks.filter((r) => r.count === 0).map((r) => r.name.toLowerCase()), rest = $("dye-rest");
  rest.hidden = box.hidden || !none.length;
  if (none.length === d.rocks.length) rest.textContent = "No rocks yet. The wheel, the reeds and Moth have them.";
  else rest.textContent = "None held: " + none.join(", ") + ".";
  const odds = $("dye-odds");
  odds.hidden = !d.level || d.busy || !rocks.length;
  if (!odds.hidden) odds.textContent = "Likely " + dyeOdds(d).slice(0, 3).map(([k, f]) => DYE_NAME[k].toLowerCase() + " " + Math.round(f * 100) + "%").join(", ") + ".";
  $("dye-load").hidden = !d.level || d.busy;
  $("dye-load").disabled = rocks.length !== 3;
  $("dye-load").textContent = "Boil them down" + (rocks.length < 3 ? ", " + rocks.length + "/3" : "");
  $("dye-collect").hidden = !d.busy;
  $("dye-collect").disabled = d.ready_in > 0;
  $("dye-build").textContent = build("Dye works", d.level, d.cost, d.mats);
  $("dye-build").disabled = !canBuild(p, d);
}

// ---------------------------------------------------------------- burrows

const BW = 56, BH = 40;

function burrowArt(n) {
  const cv = document.createElement("canvas"), g = cv.getContext("2d"), s = 2;
  cv.width = BW * s; cv.height = BH * s;
  g.setTransform(s, 0, 0, s, 0, 0);
  g.fillStyle = "#3b5a6a"; g.fillRect(0, 0, BW, BH);
  g.fillStyle = "#5a4630";
  for (let y = 6; y < BH; y++) {
    const w = Math.min(BW, Math.round(Math.sqrt(Math.max(0, (y - 4) * 90))));
    g.fillRect(Math.floor((BW - w) / 2), y, w, 1);
  }
  g.fillStyle = "#2f5a3a";
  for (let x = 6; x < BW - 6; x += 5) g.fillRect(x, 6 + Math.abs(x - BW / 2) / 3, 2, 3);
  g.fillStyle = "#6c5538"; g.fillRect(20, 15, 16, 1); g.fillRect(18, 16, 20, 1);
  g.fillStyle = "#1a120b";
  for (let y = 17; y < 34; y++) {
    const w = y < 20 ? 12 + (y - 17) * 2 : 18;
    g.fillRect(Math.floor((BW - w) / 2), y, w, 1);
  }
  g.fillStyle = "#123a3d"; g.fillRect(0, 34, BW, 6);
  if (n) {
    const ge = genome(n.seed);
    draw(g, cells(ge, "frog", { hat: n.hat.shape }), W, H, colours(ge, n.hat), Math.floor((BW - W) / 2), BH - H - 2, 1);
  }
  return cv;
}

function renderBurrows(p) {
  const bw = p.bank.burrows, all = p.neighbours.all;
  $("burrow-level").textContent = bw.level ? bw.level + "/5" : "0";
  $("burrow-rate").textContent = bw.rate ? bw.rate + "/h" : "—";
  $("burrow-held").textContent = bw.rate ? bw.held + "/" + bw.holds : "—";
  const row = $("burrow-row");
  row.textContent = "";
  for (let i = 0; i < bw.level; i++) {
    const n = all[bw.lodgers[i]], fig = document.createElement("figure"), cap = document.createElement("figcaption");
    cap.textContent = n ? n.name : "Empty";
    fig.append(burrowArt(n), cap);
    row.append(fig);
  }
  row.hidden = !bw.level;
  let note;
  if (!bw.level) note = "Not dug yet. Each burrow comes with a lily pad.";
  else if (!bw.lodgers.length) note = "Nobody's moved in. A neighbour with two hearts will, if you ask it in town.";
  else if (bw.held >= bw.holds) note = "Full. The rent stops piling up until it's collected.";
  else note = "The next pebble in " + hours(bw.next_in) + (bw.chatted ? "." : ". Collect today and everyone in the burrows gets a little friendlier.");
  if (bw.level && bw.lodgers.length && bw.lodgers.length < bw.level) note += " There's room for " + (bw.level - bw.lodgers.length) + " more.";
  say("burrow-note", note);
  $("burrow-collect").hidden = !bw.lodgers.length;
  $("burrow-collect").disabled = bw.held < 1;
  $("burrow-build").textContent = build("Burrows", bw.level, bw.cost, bw.mats);
  $("burrow-build").disabled = !canBuild(p, bw);
}

function nameOf(kind) {
  const it = state.pond.kitchen.ingredients.find((i) => i.kind === kind);
  return it ? it.name.toLowerCase() : kind;
}

export function enterBank() {
  on = true;
  cancelAnimationFrame(raf);
  raf = requestAnimationFrame(tick);
}

export function leaveBank() { on = false; }

export function initBank() {
  watch(render);
  $("gravel-collect").addEventListener("click", () => {
    $("gravel-collect").disabled = true;
    post("/api/bank/collect").then((d) => {
      say("gravel-out", d.pebbles + " pebbles off the gravel bar.");
      setPond(d.pond);
    }, (e) => say("gravel-out", e.message));
  });
  const builder = (what, out) => () => {
    post("/api/bank/build", { build: what }).then((v) => {
      const lv = v.bank[what].level;
      say(out, lv === 1 ? "Built." : "Up to level " + lv + ".");
      setPond(v);
    }, (e) => say(out, e.message));
  };
  $("gravel-build").addEventListener("click", builder("gravel", "gravel-out"));
  $("garden-build").addEventListener("click", builder("garden", "garden-out"));
  $("dye-build").addEventListener("click", builder("dye", "dye-out"));
  $("burrow-build").addEventListener("click", () => {
    post("/api/bank/build", { build: "burrow" }).then((v) => {
      say("burrow-out", "Dug a burrow, and a lily pad came with it.");
      setPond(v);
    }, (e) => say("burrow-out", e.message));
  });
  $("burrow-collect").addEventListener("click", () => {
    $("burrow-collect").disabled = true;
    post("/api/burrows/collect").then((d) => {
      say("burrow-out", d.line);
      setPond(d.pond);
      presentsFrom(d);
    }, (e) => say("burrow-out", e.message));
  });
  $("dye-load").addEventListener("click", () => {
    if (rocks.length !== 3) return;
    const sent = rocks;
    rocks = [];
    $("dye-load").disabled = true;
    post("/api/dye/load", { rocks: sent }).then((v) => {
      say("dye-out", "In the pot. Come back in " + v.bank.dye.hours + " hours.");
      setPond(v);
    }, (e) => { say("dye-out", e.message); renderDye(state.pond); });
  });
  $("dye-collect").addEventListener("click", () => {
    $("dye-collect").disabled = true;
    post("/api/dye/collect").then((d) => {
      say("dye-out", "A bottle of " + d.dye.name.toLowerCase() + ". It's on the egg bench.");
      setPond(d.pond);
    }, (e) => say("dye-out", e.message));
  });
  $("garden-pick").addEventListener("click", () => {
    $("garden-pick").disabled = true;
    post("/api/garden/pick").then((d) => {
      const names = d.got.map(nameOf);
      say("garden-out", "Picked " + (names.length > 1 ? names.slice(0, -1).join(", ") + " and " + names[names.length - 1] : names[0]) + ", and sowed again.");
      setPond(d.pond);
    }, (e) => say("garden-out", e.message));
  });
  $("garden-water").addEventListener("click", () => {
    post("/api/garden/water").then((v) => {
      say("garden-out", "Watered. That's an hour and a half off everything growing.");
      setPond(v);
    }, (e) => say("garden-out", e.message));
  });
  $("larder").addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b || b.disabled) return;
    if (picked.length >= 2) picked = [];
    picked.push(b.dataset.kind);
    render(state.pond);
  });
  $("cook").addEventListener("click", () => {
    if (picked.length !== 2) return;
    const [a, b] = picked;
    picked = [];
    $("cook").disabled = true;
    post("/api/kitchen/cook", { a, b }).then((d) => {
      say("cook-out", (d.new ? "New dish. " : "") + d.dish.name + ", " + TIERS[d.tier].toLowerCase() + ".");
      setPond(d.pond);
    }, (e) => { say("cook-out", e.message); render(state.pond); });
  });
}


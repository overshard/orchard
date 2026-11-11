import { $, post, setPond, reduced, TIERS } from "./api.js";
import { genome, cells, colours, draw, W, H } from "./frog.js";
import { thingArt, reveal, button } from "./ui.js";

// The hooded frog is any frog with its head and back covered, its eyes and
// belly left showing, so it never needs a sprite of its own.
const HOODED = genome(31415);
const CLOAK = 12, TRIM = 13;

function hooded(blink) {
  const c = cells(HOODED, "frog", { legs: true, blink });
  let eyeTop = H, eyeBot = 0;
  for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
    if (c[y * W + x] === 4 || c[y * W + x] === 5) { eyeTop = Math.min(eyeTop, y); eyeBot = Math.max(eyeBot, y); }
  }
  const out = new Uint8Array(c);
  for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
    const v = c[y * W + x];
    if (v === 4 || v === 5 || v === 2 || v === 7) continue;
    if (v) { out[y * W + x] = y <= eyeBot + 1 ? CLOAK : TRIM; continue; }
    // The hood stands a little proud of the head and comes to a point.
    const near = (dx, dy) => { const xx = x + dx, yy = y + dy; return xx >= 0 && xx < W && yy >= 0 && yy < H && c[yy * W + xx]; };
    if (y <= eyeBot && (near(0, 1) || near(-1, 0) || near(1, 0) || near(0, 2))) out[y * W + x] = CLOAK;
  }
  const mid = W >> 1;
  for (let y = Math.max(0, eyeTop - 8); y < eyeTop - 3; y++) {
    const w = y - (eyeTop - 8);
    for (let x = mid - w; x <= mid + w; x++) if (!out[y * W + x]) out[y * W + x] = CLOAK;
  }
  return out;
}

const SCALE = 3, CW = 352, CH = 168;
let raf = 0, on = false, open = false, sign = "";

function brick(x) {
  x.fillStyle = "#17120f"; x.fillRect(0, 0, CW, CH);
  x.fillStyle = "#211915";
  for (let r = 0; r < CH; r += 12) for (let c = (r / 12) % 2 ? -12 : 0; c < CW; c += 24) x.fillRect(c + 1, r + 1, 22, 10);
  x.fillStyle = "#0d0a08"; x.fillRect(0, CH - 22, CW, 22);
}

function lantern(x, lx, ly, lit, t) {
  x.fillStyle = "#3a3228"; x.fillRect(lx + 5, 0, 2, ly);
  if (lit) {
    const g = 0.18 + (reduced ? 0 : 0.05 * Math.sin(t * 3.1));
    x.fillStyle = "rgba(255, 196, 80, " + g + ")";
    x.beginPath(); x.arc(lx + 6, ly + 12, 54, 0, Math.PI * 2); x.fill();
  }
  x.fillStyle = "#2a241d"; x.fillRect(lx, ly, 12, 3); x.fillRect(lx, ly + 18, 12, 3);
  x.fillStyle = lit ? "#ffd27a" : "#3a3228"; x.fillRect(lx + 2, ly + 3, 8, 15);
  if (lit) { x.fillStyle = "#fff6dc"; x.fillRect(lx + 5, ly + 8, 2, 6); }
}

function frame(now) {
  paint(now / 1000);
  if (on && !reduced && $("bm-art").offsetParent) raf = requestAnimationFrame(frame);
  else on = false;
}

function paint(t) {
  const cv = $("bm-art"), x = cv.getContext("2d");
  brick(x);
  if (open) {
    lantern(x, 52, 30, true, t);
    const col = Object.assign({}, colours(HOODED, null), { [CLOAK]: "#2b2436", [TRIM]: "#3b3348" });
    const bob = reduced ? 0 : Math.round(Math.sin(t * 1.3)) * SCALE;
    draw(x, hooded(!reduced && t % 4.3 < 0.14), W, H, col, (CW - W * SCALE) / 2, 14 + bob, SCALE);
    // A crate for a counter.
    x.fillStyle = "#5a3f24"; x.fillRect(96, CH - 40, 160, 40);
    x.fillStyle = "#3e2a17";
    for (let i = 0; i < 4; i++) x.fillRect(96, CH - 40 + i * 10, 160, 2);
    x.fillRect(96, CH - 40, 4, 40); x.fillRect(252, CH - 40, 4, 40);
  } else {
    lantern(x, 52, 30, false, t);
    x.fillStyle = "#2b1f15"; x.fillRect(132, 28, 88, CH - 50);
    x.fillStyle = "#3a2a1c"; x.fillRect(138, 34, 76, CH - 56);
    x.fillStyle = "#2b1f15"; for (let i = 0; i < 3; i++) x.fillRect(138, 54 + i * 30, 76, 3);
    x.fillStyle = "#c9974f"; x.fillRect(200, 92, 5, 5);
    // A card on a string, hung a little crooked.
    x.save(); x.translate(176, 60); x.rotate(reduced ? -0.05 : -0.05 + 0.02 * Math.sin(t));
    x.fillStyle = "#c9b48a"; x.fillRect(-40, 0, 80, 26);
    x.fillStyle = "#2a241d"; x.font = "700 10px 'JetBrains Mono', monospace"; x.textAlign = "center";
    x.fillText("BACK AT", 0, 11); x.fillText(sign, 0, 22);
    x.restore();
  }
}

export function showBlack(yes) {
  cancelAnimationFrame(raf);
  on = yes;
  if (yes) raf = requestAnimationFrame(frame);
}

function hours(sec) {
  const all = Math.max(1, Math.round(sec / 60)), h = Math.floor(all / 60), m = all % 60;
  if (!h) return m + " min";
  return h + " h" + (m ? " " + m + " min" : "");
}

const TAG = { great: "A STEAL", fair: "FAIR", scam: "STUNG", fake: "FAKE" };

// Each outcome has its own reveal, and a fake is a joke at nobody's expense.
function told(d) {
  const g = d.got, what = g.kind === "peb" ? { kind: "peb" } : g.hat ? { hat: g.hat } : dishOf(g.kind) || { kind: g.kind };
  const r = { art: thingArt(what, 8), name: g.n > 1 ? g.n + " " + (g.name === "Cricket" ? "crickets" : g.name.toLowerCase()) : g.name, tier: g.tier };
  if (g.outcome === "great") {
    const better = g.kind !== d.kind || g.n > d.n;
    return Object.assign(r, { kick: "A steal!", line: better ? "It isn't even what the sign said, it's better. The hooded frog looks like it wants it back." : "It's the real thing for a fraction of what the shops ask. The hooded frog is already wondering why it did that.", ok: "Run" });
  }
  if (g.outcome === "fair") return Object.assign(r, { kick: "Fair enough", line: "It's real, and you paid about what it's worth. Nobody's quite sure how that happened back here.", ok: "Fine" });
  if (g.outcome === "scam") return Object.assign(r, { kick: "Stung", bad: true, line: "It's real, but the shops ask about a tenth of what you paid. The hooded frog has already gone round the corner.", ok: "Oh well" });
  const fake = g.kind === "peb" ? "Under all that wrapping is one pebble. It's a nice pebble, at least." :
    g.hat ? "It was a leaf all along. Your frog thinks it makes a lovely hat anyway." :
    "The paint comes off on your fingers. Underneath it's a river agate, and not a very good one.";
  return Object.assign(r, { kick: "It's a fake", bad: true, tier: undefined, line: fake, ok: "Ha" });
}

function dishOf(kind) {
  const m = /^dish_(\d+)_(\d+)$/.exec(kind);
  return m ? { a: +m[1], b: +m[2], kind } : null;
}

function buy(d, b) {
  b.disabled = true;
  post("/api/black", { slot: d.slot }).then((r) => {
    setPond(r.pond);
    reveal(told(r.deal));
  }, (e) => { $("bm-out").textContent = e.message; b.disabled = false; });
}

function row(d, coins) {
  const li = document.createElement("li"), what = document.createElement("span"), name = document.createElement("b"), say = document.createElement("span");
  what.className = "what";
  name.textContent = d.name;
  say.className = "sub patter";
  say.textContent = "“" + d.patter + "”";
  const tier = document.createElement("span");
  tier.className = "tier t" + d.tier; tier.textContent = TIERS[d.tier];
  what.append(name, tier, say);
  const art = d.hat ? { hat: d.hat } : dishOf(d.kind) || { kind: d.kind };
  li.append(thingArt(art, 3), what);
  if (d.got) {
    li.className = "sold " + d.got.outcome;
    const tag = document.createElement("span");
    tag.className = "soldtag"; tag.textContent = TAG[d.got.outcome];
    li.append(tag);
    return li;
  }
  const key = document.createElement("button"), sub = document.createElement("span");
  key.type = "button";
  key.className = "coinkey";
  key.textContent = d.coins + " COIN";
  sub.textContent = "about " + d.about.toLocaleString("en-US");
  key.append(sub);
  key.setAttribute("aria-label", "Buy " + d.name + " for " + d.coins + " Glowworm Coin, about " + d.about + " pebbles");
  key.disabled = coins < d.coins;
  key.addEventListener("click", () => buy(d, key));
  li.append(key);
  return li;
}

export function renderBlack(p, toCoin) {
  const b = p.black;
  open = b.open;
  sign = (b.back || "").toUpperCase();
  const when = b.hours.map((h) => h[0] + " to " + h[1]).join(" and ");
  $("bm-line").textContent = b.open
    ? "Open for " + hours(b.edge) + " more. Paid in Glowworm Coin only, and a deal here is a gamble. Some are steals, most are fair or worse, and some aren't what they say."
    : "Shut. Back at " + b.back + ", in " + hours(b.edge) + ". Open " + when + ", pond time.";
  const coin = button("Get coin", toCoin);
  const tiles = $("bm-coins");
  const glass = document.createElement("span"), k = document.createElement("span"), v = document.createElement("span");
  glass.className = "readout"; k.className = "k"; v.className = "v";
  k.textContent = "Coin held"; v.textContent = b.coins;
  glass.append(k, v);
  tiles.replaceChildren(glass, coin);
  $("bm-deals").replaceChildren(...b.deals.map((d) => row(d, b.coins)));
  $("bm-deals").hidden = !b.deals.length;
  paint(performance.now() / 1000);
}

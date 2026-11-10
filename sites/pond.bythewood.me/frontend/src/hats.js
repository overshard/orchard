import { $, post, state, setPond, watch, titled, TIERS } from "./api.js";
import { cells, colours, draw as drawCells, hatCells, hatColours, W, H } from "./frog.js";

const boxes = {};

// A hat by itself, in its colours when found and as a shadow when not.
export function hatTile(h, have) {
  const cv = document.createElement("canvas"), ctx = cv.getContext("2d");
  cv.width = 76; cv.height = 48;
  const b = boxes[h.shape] || (boxes[h.shape] = hatCells(h.shape)), col = hatColours(h);
  const ox = Math.floor((76 - (b.x1 - b.x0 + 1) * 4) / 2), oy = Math.floor((48 - (b.y1 - b.y0 + 1) * 4) / 2);
  for (let y = b.y0; y <= b.y1; y++) for (let x = b.x0; x <= b.x1; x++) {
    const v = b.c[y * W + x];
    if (v !== 10 && v !== 11) continue;
    ctx.fillStyle = have ? col[v - 10] : (v === 10 ? "#2f2820" : "#3a3228");
    ctx.fillRect(ox + (x - b.x0) * 4, oy + (y - b.y0) * 4, 4, 4);
  }
  return cv;
}

function tierChip(t) {
  const s = document.createElement("span");
  s.className = "tier t" + t; s.textContent = TIERS[t];
  return s;
}

function same(a, b) { return a.shape === b.shape && a.colour === b.colour; }

function say(msg) { $("box-out").textContent = msg; }

function wear(h) {
  post("/api/hats/wear", { shape: h.shape, colour: h.colour }).then(setPond, (e) => say(e.message));
}

function render(p) {
  const f = p.frog, g = state.g;
  if (!g) return;
  // The frog as it is today, so an egg keeps what it'll grow into to itself.
  const pv = $("hat-preview").getContext("2d");
  pv.clearRect(0, 0, 176, 136);
  drawCells(pv, cells(g, f.stage, { hat: f.hat.shape, legs: f.day >= 6 }), W, H, colours(g, f.hat), 0, 0, 4);

  const found = p.hats.filter((h) => h.count > 0).length, worn = p.hats.find((h) => same(h, f.hat));
  $("hat-count").textContent = found + " of " + p.hats.length + " hats";
  $("hat-wearing").textContent = worn ? titled(f.name) + " is wearing the " + worn.name.toLowerCase() + "." : "No hat on.";
  $("hat-off").hidden = !worn;

  const open = $("box-open");
  open.textContent = "Open a hat box, " + p.box.cost;
  open.disabled = p.pebbles < p.box.cost;
  $("box-odds").textContent = "What's in a box: " + p.box.odds.map((o, t) => TIERS[t].toLowerCase() + " " + o + "%").join(", ") + ".";

  const grid = $("hatgrid");
  grid.textContent = "";
  p.hats.forEach((h) => {
    const have = h.count > 0, b = document.createElement("button"), n = document.createElement("span");
    b.type = "button"; b.className = "hattile"; b.disabled = !have;
    b.setAttribute("aria-pressed", worn && same(h, worn) ? "true" : "false");
    b.setAttribute("aria-label", have ? h.name + ", " + TIERS[h.tier] + ", found, press to wear" : "A " + TIERS[h.tier] + " hat not found yet");
    n.textContent = have ? h.name + (h.count > 1 ? " x" + h.count : "") : "?";
    b.append(hatTile(h, have), n, tierChip(h.tier));
    if (have) b.addEventListener("click", () => wear(h));
    grid.append(b);
  });

  const spares = $("spares");
  spares.textContent = "";
  p.hats.filter((h) => h.count > 1).forEach((h) => {
    const row = document.createElement("div"), n = document.createElement("span"), b = document.createElement("button");
    row.className = "spare";
    n.textContent = h.name + (h.count > 2 ? " x" + (h.count - 1) : "");
    b.type = "button"; b.textContent = "Sell to Wren, " + h.wren;
    b.addEventListener("click", () => {
      b.disabled = true;
      post("/api/hats/sell", { shape: h.shape, colour: h.colour }).then((np) => {
        say("Wren took the spare " + h.name.toLowerCase() + " for " + h.wren + " pebbles.");
        setPond(np);
      }, (e) => { say(e.message); b.disabled = false; });
    });
    row.append(hatTile(h, true), n, b);
    spares.append(row);
  });
  if (!spares.children.length) {
    const none = document.createElement("p");
    none.className = "dim"; none.textContent = "No spares. Wren buys any hat you have two of.";
    spares.append(none);
  }
}

export function initHats() {
  watch(render);
  $("hat-off").addEventListener("click", () => wear({ shape: 0, colour: 0 }));
  $("box-open").addEventListener("click", function () {
    this.disabled = true;
    post("/api/hats/box").then((d) => {
      const z = d.prize;
      setPond(d.pond);
      if (z.spare) say("A second " + z.hat_name.toLowerCase() + ". That's a spare, and Wren will buy it.");
      else { say("New hat. " + z.hat_name + ", " + TIERS[z.hat_tier].toLowerCase() + "."); wear(z.hat); }
    }, (e) => { say(e.message); this.disabled = false; });
  });
}

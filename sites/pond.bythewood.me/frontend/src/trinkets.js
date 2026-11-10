import { $, post, state, setPond, watch, TIERS } from "./api.js";
import { drawIcon } from "./icons.js";

const SPOT_NAMES = ["Left of the pad", "Right of the pad", "On the water"];
let spot = 0;

function say(msg) { $("trinket-out").textContent = msg; }

// A trinket by itself, in its colours when found and as a shadow when not.
function tile(kind, have) {
  const cv = document.createElement("canvas"), ctx = cv.getContext("2d");
  cv.width = 76; cv.height = 48;
  drawIcon(ctx, kind, 22, 8, 4);
  if (!have) {
    ctx.globalCompositeOperation = "source-atop";
    ctx.fillStyle = "#2f2820"; ctx.fillRect(0, 0, 76, 48);
  }
  return cv;
}

function tierChip(t) {
  const s = document.createElement("span");
  s.className = "tier t" + t; s.textContent = TIERS[t];
  return s;
}

function render(p) {
  const tv = p.trinkets, spots = tv.spots;
  document.querySelectorAll("#spot-pick button").forEach((b, i) => {
    b.setAttribute("aria-pressed", i === spot ? "true" : "false");
    const on = tv.all.find((t) => t.kind === spots[i]);
    b.textContent = ["Left", "Right", "Water"][i] + (on ? ", " + on.name.toLowerCase() : "");
  });
  $("trinket-clear").disabled = !spots[spot];
  const box = $("trinket-box");
  box.textContent = "Open a trinket box, " + tv.box_cost;
  box.disabled = p.pebbles < tv.box_cost;
  $("trinket-count").textContent = tv.all.filter((t) => t.found).length + " OF " + tv.all.length;

  const grid = $("trinket-grid");
  grid.textContent = "";
  tv.all.forEach((t) => {
    const b = document.createElement("button"), n = document.createElement("span");
    const have = t.count > 0;
    b.type = "button"; b.className = "hattile"; b.disabled = !have;
    b.setAttribute("aria-pressed", spots[spot] === t.kind ? "true" : "false");
    b.setAttribute("aria-label", have ? t.name + ", " + TIERS[t.tier] + ", press to set it " + SPOT_NAMES[spot].toLowerCase()
      : t.found ? t.name + ", found and none left" : "A " + TIERS[t.tier] + " trinket not found yet");
    n.textContent = t.found ? t.name + (t.count > 1 ? " x" + t.count : "") + (t.found && !have ? ", none left" : "") : "?";
    b.append(tile(t.kind, have), n, tierChip(t.tier));
    if (have) b.addEventListener("click", () => {
      const kind = spots[spot] === t.kind ? "" : t.kind;
      post("/api/trinkets/place", { spot, kind }).then((v) => {
        say(kind ? t.name + " is " + SPOT_NAMES[spot].toLowerCase() + " now." : "Taken back in.");
        setPond(v);
      }, (e) => say(e.message));
    });
    grid.append(b);
  });

  const spares = $("trinket-spares");
  spares.textContent = "";
  tv.all.filter((t) => t.count > 1).forEach((t) => {
    const row = document.createElement("div"), n = document.createElement("span"), b = document.createElement("button");
    row.className = "spare";
    n.textContent = t.name + (t.count > 2 ? " x" + (t.count - 1) : "");
    b.type = "button"; b.textContent = "Sell to Moss, " + t.moss;
    b.addEventListener("click", () => {
      b.disabled = true;
      post("/api/trinkets/sell", { kind: t.kind }).then((v) => {
        say("Old Moss took the spare " + t.name.toLowerCase() + " for " + t.moss + " pebbles.");
        setPond(v);
      }, (e) => { say(e.message); b.disabled = false; });
    });
    row.append(tile(t.kind, true), n, b);
    spares.append(row);
  });
  $("trinket-spares-head").hidden = spares.children.length === 0;
}

export function initTrinkets() {
  watch(render);
  $("spot-pick").addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    spot = Number(b.dataset.spot);
    render(state.pond);
  });
  $("trinket-clear").addEventListener("click", () => {
    post("/api/trinkets/place", { spot, kind: "" }).then((v) => { say("Taken back in."); setPond(v); }, (e) => say(e.message));
  });
  $("trinket-box").addEventListener("click", function () {
    this.disabled = true;
    post("/api/trinkets/box").then((d) => {
      const z = d.prize;
      setPond(d.pond);
      say(z.spare ? "Another " + z.text.toLowerCase() + ". That's a spare, and Old Moss will buy it."
        : "New trinket. " + z.text + ", " + TIERS[z.hat_tier].toLowerCase() + ". Pick it to set it out.");
    }, (e) => { say(e.message); this.disabled = false; });
  });
}

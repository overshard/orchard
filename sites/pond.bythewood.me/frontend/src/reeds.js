import { drawReeds, drawCard } from "./scenes.js";
import { $, post, state, setPond, watch, titled, workNow, TIERS } from "./api.js";
import { iconCanvas } from "./icons.js";
import { collect } from "./work.js";

// A card a frog, each on its own pad, and a dashed card for every pad
// nobody's on and for the next one to buy.
const cards = new Map();

export function span(sec) {
  if (sec < 3600) return Math.max(1, Math.round(sec / 60)) + "m";
  if (sec < 86400 * 2) return Math.round(sec / 3600) + "h";
  return Math.round(sec / 86400) + "d";
}

export function jobName(p, key) {
  const d = p.jobs.defs.find((x) => x.key === key);
  return d ? d.name : "";
}

// The one line a card says, and whether its LED is lit. A full basket and
// an egg ready to lay come first, since they're waiting on the player.
export function statusOf(p, f) {
  const n = f.work && f.work.act ? workNow(f.work) : null;
  if (n && n.stop === "full") return ["Basket full", "ok"];
  if (f.lay_in === 0) return [p.frogs.length < p.pads ? "Ready to lay" : "Ready to lay, pads full", p.frogs.length < p.pads ? "ok" : ""];
  if (f.stage === "egg") return ["Hatches in " + span(f.grows_in), ""];
  if (f.work && f.work.job) {
    const l = f.work.levels.find((x) => x.job === f.work.job);
    return [jobName(p, f.work.job) + " " + l.level + (n ? " · " + n.n + "/" + f.work.cap : ""), "on"];
  }
  if (f.fed) return ["Full for " + span(f.fed.left), ""];
  if (f.lay_in > 0) return ["Lays again in " + span(f.lay_in), ""];
  if (f.grows_in > 0 && f.stage !== "frog") return ["Grows in " + span(f.grows_in), ""];
  return ["No job yet", ""];
}

function card(p, f) {
  let c = cards.get(f.id);
  if (!c) {
    const a = document.createElement("a"), cv = document.createElement("canvas"), n = document.createElement("span");
    a.className = "fcard";
    cv.className = "vista";
    n.className = "fcard-n";
    a.append(cv, n);
    c = { a, cv, n, key: "" };
    cards.set(f.id, c);
  }
  c.a.href = "#frog-" + f.id;
  c.a.classList.toggle("front", f.front);
  const key = [f.stage, f.day, f.hat.shape, f.hat.colour, f.front, f.work && f.work.job, JSON.stringify(f.mods), p.day && p.day.night].join();
  if (key !== c.key) { c.key = key; drawCard(c.cv, p, f); }
  const [line, led] = statusOf(p, f), name = document.createElement("b"), tier = document.createElement("span"), st = document.createElement("span"), dot = document.createElement("i");
  name.textContent = titled(f.name);
  tier.className = "tier t" + f.rarity.tier; tier.textContent = TIERS[f.rarity.tier];
  dot.className = "led" + (led ? " " + led : "");
  st.className = "fcard-st";
  st.append(dot, line);
  const top = document.createElement("span");
  top.className = "fcard-top";
  top.append(name, tier);
  c.n.replaceChildren(top, st);
  c.cv.setAttribute("aria-hidden", "true");
  c.a.setAttribute("aria-label", titled(f.name) + ", " + TIERS[f.rarity.tier] + ", " + line);
  return c.a;
}

function render(p) {
  drawReeds($("reeds-art"), p);
  const front = p.frogs.find((f) => f.front);
  $("reeds-count").textContent = p.frogs.length + " of " + p.pads + " pads";
  const box = $("reeds");
  const all = [front].concat(p.frogs.filter((f) => !f.front)).map((f) => card(p, f));
  for (let i = p.frogs.length; i < p.pads; i++) {
    const d = document.createElement("div");
    d.className = "fcard add";
    d.append(Object.assign(document.createElement("b"), { textContent: "Empty pad" }), Object.assign(document.createElement("span"), { textContent: "An egg can go here" }));
    all.push(d);
  }
  if (p.pad_cost) {
    const b = document.createElement("button");
    b.type = "button"; b.className = "fcard add buy";
    b.disabled = p.pebbles < p.pad_cost;
    const plus = document.createElement("b"), what = document.createElement("span");
    plus.textContent = "+"; what.textContent = "Another pad, " + p.pad_cost.toLocaleString("en-US");
    b.append(plus, what);
    b.addEventListener("click", () => {
      b.disabled = true;
      post("/api/pads/buy").then(setPond, (e) => { $("reeds-out").textContent = e.message; b.disabled = false; });
    });
    all.push(b);
  }
  box.replaceChildren(...all);

  const baskets = p.frogs.filter((f) => f.work && f.work.act && workNow(f.work).n > 0);
  const row = $("reeds-take");
  row.hidden = baskets.length === 0;
  if (baskets.length) {
    const full = baskets.filter((f) => workNow(f.work).stop === "full").length;
    row.querySelector(".nrow-what").textContent = (baskets.length === 1 ? "A basket" : baskets.length + " baskets") + " to take" + (full ? ", " + full + " full" : "");
  }
}

export function initReeds() {
  watch(render);
  const row = $("reeds-take");
  row.prepend(iconCanvas("basket", 2));
  $("jobs-collect").addEventListener("click", function () {
    collect(this).then(() => { this.disabled = false; });
  });
  setInterval(() => { if (state.pond && !$("v-reeds").hidden) render(state.pond); }, 20000);
}

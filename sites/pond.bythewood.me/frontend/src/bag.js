import { $, post, state, setPond, watch, titled } from "./api.js";
import { slot, thingArt, openSheet, closeSheet, sheetSay, button, reveal, ghost } from "./ui.js";
import { pricePicker, toWizard } from "./town.js";

const SORTS = [["all", "All"], ["hat", "Hats"], ["trinket", "Trinkets"], ["rock", "Rocks"], ["food", "Food"], ["dish", "Dishes"], ["material", "Materials"], ["thing", "Other"]];
const SPOTS = ["Left", "Right", "Water"];
let sort = "all";

const USE = {
  food: "Cook it in the garden.",
  rock: "Boil three in the dye works for a dye.",
  dye: "Use it on the egg bench in the reeds.",
  material: "A frog at the craft bench makes things out of it.",
};

function useOf(t) {
  if (t.kind === "geode") return "Crack it to see what's inside.";
  if (/^(dye|stamp|drops)_/.test(t.kind)) return USE.dye;
  if (t.sort === "hat") return "Put it on your frog.";
  if (t.sort === "trinket") return "Set it out round the pad.";
  if (t.sort === "dish") return "A frog that eats one lays a better egg, and Brindle buys anything you cook.";
  return USE[t.sort] || "";
}

function same(h, w) { return h && w && h.shape === w.shape && h.colour === w.colour; }

function act(path, body, done) {
  return (b) => {
    b.disabled = true;
    post(path, body).then((d) => {
      const v = d.pond || d;
      setPond(v);
      if (done) done(d);
      reopen();
    }, (e) => { sheetSay(e.message); b.disabled = false; });
  };
}

let current = "";

function reopen() {
  const t = state.pond.bag.find((x) => x.kind === current);
  if (t) show(t); else closeSheet();
}

export function show(t) {
  current = t.kind;
  const p = state.pond;
  openSheet(t, (t.count > 1 ? "You have " + t.count + ". " : "") + useOf(t), (body) => {
    const row = () => { const r = document.createElement("div"); r.className = "row"; body.append(r); return r; };
    if (t.hat) {
      const r = row();
      if (same(t.hat, p.frog.hat)) r.append(button("Take it off", act("/api/hats/wear", { shape: 0, colour: 0 })));
      else r.append(button("Wear it", act("/api/hats/wear", t.hat), "primary"));
      const hv = p.hats.find((h) => same(h, t.hat));
      if (t.free > 0 && hv) r.append(button("Sell to Wren, " + hv.wren, act("/api/hats/sell", t.hat, () => sheetSay("Wren took it for " + hv.wren + "."))));
    }
    if (t.sort === "trinket") {
      const r = row(), spots = p.trinkets.spots;
      r.classList.add("seg");
      SPOTS.forEach((name, i) => {
        const on = spots[i] === t.kind, b = button(name, act("/api/trinkets/place", { spot: i, kind: on ? "" : t.kind }));
        b.setAttribute("aria-pressed", on ? "true" : "false");
        r.append(b);
      });
      const tv = p.trinkets.all.find((x) => x.kind === t.kind);
      if (t.free > 0 && tv) row().append(button("Sell to Old Moss, " + tv.moss, act("/api/trinkets/sell", { kind: t.kind }, () => sheetSay("Old Moss took it for " + tv.moss + "."))));
    }
    if (t.kind === "geode") {
      row().append(button("Crack it", (b) => {
        b.disabled = true;
        post("/api/geode").then((d) => {
          setPond(d.pond);
          closeSheet();
          reveal({ kick: "Crack!", art: thingArt({ kind: d.prize.item || "peb" }, 8), name: d.prize.text, tier: undefined });
        }, (e) => { sheetSay(e.message); b.disabled = false; });
      }, "primary"));
    }
    if (t.sort === "dish") {
      const hungry = p.frogs.filter((f) => f.stage !== "egg" && !f.fed).sort((a, b) => b.front - a.front);
      if (hungry.length) {
        const r = row();
        hungry.forEach((f, i) => r.append(button("Feed " + titled(f.name), act("/api/frogs/feed", { frog: f.id, dish: t.kind },
          () => sheetSay(titled(f.name) + " ate it up.")), i === 0 ? "primary" : "")));
      }
      row().append(button("Sell to Brindle, " + t.worth, act("/api/kitchen/sell", { dish: t.kind }, () => sheetSay("Brindle took it."))));
    }
    if (t.free > 0) {
      const r = row();
      r.append(button("Put it on your stall", () => { body.textContent = ""; pricePicker(body, t); }));
      if (t.free >= p.wizard.takes && t.sort !== "material") r.append(button("Take three to the wizard", () => { closeSheet(); toWizard(t.kind); }));
    }
  });
}

function render(p) {
  const counts = {};
  p.bag.forEach((t) => { counts[t.sort] = (counts[t.sort] || 0) + 1; });
  const chips = $("bag-sorts");
  chips.textContent = "";
  SORTS.forEach(([k, label]) => {
    if (k !== "all" && !counts[k]) return;
    const b = document.createElement("button");
    b.type = "button"; b.textContent = label;
    b.setAttribute("aria-pressed", k === sort ? "true" : "false");
    b.addEventListener("click", () => { sort = k; render(state.pond); });
    chips.append(b);
  });
  if (sort !== "all" && !counts[sort]) sort = "all";
  const grid = $("bag-grid");
  grid.textContent = "";
  const wearing = p.frog.hat, out = p.trinkets.spots;
  p.bag.filter((t) => sort === "all" || t.sort === sort).forEach((t) => {
    const on = (t.hat && same(t.hat, wearing)) || (t.sort === "trinket" && out.includes(t.kind));
    grid.append(slot(thingArt(t, 4), { tier: t.tier, name: t.name, count: t.count, on, onClick: () => show(t) }));
  });
  $("bag-none").hidden = p.bag.length > 0;
}

export function initBag() { $("bag-none").prepend(ghost("i_bag")); watch(render); }

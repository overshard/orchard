import { $, post, setPond, watch, TIERS } from "./api.js";
import { iconCanvas, furnCanvas } from "./icons.js";
import { hatTile } from "./hats.js";

const SHOPS = ["wren", "brindle", "fenn", "moss"];

function hours(sec) {
  if (sec < 3600) return Math.max(1, Math.round(sec / 60)) + " min";
  return Math.round(sec / 3600) + " h";
}

function tierChip(t) {
  const s = document.createElement("span");
  s.className = "tier t" + t; s.textContent = TIERS[t];
  return s;
}

function thanks(shop, l, d) {
  const name = l.name.toLowerCase();
  if (shop === "wren" && l.wanted) return "Little Tully found the " + name + " you asked for. It's in your hats.";
  if (shop === "wren") return d.spare ? "Little Tully wraps the " + name + ". You had one already, so it's a spare." : "Little Tully wraps the " + name + ". It's in your hats.";
  if (shop === "fenn") return "Tansy sets the " + name.replace(/^a /, "") + " down by the water. Go and have a look.";
  if (shop === "brindle") return "Big Bean hands over the " + name + ". It's in the kitchen.";
  if (l.kind.startsWith("drops_")) return "Moth holds the " + name + " up to the light. They're on the egg bench.";
  if (l.kind.startsWith("stamp_")) return "Moth blows the dust off the " + name + ". It's on the egg bench.";
  if (l.kind.startsWith("dye_")) return "Moth hands over the " + name + ". It's on the egg bench.";
  return "Moth hands over the " + name + ". It's in your purse.";
}

function shelf(shop, sv, pebbles) {
  const ul = $("shelf-" + shop);
  ul.textContent = "";
  sv.lines.forEach((l) => {
    const li = document.createElement("li"), what = document.createElement("span"), name = document.createElement("b");
    const left = l.limit - l.bought, buy = document.createElement("button");
    name.textContent = l.name + (l.limit > 1 && left > 0 ? " x" + left : "");
    what.className = "what";
    what.append(name);
    if (l.hat) what.append(tierChip(l.tier));
    if (l.wanted) {
      const tag = document.createElement("span");
      tag.className = "wantedtag"; tag.textContent = "WANTED";
      what.append(tag);
    }
    if (l.was) {
      const was = document.createElement("s");
      was.textContent = l.was.toLocaleString("en-US");
      what.append(was);
    }
    li.className = left > 0 ? "" : "sold";
    li.append(l.hat ? hatTile(l.hat, true) : l.kind.startsWith("f_") ? furnCanvas(l.kind) : iconCanvas(l.kind, 3), what);
    if (left > 0) {
      buy.type = "button";
      buy.textContent = l.price.toLocaleString("en-US");
      buy.setAttribute("aria-label", "Buy the " + l.name.toLowerCase() + " for " + l.price + " pebbles");
      buy.disabled = pebbles < l.price;
      buy.addEventListener("click", () => {
        buy.disabled = true;
        post("/api/shop/buy", { shop, slot: l.slot }).then((d) => {
          $("shop-out-" + shop).textContent = thanks(shop, l, d);
          setPond(d.pond);
        }, (e) => { $("shop-out-" + shop).textContent = e.message; buy.disabled = false; });
      });
      li.append(buy);
    } else {
      const sold = document.createElement("span");
      sold.className = "soldtag"; sold.textContent = l.kind.startsWith("f_") ? "YOURS" : "SOLD";
      li.append(sold);
    }
    ul.append(li);
  });
}

function render(p) {
  SHOPS.forEach((shop) => shelf(shop, p.shops[shop], p.pebbles));
  $("wren-note").textContent = "New hats at midnight, in " + hours(p.shops.wren.next_in) + ". One of each.";
  const w = p.want, note = $("wren-want");
  note.hidden = !w.hat || w.ready;
  if (w.hat && !w.ready) note.textContent = "Looking out for the hat you asked for. " + w.fill.toLocaleString("en-US") + " of " + w.bar.toLocaleString("en-US") + " in spares so far.";
  $("brindle-note").textContent = p.shops.brindle.sale
    ? "The market's down today, so everything's a quarter off. New stock at midnight."
    : "Prices follow the market, and a down day is a sale. New stock at midnight.";
}

export function initShops() {
  watch(render);
}

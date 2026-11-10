import { $, post, setPond, watch, TIERS } from "./api.js";
import { iconCanvas } from "./icons.js";
import { hatTile } from "./hats.js";

const SHOPS = ["wren", "brindle", "moss"];

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
  if (shop === "wren") return d.spare ? "Wren wraps the " + name + ". You had one already, so it's a spare." : "Wren wraps the " + name + ". It's in your hats.";
  if (shop === "brindle") return "Brindle hands over the " + name + ". It's in the kitchen.";
  if (l.kind.startsWith("stamp_")) return "Old Moss blows the dust off the " + name + ". It's on the egg bench.";
  if (l.kind.startsWith("dye_")) return "Old Moss hands over the " + name + ". It's on the egg bench.";
  return "Old Moss hands over the " + name + ". It's in your purse.";
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
    if (l.was) {
      const was = document.createElement("s");
      was.textContent = l.was.toLocaleString("en-US");
      what.append(was);
    }
    li.className = left > 0 ? "" : "sold";
    li.append(l.hat ? hatTile(l.hat, true) : iconCanvas(l.kind, 3), what);
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
      sold.className = "soldtag"; sold.textContent = "SOLD";
      li.append(sold);
    }
    ul.append(li);
  });
}

function render(p) {
  SHOPS.forEach((shop) => shelf(shop, p.shops[shop], p.pebbles));
  $("wren-note").textContent = "New hats at midnight, in " + hours(p.shops.wren.next_in) + ". One of each.";
  $("brindle-note").textContent = p.shops.brindle.sale
    ? "The market's down today, so everything's a quarter off. New stock at midnight."
    : "Prices follow the market, and a down day is a sale. New stock at midnight.";
}

export function initShops() {
  watch(render);
}

import { genome, bred } from "./frog.js";

// Every route that changes anything takes JSON and nothing else.
export async function post(path, body) {
  const res = await fetch(path, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body || {}),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data.error || "That didn't work. Try again in a moment.");
    err.status = res.status;
    throw err;
  }
  return data;
}

export const $ = (id) => document.getElementById(id);

export const reduced = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

// "OLD PIP" to "Old Pip".
export function titled(name) {
  return name.toLowerCase().replace(/(^| )[a-z]/g, (c) => c.toUpperCase());
}

export const TIERS = ["Common", "Uncommon", "Rare", "Epic", "Legendary"];

// The page keeps one copy of the pond as the server last described it, and
// every view redraws from it when it changes.
export const state = { pond: null, g: null, held: 0 };
const watchers = [];
// canBuild is whether the pond has the pebbles and the materials for it.
export function canBuild(p, b) {
  if (!b.cost || p.pebbles < b.cost) return false;
  return Object.entries(b.mats || {}).every(([k, n]) => {
    const t = p.bag.find((x) => x.kind === k);
    return t && t.count >= n;
  });
}

// heldNow carries a collector like the gravel bar on from what the server
// said, so the count climbs between fetches.
export function heldNow(c) {
  if (!c.rate || c.held >= c.holds) return c.held;
  const t = (performance.now() - state.at) / 1000;
  if (t < c.next_in) return c.held;
  return Math.min(c.holds, c.held + 1 + Math.floor((t - c.next_in) * c.rate / 3600));
}

export function watch(fn) { watchers.push(fn); }
let gKey = "";
export function setPond(pond) {
  state.pond = pond;
  state.at = performance.now();
  const key = pond.frog.seed + JSON.stringify(pond.frog.mods);
  if (key !== gKey) { gKey = key; state.g = bred(genome(pond.frog.seed), pond.frog.mods); }
  watchers.forEach((fn) => fn(pond));
}

// The room a full window game has for its field, measured off the laid out
// view since a phone's toolbar makes window.innerHeight unreliable.
export function room(view) {
  const hud = view.querySelector(".dive-hud");
  return { w: view.clientWidth, h: view.clientHeight - (hud ? hud.offsetHeight : 0) };
}

export function onResize(fn) {
  let t = 0;
  const go = () => { clearTimeout(t); t = setTimeout(fn, 120); };
  window.addEventListener("resize", go);
  if (window.visualViewport) window.visualViewport.addEventListener("resize", go);
}

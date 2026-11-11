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

// The same words the Play cards use, short enough for a readout.
export function payWord(a, low) {
  return a.paid >= a.cap ? "Paid out" : a.runs < a.full ? "Full" : a.daily ? "Free" : low || "Quarter";
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

// workNow carries a frog's basket on from what the server said. The count of
// actions comes off the clock and what they brought off the ones it dealt
// ahead, so the page never puts anything in the basket on its own.
export function workNow(w) {
  const t = w.since + (performance.now() - state.at) / 1000;
  const due = w.stop || !w.act ? w.done : Math.min(w.cap, Math.floor(t / w.act));
  const extra = Math.max(0, Math.min(due - w.done, w.ahead.length));
  const n = w.done + extra, basket = w.basket.map((x) => ({ ...x }));
  let misses = w.misses, xp = w.xp;
  for (let i = 0; i < extra; i++) {
    const a = w.ahead[i];
    misses += a.miss ? 1 : 0;
    xp += a.xp;
    a.got.forEach((x) => {
      const have = basket.find((b) => b.kind === x.kind);
      if (have) have.n += x.n; else basket.push({ ...x });
    });
  }
  const stop = w.stop || (n >= w.cap ? "full" : due > n && w.ahead_short ? "short" : "");
  const waiting = !stop && due > n;
  return { n, basket, misses, xp, stop, waiting, frac: stop || waiting ? 1 : Math.max(0, (t - n * w.act) / w.act), left: stop || waiting ? 0 : (n + 1) * w.act - t };
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

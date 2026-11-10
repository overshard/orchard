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
export function watch(fn) { watchers.push(fn); }
let gKey = "";
export function setPond(pond) {
  state.pond = pond;
  const key = pond.frog.seed + JSON.stringify(pond.frog.mods);
  if (key !== gKey) { gKey = key; state.g = bred(genome(pond.frog.seed), pond.frog.mods); }
  watchers.forEach((fn) => fn(pond));
}

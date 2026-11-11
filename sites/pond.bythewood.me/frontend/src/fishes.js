// Every fish, how it looks and how it fights. d is how deep it swims, win how
// long the bite lasts and pull how fast the line strains when held through a
// run. The server has the same kinds and points and says which are biting.
export const KINDS = [
  { k: "minnow", one: "minnow", many: "minnows", pts: 2, d: [0, 0.35], len: 8, c: "#9fb7c0", b: "#dfe9ec", c2: "#e9f2f4", pat: "line", v: [16, 26], win: 1.1, nib: 1, pull: 0.6, run: [0.4, 0.6], rest: [1, 1.6], rv: 32, stam: 0.6, heavy: 0.8, shy: 1.6 },
  { k: "gudgeon", one: "gudgeon", many: "gudgeon", pts: 3, d: [0.75, 1], len: 9, c: "#8a7a5a", b: "#d9cfae", c2: "#4a3f2a", pat: "spots", v: [10, 18], win: 1.1, nib: 2, pull: 0.7, run: [0.4, 0.6], rest: [1, 1.6], rv: 30, stam: 0.8, heavy: 0.85, shy: 1.6 },
  { k: "roach", one: "roach", many: "roach", pts: 4, d: [0.1, 0.5], len: 10, c: "#aab4b8", b: "#e6ecee", c2: "#7d8a90", fin: "#d2553a", pat: "back", v: [14, 24], win: 1, nib: 2, pull: 0.8, run: [0.4, 0.7], rest: [1, 1.6], rv: 38, stam: 1, heavy: 0.9, shy: 1.6 },
  { k: "rudd", one: "rudd", many: "rudd", pts: 5, d: [0, 0.3], len: 11, fat: 0.46, c: "#c9b56a", b: "#f0e6b8", c2: "#8a7a3a", fin: "#e2412d", pat: "back", v: [14, 24], win: 1, nib: 2, pull: 0.85, run: [0.4, 0.7], rest: [1, 1.6], rv: 40, stam: 1.2, heavy: 0.95, shy: 1.6 },
  { k: "perch", one: "perch", many: "perch", pts: 6, d: [0.2, 0.6], len: 12, c: "#c9b04a", b: "#efe2a0", c2: "#3f5a2a", fin: "#d2553a", pat: "bars", v: [13, 22], win: 0.95, nib: 2, pull: 0.9, run: [0.5, 0.9], rest: [0.9, 1.5], rv: 44, stam: 1.4, heavy: 1, shy: 1.6 },
  { k: "bream", one: "bream", many: "bream", pts: 9, d: [0.45, 0.85], len: 15, fat: 0.5, c: "#c9974f", b: "#ecd1a0", c2: "#8a5a2b", pat: "back", v: [10, 17], win: 0.95, nib: 2, pull: 1, run: [0.6, 1], rest: [0.9, 1.4], rv: 40, stam: 2, heavy: 1.15, shy: 1.4 },
  { k: "trout", one: "trout", many: "trout", pts: 12, d: [0.15, 0.55], len: 16, fat: 0.34, c: "#8f9a6a", b: "#e8c9b4", c2: "#3a3324", pat: "spots", v: [20, 30], win: 0.8, nib: 1, pull: 1.2, run: [0.5, 0.9], rest: [0.7, 1.2], rv: 62, stam: 2, heavy: 1, shy: 1.2 },
  { k: "grayling", one: "grayling", many: "grayling", pts: 13, d: [0.1, 0.5], len: 16, fat: 0.3, c: "#8a94a8", b: "#dfe4ec", c2: "#6a4a8a", fin: "#8a5ccf", pat: "back", v: [18, 28], win: 0.8, nib: 1, pull: 1.2, run: [0.5, 0.9], rest: [0.7, 1.2], rv: 60, stam: 2, heavy: 1, shy: 1.2 },
  { k: "pike", one: "pike", many: "pike", pts: 15, d: [0.35, 0.8], len: 22, fat: 0.3, c: "#6f9a4a", b: "#cfe0a8", c2: "#e9e3d6", pat: "spots", v: [18, 28], win: 0.8, nib: 1, pull: 1.4, run: [0.6, 1.1], rest: [0.7, 1.2], rv: 66, stam: 2.2, heavy: 1.1, shy: 1.2 },
  { k: "eel", one: "eel", many: "eels", pts: 16, d: [0.7, 1], len: 26, fat: 0.14, c: "#4a4a3a", b: "#b9b08a", c2: "#2a2a20", pat: "back", v: [12, 20], win: 0.9, nib: 2, pull: 1.3, run: [0.5, 0.8], rest: [0.5, 0.9], rv: 50, stam: 2.6, heavy: 1, shy: 1.2 },
  { k: "catfish", one: "catfish", many: "catfish", pts: 18, d: [0.85, 1], len: 19, c: "#6b5a4a", b: "#b9a58a", c2: "#2a2218", pat: "back", whisk: true, v: [7, 12], win: 1, nib: 3, pull: 1.2, run: [0.9, 1.5], rest: [0.8, 1.3], rv: 32, stam: 2.7, heavy: 1.5, shy: 1.3 },
  { k: "stickleback", one: "stickleback", many: "sticklebacks", pts: 5, d: [0.05, 0.4], len: 7, c: "#5f8a8a", b: "#e2412d", c2: "#2a4a4a", pat: "bars", v: [20, 30], win: 0.85, nib: 1, pull: 0.7, run: [0.3, 0.5], rest: [0.8, 1.2], rv: 44, stam: 0.8, heavy: 0.8, shy: 1.4 },
  { k: "tench", one: "tench", many: "tench", pts: 14, d: [0.6, 1], len: 18, fat: 0.44, c: "#5a6a3a", b: "#b8b070", c2: "#3a4424", fin: "#3a4424", pat: "back", v: [8, 14], win: 0.95, nib: 3, pull: 1.1, run: [0.7, 1.2], rest: [0.8, 1.3], rv: 38, stam: 2.6, heavy: 1.35, shy: 1.2 },
  { k: "zander", one: "zander", many: "zander", pts: 22, d: [0.4, 0.9], len: 24, fat: 0.26, c: "#8a8f6a", b: "#e0dcc0", c2: "#3a3f2a", pat: "bars", v: [16, 26], win: 0.75, nib: 1, pull: 1.45, run: [0.6, 1.1], rest: [0.6, 1.1], rv: 68, stam: 2.8, heavy: 1.25, shy: 1.1 },
  { k: "carp", one: "carp", many: "carp", pts: 25, d: [0.5, 0.95], len: 26, fat: 0.46, c: "#a8853a", b: "#e6cf8a", c2: "#6e5520", pat: "scales", v: [8, 14], win: 0.9, nib: 3, pull: 1.3, run: [0.8, 1.3], rest: [0.8, 1.3], rv: 42, stam: 3.2, heavy: 1.6, shy: 1.1 },
  { k: "koi", one: "golden koi", many: "golden koi", pts: 40, d: [0.1, 0.9], len: 14, c: "#ffd23a", b: "#fff6dc", c2: "#f07a2a", pat: "patch", v: [22, 32], win: 0.65, nib: 2, pull: 1.6, run: [0.5, 0.9], rest: [0.5, 0.9], rv: 80, stam: 2.4, heavy: 1, shy: 0.9, glint: true },
  { k: "monster", one: "monster pike", many: "monster pike", pts: 50, d: [0.3, 0.8], len: 36, fat: 0.27, huge: true, c: "#56803a", b: "#c2d69a", c2: "#e9e3d6", pat: "spots", v: [14, 22], win: 0.8, nib: 1, pull: 1.6, run: [0.7, 1.2], rest: [0.7, 1.1], rv: 70, stam: 4.2, heavy: 1.9, shy: 1 },
  { k: "sturgeon", one: "sturgeon", many: "sturgeon", pts: 60, d: [0.75, 1], len: 42, fat: 0.24, huge: true, c: "#5a6470", b: "#c9ced2", c2: "#d9dde0", pat: "ridge", v: [7, 12], win: 0.9, nib: 2, pull: 1.5, run: [1, 1.6], rest: [0.8, 1.3], rv: 46, stam: 5, heavy: 2.2, shy: 1 },
  { k: "wels", one: "giant catfish", many: "giant catfish", pts: 80, d: [0.85, 1], len: 46, fat: 0.34, huge: true, c: "#4f463a", b: "#a8997e", c2: "#231c14", pat: "back", whisk: true, v: [5, 9], win: 0.9, nib: 3, pull: 1.7, run: [1.1, 1.8], rest: [0.8, 1.2], rv: 40, stam: 6, heavy: 2.6, shy: 0.9 },
  { k: "moonkoi", one: "moon koi", many: "moon koi", pts: 45, d: [0.1, 0.9], len: 15, c: "#e6eef6", b: "#ffffff", c2: "#8fa3b8", pat: "patch", v: [22, 32], win: 0.65, nib: 2, pull: 1.6, run: [0.5, 0.9], rest: [0.5, 0.9], rv: 82, stam: 2.6, heavy: 1, shy: 0.9, glint: true },
];

// A fish is drawn a column at a time from the nose back, then its tail. In
// the water it's only a shadow, so its size is all there is to go on.
const SHADOW = "#041118";

export function drawFish(x, sp, cx, cy, dir, t, flash, shadow) {
  const L = sp.len, hh = Math.max(3, Math.round(L * (sp.fat || 0.42))), x0 = Math.round(cx), y0 = Math.round(cy), half = Math.round(L / 2);
  const at = (i) => x0 + (dir > 0 ? half - i : i - half);
  const tall = (i) => Math.max(2, Math.round(hh * Math.pow(Math.sin(Math.PI * (i + 0.6) / (L + 0.4)), 0.55)));
  const flat = flash ? "#fff6dc" : shadow ? SHADOW : "";
  for (let i = 0; i < L; i++) {
    const h = tall(i), px = at(i), top = y0 - Math.floor(h / 2), belly = Math.max(1, Math.floor(h / 3));
    x.fillStyle = flat || sp.c; x.fillRect(px, top, 1, h);
    if (flat) continue;
    x.fillStyle = sp.b; x.fillRect(px, top + h - belly, 1, belly);
    x.fillStyle = sp.c2;
    if (sp.pat === "line" && i > 1 && i < L - 1) x.fillRect(px, y0, 1, 1);
    else if (sp.pat === "bars" && i > 2 && i % 3 === 1) x.fillRect(px, top, 1, Math.max(1, h - belly - 1));
    else if (sp.pat === "back" && i > 1) x.fillRect(px, top, 1, 1);
    else if (sp.pat === "spots" && i > 3 && (i * 7) % 5 === 0) x.fillRect(px, top + 1 + (i % 2), 1, 1);
    else if (sp.pat === "scales" && i > 3 && h > 4) x.fillRect(px, top + 1 + (i % 2) * 2, 1, 1);
    else if (sp.pat === "ridge" && i > 4 && i % 4 === 0) { x.fillRect(px, top - 1, 1, 2); x.fillRect(px, y0, 1, 1); }
    else if (sp.pat === "patch" && i > 2 && i % 5 < 2) x.fillRect(px, top, 1, Math.ceil(h / 2));
  }
  const wag = Math.round(Math.sin(t * 12) * (sp.huge ? 2 : 1)), fins = Math.max(3, Math.round(L / 7));
  x.fillStyle = flat || sp.fin || sp.c;
  for (let j = 0; j < fins; j++) { const h = 2 + j * 2; x.fillRect(at(L + j), y0 - Math.floor(h / 2) + wag, 1, h); }
  if (sp.huge && !flash) { x.fillStyle = flat || sp.c; for (let j = 0; j < 4; j++) x.fillRect(at(Math.round(L * 0.45) + j), y0 - Math.floor(tall(Math.round(L * 0.45)) / 2) - 1 - Math.min(j, 2), 1, 2); }
  if (flat) return;
  const ei = L > 30 ? 4 : L > 10 ? 2 : 1;
  x.fillStyle = "#0a0908"; x.fillRect(at(ei), y0 - Math.floor(tall(ei) / 2) + 1, sp.huge ? 2 : 1, sp.huge ? 2 : 1);
  if (sp.whisk) {
    const n = sp.huge ? 6 : 3;
    x.fillStyle = sp.c2; for (let j = 1; j <= n; j++) x.fillRect(at(-j), y0 + j - 1, 1, 1);
  }
  if (sp.glint && Math.floor(t * 3) % 4 === 0) { x.fillStyle = "#fff6dc"; x.fillRect(at(4), y0 - 2, 1, 1); }
}

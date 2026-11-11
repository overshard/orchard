import { state, titled } from "./api.js";
import { drawIcon } from "./icons.js";
import { openSheet, closeSheet } from "./ui.js";

export const MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

// A postcard is the frog's screen as it stands, twice the size on a card
// with its name, the pond's date and the day's weather under it. A frame
// from Marsh changes the card round the picture and nothing in it.
const FRAMES = {
  plain: { name: "Plain card", bg: "#f1e4c8", ink: "#2a241d", dim: "#6b5d48", ring: "#b3a583" },
  pc_lily: { name: "Lily border", bg: "#f1e4c8", ink: "#2a241d", dim: "#6b5d48", ring: "#b3a583", deco: lilies },
  pc_stamp: { name: "Postage stamp", bg: "#fbf6ea", ink: "#2a241d", dim: "#6b5d48", ring: "#c94a4a", deco: perforate },
  pc_night: { name: "Starry night", bg: "#14203a", ink: "#fff6dc", dim: "#9fb7c0", ring: "#3d5a8a", deco: stars },
  pc_gilt: { name: "Gilt frame", bg: "#2a1d10", ink: "#ffe27a", dim: "#c9974f", ring: "#8a5a10", deco: gilt },
};

function lilies(x) {
  const pad = (cx, cy, r) => {
    x.fillStyle = "#3f9a4d"; x.beginPath(); x.moveTo(cx, cy); x.arc(cx, cy, r, 0.4, Math.PI * 2 - 0.1); x.fill();
    x.fillStyle = "#2f7a3f"; x.fillRect(cx - r / 2, cy - 2, r, 4);
  };
  for (let i = 0; i < 9; i++) { pad(40 + i * 90, 22, 18); pad(80 + i * 90, 738, 16); }
  for (let i = 0; i < 7; i++) { pad(22, 110 + i * 70, 16); pad(778, 90 + i * 70, 18); }
  x.fillStyle = "#f59ab3";
  [[30, 30], [770, 30], [26, 620], [770, 700], [420, 22]].forEach(([a, b]) => { x.fillRect(a - 6, b - 6, 12, 12); x.fillStyle = "#ffe27a"; x.fillRect(a - 2, b - 2, 4, 4); x.fillStyle = "#f59ab3"; });
}

function perforate(x) {
  x.fillStyle = "#3a3228";
  for (let i = 0; i <= 800; i += 24) { x.beginPath(); x.arc(i, 0, 8, 0, Math.PI * 2); x.arc(i, 760, 8, 0, Math.PI * 2); x.fill(); }
  for (let i = 0; i <= 760; i += 24) { x.beginPath(); x.arc(0, i, 8, 0, Math.PI * 2); x.arc(800, i, 8, 0, Math.PI * 2); x.fill(); }
  x.fillStyle = "#c94a4a"; x.font = "700 20px 'JetBrains Mono', monospace"; x.textAlign = "right";
  x.fillText("POND POST", 752, 34); x.textAlign = "left";
}

function stars(x) {
  let seed = 7;
  const r = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
  for (let i = 0; i < 400; i++) {
    const sx = r() * 800, sy = r() * 760;
    if (sx > 40 && sx < 760 && sy > 40 && sy < 600) continue;
    x.fillStyle = r() < 0.2 ? "#ffe27a" : "#fff6dc";
    const z = r() < 0.15 ? 4 : 2;
    x.fillRect(Math.round(sx), Math.round(sy), z, z);
  }
}

function gilt(x) {
  x.fillStyle = "#e0b030"; x.fillRect(24, 24, 752, 592);
  x.fillStyle = "#8a5a10"; x.fillRect(32, 32, 736, 576);
  x.fillStyle = "#ffe27a"; x.fillRect(36, 36, 728, 568);
  x.fillStyle = "#a87a12";
  for (let i = 40; i < 760; i += 16) { x.fillRect(i, 28, 8, 4); x.fillRect(i, 608, 8, 4); }
  [[16, 16], [760, 16], [16, 600], [760, 600]].forEach(([a, b]) => { x.fillStyle = "#e0b030"; x.fillRect(a, b, 24, 24); x.fillStyle = "#8a5a10"; x.fillRect(a + 8, b + 8, 8, 8); });
}

function card(scene, frame) {
  const p = state.pond, f = FRAMES[frame] || FRAMES.plain, cv = document.createElement("canvas"), x = cv.getContext("2d");
  cv.width = 800; cv.height = 760;
  x.fillStyle = f.bg; x.fillRect(0, 0, 800, 760);
  if (f.deco) f.deco(x);
  x.fillStyle = "#2a241d"; x.fillRect(44, 44, 712, 552);
  x.imageSmoothingEnabled = false;
  x.drawImage(scene, 48, 48, 704, 544);
  x.fillStyle = f.ink; x.textBaseline = "alphabetic";
  x.font = "700 52px 'Space Grotesk', sans-serif";
  x.fillText(titled(p.frog.name), 46, 668, 520);
  const [y, m, d] = p.today.split("-").map(Number), sky = p.gazette && p.gazette.sky;
  x.fillStyle = f.dim;
  x.font = "22px 'JetBrains Mono', monospace";
  x.fillText(d + " " + MONTHS[m - 1] + " " + y + (sky ? " · " + sky : ""), 48, 712);
  x.textAlign = "right";
  x.font = "18px 'JetBrains Mono', monospace";
  x.fillText("pond.bythewood.me", 752, 712);
  // A postmark in the corner, the pebble off the purse in a ring.
  x.strokeStyle = f.ring; x.lineWidth = 3;
  x.beginPath(); x.arc(706, 652, 34, 0, Math.PI * 2); x.stroke();
  drawIcon(x, "peb", 706 - 20, 652 - 20, 5);
  return cv;
}

// The picture is made here and opened from a blob, since a new tab won't
// go to a data address. With no new tab to be had it's saved as a file.
export function openPicture(cv, name) {
  const data = atob(cv.toDataURL("image/png").split(",")[1]), buf = new Uint8Array(data.length);
  for (let i = 0; i < data.length; i++) buf[i] = data.charCodeAt(i);
  const href = URL.createObjectURL(new Blob([buf], { type: "image/png" }));
  let tab = null;
  try { tab = window.open(href, "_blank"); } catch (e) { /* blocked */ }
  if (tab) return;
  const a = document.createElement("a");
  a.href = href; a.download = name + ".png";
  document.body.append(a); a.click(); a.remove();
}

function snap(scene, frame) {
  const p = state.pond;
  if (p) openPicture(card(scene, frame), p.frog.name.toLowerCase().replace(/ /g, "-") + "-" + p.today);
}

// With a frame or two from Marsh, the camera asks which card first.
function pick(scene) {
  const owned = Object.keys(FRAMES).filter((k) => k === "plain" || (state.pond.frames || []).includes(k));
  openSheet({ bare: true, name: "Postcard" }, "", (body) => {
    const grid = document.createElement("div");
    grid.className = "framepick";
    owned.forEach((k) => {
      const b = document.createElement("button"), small = document.createElement("canvas"), name = document.createElement("span");
      b.type = "button";
      small.width = 160; small.height = 152;
      small.getContext("2d").drawImage(card(scene, k), 0, 0, 160, 152);
      name.textContent = FRAMES[k].name;
      b.append(small, name);
      b.addEventListener("click", () => { closeSheet(); snap(scene, k); });
      grid.append(b);
    });
    body.append(grid);
  });
}

export function initPostcard(scene, key, redraw) {
  drawIcon(key.querySelector("canvas").getContext("2d"), "i_cam", 0, 0, 2);
  key.addEventListener("click", () => {
    redraw();
    if (state.pond && state.pond.frames && state.pond.frames.length) pick(scene); else snap(scene);
  });
}

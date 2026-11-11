import { state, titled } from "./api.js";
import { drawIcon } from "./icons.js";

export const MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

// A postcard is the frog's screen as it stands, twice the size on a cream
// card with its name, the pond's date and the day's weather under it.
function card(scene) {
  const p = state.pond, cv = document.createElement("canvas"), x = cv.getContext("2d");
  cv.width = 800; cv.height = 760;
  x.fillStyle = "#f1e4c8"; x.fillRect(0, 0, 800, 760);
  x.fillStyle = "#2a241d"; x.fillRect(44, 44, 712, 552);
  x.imageSmoothingEnabled = false;
  x.drawImage(scene, 48, 48, 704, 544);
  x.fillStyle = "#2a241d"; x.textBaseline = "alphabetic";
  x.font = "700 52px 'Space Grotesk', sans-serif";
  x.fillText(titled(p.frog.name), 46, 668, 520);
  const [y, m, d] = p.today.split("-").map(Number), sky = p.gazette && p.gazette.sky;
  x.fillStyle = "#6b5d48";
  x.font = "22px 'JetBrains Mono', monospace";
  x.fillText(d + " " + MONTHS[m - 1] + " " + y + (sky ? " · " + sky : ""), 48, 712);
  x.textAlign = "right";
  x.font = "18px 'JetBrains Mono', monospace";
  x.fillText("pond.bythewood.me", 752, 712);
  // A postmark in the corner, the pebble off the purse in a ring.
  x.strokeStyle = "#b3a583"; x.lineWidth = 3;
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

function snap(scene) {
  const p = state.pond;
  if (p) openPicture(card(scene), p.frog.name.toLowerCase().replace(/ /g, "-") + "-" + p.today);
}

export function initPostcard(scene, key, redraw) {
  drawIcon(key.querySelector("canvas").getContext("2d"), "i_cam", 0, 0, 2);
  key.addEventListener("click", () => { redraw(); snap(scene); });
}

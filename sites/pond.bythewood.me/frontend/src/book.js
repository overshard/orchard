import { $, state, watch, titled } from "./api.js";
import { iconCanvas } from "./icons.js";
import { stampCard, paintingArt, PAINTINGS, PIECES } from "./company.js";
import { drawStampDesk } from "./scenes.js";

// The stamp book under Frogs, the painting the stamps are filling in and the
// frogs who stamped this pond's page. The book is asked for on opening it.
let on = false, book = null;

const SAID = { heart: "a heart", fly: "a fly", wave: "a wave", star: "a star", leaf: "a leaf", pebble: "a pebble" };
const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

function when(day, today) {
  const d = new Date(day + "T12:00:00Z"), n = Math.round((new Date(today + "T12:00:00Z") - d) / 86400000);
  if (n <= 0) return "today";
  if (n === 1) return "yesterday";
  if (n < 7) return DAYS[d.getUTCDay()];
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric", timeZone: "UTC" });
}

// Which painting is under way and how much of it is in, or the last one
// whole once all four are done.
function painting(paint) {
  const all = PIECES * PAINTINGS.length;
  if (paint >= all) return [PAINTINGS.length - 1, PIECES, true];
  const at = Math.floor(paint / PIECES);
  return [at, paint - at * PIECES, false];
}

function figure(st, today) {
  const fig = document.createElement("figure"), cap = document.createElement("figcaption"), b = document.createElement("b"), line = document.createElement("span");
  fig.append(stampCard(st, 2), cap);
  cap.append(b, line);
  if (!st) {
    fig.className = "miss";
    b.textContent = "?"; line.textContent = "not yet";
    return fig;
  }
  b.textContent = st.from ? st.from.name : "A quiet frog";
  line.textContent = SAID[st.stamp] + ", " + when(st.day, today);
  if (st.times > 1) {
    const n = document.createElement("span");
    n.textContent = st.times + " days this month";
    cap.append(n);
  }
  return fig;
}

function render(p) {
  const name = titled(p.frog.name);
  drawStampDesk($("stamps-art"), p, book && book.stamps);
  $("book-share").href = p.share.path;
  const [at, have, done] = painting(book ? book.paint : p.bank.burrows.paint), art = paintingArt(at, have, 8, true), cv = $("book-paint");
  cv.width = art.width; cv.height = art.height;
  cv.getContext("2d").drawImage(art, 0, 0);
  $("book-paint-name").textContent = PAINTINGS[at];
  $("book-paint-line").textContent = done ? "All four are done and hung in the burrows" : have + " of " + PIECES + " pieces · each stamp is one";
  $("book-head").textContent = "Frogs who stamped " + name + "'s page";
  if (!book) return;
  const n = book.stamps.length, slots = n >= 12 ? n : Math.min(12, Math.max(6, Math.ceil((n + 1) / 3) * 3));
  $("book-n").textContent = book.ponds || "";
  const figs = [];
  for (let i = 0; i < slots; i++) figs.push(figure(book.stamps[i], p.today));
  $("book-album").replaceChildren(...figs);
  $("book-note").textContent = n ? "A stamp stays in the book for a month." : "Nobody's stamped " + name + "'s page yet. Share it, and any frog with a pond can press a stamp on it once a day.";
  $("book-left").textContent = book.left ? book.left + " more stamp" + (book.left > 1 ? "s" : "") + " to give today" : "no more stamps today";
}

// The row on Frogs that opens the book says how the painting's coming on.
function line(p) {
  const [at, have, done] = painting(p.bank.burrows.paint);
  $("reeds-book-line").textContent = done ? "· all four paintings done" : "· " + PAINTINGS[at].toLowerCase() + ", " + have + " of " + PIECES;
}

export function enterBook() {
  on = true;
  if (state.pond) render(state.pond);
  fetch("/api/book", { credentials: "same-origin" }).then((r) => (r.ok ? r.json() : null)).then((b) => {
    if (!b) return;
    book = b;
    if (on && state.pond) render(state.pond);
  }, () => {});
}

export function leaveBook() { on = false; }

export function initBook() {
  $("reeds-book").prepend(iconCanvas("pc_stamp", 2));
  watch((p) => { line(p); if (on) render(p); });
}

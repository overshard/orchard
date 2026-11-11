import { $, post, state, setPond, watch, titled } from "./api.js";
import { genome, bred, cells, colours, draw, W, H } from "./frog.js";
import { soundOn, setSound, sfx } from "./sound.js";
import { dishCanvas } from "./icons.js";
import { hatTile } from "./hats.js";
import { slot } from "./ui.js";
import { tapsToReel, setTapReel } from "./fish.js";

let which = 0, offers = [];
// The diary on the page, whose it is and the day it was read.
let diary = { frog: 0, day: "", lines: [], more: false };

function drawDiary() {
  const dl = $("frog-diary");
  dl.textContent = "";
  diary.lines.forEach((r) => {
    const [y, m, d] = r.day.split("-").map(Number), dt = document.createElement("dt"), dd = document.createElement("dd");
    dt.textContent = new Date(y, m - 1, d).toLocaleDateString("en-US", { weekday: "short", day: "numeric", month: "short" });
    dd.textContent = r.line;
    dl.append(dt, dd);
  });
  dl.hidden = diary.lines.length === 0;
  $("frog-diary-none").hidden = diary.lines.length > 0;
  $("diary-more").hidden = !diary.more;
}

function readDiary(older) {
  const frog = which, last = diary.lines[diary.lines.length - 1];
  fetch("/api/diary?frog=" + frog + (older && last ? "&before=" + last.day : ""), { credentials: "same-origin" })
    .then((r) => r.ok ? r.json() : null).then((d) => {
      if (!d || frog !== which) return;
      diary.lines = older ? diary.lines.concat(d.lines) : d.lines;
      diary.more = d.more;
      drawDiary();
    });
}

function frogNow(p) {
  return p.frogs.find((f) => f.id === which) || p.frogs.find((f) => f.front);
}

function render(p) {
  const f = frogNow(p);
  which = f.id;
  const seg = $("name-which");
  seg.textContent = "";
  seg.hidden = p.frogs.length < 2;
  p.frogs.forEach((x) => {
    const b = document.createElement("button");
    b.type = "button"; b.textContent = titled(x.name);
    b.setAttribute("aria-pressed", x.id === which ? "true" : "false");
    b.addEventListener("click", () => { which = x.id; offers = []; render(state.pond); });
    seg.append(b);
  });
  $("name-now").textContent = titled(f.name);
  const cv = $("name-frog"), ctx = cv.getContext("2d"), g = bred(genome(f.seed), f.mods);
  ctx.clearRect(0, 0, cv.width, cv.height);
  draw(ctx, cells(g, f.stage, { hat: f.hat.shape, legs: f.day >= 6 }), W, H, colours(g, f.hat), 0, 0, 3);

  const picks = $("name-picks");
  picks.textContent = "";
  offers.forEach((n) => {
    const b = document.createElement("button");
    b.type = "button"; b.textContent = titled(n);
    b.addEventListener("click", () => {
      post("/api/frogs/name", { frog: which, name: n }).then((v) => {
        offers = [];
        $("name-out").textContent = "Hello, " + titled(n) + ".";
        setPond(v);
      }, (e) => { $("name-out").textContent = e.message; });
    });
    picks.append(b);
  });

  if (diary.frog !== which || diary.day !== p.today) {
    diary = { frog: which, day: p.today, lines: [], more: false };
    drawDiary();
    readDiary(false);
  }

  const favs = $("frog-favs");
  favs.textContent = "";
  favs.hidden = !f.fav;
  $("frog-favs-none").hidden = !!f.fav;
  if (f.fav) {
    const full = f.fed && f.fed.fav ? "Full of it now" : "Full half as long again";
    favs.append(slot(dishCanvas(f.fav.dish, 4), { tier: f.fav.dish.tier, name: f.fav.dish.name, sub: full }),
      slot(hatTile(f.fav.hat, true), { tier: f.fav.hat_tier, name: f.fav.hat_name, sub: f.hat.shape === f.fav.hat.shape && f.hat.colour === f.fav.hat.colour ? "Wearing it now" : "" }));
  }

  $("days-n").textContent = p.visited.toLocaleString("en-US");
  const mark = [7, 30, 100, 365].find((n) => n > p.visited);
  $("days-what").textContent = mark ? "A keepsake comes at " + mark + ". They don't have to be in a row." : "Every one of them counted.";

  $("power-n").textContent = p.share.power.total.toLocaleString("en-US");
  const pw = p.share.power;
  $("power-rank").textContent = pw.rank;
  $("power-bar").style.width = pw.fill + "%";
  $("power-next").textContent = pw.next ? pw.next.toLocaleString("en-US") + " more to " + pw.next_name + "." : "The top of the ladder.";
  $("power-ranks").replaceChildren(...pw.ranks.map((r) => {
    const li = document.createElement("li"), b = document.createElement("b"), at = document.createElement("span");
    li.className = r.got ? "got" : "";
    b.textContent = r.name; at.textContent = r.at.toLocaleString("en-US");
    li.append(b, at);
    return li;
  }));
  $("stay-home").setAttribute("aria-pressed", p.home ? "true" : "false");
  $("share-link").href = p.share.path;
  $("dev").hidden = !p.dev;
}

export function initSettings() {
  watch(render);
  $("tap-reel").setAttribute("aria-pressed", tapsToReel() ? "true" : "false");
  $("tap-reel").addEventListener("click", function () {
    setTapReel(!tapsToReel());
    this.setAttribute("aria-pressed", tapsToReel() ? "true" : "false");
  });
  document.querySelectorAll("#sound-keys button[data-sound]").forEach((b) => {
    const kind = b.dataset.sound;
    if (kind === "buzz" && !navigator.vibrate) { b.hidden = true; return; }
    b.setAttribute("aria-pressed", soundOn(kind) ? "true" : "false");
    b.addEventListener("click", () => {
      setSound(kind, !soundOn(kind));
      b.setAttribute("aria-pressed", soundOn(kind) ? "true" : "false");
      if (soundOn(kind)) sfx(kind === "buzz" ? "bonk" : "catch");
    });
  });
  $("stay-home").addEventListener("click", function () {
    this.disabled = true;
    post("/api/home", { home: !state.pond.home }).then((v) => { this.disabled = false; setPond(v); }, () => { this.disabled = false; });
  });
  $("diary-more").addEventListener("click", () => readDiary(true));
  $("name-roll").addEventListener("click", function () {
    this.disabled = true;
    post("/api/frogs/names").then((d) => {
      offers = d.names;
      $("name-out").textContent = "Tap one to keep it.";
      this.disabled = false;
      render(state.pond);
    }, (e) => { $("name-out").textContent = e.message; this.disabled = false; });
  });
}

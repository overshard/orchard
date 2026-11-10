// The race laid over Dive's water: who's still in, your place, who just went
// out, the count in, and the results at the end. It's HTML over the canvas so
// the words stay sharp at any size, and it never takes a tap.
import { $, titled } from "./api.js";
import { cells, colours, draw, W, H } from "./frog.js";

const ORD = { 1: "ST", 2: "ND", 3: "RD" };
export function suffix(n) { return n % 100 > 10 && n % 100 < 14 ? "TH" : ORD[n % 10] || "TH"; }

let racers = [], gone = [], rows = {}, shown = 0, live = [], liveKey = "";

function icon(src) {
  const cv = document.createElement("canvas");
  cv.width = src.width; cv.height = src.height;
  cv.getContext("2d").drawImage(src, 0, 0);
  return cv;
}

// racers is everybody in the run as { id, name, spr, you }, in lineup order.
export function raceStart(list, order) {
  racers = list; gone = []; rows = {}; live = order || list.map((r) => r.id); liveKey = "";
  const ol = $("standings");
  ol.textContent = "";
  racers.forEach((r) => {
    const li = document.createElement("li"), pos = document.createElement("b"), name = document.createElement("span");
    li.className = r.you ? "you" : "";
    name.textContent = r.you ? "YOU" : r.name;
    li.append(pos, icon(r.spr), name);
    ol.append(li);
    rows[r.id] = { li, pos };
  });
  ol.hidden = false;
  shown = 0;
  layout();
  $("results").hidden = true;
  $("confetti").textContent = "";
}

// Whoever is still in by where they are on the water, then whoever went out
// most lately.
function order() {
  const still = live.filter((id) => !gone.includes(id)).map((id) => racers.find((r) => r.id === id));
  return still.concat(gone.slice().reverse().map((id) => racers.find((r) => r.id === id)));
}

// raceOrder takes the ids still in, front first, every frame, and only moves
// anything when it changed.
export function raceOrder(ids) {
  const key = ids.join(",");
  if (key === liveKey) return;
  liveKey = key; live = ids;
  const before = shown;
  layout();
  if (shown < before && shown <= 3) callout(shown === 1 ? "You're in front" : shown === 2 ? "Up to 2nd" : "Onto the podium", "up");
}

function layout(list) {
  (list || order()).forEach((r, i) => {
    const row = rows[r.id];
    row.li.style.transform = "translateY(" + i * 100 + "%)";
    row.pos.textContent = i + 1;
    row.li.classList.toggle("out", gone.includes(r.id));
  });
  setPlace(1 + Math.max(0, live.filter((id) => !gone.includes(id)).indexOf("you")));
}

function setPlace(n) {
  const el = $("place");
  el.hidden = false;
  if (n === shown) return;
  shown = n;
  $("place-n").textContent = n; $("place-sfx").textContent = suffix(n);
  el.className = "place p" + Math.min(n, 4);
  // Restarting the pop means taking the class off and putting it back after
  // the browser has seen it gone.
  el.classList.remove("pop"); void el.offsetWidth; el.classList.add("pop");
}

export function callout(text, kind) {
  const box = $("callouts"), p = document.createElement("p");
  p.className = "callout" + (kind ? " " + kind : "");
  p.textContent = text;
  box.append(p);
  while (box.children.length > 3) box.firstChild.remove();
  setTimeout(() => p.remove(), 1800);
}

export function raceOut(id) {
  if (gone.includes(id)) return;
  gone.push(id);
  const r = racers.find((x) => x.id === id);
  layout();
  callout(r.name + " is out", "out");
}

export function countdown(step) {
  const el = $("countdown");
  el.hidden = step < 0;
  if (step < 0) return;
  el.querySelectorAll(".lights i").forEach((l, i) => { l.className = step >= 3 ? "go" : i < step + 1 ? "on" : ""; });
  $("count-word").textContent = step >= 3 ? "GO!" : String(3 - step);
  el.classList.toggle("go", step >= 3);
}

export function raceHide() {
  $("standings").hidden = true;
  $("place").hidden = true;
  $("results").hidden = true;
  $("callouts").textContent = "";
  $("confetti").textContent = "";
  countdown(-1);
}

function portrait(g, stage, hat, px) {
  const cv = document.createElement("canvas");
  cv.width = W * px; cv.height = H * px;
  draw(cv.getContext("2d"), cells(g, stage, { hat: hat ? hat.shape : 0, legs: true }), W, H, colours(g, hat), 0, 0, px);
  return cv;
}

function countUp(el, to, prefix) {
  const t0 = performance.now(), len = 700;
  (function tick(now) {
    const k = Math.min(1, (now - t0) / len);
    el.textContent = prefix + Math.round(to * (1 - Math.pow(1 - k, 3)));
    if (k < 1) requestAnimationFrame(tick);
  })(t0);
}

function confetti() {
  const box = $("confetti"), cols = ["#ffb000", "#ffe27a", "#e2567a", "#7fd6c2", "#fff6dc", "#6cb8ff"];
  box.textContent = "";
  for (let i = 0; i < 36; i++) {
    const s = document.createElement("i");
    s.style.left = Math.random() * 100 + "%";
    s.style.background = cols[i % cols.length];
    s.style.animationDelay = Math.random() * 0.8 + "s";
    s.style.animationDuration = 1.6 + Math.random() * 1.2 + "s";
    s.style.setProperty("--spin", (Math.random() < 0.5 ? -1 : 1) * (200 + Math.random() * 500) + "deg");
    box.append(s);
  }
}

// finish is everybody's gates, worked out by the page from the deal, then
// the place and prizes once the server has said.
export function raceResults(r) {
  // Most gates first, and a ghost that went out at your gate is behind you.
  const list = r.racers.slice().sort((a, b) => b.gates - a.gates || (b.you ? 1 : 0) - (a.you ? 1 : 0));
  layout(list);
  $("standings").hidden = true;
  $("place").hidden = true;
  const place = r.place || 1 + list.findIndex((x) => x.you);

  $("res-why").textContent = r.why;
  $("res-place").textContent = place; $("res-sfx").textContent = suffix(place);
  $("res-of").textContent = "OF " + list.length;
  $("results").className = "results p" + Math.min(place, 4);

  const pod = $("podium");
  pod.textContent = "";
  [1, 0, 2].forEach((i) => {
    const x = list[i], step = document.createElement("div"), name = document.createElement("span"), block = document.createElement("b");
    step.className = "step s" + (i + 1) + (x.you ? " you" : "");
    name.textContent = x.you ? "YOU" : x.name;
    block.textContent = i + 1;
    step.append(portrait(x.g, x.stage, x.hat, 2), name, block);
    pod.append(step);
  });

  const ol = $("final");
  ol.textContent = "";
  list.forEach((x, i) => {
    const li = document.createElement("li"), n = document.createElement("b"), name = document.createElement("span"), gates = document.createElement("em");
    li.className = x.you ? "you" : "";
    n.textContent = i + 1 + suffix(i + 1);
    name.textContent = x.you ? titled(x.name) + " (you)" : titled(x.name);
    gates.textContent = x.gates + (x.gates === 1 ? " gate" : " gates");
    li.append(n, icon(x.spr), name, gates);
    ol.append(li);
  });

  const win = $("winnings");
  win.textContent = "";
  const add = (k, v, cls) => {
    const dt = document.createElement("dt"), dd = document.createElement("dd");
    dt.textContent = k; dd.className = cls || "";
    win.append(dt, dd);
    if (typeof v === "number") countUp(dd, v, "+"); else dd.textContent = v;
    dd.style.animationDelay = dt.style.animationDelay = 0.7 + win.children.length * 0.06 + "s";
  };
  if (r.paid == null) add("Pebbles", "counting");
  else {
    add("Run", r.paid);
    if (r.bonus) add(suffix(place) === "ST" ? "Winner" : "Podium", r.bonus, "gold");
    else if (r.had && place <= 3) add("Podium", "won already today");
    if (r.hat) add("Hat", r.hat.hat_name + (r.hat.spare ? ", a spare" : ", new"), "gold");
    if (r.best) add("Best", r.score + " points, a new best", "gold");
    else add("Points", String(r.score));
  }
  $("results").hidden = false;
  if (place <= 3 && !r.again) confetti();
}

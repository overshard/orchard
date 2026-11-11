import { $, watch, state, reduced } from "./api.js";
import { openSheet, reveal } from "./ui.js";

// The plate under the frog's screen. It only ever counts up, and only on the
// pond, so a rise that came while the player was elsewhere waits for them.
let shown = null, pending = null, timer = 0;

const n = (x) => x.toLocaleString("en-US");

function rung(pw) {
  const i = pw.ranks.findIndex((r) => r.name === pw.rank);
  const at = pw.ranks[i], next = pw.ranks[i + 1];
  return { at, next, step: next ? Math.max(0, Math.min(1, (pw.total - at.at) / (next.at - at.at))) : 1 };
}

// paint puts a number on the plate with the rung it stands on. During a
// count up that's the higher of the rung it started on and the number's own.
let rungShown = 0;
function paint(total) {
  const pw = state.pond.share.power;
  let i = pw.ranks.findIndex((k) => k.name === pw.rank);
  if (total < pw.total) i = Math.max(rungShown, pw.ranks.filter((k) => k.at <= total).length - 1);
  rungShown = i;
  const on = pw.ranks[i], next = pw.ranks[i + 1];
  $("power-now").textContent = n(total);
  $("power-at").textContent = on.name;
  $("power-step").style.transform = "scaleX(" + (next ? Math.max(0, Math.min(1, (total - on.at) / (next.at - on.at))) : 1) + ")";
  $("power-plate").setAttribute("aria-label", "Frog power " + n(total) + ", " + on.name + ". Open the breakdown.");
}

function take(p) {
  const pw = p.share.power, r = pw.rise;
  if (r) {
    if (!pending) pending = { from: shown === null ? r.from : shown, what: r.what, rank: "", n: 0 };
    pending.n++;
    pending.what = r.what;
    if (r.rank) pending.rank = r.rank;
  }
  if (shown === null) shown = pending ? pending.from : pw.total;
  if (!pending) shown = pw.total;
  if (!timer) paint(shown);
  // A prize that came with the same move pops up first.
  setTimeout(play, 300);
}

// A count up waits for the pond to be in view with no prize over it.
function onPond() {
  return !$("v-pond").hidden && !document.hidden && $("reveal").hidden;
}

function play() {
  if (!pending || !onPond() || timer) return;
  const pw = state.pond.share.power, from = pending.from, to = pw.total, rise = pending;
  pending = null;
  if (to <= from) { shown = to; paint(to); return; }
  const line = $("power-line");
  line.textContent = "+" + n(to - from) + ", " + (rise.what || "more done") + (rise.n > 1 && rise.what && !rise.what.endsWith("and more") ? " and more" : "");
  line.hidden = false;
  if (!reduced) { line.style.animation = "none"; void line.offsetWidth; line.style.animation = ""; }
  const done = () => {
    shown = to; paint(to); timer = 0;
    setTimeout(() => { line.hidden = true; }, reduced ? 3000 : 1600);
    if (rise.rank) rankUp(rise.rank, to);
    play();
  };
  if (reduced) { done(); return; }
  const t0 = performance.now(), took = Math.min(1600, 500 + (to - from) / 4);
  const step = (now) => {
    const k = Math.min(1, (now - t0) / took), e = 1 - Math.pow(1 - k, 3);
    paint(Math.round(from + (to - from) * e));
    if (k < 1) timer = requestAnimationFrame(step); else done();
  };
  timer = requestAnimationFrame(step);
}

function rankUp(name, total) {
  const art = document.createElement("div");
  art.className = "readout";
  const k = document.createElement("span"), v = document.createElement("span");
  k.className = "k"; k.textContent = "Frog power";
  v.className = "v"; v.textContent = n(total);
  art.append(k, v);
  reveal({ kick: "A new rank", name, glow: 4, art, line: "Your pond climbed a rung on the ladder.", ok: "Nice" });
}

function openPower() {
  const pw = state.pond.share.power, r = rung(pw);
  openSheet({ bare: true, name: "Frog power" }, "", (body) => {
    const box = document.createElement("div");
    box.className = "powersheet";
    const head = document.createElement("p");
    head.className = "power";
    const b = document.createElement("b"), s = document.createElement("span");
    b.textContent = n(pw.total); s.textContent = pw.rank;
    head.append(b, s);
    const rail = document.createElement("div");
    rail.className = "ladder";
    rail.innerHTML = '<div class="rail"><i></i></div>';
    rail.querySelector("i").style.width = Math.round(r.step * 100) + "%";
    const next = document.createElement("p");
    next.className = "dim";
    next.textContent = r.next ? n(r.next.at - pw.total) + " more to " + r.next.name + "." : "The top of the ladder.";
    const parts = document.createElement("div");
    parts.className = "parts";
    [["finds", "Finds"], ["frogs", "Frogs and work"], ["games", "Games"]].forEach(([g, label]) => {
      const h = document.createElement("h3"), ul = document.createElement("ul");
      h.textContent = label;
      ul.className = "tally";
      pw.parts.filter((x) => x.group === g).forEach((x) => {
        const li = document.createElement("li"), nm = document.createElement("b"), says = document.createElement("span"), pts = document.createElement("em"), bar = document.createElement("i");
        nm.textContent = x.name; says.textContent = x.says; pts.textContent = n(x.pts);
        bar.style.width = Math.round(100 * x.pts / x.of) + "%";
        li.append(nm, says, pts, bar);
        ul.append(li);
      });
      parts.append(h, ul);
    });
    box.append(head, rail, next, parts);
    body.append(box);
  });
}

// seePower is called when the pond comes into view.
export function seePower() { setTimeout(play, 60); }

export function initPower() {
  watch(take);
  $("power-plate").addEventListener("click", openPower);
  document.addEventListener("visibilitychange", play);
  new MutationObserver(() => setTimeout(play, 250)).observe($("reveal"), { attributes: true, attributeFilter: ["hidden"] });
}

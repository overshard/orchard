import { $, post, state, setPond, watch, reduced, titled, workNow } from "./api.js";
import { iconCanvas } from "./icons.js";
import { openSheet, closeSheet, slot, shade, reveal } from "./ui.js";
import { drawJob, JOB_THING } from "./scenes.js";
import { hatTile } from "./hats.js";
import { genome, bred, cells, colours, draw, W, H } from "./frog.js";

// A frog's job as its own screen: the frog at it in a window, the action
// bar, the basket and the targets. The server deals every action, and this
// only plays out what it said.

let frogId = 0, jobKey = "", pending = null, on = false, raf = 0, shown = "", drawnAt = 0, popAt = 0, lastN = -1, fetched = 0;

export function iconOf(kind) { return kind === "season" ? "duckweed" : kind; }

export function mins(m) {
  if (m < 60) return Math.max(1, Math.round(m)) + "m";
  const h = Math.floor(m / 60), r = Math.round(m % 60);
  return h + "h" + (r ? " " + r + "m" : "");
}

function clock(s) {
  s = Math.max(0, Math.ceil(s));
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), x = s % 60;
  return (h ? h + ":" + String(m).padStart(2, "0") : m) + ":" + String(x).padStart(2, "0");
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function frogArt(f, s) {
  const cv = document.createElement("canvas"), g = bred(genome(f.seed), f.mods);
  cv.width = W * s; cv.height = H * s;
  draw(cv.getContext("2d"), cells(g, f.stage, { hat: f.hat.shape, legs: f.day >= 6 }), W, H, colours(g, f.hat), 0, 0, s);
  return cv;
}

export function defOf(key) { return state.pond.jobs.defs.find((d) => d.key === key); }

function levelOf(f, key) { return f.work.levels.find((l) => l.job === key); }

// How long an action takes this frog, the same sum the server does.
function actMins(f, d, t) {
  const l = levelOf(f, d.key).level;
  let m = t.mins / (1 + (l - 1) / 98);
  if (d.nature && f.nature === d.nature) m /= 1.25;
  return Math.max(1, m);
}

function yieldLine(t) {
  if (t.in) return "uses " + Object.entries(t.in).map(([k, n]) => n + " " + k).join(" and ");
  if (t.kind === "season") return "whatever's in season";
  if (t.kind === "peb") return t.n + (t.n === 1 ? " pebble" : " pebbles");
  if (t.kind === "clay") return t.n + " clay";
  if (t.kind === "sticks" || t.kind === "reeds") return t.n + " " + (t.n === 1 ? t.kind.slice(0, -1) : t.kind);
  const one = t.thing.toLowerCase();
  return t.n + " " + (t.n === 1 || /(cress|weed)$/.test(one) ? one : one.replace(/y$/, "ie") + "s");
}

// openJob shows a frog's job screen, for the job it's at or one to look over.
export function openJob(f, job) {
  pending = { frog: f.id, job: job || f.work.job || "garden" };
  if (location.hash === "#job") enterJob(); else location.hash = "#job";
}

// With nothing asked for it opens on a full basket first, then the frog
// looked at last, then anybody working.
function choose(p) {
  const workers = p.frogs.filter((f) => f.work);
  if (pending) { frogId = pending.frog; jobKey = pending.job; pending = null; }
  else {
    const stuck = workers.find((f) => f.work.stop);
    const was = workers.find((f) => f.id === frogId);
    const pick = stuck || was || workers.find((f) => f.work.job) || workers[0];
    if (pick) { frogId = pick.id; jobKey = pick === was && jobKey ? jobKey : pick.work.job || "garden"; }
  }
}

function frogOf(p) { return p.frogs.find((f) => f.id === frogId && f.work); }

export function enterJob() {
  on = true;
  if (state.pond) { choose(state.pond); render(state.pond); }
  cancelAnimationFrame(raf);
  raf = requestAnimationFrame(tick);
}

export function leaveJob() { on = false; cancelAnimationFrame(raf); }

function setJob(f, job, target, b) {
  if (b) b.disabled = true;
  return post("/api/jobs/set", { frog: f.id, job, target: target || "" }).then((d) => {
    setPond(d.pond);
    if (d.done.length) awayCard(d.done, true);
    return d;
  }, (e) => { $("job-out").textContent = e.message; if (b) b.disabled = false; });
}

// The list of jobs, each with the frog's level in it and a bar to the next.
export function jobSheet(f) {
  openSheet({ art: frogArt(f, 4), name: titled(f.name) }, "Total level " + f.work.total + ". Pick a job to see what it can do.", (body) => {
    state.pond.jobs.defs.forEach((d) => {
      const l = levelOf(f, d.key), b = el("button", "jobrow" + (f.work.job === d.key ? " on" : ""));
      b.type = "button";
      const bar = el("span", "meter"), fill = document.createElement("i");
      fill.style.transform = "scaleX(" + (l.level >= 99 ? 1 : (l.xp - l.from) / Math.max(1, l.to - l.from)) + ")";
      bar.append(fill);
      b.append(iconCanvas(JOB_THING[d.key], 3), el("b", "", d.name + (d.nature && f.nature === d.nature ? " ★" : "")), el("span", "lv", l.level), bar);
      b.addEventListener("click", () => { closeSheet(); openJob(f, d.key); });
      body.append(b);
    });
    if (f.work.job) {
      const r = el("div", "row"), b = el("button", "", "No job");
      b.type = "button";
      b.addEventListener("click", () => setJob(f, "", "", b).then(closeSheet));
      r.append(b);
      body.append(r);
    }
  });
}

function render(p) {
  if (!on) return;
  if (!frogOf(p)) choose(p);
  const f = frogOf(p);
  if (!f) { location.hash = "#reeds"; return; }
  const d = defOf(jobKey), l = levelOf(f, jobKey), at = f.work.job === jobKey;
  const t = at && d.targets.find((x) => x.key === f.work.target);
  $("job-name").textContent = titled(f.name);
  $("job-what").textContent = d.name + (t ? ", " + t.name.toLowerCase() : "");
  $("job-art").setAttribute("aria-label", titled(f.name) + (t ? " " + d.verb : " by the " + d.name.toLowerCase()));
  $("job-level").textContent = l.level;
  $("job-xp").style.transform = "scaleX(" + (l.level >= 99 ? 1 : (l.xp - l.from) / Math.max(1, l.to - l.from)) + ")";
  const next = d.targets.find((x) => x.level > l.level) || d.finds.find((x) => x.level > l.level);
  $("job-next").textContent = l.level >= 99 ? "Level 99, every target open." : next
    ? "Level " + next.level + " opens " + next.name.toLowerCase() + (next.key ? "" : " as a find") + "."
    : (l.to - l.xp).toLocaleString("en-US") + " xp to level " + (l.level + 1) + ".";
  $("job-work").hidden = !at;
  $("job-basket-box").hidden = !at;
  $("job-pick").hidden = !!at;
  $("job-pick").textContent = "Pick a target and " + titled(f.name) + " starts " + d.verb + ".";
  $("job-out").textContent = "";

  const list = $("job-targets");
  list.textContent = "";
  d.targets.forEach((x) => list.append(targetRow(f, d, x, l.level, at && x.key === f.work.target)));
  const finds = $("job-finds");
  finds.textContent = "";
  d.finds.forEach((x) => {
    const got = l.level >= x.level;
    finds.append(slot(iconCanvas(x.kind, 4), { tier: x.tier, name: got ? x.name : "Level " + x.level, found: got ? undefined : false }));
    if (!got) finds.lastChild.querySelector(".nm").textContent = "Level " + x.level;
  });
  $("job-finds-head").hidden = d.finds.length === 0;
  shown = "";
  lastN = -1;
  live(p, f, true);
}

function targetRow(f, d, x, level, cur) {
  const open = level >= x.level, b = el("button", "wk-tgt" + (cur ? " on" : ""));
  b.type = "button";
  const art = iconCanvas(iconOf(x.kind), 3);
  if (!open) shade(art);
  const text = el("span", "wk-what");
  text.append(el("b", "", open ? x.name : "???"));
  if (open) {
    const bits = [mins(actMins(f, d, x)), x.xp.toLocaleString("en-US") + " xp", yieldLine(x)];
    if (x.miss) bits.push(Math.round(x.miss * 10) + " in 10 missed");
    text.append(el("span", "", bits.join(" · ")));
  } else text.append(el("span", "", "Opens at level " + x.level));
  b.append(art, text, el("span", "wk-lv", x.level));
  b.disabled = !open;
  b.setAttribute("aria-pressed", cur ? "true" : "false");
  b.addEventListener("click", () => { if (!cur) setJob(f, d.key, x.key, b); });
  return b;
}

// live redraws what moves: the bar, the time left and the basket when it
// gains an action.
function live(p, f, force) {
  const w = f.work;
  if (w.job !== jobKey || !w.act) return null;
  const n = workNow(w);
  $("job-act").style.transform = "scaleX(" + Math.min(1, n.frac) + ")";
  $("job-left").textContent = n.stop === "full" ? "FULL" : n.stop ? "OUT" : n.waiting ? "..." : clock(n.left);
  $("job-count").textContent = n.n + "/" + w.cap;
  if (n.n !== lastN || force) {
    if (lastN >= 0 && n.n > lastN && !reduced) popAt = performance.now();
    lastN = n.n;
    const box = $("job-basket");
    box.textContent = "";
    n.basket.forEach((x) => box.append(slot(iconCanvas(iconOf(x.kind), 4), { tier: x.tier, name: x.name, count: x.n, sub: x.find ? "find" : "" })));
    if (!n.basket.length) box.append(el("p", "dim wk-empty", n.misses ? "Nothing yet, " + n.misses + " missed." : "Empty for now."));
    $("job-tally").textContent = n.n + (n.n === 1 ? " action" : " actions") + (n.misses ? ", " + n.misses + " missed" : "") + ", " + n.xp.toLocaleString("en-US") + " xp waiting";
    $("job-take").disabled = n.n === 0;
  }
  const d = defOf(w.job);
  const line = n.stop === "full" ? "Basket full, collect to keep " + d.keep
    : n.stop === "short" ? titled(f.name) + " ran out of " + w.short + ". Collect what's made, or bring more."
      : titled(f.name) + " is " + d.verb + ". The basket holds " + w.cap + ".";
  if (line !== shown) { shown = line; $("job-line").textContent = line; $("job-line").classList.toggle("stop", !!n.stop); }
  if (n.waiting && Date.now() - fetched > 20000) {
    fetched = Date.now();
    fetch("/api/pond", { credentials: "same-origin" }).then((r) => r.ok ? r.json() : null).then((v) => { if (v) setPond(v); }, () => {});
  }
  return n;
}

function tick(now) {
  if (!on) return;
  const p = state.pond, f = p && frogOf(p);
  if (f) {
    const n = live(p, f, false), w = f.work, d = defOf(jobKey);
    if (now - drawnAt > 90) {
      drawnAt = now;
      const t = w.job === jobKey && d.targets.find((x) => x.key === w.target);
      const pop = popAt ? Math.max(0, 1 - (now - popAt) / 600) : 0;
      const got = n && n.basket.length ? iconOf(n.basket[0].kind) : "";
      drawJob($("job-art"), {
        job: jobKey, frog: f, night: p.day && p.day.night, working: !!(t && n && !n.stop), still: reduced,
        t: now / 1000, k: n ? n.frac : 0, pop, got, thing: t ? iconOf(t.kind) : JOB_THING[jobKey],
        fill: n ? n.n / w.cap : 0, full: n && n.stop === "full",
      });
    }
  }
  raf = requestAnimationFrame(tick);
}

// ---------------------------------------------------------------- the away card

function rowOf(f) {
  const w = f.work, d = defOf(w.job), t = d.targets.find((x) => x.key === w.target), n = workNow(w);
  const l = levelOf(f, w.job);
  return { frog: titled(f.name), seed: f, job: d.name, target: t ? t.name : "", actions: n.n, misses: n.misses, things: n.basket, xp: n.xp, level: l.level, up: 0, stop: n.stop, why: t ? t.why : "", keep: d.keep, short: w.short };
}

// awayCard is one card for a collect, a block a frog with what it did, what
// came in and what it learned. Before the basket is taken it carries the key
// that takes it.
export function awayCard(rows, took) {
  const p = state.pond;
  openSheet({ bare: true, name: took ? "Collected" : "While you were away" }, "", (body) => {
    body.append(el("h3", "wk-head", took ? "Collected" : "While you were away"));
    rows.forEach((r) => {
      const box = el("div", "wk-away"), head = el("div", "wk-who");
      const f = r.seed || p.frogs.find((x) => titled(x.name) === r.frog);
      if (f) head.append(frogArt(f, 1));
      const name = el("span");
      name.append(el("b", "", r.frog), el("span", "", r.job + (r.target ? ", " + r.target.toLowerCase() : "")));
      head.append(name);
      box.append(head);
      const said = r.actions + (r.actions === 1 ? " action" : " actions") + (r.misses ? ", " + r.misses + " missed, " + (r.why || "nothing came of them") : "") + ".";
      box.append(el("p", "", said));
      const slots = el("div", "slots wk-slots");
      (r.things || []).forEach((x) => slots.append(slot(iconCanvas(iconOf(x.kind), 3), { tier: x.tier, name: x.name, count: x.n, sub: x.find ? "find" : "" })));
      if (slots.childElementCount) box.append(slots);
      const lv = el("p", "wk-lv-line");
      lv.append(el("span", "readout", ""));
      lv.firstChild.append(el("span", "k", "xp"), el("span", "v", "+" + r.xp.toLocaleString("en-US")));
      lv.append(el("span", "", r.up ? "Up " + r.up + (r.up === 1 ? " level" : " levels") + " to " + r.level + "!" : "Level " + r.level));
      if (r.up) lv.lastChild.className = "up";
      box.append(lv);
      if (!took && r.stop === "full") box.append(el("p", "wk-stop", "Basket full, collect to keep " + r.keep));
      if (r.stop === "short") box.append(el("p", "wk-stop", "Ran out of " + (r.short || "materials") + "."));
      body.append(box);
    });
    if (!took) {
      const row = el("div", "row"), b = el("button", "primary", "Take it all");
      b.type = "button";
      b.addEventListener("click", () => collect(b));
      row.append(b);
      body.append(row);
    }
  });
}

export function collect(b) {
  if (b) b.disabled = true;
  return post("/api/jobs/collect").then((d) => {
    setPond(d.pond);
    awayCard(d.done, true);
    big(d.done);
  }, (e) => { if (b) { b.disabled = false; b.textContent = e.message; } });
}

// The greeting shows the card when a basket is full or half way there.
export function greetAway(p) {
  const rows = p.frogs.filter((f) => f.work && f.work.act).map(rowOf).filter((r) => r.actions > 0);
  const lots = p.frogs.some((f) => f.work && f.work.act && (f.work.stop || workNow(f.work).n * 2 >= f.work.cap));
  if (rows.length && lots) awayCard(rows, false);
}

// A pet or a hood is rare enough to pop up like a prize.
export function big(done) {
  const p = state.pond;
  for (const w of done) {
    const pet = Object.keys(w.finds || {}).map((k) => p.trinkets.all.find((t) => t.kind === k && t.pet)).find(Boolean);
    if (pet) reveal({ kick: "A pet!", art: iconCanvas(pet.kind, 8), name: pet.name, tier: pet.tier, line: w.frog + " found it at work. It's in your bag." });
    const hood = w.up && w.level === 99 && p.hoods.find((h) => h.name.startsWith(w.job));
    if (hood) reveal({ kick: "Level 99!", art: hatTile(hood, true), name: hood.name, tier: 4, line: w.frog + " maxed " + w.job.toLowerCase() + ". Any frog can wear the hood, it's in your bag." });
  }
}

export function initJob() {
  watch((p) => { if (on) render(p); });
  $("job-take").addEventListener("click", function () { collect(this); });
  $("job-change").addEventListener("click", () => {
    const f = state.pond && frogOf(state.pond);
    if (f) jobSheet(f);
  });
}

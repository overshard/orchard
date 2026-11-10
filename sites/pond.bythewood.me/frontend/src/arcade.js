import { $, post, state, setPond, watch, room, onResize, payWord } from "./api.js";

// The frame round a side game. A game is a fixed field W by H, drawn at twice
// that and scaled to the window, with reset, step, draw, and input hooks. This
// starts and finishes runs on the server and shows the title and results.

export function arcade(game) {
  const k = game.key, view = $("v-" + k), cv = $(k + "-cv"), cx = cv.getContext("2d");
  const A = { state: "idle", last: 0, overAt: 0, paid: null, best: false, scale: 1 };
  let net = Promise.resolve(), started = null, down = null;

  function info() { return state.pond && state.pond.arcade.find((a) => a.key === k); }

  function note() {
    const a = info();
    if (!a) return;
    $(k + "-best").textContent = a.best;
    $(k + "-today").textContent = a.paid + "/" + a.cap;
    $(k + "-pays").textContent = payWord(a);
  }

  function fit() {
    view.classList.add("full");
    const { w, h } = room(view);
    A.scale = Math.min(w / game.W, h / game.H);
    cv.width = game.W * 2; cv.height = game.H * 2;
    cv.style.width = Math.round(game.W * A.scale) + "px";
    back.width = screen.clientWidth; back.height = screen.clientHeight;
  }

  // A tall phone has room over and under the field, and the field's top and
  // bottom rows are stretched into it so the scene carries on to the edges.
  const screen = view.querySelector(".screen"), back = document.createElement("canvas"), bx = back.getContext("2d");
  back.className = "arcade-back";
  back.setAttribute("aria-hidden", "true");
  screen.prepend(back);

  function backdrop() {
    const bw = back.width, bh = back.height, w = game.W * A.scale, h = game.H * A.scale;
    const left = (bw - w) / 2, gap = Math.ceil((bh - h) / 2) + 1;
    if (gap < 2) return;
    bx.imageSmoothingEnabled = false;
    bx.drawImage(cv, 0, 0, cv.width, 2, left, 0, w, gap);
    bx.drawImage(cv, 0, cv.height - 2, cv.width, 2, left, bh - gap, w, gap);
  }

  // The title and results are a panel over the field, the same as Dive's.
  const card = document.createElement("section");
  card.className = "arcade-card";
  view.querySelector(".screen").append(card);

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function showCard(now) {
    card.hidden = A.state === "play";
    if (card.hidden) return;
    const win = el("div", "arcade-win"), art = el("canvas");
    art.width = 168; art.height = 96;
    const from = $("play-" + k + "-art");
    if (from) art.getContext("2d").drawImage(from, 0, 0);
    win.append(art);
    const kids = [win];
    if (A.state === "idle") {
      kids.push(el("b", "", game.title));
      game.help.filter((l) => l !== "TAP TO START").forEach((l) => kids.push(el("p", "dim", l)));
      kids.push(el("p", "again", "Tap to start"));
    } else {
      kids.push(el("p", "why", game.why() || "Done"));
      const r = el("span", "readout big");
      r.append(el("span", "k", "Score"), el("span", "v", String(game.score())));
      kids.push(r);
      if (game.tally) game.tally().forEach((l) => kids.push(el("p", "dim", l)));
      kids.push(el("p", A.paid ? "won" : "dim", A.paid === null ? "Counting..." : "+" + A.paid + " pebbles"));
      if (A.best) kids.push(el("p", "won", "A new best"));
      if (now - A.overAt > 900) kids.push(el("p", "again", "Tap to go again"));
    }
    card.replaceChildren(...kids);
  }

  function paint(now) {
    cx.setTransform(2, 0, 0, 2, 0, 0);
    cx.imageSmoothingEnabled = false;
    game.draw(cx, now);
    backdrop();
    if (A.state !== "play") showCard(now);
  }

  function loop(now) {
    if (A.state !== "play" || view.hidden) return;
    const dt = Math.min(0.05, (now - A.last) / 1000);
    A.last = now;
    game.step(dt);
    if (game.over()) finish(now);
    paint(now);
    if (A.state === "play") requestAnimationFrame(loop);
  }

  function finish(now) {
    A.state = "over"; A.overAt = now; A.paid = null; A.best = false;
    const score = game.score();
    net = started.then((run) => run ? post("/api/arcade/finish", { game: k, run, score }) : null).then((d) => {
      if (!d) { A.paid = 0; return; }
      A.paid = d.paid; A.best = d.best;
      setPond(d.pond);
    }, () => { A.paid = 0; }).then(() => { if (A.state === "over") paint(performance.now()); });
    setTimeout(() => { if (A.state === "over") paint(performance.now()); }, 950);
  }

  function start() {
    game.reset(state.g, state.pond);
    A.state = "play"; A.last = performance.now();
    card.hidden = true;
    started = net = net.then(() => post("/api/arcade/start", { game: k })).then((d) => d.run, () => null);
    requestAnimationFrame(loop);
  }

  function point(e) {
    const r = cv.getBoundingClientRect();
    return { x: (e.clientX - r.left) / r.width * game.W, y: (e.clientY - r.top) / r.height * game.H };
  }

  function press(e) {
    if (!state.pond) return;
    if (A.state === "over" && performance.now() - A.overAt < 900) return;
    if (A.state !== "play") { start(); return; }
    if (e) { down = point(e); down.moved = false; }
    if (e && game.press) game.press(down.x, down.y);
    if (e && game.drag) game.drag(down.x, down.y);
  }

  // A swipe fires as soon as the finger has gone far enough, so a drag can
  // turn more than once before it lifts.
  function move(e) {
    if (A.state !== "play" || !down) return;
    const p = point(e);
    if (game.drag) game.drag(p.x, p.y);
    if (!game.swipe) return;
    const dx = p.x - down.x, dy = p.y - down.y;
    if (Math.max(Math.abs(dx), Math.abs(dy)) * A.scale < 18) return;
    game.swipe(Math.abs(dx) > Math.abs(dy) ? (dx > 0 ? "right" : "left") : (dy > 0 ? "down" : "up"));
    down = { x: p.x, y: p.y, moved: true };
  }

  function lift(e) {
    if (game.release) game.release();
    if (A.state !== "play" || !down || !game.swipe) { down = null; return; }
    const p = point(e), moved = down.moved;
    down = null;
    if (moved) return;
    if (game.tap) game.tap(p.x, p.y);
    else game.swipe("up");
  }

  $(k + "-exit").addEventListener("pointerdown", (e) => e.stopPropagation());
  $(k + "-exit").addEventListener("click", () => { location.hash = "#play"; });
  view.addEventListener("pointerdown", (e) => { e.preventDefault(); press(e); });
  view.addEventListener("pointermove", move);
  view.addEventListener("pointerup", lift);
  view.addEventListener("pointercancel", lift);
  view.addEventListener("contextmenu", (e) => e.preventDefault());
  document.addEventListener("keydown", (e) => {
    if (view.hidden) return;
    const dir = { ArrowUp: "up", KeyW: "up", ArrowDown: "down", KeyS: "down", ArrowLeft: "left", KeyA: "left", ArrowRight: "right", KeyD: "right" }[e.code];
    if (dir || e.code === "Space" || e.code === "Enter") e.preventDefault();
    if (e.repeat) return;
    if (A.state !== "play") { if (dir || e.code === "Space" || e.code === "Enter") press(null); return; }
    // A game with its own keys gets every one, down and up.
    if (game.keys) { game.keys(e.code, true); return; }
    if (dir && game.hold) game.hold(dir, true);
    if (dir && game.swipe) game.swipe(dir);
    else if ((e.code === "Space" || e.code === "Enter") && game.keyPress) game.keyPress();
  });
  document.addEventListener("keyup", (e) => {
    if (view.hidden) return;
    if (game.keys) { if (A.state === "play") game.keys(e.code, false); return; }
    const dir = { ArrowLeft: "left", KeyA: "left", ArrowRight: "right", KeyD: "right" }[e.code];
    if (dir && game.hold) game.hold(dir, false);
  });
  watch(() => {
    note();
    if (A.state === "play" || view.hidden) return;
    if (A.state === "idle") game.reset(state.g, state.pond);
    paint(performance.now());
  });
  onResize(() => { if (!view.hidden) { fit(); if (A.state !== "play") paint(performance.now()); } });
  if (document.fonts && document.fonts.ready) document.fonts.ready.then(() => { if (!view.hidden && A.state !== "play") paint(performance.now()); });

  return {
    enter() {
      fit();
      if (A.state !== "play") { A.state = "idle"; if (state.pond) game.reset(state.g, state.pond); }
      note(); paint(performance.now());
    },
    // Leaving mid run ends it, so the points still count.
    leave() {
      if (A.state === "play") finish(performance.now());
      view.classList.remove("full");
    },
  };
}

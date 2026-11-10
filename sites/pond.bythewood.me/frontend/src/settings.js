import { $, post, state, setPond, watch, titled } from "./api.js";
import { genome, bred, cells, colours, draw, W, H } from "./frog.js";

let which = 0, offers = [];

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

  $("power-n").textContent = p.share.power.total.toLocaleString("en-US");
  $("power-rank").textContent = p.share.power.rank;
  $("share-link").href = p.share.path;
  $("dev").hidden = !p.dev;
}

export function initSettings() {
  watch(render);
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

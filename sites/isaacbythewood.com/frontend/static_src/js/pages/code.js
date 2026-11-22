import { W, H, genome, cells, colours, draw } from "../frog.js";

const PX = 6;
const still = matchMedia("(prefers-reduced-motion: reduce)").matches;

export function initCode() {
  initFrog();
  initTiles();
  initMap();
  initCounts();
}

// The pond tile draws a frog off a fresh seed each visit, the same way pond
// deals one to every visitor, and a tap deals another.
function initFrog() {
  const cv = document.querySelector("[data-pond-frog]");
  if (!cv) return;
  const ctx = cv.getContext("2d");
  cv.width = W * PX;
  cv.height = H * PX;
  let g, hat, hop, last = 0, blinkAt = 0;

  function deal() {
    g = genome((Math.random() * 2 ** 32) >>> 0);
    const shape = Math.random() < 0.7 ? 1 + Math.floor(Math.random() * 15) : 0;
    hat = { shape, colour: 0 };
    hop = still ? 0 : 1;
  }

  function frame(ms) {
    const t = ms / 1000, dt = last ? t - last : 0;
    last = t;
    if (!blinkAt) blinkAt = t + 2 + Math.random() * 4;
    const blink = t > blinkAt;
    if (t > blinkAt + 0.14) blinkAt = t + 2 + Math.random() * 5;
    const lift = hop > 0 ? Math.sin(Math.PI * (1 - hop)) * 5 : 0;
    hop = Math.max(0, hop - dt * 1.6);
    const c = cells(g, "frog", { t: still ? 0 : t, hat: hat.shape, blink, breathe: still ? 0 : Math.sin(t * 2.2) * 0.35, legs: true });
    ctx.clearRect(0, 0, cv.width, cv.height);
    draw(ctx, c, W, H, colours(g, hat), 0, -Math.round(lift) * PX, PX);
    if (!still) requestAnimationFrame(frame);
  }

  deal();
  requestAnimationFrame(frame);
  cv.addEventListener("click", () => {
    deal();
    if (still) requestAnimationFrame(frame);
  });
}

// One detail open at a time across every group, and tapping the open tile
// closes it.
function initTiles() {
  const tiles = [...document.querySelectorAll("[data-tile]")];
  tiles.forEach((tile) => {
    tile.addEventListener("click", (e) => {
      const opening = tile.getAttribute("aria-expanded") !== "true" || e.target.matches("[data-pond-frog]");
      tiles.forEach((t) => t.setAttribute("aria-expanded", "false"));
      document.querySelectorAll("[data-detail]").forEach((d) => d.classList.remove("is-open"));
      if (!opening) return;
      tile.setAttribute("aria-expanded", "true");
      const detail = document.querySelector(`[data-detail="${tile.dataset.tile}"]`);
      detail.classList.add("is-open");
      const box = detail.getBoundingClientRect();
      if (box.bottom > innerHeight) {
        detail.scrollIntoView({ behavior: still ? "auto" : "smooth", block: "nearest" });
      }
    });
  });
}

// The readout under the map reads what a node is off its tile, so the
// description is written once in the markup.
function initMap() {
  const readout = document.querySelector("[data-readout]");
  if (!readout) return;
  const nodes = [...document.querySelectorAll(".code-topoSite")];

  const show = (node) => {
    const tile = document.querySelector(`[data-tile="${node.dataset.site}"]`);
    if (!tile) return;
    nodes.forEach((n) => n.classList.toggle("is-on", n.dataset.site === node.dataset.site));
    const rows = [["site", tile.dataset.host], ["stack", tile.dataset.stack], ["what", tile.dataset.desc]];
    readout.replaceChildren(...rows.flatMap(([k, v]) => {
      const dt = document.createElement("dt");
      const dd = document.createElement("dd");
      dt.textContent = k;
      dd.textContent = v;
      return [dt, dd];
    }));
  };

  nodes.forEach((node) => {
    node.addEventListener("mouseenter", () => show(node));
    node.addEventListener("focus", () => show(node));
    node.addEventListener("click", () => show(node));
    node.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        document.querySelector(`[data-tile="${node.dataset.site}"]`)?.click();
      }
    });
  });

  if (still) document.querySelectorAll(".code-topo animateMotion").forEach((a) => a.parentNode.remove());
}

// Counts up from zero, but the real number is already in the markup, so a
// failed script or a screenshot still shows it.
function initCounts() {
  if (still) return;
  document.querySelectorAll("[data-count]").forEach((el) => {
    const end = Number(el.dataset.count);
    if (!(end > 1)) return;
    const t0 = performance.now();
    const tick = (now) => {
      const k = Math.min(1, (now - t0) / 1500);
      const e = 1 - Math.pow(1 - k, 3);
      el.textContent = Math.round(end * e).toLocaleString("en-US");
      if (k < 1) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
}

// One readout card for any text the page had to cut short. data-full opens it
// only when the element is actually clipped, and data-tip always, for a source
// tag whose article title isn't on the page at all. A mouse opens it on a short
// hover, a keyboard on focus, and a touch on a tap, or a long press on a link so
// the tap still follows it.

const HOVER_MS = 280;
const PRESS_MS = 450;
const GUTTER = 8;
const GAP = 9;

const card = document.createElement("div");
card.className = "tip";
card.setAttribute("role", "tooltip");
card.id = "tip";
card.hidden = true;
card.innerHTML =
  '<div class="bracket tl"></div><div class="bracket tr"></div>' +
  '<div class="bracket bl"></div><div class="bracket br"></div>' +
  '<i class="tip-lead"></i><div class="tip-plate"><span class="pk"></span><span class="pv"></span></div><p class="tip-txt"></p>';
document.body.append(card);

const lead = card.querySelector(".tip-lead");
const key = card.querySelector(".pk");
const val = card.querySelector(".pv");
const txt = card.querySelector(".tip-txt");

let current = null;
let timer = 0;
let swallowClick = null;

function clipped(el) {
  return el.scrollWidth > el.clientWidth + 1 || el.scrollHeight > el.clientHeight + 1;
}

function target(node) {
  const el = node instanceof Element ? node.closest("[data-full], [data-tip]") : null;
  if (!el) return null;
  if (el.dataset.tip) return el;
  return clipped(el) ? el : null;
}

// Where the text came from, named the way the panel names it: the panel's
// title, then the row number when it sits in a ranked list.
function label(el) {
  const panel = el.closest("[data-panel]");
  const name = panel ? panel.querySelector(".panel-head h2") : null;
  const rank = el.closest("li") ? el.closest("li").querySelector(".rank, .tag") : null;
  return [name ? name.textContent : "", rank && rank.textContent !== "--" ? rank.textContent : ""];
}

function place(el) {
  const r = el.getBoundingClientRect();
  const vw = document.documentElement.clientWidth;
  const vh = window.innerHeight;
  card.style.maxWidth = `${Math.min(520, vw - GUTTER * 2)}px`;
  const w = card.offsetWidth;
  const h = card.offsetHeight;
  const left = Math.max(GUTTER, Math.min(r.left, vw - w - GUTTER));
  const below = r.bottom + GAP + h <= vh - GUTTER || r.top - GAP - h < GUTTER;
  const top = below ? r.bottom + GAP : r.top - GAP - h;
  card.style.left = `${left}px`;
  card.style.top = `${top}px`;
  card.dataset.side = below ? "below" : "above";
  // The leader sits over the start of the text it came from, kept inside the card.
  lead.style.left = `${Math.max(10, Math.min(r.left - left + 12, w - 12))}px`;
}

function show(el) {
  if (current === el && !card.hidden) return;
  current = el;
  const [panel, row] = label(el);
  const tipLabel = el.dataset.tipLabel;
  key.textContent = tipLabel || panel || "READOUT";
  val.textContent = tipLabel ? panel : row;
  val.hidden = !val.textContent;
  txt.textContent = el.dataset.tip || el.dataset.full || el.textContent;
  card.hidden = false;
  card.classList.remove("on");
  place(el);
  void card.offsetWidth;
  card.classList.add("on");
  el.setAttribute("aria-describedby", "tip");
}

function hide() {
  clearTimeout(timer);
  if (current) current.removeAttribute("aria-describedby");
  current = null;
  card.hidden = true;
  card.classList.remove("on");
}

document.addEventListener("pointerover", (e) => {
  if (e.pointerType !== "mouse") return;
  const el = target(e.target);
  clearTimeout(timer);
  if (!el) {
    if (current && !card.contains(e.target)) timer = setTimeout(hide, 120);
    return;
  }
  timer = setTimeout(() => show(el), current ? 0 : HOVER_MS);
});

document.addEventListener("focusin", (e) => {
  const el = target(e.target);
  if (el && e.target.matches(":focus-visible")) show(el);
});
document.addEventListener("focusout", () => hide());

// A tap opens on the way up, since a finger that starts a scroll gets a
// pointercancel instead and shouldn't flash the card on its way past.
let tapped = null;
document.addEventListener("pointerdown", (e) => {
  if (e.pointerType === "mouse") return;
  const el = target(e.target);
  clearTimeout(timer);
  tapped = null;
  if (!el) {
    if (!card.contains(e.target)) hide();
    return;
  }
  if (el.closest("a")) {
    timer = setTimeout(() => {
      show(el);
      swallowClick = el.closest("a");
    }, PRESS_MS);
  } else {
    tapped = el;
  }
});
document.addEventListener("pointerup", () => {
  if (!swallowClick) clearTimeout(timer);
  if (!tapped) return;
  if (current === tapped) hide();
  else show(tapped);
  tapped = null;
});
document.addEventListener("pointercancel", () => {
  clearTimeout(timer);
  tapped = null;
});

// A long press that opened the card shouldn't also follow the link.
document.addEventListener(
  "click",
  (e) => {
    if (swallowClick && swallowClick.contains(e.target)) {
      e.preventDefault();
      e.stopPropagation();
    }
    swallowClick = null;
  },
  true,
);
document.addEventListener("contextmenu", (e) => {
  if (swallowClick) e.preventDefault();
});

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") hide();
});
window.addEventListener("scroll", hide, { passive: true });

// The stream redraws whole lists, so the row a card was opened on can vanish
// from under it.
new MutationObserver(() => {
  if (current && !current.isConnected) hide();
}).observe(document.body, { childList: true, subtree: true });
window.addEventListener("resize", hide);

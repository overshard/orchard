// One readout card for the page. A data-card line opens it on a click, a tap or
// Enter, filled from the row around it with the links that row carries, and a
// mouse gets a preview on hover when the line is cut. data-full is text the
// page had to cut short and data-tip a source tag's headline, which a mouse
// sees on hover and a keyboard on focus. Nothing waits on a long press, since
// a phone keeps that for its own link menu.

const HOVER_MS = 280;
const GUTTER = 8;
const GAP = 9;

const card = document.createElement("div");
card.className = "tip";
card.id = "tip";
card.hidden = true;
card.innerHTML =
  '<div class="bracket tl"></div><div class="bracket tr"></div>' +
  '<div class="bracket bl"></div><div class="bracket br"></div>' +
  '<i class="tip-lead"></i><div class="tip-body"><div class="tip-plate"><span class="pk"></span><span class="pv"></span></div>' +
  '<p class="tip-txt"></p><div class="tip-meta"></div><div class="tip-talk"></div><div class="tip-links"></div></div>';
document.body.append(card);

const lead = card.querySelector(".tip-lead");
const body = card.querySelector(".tip-body");
const key = card.querySelector(".pk");
const val = card.querySelector(".pv");
const txt = card.querySelector(".tip-txt");
const metaBox = card.querySelector(".tip-meta");
const talkBox = card.querySelector(".tip-talk");
const linkBox = card.querySelector(".tip-links");

let current = null;
let pinned = false;
let timer = 0;

function clipped(el) {
  return el.scrollWidth > el.clientWidth + 1 || el.scrollHeight > el.clientHeight + 1;
}

// What a hover or focus would show: a source tag always, a card line or cut
// text only when it's cut.
function preview(node) {
  const el = node instanceof Element ? node.closest("[data-card], [data-full], [data-tip]") : null;
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

function span(className, text) {
  const s = document.createElement("span");
  s.className = className;
  s.textContent = text;
  return s;
}

function cardLink(href, k, t) {
  const a = document.createElement("a");
  a.className = "tip-link";
  a.href = href;
  a.rel = "noopener noreferrer";
  a.target = "_blank";
  a.append(span("k", k), span("t", t), span("arrow", "↗"));
  return a;
}

function section(head, nodes) {
  const box = document.createElement("div");
  box.className = "tip-sec";
  box.append(span("tip-head", head), ...nodes);
  return box;
}

// What a row's card says beyond its links, which the row carries hidden: a
// TL;DR of the article or of every outlet's report, then on a feed row the
// brief's read of the commenters and the top few comments.
function talk(row) {
  const out = [];
  const gist = row.querySelector(".talk .talk-gist");
  if (gist) {
    const p = document.createElement("p");
    p.className = "tip-read";
    p.textContent = gist.textContent;
    out.push(section(gist.dataset.at ? `TL;DR / READ ${gist.dataset.at}` : "TL;DR", [p]));
  }
  const read = row.querySelector(".talk .talk-read");
  if (read) {
    const p = document.createElement("p");
    p.className = "tip-read";
    p.textContent = read.textContent;
    out.push(section(gist ? "COMMENTERS" : read.dataset.at ? `COMMENTERS / READ ${read.dataset.at}` : "COMMENTERS", [p]));
  }
  const said = [...row.querySelectorAll(".talk .talk-said")].map((c) => {
    const p = document.createElement("p");
    p.className = "tip-said";
    p.textContent = c.textContent;
    return p;
  });
  if (said.length) out.push(section("TOP COMMENTS", said));
  const steps = [...row.querySelectorAll(".talk .talk-step")].map((c) => {
    const p = document.createElement("p");
    p.className = "tip-said";
    const day = document.createElement("b");
    day.textContent = `${c.dataset.day} `;
    p.append(day, c.textContent);
    return p;
  });
  if (steps.length) out.push(section("EARLIER", steps));
  return out;
}

// A story row carries its article and comments, a brief row or a read carries
// its sources, each with the headline the row only had room to name.
function extras(el) {
  const meta = [];
  const links = [];
  const talks = [];
  const row = el.closest(".lede, .afd") || el.closest("li");
  if (!row) return [meta, links, talks];
  if (row.classList.contains("lede")) {
    const head = row.querySelector(".head");
    if (head) meta.push([...head.children].map((c) => c.textContent.trim()).filter(Boolean).join(" "));
  }
  const go = row.querySelector(":scope > a.go");
  const cmt = row.querySelector(".cmt[data-href]");
  if (go && cmt) {
    const host = row.querySelector(".host");
    const pts = row.querySelector(".pts");
    const age = row.querySelector(".age");
    if (pts) meta.push(`${pts.firstChild.textContent} PTS`);
    meta.push(`${cmt.firstChild.textContent} CMT`);
    if (age) meta.push(age.textContent);
    links.push(cardLink(go.href, "ARTICLE", host ? host.textContent : new URL(go.href).host));
    links.push(cardLink(cmt.dataset.href, "COMMENTS", new URL(cmt.dataset.href).host));
  }
  talks.push(...talk(row));
  for (const s of row.querySelectorAll(".meta .mark, .meta .cov, .meta .note")) meta.push(s.textContent);
  for (const a of row.querySelectorAll(".meta a.src, .afd-head a.src")) links.push(cardLink(a.href, a.dataset.tipLabel || a.textContent, a.dataset.tip || a.href));
  return [meta, links, talks];
}

function fill(el) {
  const [panel, row] = label(el);
  const tipLabel = el.dataset.tipLabel;
  key.textContent = tipLabel || panel || "READOUT";
  val.textContent = tipLabel ? panel : row || (el.closest(".lede") ? "READ" : "");
  val.hidden = !val.textContent;
  txt.textContent = el.dataset.tip || el.dataset.full || el.textContent.trim();
  const [meta, links, talks] = "card" in el.dataset ? extras(el) : [[], [], []];
  metaBox.replaceChildren(...meta.map((m) => span("", m)));
  metaBox.hidden = meta.length === 0;
  talkBox.replaceChildren(...talks);
  talkBox.hidden = talks.length === 0;
  linkBox.replaceChildren(...links);
  linkBox.hidden = links.length === 0;
}

function place(el) {
  const r = el.getBoundingClientRect();
  const vw = document.documentElement.clientWidth;
  const vh = window.innerHeight;
  card.style.maxWidth = `${Math.min(520, vw - GUTTER * 2)}px`;
  body.style.maxHeight = "";
  const w = card.offsetWidth;
  const h = card.offsetHeight;
  const room = { below: vh - GUTTER - r.bottom - GAP, above: r.top - GAP - GUTTER };
  const below = h <= room.below || room.below >= room.above;
  // The body scrolls rather than the card, since the brackets and the leader
  // sit just outside the card and would give it scrollbars of its own.
  body.style.maxHeight = `${Math.max(120, below ? room.below : room.above) - (h - body.offsetHeight)}px`;
  const left = Math.max(GUTTER, Math.min(r.left, vw - w - GUTTER));
  card.style.left = `${left}px`;
  card.style.top = below ? `${r.bottom + GAP}px` : `${r.top - GAP - Math.min(h, card.offsetHeight)}px`;
  card.dataset.side = below ? "below" : "above";
  // The leader sits over the start of the text it came from, kept inside the card.
  lead.style.left = `${Math.max(10, Math.min(r.left - left + 12, w - 12))}px`;
}

function show(el, pin) {
  clearTimeout(timer);
  if (current === el && !card.hidden) {
    pinned = pinned || pin;
    return;
  }
  if (current) release(current);
  current = el;
  pinned = pin;
  fill(el);
  card.hidden = false;
  card.classList.remove("on");
  place(el);
  void card.offsetWidth;
  card.classList.add("on");
  if ("card" in el.dataset) el.setAttribute("aria-expanded", "true");
  else el.setAttribute("aria-describedby", "tip");
}

function release(el) {
  el.removeAttribute("aria-describedby");
  if ("card" in el.dataset) el.setAttribute("aria-expanded", "false");
}

function hide() {
  clearTimeout(timer);
  if (current) release(current);
  current = null;
  pinned = false;
  card.hidden = true;
  card.classList.remove("on");
}

function toggle(el) {
  if (current === el && pinned) hide();
  else show(el, true);
}

document.addEventListener("pointerover", (e) => {
  if (e.pointerType !== "mouse" || pinned) return;
  const el = preview(e.target);
  clearTimeout(timer);
  if (!el) {
    if (current && !card.contains(e.target)) timer = setTimeout(hide, 120);
    return;
  }
  timer = setTimeout(() => show(el, false), current ? 0 : HOVER_MS);
});

// A click is what a tap becomes once the browser is sure it wasn't a scroll,
// so a finger that starts a scroll on a headline never opens anything.
document.addEventListener("click", (e) => {
  if (card.contains(e.target)) {
    if (e.target.closest("a")) hide();
    return;
  }
  // Anywhere on a row that isn't a link counts as its headline, so a thumb that
  // lands on the meta line under it still opens the card.
  const row = !e.target.closest("a") && e.target.closest(".rows li, .lede");
  const line = e.target.closest("[data-card]") || (row && row.querySelector("[data-card]"));
  if (line) {
    toggle(line);
    return;
  }
  const cut = e.target.closest("[data-full]");
  if (cut && clipped(cut)) {
    toggle(cut);
    return;
  }
  if (current) hide();
});

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && current) {
    const back = pinned && card.contains(document.activeElement) ? current : null;
    hide();
    if (back) back.focus();
    return;
  }
  const line = e.target instanceof Element && e.target.closest("[data-card]");
  if (line && (e.key === "Enter" || e.key === " ")) {
    e.preventDefault();
    toggle(line);
  }
});

document.addEventListener("focusin", (e) => {
  if (card.contains(e.target) || pinned) return;
  const el = preview(e.target);
  if (el && e.target.matches(":focus-visible")) show(el, false);
});
document.addEventListener("focusout", (e) => {
  if (pinned || card.contains(e.relatedTarget)) return;
  if (current) hide();
});

window.addEventListener("scroll", () => current && hide(), { passive: true });

// The stream redraws whole lists, so the row a card was opened on can vanish
// from under it.
new MutationObserver(() => {
  if (current && !current.isConnected) hide();
}).observe(document.body, { childList: true, subtree: true });
window.addEventListener("resize", () => current && hide());

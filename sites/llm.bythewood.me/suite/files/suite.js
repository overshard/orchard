// Margin pins on a home page hero. Each [data-co] inside the staged window
// gets a numbered pin in the gutter beside it and a hairline back to it.
const draw = () => {
  for (const cw of document.querySelectorAll(".o-cw")) {
    const ov = cw.querySelector(".o-ov");
    const win = cw.querySelector(".o-win");
    if (!ov || !win) continue;
    const base = cw.getBoundingClientRect();
    const rel = (el) => {
      const r = el.getBoundingClientRect();
      return { x: r.left - base.left, y: r.top - base.top, w: r.width, h: r.height };
    };
    const w = rel(win);
    let px = w.x + w.w + 18;
    const left = px > cw.clientWidth - 12;
    if (left) px = w.x - 18;
    if (left && px < 12) {
      ov.replaceChildren();
      continue;
    }
    let lines = "";
    let pins = "";
    [...win.querySelectorAll("[data-co]")].forEach((el, i) => {
      const r = rel(el);
      const cy = r.y + Math.min(r.h / 2, 14);
      const sx = left ? r.x - 2 : r.x + r.w + 2;
      lines += `<line x1="${sx}" y1="${cy}" x2="${left ? px + 10 : px - 10}" y2="${cy}" stroke="rgba(238,240,242,.4)" stroke-width="1"/>`;
      pins += `<span class="anc" style="left:${sx}px;top:${cy}px;--d:${i * 0.6}s"></span><span class="pin" style="left:${px}px;top:${cy}px">${i + 1}</span>`;
    });
    ov.innerHTML = `<svg aria-hidden="true">${lines}</svg>${pins}`;
  }
};

if (document.querySelector(".o-cw")) {
  draw();
  addEventListener("resize", draw);
  document.fonts?.ready.then(draw);
}

// On a narrow screen the tool tabs scroll, so start with this tool in view.
const toolTabs = document.querySelector(".o-tools");
if (toolTabs) {
  const on = toolTabs.querySelector("a.on");
  if (on) toolTabs.scrollLeft += on.getBoundingClientRect().left - toolTabs.getBoundingClientRect().left - toolTabs.clientWidth / 2 + on.offsetWidth / 2;
  const fade = () => toolTabs.classList.toggle("more", toolTabs.scrollLeft + toolTabs.clientWidth < toolTabs.scrollWidth - 2);
  toolTabs.addEventListener("scroll", fade, { passive: true });
  addEventListener("resize", fade);
  fade();
}

// The skip link lands on the page's main, which no site has to give an id.
document.querySelector(".o-skip")?.addEventListener("click", (e) => {
  const main = document.querySelector("main");
  if (!main) return;
  e.preventDefault();
  if (!main.hasAttribute("tabindex")) main.tabIndex = -1;
  main.focus();
});

// A table row marked data-href opens on a click anywhere in it, and the link
// inside it stays the real target for keyboards and new tabs.
for (const tr of document.querySelectorAll("tr[data-href]")) {
  tr.classList.add("link");
  tr.addEventListener("click", (e) => {
    if (e.target.closest("a, button, form, input, select, textarea")) return;
    if (getSelection()?.toString()) return;
    location.href = tr.dataset.href;
  });
}

const el = (tag, cls, text) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
};
const clamp = (n, lo, hi) => Math.min(hi, Math.max(lo, n));
const typing = (t) => t.closest?.("input, textarea, select, [contenteditable]") && t.type !== "checkbox";
const thisTool = document.querySelector(".o-tools a.on")?.dataset.tool || "";

// ------------------------------------------------------------------ toasts

const shelf = () => {
  let s = document.querySelector(".o-toasts");
  if (!s) {
    s = el("div", "o-toasts");
    s.setAttribute("aria-live", "polite");
    document.body.append(s);
  }
  return s;
};

// toast shows a line in the corner for ms. With undo it carries an Undo
// button, and done runs once it runs out without being undone.
export const toast = (text, { ms = 3200, undo, done, kind = "" } = {}) => {
  const t = el("div", `o-toast ${kind}`);
  t.append(el("span", "", text));
  t.style.setProperty("--ms", `${ms}ms`);
  let over = false;
  const end = (undone) => {
    if (over) return;
    over = true;
    clearTimeout(timer);
    t.remove();
    if (undone) undo?.();
    else done?.();
  };
  if (undo) {
    const b = el("button", "", "Undo");
    b.type = "button";
    b.addEventListener("click", () => end(true));
    t.append(b);
  }
  t.append(el("i", "cd"));
  shelf().append(t);
  const timer = setTimeout(() => end(false), ms);
  const drop = () => {
    over = true;
    clearTimeout(timer);
    t.remove();
  };
  return { end, drop };
};

// A site's own script can't import this module under its hashed URL, so it
// asks for a toast with an event instead.
document.addEventListener("o-toast", (e) => toast(e.detail?.text || "", e.detail || {}));
for (const n of document.querySelectorAll("[data-toast]")) {
  toast(n.dataset.toast || n.textContent.trim(), { kind: n.dataset.kind || "" });
  n.remove();
}

// ------------------------------------------------------------------ undo

// A form with data-undo posts only once its toast runs out, and the row it
// sits in stays struck through until then. Leaving the page inside the window
// sends it with a beacon, since a navigation can't be started from pagehide.
const pending = new Set();
const bulkRows = new WeakMap();
let beaconed = false;

document.addEventListener("submit", (e) => {
  const f = e.target;
  if (!(f instanceof HTMLFormElement) || !f.dataset.undo || f.method !== "post") return;
  if (f.dataset.undoGo) return;
  e.preventDefault();
  const sub = e.submitter?.form === f ? e.submitter : null;
  const rows = bulkRows.get(f)?.() || [f.closest("tr, li")].filter(Boolean);
  for (const r of rows) r.classList.add("o-gone");
  const job = { f, sub };
  pending.add(job);
  job.toast = toast(f.dataset.undo, {
    ms: 5000,
    undo: () => {
      pending.delete(job);
      for (const r of rows) r.classList.remove("o-gone");
    },
    done: () => {
      pending.delete(job);
      f.dataset.undoGo = "1";
      f.requestSubmit(sub || undefined);
    },
  });
});

addEventListener("pagehide", () => {
  for (const { f, sub, toast: t } of pending) {
    let body;
    try {
      body = new URLSearchParams(new FormData(f, sub));
    } catch {
      body = new URLSearchParams(new FormData(f));
    }
    if (navigator.sendBeacon(f.action, body)) beaconed = true;
    t.drop();
  }
  pending.clear();
});
addEventListener("pageshow", (e) => {
  if (e.persisted && beaconed) location.reload();
});

// ------------------------------------------------------------------ tooltips

// Any element with data-tip shows it beside the pointer, or under itself when
// it has keyboard focus. Moved with transform so it never counts as a shift.
const tip = el("div", "o-tip");
tip.hidden = true;
tip.setAttribute("role", "tooltip");
let tipFor = null;
const placeTip = (x, y, below) => {
  const w = tip.offsetWidth;
  const h = tip.offsetHeight;
  let left = x + 14;
  let top = below ? y + 6 : y + 14;
  if (left + w > innerWidth - 8) left = Math.max(8, x - w - 14);
  if (top + h > innerHeight - 8) top = Math.max(8, y - h - 14);
  tip.style.transform = `translate(${Math.round(left)}px, ${Math.round(top)}px)`;
};
const showTip = (t) => {
  tipFor = t;
  if (!tip.isConnected) {
    tip.style.left = tip.style.top = "0";
    document.body.append(tip);
  }
  tip.textContent = t.dataset.tip;
  tip.hidden = false;
};
const hideTip = () => {
  tipFor = null;
  tip.hidden = true;
};
document.addEventListener("pointerover", (e) => {
  const t = e.target.closest?.("[data-tip]");
  if (t === tipFor) return;
  if (!t || !t.dataset.tip) return hideTip();
  showTip(t);
  placeTip(e.clientX, e.clientY);
});
document.addEventListener("pointermove", (e) => {
  if (tipFor) placeTip(e.clientX, e.clientY);
});
document.addEventListener("pointerout", (e) => {
  if (!e.relatedTarget) hideTip();
});
document.addEventListener("focusin", (e) => {
  const t = e.target.closest?.("[data-tip]");
  if (!t || !t.dataset.tip) return;
  showTip(t);
  const r = t.getBoundingClientRect();
  placeTip(r.left - 14, r.bottom, true);
});
document.addEventListener("focusout", hideTip);
addEventListener("scroll", hideTip, { passive: true });

// ------------------------------------------------------------------ crosshair

// A tile's number written the way the tile wrote it, so 1,234 stays grouped,
// 412ms keeps its unit and 1.2k stays in thousands.
const NUM = /\d[\d,]*(\.\d+)?/;
const SCALE = { k: 1e3, K: 1e3, M: 1e6, B: 1e9 };
const formatLike = (sample, v) => {
  const m = sample.match(NUM);
  if (!m) return String(Math.round(v * 100) / 100);
  let pre = sample.slice(0, m.index);
  const post = sample.slice(m.index + m[0].length);
  const plus = /\+$/.test(pre);
  pre = pre.replace(/[+\-−]$/, "");
  const dec = m[1] ? m[1].length - 1 : 0;
  const n = v / (SCALE[post[0]] || 1);
  const s = Math.abs(n).toLocaleString("en-US", { minimumFractionDigits: dec, maximumFractionDigits: dec, useGrouping: m[0].includes(",") });
  const sign = n < 0 ? "−" : plus && n > 0 ? "+" : "";
  return sign + pre + s + post;
};

// The same scale suite.Spark draws with, so the dot lands on the line.
const sparkY = (vals, v) => {
  const s = [...vals].sort((a, b) => a - b);
  let lo = s[0];
  let hi = s[s.length - 1];
  if (hi - lo < 1e-9) (lo -= 1), (hi += 1);
  return 1.5 + (1 - (v - lo) / (hi - lo)) * 29;
};

for (const row of document.querySelectorAll(".o-strip, .o-tiles")) {
  const tiles = [];
  for (const t of row.querySelectorAll(".o-tile")) {
    const svg = t.querySelector("svg.o-spark[data-v]");
    if (!svg) continue;
    let labels = null;
    try {
      labels = JSON.parse(svg.dataset.l || "null");
    } catch {}
    const b = t.querySelector("b");
    const vals = svg.dataset.v.split(",").map(Number);
    const sorted = [...vals].sort((x, y) => x - y);
    tiles.push({ t, svg, vals, labels, b, d: t.querySelector(".o-delta"), f: b?.dataset.f || b?.textContent.trim() || "", mid: sorted[sorted.length >> 1] });
  }
  const n = tiles[0]?.vals.length;
  if (!n || n < 2 || tiles.some((x) => x.vals.length !== n)) continue;
  row.classList.add("o-xs");
  const labels = tiles.find((x) => x.labels?.length === n)?.labels;
  const when = row.parentElement?.querySelector("[data-xs-when]");
  let saved = null;

  const set = (i) => {
    if (!saved) {
      saved = { tiles: tiles.map((x) => [x.b?.textContent, x.d?.textContent]), when: when && [...when.childNodes] };
      for (const x of tiles) {
        x.xl = el("i", "o-xl");
        x.xd = el("i", "o-xd");
        x.t.append(x.xl, x.xd);
      }
    }
    const label = labels?.[i] || "";
    for (const x of tiles) {
      const tr = x.t.getBoundingClientRect();
      const sr = x.svg.getBoundingClientRect();
      const left = sr.left - tr.left - x.t.clientLeft + (i / (n - 1)) * sr.width;
      const top = sr.top - tr.top - x.t.clientTop;
      x.xl.style.height = `${sr.height}px`;
      x.xl.style.transform = `translate(${left}px, ${top}px)`;
      x.xd.style.transform = `translate(${left}px, ${top + (sparkY(x.vals, x.vals[i]) / 32) * sr.height}px)`;
      if (x.b) x.b.textContent = formatLike(x.f, x.vals[i]);
      if (!x.d) continue;
      if (label && !when) x.d.textContent = label;
      else {
        const pct = x.mid ? Math.round(((x.vals[i] - x.mid) / Math.abs(x.mid)) * 100) : 0;
        x.d.textContent = `${pct > 0 ? "+" : ""}${pct}% on typical`;
      }
    }
    if (when && label) when.replaceChildren("at · ", el("b", "", label));
  };
  const reset = () => {
    if (!saved) return;
    tiles.forEach((x, k) => {
      if (x.b) x.b.textContent = saved.tiles[k][0];
      if (x.d) x.d.textContent = saved.tiles[k][1];
      x.xl.remove();
      x.xd.remove();
    });
    if (when) when.replaceChildren(...saved.when);
    saved = null;
  };
  row.addEventListener("pointermove", (e) => {
    const x = tiles.find((x) => x.t.contains(e.target));
    if (!x) return reset();
    const r = x.svg.getBoundingClientRect();
    set(Math.round(clamp((e.clientX - r.left) / r.width, 0, 1) * (n - 1)));
  });
  row.addEventListener("pointerleave", reset);
}

// ------------------------------------------------------------------ keyboard rows

// A table with data-keys moves with j and k, opens a row with enter and picks
// one with x. data-bulk names a form that gets the picked rows' ids and shows
// as a bar while any are picked.
const keyTables = [...document.querySelectorAll("table[data-keys]")].map((tb) => ({
  tb,
  cur: -1,
  form: tb.dataset.bulk ? document.getElementById(tb.dataset.bulk) : null,
}));
let active = keyTables[0];
const rowsOf = (st) => [...st.tb.tBodies].flatMap((b) => [...b.rows]).filter((r) => !r.classList.contains("o-drawer"));
const boxOf = (r) => r.querySelector('input[type="checkbox"]');
const idOf = (r, b) => (b.hasAttribute("value") ? b.value : r.dataset.id || "");

const setCur = (st, i) => {
  const rows = rowsOf(st);
  if (!rows.length) return;
  for (const r of st.tb.querySelectorAll("tr.o-cur")) {
    r.classList.remove("o-cur");
    r.removeAttribute("aria-current");
  }
  st.cur = clamp(i, 0, rows.length - 1);
  const r = rows[st.cur];
  r.classList.add("o-cur");
  r.setAttribute("aria-current", "true");
  r.scrollIntoView({ block: "nearest" });
};

const sync = (st) => {
  const rows = rowsOf(st);
  const on = [];
  for (const r of rows) {
    const b = boxOf(r);
    r.classList.toggle("o-sel", !!b?.checked);
    if (b?.checked) on.push([r, b]);
  }
  const all = st.tb.tHead?.querySelector('input[type="checkbox"]');
  if (all) {
    const boxes = rows.filter(boxOf).length;
    all.checked = boxes > 0 && on.length === boxes;
    all.indeterminate = on.length > 0 && on.length < boxes;
  }
  const f = st.form;
  if (!f) return;
  for (const i of f.querySelectorAll("input[data-bulk-id]")) i.remove();
  for (const [r, b] of on) {
    const i = el("input");
    Object.assign(i, { type: "hidden", name: f.dataset.name || "id", value: idOf(r, b) });
    i.dataset.bulkId = "";
    f.append(i);
  }
  let count = f.querySelector(".o-bulk-n");
  if (!count) {
    count = el("span", "o-bulk-n");
    f.prepend(count);
  }
  count.textContent = `${on.length} selected`;
  f.hidden = !on.length;
};

const toggleRow = (st, r) => {
  const d = r.nextElementSibling;
  if (d?.classList.contains("o-drawer")) {
    d.hidden = !d.hidden;
    r.setAttribute("aria-expanded", String(!d.hidden));
    return true;
  }
  return false;
};

let keysDlg = null;
const showKeys = () => {
  if (!keysDlg) {
    keysDlg = el("dialog", "o-keys");
    keysDlg.setAttribute("aria-label", "Keys");
    keysDlg.append(el("span", "o-lab", "keys"));
    const keys = [
      [["j", "↓"], "next row"],
      [["k", "↑"], "previous row"],
      [["enter"], "open the row"],
      [["x"], "pick the row"],
      [["esc"], "clear the picks"],
      [["?"], "show or hide this"],
      [["⌘K", "/"], "jump or ask"],
      [[">"], "in jump, actions only"],
    ];
    for (const [ks, what] of keys) {
      const d = el("div");
      for (const k of ks) d.append(el("kbd", "", k));
      d.append(what);
      keysDlg.append(d);
    }
    keysDlg.addEventListener("click", (e) => {
      if (e.target === keysDlg) keysDlg.close();
    });
    document.body.append(keysDlg);
  }
  keysDlg.open ? keysDlg.close() : keysDlg.showModal();
};

for (const st of keyTables) {
  if (st.form) {
    st.form.classList.add("o-bulk");
    bulkRows.set(st.form, () => rowsOf(st).filter((r) => boxOf(r)?.checked));
  }
  for (const d of st.tb.querySelectorAll("tr.o-drawer")) {
    d.hidden = true;
    d.previousElementSibling?.setAttribute("aria-expanded", "false");
    d.previousElementSibling?.classList.add("link");
  }
  st.tb.addEventListener("change", (e) => {
    if (e.target.closest("thead")) for (const r of rowsOf(st)) if (boxOf(r)) boxOf(r).checked = e.target.checked;
    sync(st);
  });
  st.tb.addEventListener("click", (e) => {
    const r = e.target.closest("tr");
    if (!r || r.closest("thead") || r.classList.contains("o-drawer")) return;
    const i = rowsOf(st).indexOf(r);
    if (i < 0) return;
    active = st;
    setCur(st, i);
    if (!e.target.closest("a, button, input, select, textarea, label, form")) toggleRow(st, r);
  });
  sync(st);
}
addEventListener("pageshow", () => keyTables.forEach(sync));

if (keyTables.length) {
  addEventListener("keydown", (e) => {
    if (e.metaKey || e.ctrlKey || e.altKey || typing(e.target)) return;
    if (document.querySelector("dialog[open]:not(.o-keys)")) return;
    if (e.key === "?") {
      e.preventDefault();
      return showKeys();
    }
    if (keysDlg?.open) return;
    const st = active;
    const rows = rowsOf(st);
    const r = rows[st.cur];
    const k = e.key;
    if (k === "j" || (k === "ArrowDown" && r)) setCur(st, st.cur + 1);
    else if (k === "k" || (k === "ArrowUp" && r)) setCur(st, st.cur < 0 ? 0 : st.cur - 1);
    else if (k === "Enter" && r && !e.target.closest("a, button, summary")) {
      if (toggleRow(st, r)) return e.preventDefault();
      const href = r.dataset.href || r.querySelector("a[href]")?.href;
      if (!href) return;
      if (e.shiftKey) open(href, "_blank", "noopener");
      else location.href = href;
    } else if (k === "x" && r && boxOf(r)) {
      boxOf(r).checked = !boxOf(r).checked;
      sync(st);
    } else if (k === "Escape" && (r || rows.some((x) => boxOf(x)?.checked))) {
      for (const x of rows) if (boxOf(x)) boxOf(x).checked = false;
      for (const x of st.tb.querySelectorAll("tr.o-cur")) x.classList.remove("o-cur");
      st.cur = -1;
      sync(st);
    } else return;
    e.preventDefault();
  });
}

// ------------------------------------------------------------------ jump

// Every tool's state and things, merged by auth and asked for through this
// page's own origin. The last copy paints first so the dots don't blink in.
const jumpBtn = document.querySelector("[data-jump]");
const STORE = "orchard-index";
let index = null;

const paint = (data) => {
  index = data;
  for (const a of document.querySelectorAll(".o-tools a[data-tool]")) {
    a.querySelector(".o-tst")?.remove();
    const st = data.tools.find((t) => t.tool === a.dataset.tool);
    if (!st) continue;
    const m = el("span", `o-tst ${st.level || ""}`);
    m.append(el("i"));
    if (st.count && st.level && st.level !== "ok") m.append(String(st.count));
    a.append(m);
    a.title = st.note || "";
  }
  toolTabs?.dispatchEvent(new Event("scroll"));
};

if (jumpBtn) {
  try {
    const kept = sessionStorage.getItem(STORE);
    if (kept) paint(JSON.parse(kept));
  } catch {}
  fetch("/suite/index.json", { credentials: "same-origin" })
    .then((r) => (r.ok ? r.json() : null))
    .then((d) => {
      if (!d) return;
      paint(d);
      try {
        sessionStorage.setItem(STORE, JSON.stringify(d));
      } catch {}
    })
    .catch(() => {});
}

// Jump or ask. Plain matching over what every tool reported, no model. A few
// words ask for something instead of naming it, like down or logs pond 1h,
// and a leading > lists only what can be done.
const TROUBLE = new Set(["down", "failing", "failed", "broken", "bad", "trouble", "wrong", "errors"]);
const ALIAS = { logs: "logging", log: "logging", uptime: "status", jobs: "cron", reminders: "cron", keys: "llm", calls: "llm", git: "repos", views: "analytics" };
const RANGES = new Set(["1h", "24h", "7d", "30d", "90d", "1y"]);
const rank = { bad: 3, warn: 1.5 };

// Letters of q in order anywhere in s, scored up for runs and word starts.
const fuzzy = (q, s) => {
  const idx = [];
  let score = 0;
  let prev = -2;
  for (let i = 0, j = 0; i < s.length && j < q.length; i++) {
    if (s[i] !== q[j]) continue;
    idx.push(i);
    score += i === prev + 1 ? 5 : 1;
    if (i === 0 || /[\s/._:-]/.test(s[i - 1])) score += 4;
    prev = i;
    j++;
  }
  if (idx.length < q.length) return null;
  return { score: score - (idx[idx.length - 1] - idx[0]) * 0.15, idx };
};

// What this page itself can do, from any button or link marked data-act.
const pageActions = () =>
  [...document.querySelectorAll("[data-act]")]
    .filter((n) => !n.closest("[hidden]"))
    .map((n) => ({ tool: thisTool, kind: "action", name: n.dataset.act || n.textContent.trim(), note: n.title || "on this page", run: () => n.click() }));

const builtinActions = () => {
  const out = [{ tool: thisTool, kind: "action", name: "copy a link to this page", note: location.host + location.pathname, run: copyLink }];
  if (keyTables.length) out.push({ tool: thisTool, kind: "action", name: "show the keys", note: "j k enter x on this table", run: showKeys });
  return out;
};
const copyLink = () => {
  navigator.clipboard?.writeText(location.href).then(
    () => toast("Copied a link to this page"),
    () => toast("Couldn't reach the clipboard", { kind: "bad" }),
  );
};

const everything = () => {
  const out = [];
  if (!index) return out;
  for (const t of index.tools) {
    const home = (document.querySelector(`.o-tools a[data-tool="${t.tool}"]`) || {}).href;
    if (home) out.push({ tool: t.tool, kind: "tool", name: t.tool, href: home, level: t.level, note: t.note || "" });
    for (const it of t.items || []) out.push({ ...it, tool: t.tool, note: it.note || "" });
  }
  return out;
};

const score = (i, words) => {
  const name = i.name.toLowerCase();
  const rest = `${i.note} ${i.kind} ${i.tool}`.toLowerCase();
  let s = 0;
  const hl = [];
  for (const w of words) {
    const at = name.indexOf(w);
    if (at >= 0) {
      s += at === 0 ? 6 : /[\s./_-]/.test(name[at - 1]) ? 4 : 3;
      for (let k = 0; k < w.length; k++) hl.push(at + k);
      continue;
    }
    const f = w.length > 1 && fuzzy(w, name);
    if (f) {
      s += 1 + 1.5 * clamp(f.score / (w.length * 9), 0, 1);
      hl.push(...f.idx);
    } else if (rest.includes(w)) s += 1;
    else return null;
  }
  return { s, hl };
};

const search = (raw) => {
  const q = raw.trim();
  const acting = q.startsWith(">");
  const all = everything();
  const actions = [...pageActions(), ...builtinActions(), ...all.filter((i) => i.kind === "action")];
  let words = (acting ? q.slice(1) : q).toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) {
    if (acting) return actions.map((i) => ({ ...i, grp: "actions" }));
    const hot = all.filter((i) => rank[i.level]).sort((a, b) => (rank[b.level] || 0) - (rank[a.level] || 0));
    return [...hot.map((i) => ({ ...i, grp: "needs you" })), ...all.filter((i) => i.kind === "tool" && !rank[i.level]).map((i) => ({ ...i, grp: "tools" }))];
  }
  let pool = acting ? actions : [...all.filter((i) => i.kind !== "action"), ...actions];
  let trouble = false;
  if (!acting && TROUBLE.has(words[0])) {
    trouble = true;
    pool = pool.filter((i) => rank[i.level]);
    words = words.slice(1);
  }
  const tool = ALIAS[words[0]] || (all.some((i) => i.kind === "tool" && i.name === words[0]) ? words[0] : null);
  if (tool && words.length > 1) {
    pool = pool.filter((i) => i.tool === tool);
    words = words.slice(1);
  }
  const asks = [];
  if (tool === "logging" || (!tool && !trouble && !acting)) {
    const sources = new Set(all.filter((i) => i.tool === "logging" && i.kind === "source").map((i) => i.name.toLowerCase()));
    const p = new URLSearchParams();
    const rest = [];
    for (const w of words) {
      if (RANGES.has(w)) p.set("range", w);
      else if (tool === "logging" && sources.has(w) && !p.has("source")) p.set("source", w);
      else rest.push(w);
    }
    if (rest.length) p.set("q", rest.join(" "));
    const what = [p.get("source"), p.get("q") && `"${p.get("q")}"`, p.get("range")].filter(Boolean).join(" ");
    if (what) asks.push({ tool: "logging", kind: acting ? "action" : "search", name: `search logs for ${what}`, href: `https://logging.bythewood.me/search?${p}`, note: "every raw line" });
    words = words.filter((w) => !RANGES.has(w));
  }
  const scored = [];
  for (const i of pool) {
    const m = score(i, words);
    if (!m) continue;
    const here = i.tool === thisTool && i.kind === "action" ? 0.5 : 0;
    scored.push([m.s + (rank[i.level] || 0) - (i.kind === "page" ? 0.5 : 0) + here, { ...i, hl: m.hl }]);
  }
  scored.sort((a, b) => b[0] - a[0]);
  const found = scored.slice(0, 40).map((x) => x[1]);
  // A window or a search word means a search was asked for, not a page.
  return tool === "logging" && asks.length ? [...asks, ...found] : [...found, ...asks];
};

// A name with the matched letters marked, built as nodes so nothing a tool
// reported is ever parsed as markup.
const marked = (name, hl) => {
  const span = el("span", "n");
  const on = new Set(hl || []);
  let run = "";
  let inMark = false;
  const flush = () => {
    if (!run) return;
    span.append(inMark ? el("mark", "", run) : run);
    run = "";
  };
  for (let i = 0; i < name.length; i++) {
    const m = on.has(i);
    if (m !== inMark) {
      flush();
      inMark = m;
    }
    run += name[i];
  }
  flush();
  return span;
};

const run = (it, fresh) => {
  if (!it) return;
  if (it.run) return it.run();
  const href = new URL(it.href, location.href);
  if (it.post && href.origin === location.origin) {
    const f = el("form");
    f.method = "post";
    f.action = it.post;
    f.hidden = true;
    document.body.append(f);
    return f.submit();
  }
  if (fresh) open(href, "_blank", "noopener");
  else location.href = href;
};

if (jumpBtn) {
  const dlg = el("dialog", "o-jd");
  dlg.setAttribute("aria-label", "Jump or ask");
  const bar = el("div", "o-jin");
  bar.append(el("span", "p", "$"));
  const input = el("input");
  Object.assign(input, { type: "text", placeholder: "jump or ask… down · failing · logs pond 1h · > for actions", autocomplete: "off", spellcheck: false });
  input.setAttribute("aria-label", "Jump to");
  input.setAttribute("role", "combobox");
  input.setAttribute("aria-controls", "o-jr");
  input.setAttribute("aria-expanded", "true");
  bar.append(input);
  const list = el("ul", "o-jr");
  list.id = "o-jr";
  list.setAttribute("role", "listbox");
  const foot = el("div", "o-jft");
  for (const t of ["↑↓ move", "↵ open", "⌘↵ new tab", "> actions", "esc close", "down · failing · logs <source> 1h"]) foot.append(el("span", "", t));
  dlg.append(bar, list, foot);
  document.body.append(dlg);

  let results = [];
  let sel = 0;
  const draw = () => {
    results = search(input.value);
    sel = clamp(sel, 0, Math.max(results.length - 1, 0));
    list.replaceChildren();
    if (!results.length) list.append(el("li", "o-jnone", index ? "Nothing matches. Try a name, down, failing or > for actions." : "Asking every tool…"));
    let grp = "";
    results.forEach((it, n) => {
      if (it.grp && it.grp !== grp) {
        grp = it.grp;
        const g = el("li", "o-jg", grp);
        g.setAttribute("role", "presentation");
        list.append(g);
      }
      const li = el("li", n === sel ? "on" : "");
      li.id = `o-jr-${n}`;
      li.dataset.i = n;
      li.setAttribute("role", "option");
      li.append(el("span", `o-dot ${it.level === "bad" ? "bad" : it.level === "warn" ? "warn" : it.kind === "search" || it.kind === "action" ? "mute" : ""}`));
      const name = marked(it.name, it.hl);
      if (it.kind === "action") name.prepend(el("span", "gt", "> "));
      li.append(name);
      li.append(el("span", "k", `${it.tool} · ${it.kind}`));
      const away = it.post && it.href && new URL(it.href, location.href).origin !== location.origin;
      li.append(el("span", "x", away ? `opens ${it.tool} to do it · ${it.note}` : it.note || ""));
      li.addEventListener("mousemove", () => {
        if (sel !== n) {
          sel = n;
          mark();
        }
      });
      li.addEventListener("click", (e) => go(it, e.metaKey || e.ctrlKey));
      list.append(li);
    });
    mark();
  };
  const mark = () => {
    for (const li of list.querySelectorAll("li[data-i]")) li.classList.toggle("on", +li.dataset.i === sel);
    const on = list.querySelector(`li[data-i="${sel}"]`);
    if (on) input.setAttribute("aria-activedescendant", on.id);
    else input.removeAttribute("aria-activedescendant");
    on?.scrollIntoView({ block: "nearest" });
  };
  const go = (it, fresh) => {
    if (!it) return;
    dlg.close();
    run(it, fresh);
  };
  const show = (start = "") => {
    if (dlg.open) return;
    input.value = start;
    sel = 0;
    draw();
    dlg.showModal();
    input.focus();
  };
  input.addEventListener("input", () => {
    sel = 0;
    draw();
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown" || (e.key === "n" && e.ctrlKey)) sel = Math.min(sel + 1, results.length - 1);
    else if (e.key === "ArrowUp" || (e.key === "p" && e.ctrlKey)) sel = Math.max(sel - 1, 0);
    else if (e.key === "PageDown") sel = Math.min(sel + 8, results.length - 1);
    else if (e.key === "PageUp") sel = Math.max(sel - 8, 0);
    else if (e.key === "Enter") return go(results[sel], e.metaKey || e.ctrlKey);
    else return;
    e.preventDefault();
    mark();
  });
  dlg.addEventListener("click", (e) => {
    if (e.target === dlg) dlg.close();
  });
  jumpBtn.addEventListener("click", () => show());
  addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
      e.preventDefault();
      dlg.open ? dlg.close() : show();
    } else if ((e.key === "/" || e.key === ">") && !dlg.open && !keysDlg?.open && !typing(e.target)) {
      e.preventDefault();
      show(e.key === ">" ? ">" : "");
    }
  });
  // A palette opened before the index arrived fills in once it does.
  const wait = setInterval(() => {
    if (index) {
      clearInterval(wait);
      if (dlg.open) draw();
    }
  }, 300);
  setTimeout(() => clearInterval(wait), 15000);
}

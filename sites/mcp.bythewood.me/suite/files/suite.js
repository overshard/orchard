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
const on = document.querySelector(".o-tools a.on");
if (on) {
  const bar = on.closest(".o-bar");
  bar.scrollLeft = on.offsetLeft - bar.clientWidth / 2 + on.offsetWidth / 2;
}

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

// Every tool's state and things, merged by auth and asked for through this
// page's own origin. The last copy paints first so the dots don't blink in.
const jumpBtn = document.querySelector("[data-jump]");
const STORE = "orchard-index";
let index = null;

const el = (tag, cls, text) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
};

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
// words ask for something instead of naming it, like down or logs pond 1h.
const TROUBLE = new Set(["down", "failing", "failed", "broken", "bad", "trouble", "wrong", "errors"]);
const ALIAS = { logs: "logging", log: "logging", uptime: "status", jobs: "cron", reminders: "cron", keys: "llm", calls: "llm", git: "repos", views: "analytics" };
const RANGES = new Set(["1h", "24h", "7d", "30d", "90d", "1y"]);
const rank = { bad: 3, warn: 1.5 };

const everything = () => {
  if (!index) return [];
  const out = [];
  for (const t of index.tools) {
    const home = (document.querySelector(`.o-tools a[data-tool="${t.tool}"]`) || {}).href;
    if (home) out.push({ tool: t.tool, kind: "tool", name: t.tool, href: home, level: t.level, note: t.note || "" });
    for (const it of t.items || []) out.push({ ...it, tool: t.tool, note: it.note || "" });
  }
  return out;
};

const search = (q) => {
  const all = everything();
  let words = q.toLowerCase().split(/\s+/).filter(Boolean);
  const actions = [];
  if (!words.length) {
    const hot = all.filter((i) => rank[i.level]).sort((a, b) => (rank[b.level] || 0) - (rank[a.level] || 0));
    return [...hot, ...all.filter((i) => i.kind === "tool" && !rank[i.level])];
  }
  let pool = all;
  let trouble = false;
  if (TROUBLE.has(words[0])) {
    trouble = true;
    pool = pool.filter((i) => rank[i.level]);
    words = words.slice(1);
  }
  const tool = ALIAS[words[0]] || (all.some((i) => i.kind === "tool" && i.name === words[0]) ? words[0] : null);
  if (tool && words.length > 1) {
    pool = pool.filter((i) => i.tool === tool);
    words = words.slice(1);
  }
  if (tool === "logging" || (!tool && !trouble)) {
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
    if (what) actions.push({ tool: "logging", kind: "search", name: `search logs for ${what}`, href: `https://logging.bythewood.me/search?${p}`, note: "every raw line" });
    words = words.filter((w) => !RANGES.has(w));
  }
  const scored = [];
  for (const i of pool) {
    const name = i.name.toLowerCase();
    const rest = `${i.note} ${i.kind} ${i.tool}`.toLowerCase();
    let s = 0;
    for (const w of words) {
      if (name.startsWith(w)) s += 6;
      else if (name.includes(` ${w}`) || name.includes(`.${w}`) || name.includes(`/${w}`)) s += 4;
      else if (name.includes(w)) s += 3;
      else if (rest.includes(w)) s += 1;
      else {
        s = -1;
        break;
      }
    }
    if (s < 0) continue;
    scored.push([s + (rank[i.level] || 0) - (i.kind === "page" ? 0.5 : 0), i]);
  }
  scored.sort((a, b) => b[0] - a[0]);
  const found = scored.slice(0, 40).map((x) => x[1]);
  // A window or a search word means a search was asked for, not a page.
  return tool === "logging" && actions.length ? [...actions, ...found] : [...found, ...actions];
};

if (jumpBtn) {
  const dlg = el("dialog", "o-jd");
  dlg.setAttribute("aria-label", "Jump or ask");
  const bar = el("div", "o-jin");
  bar.append(el("span", "p", "$"));
  const input = el("input");
  Object.assign(input, { type: "text", placeholder: "jump or ask… down · failing · logs pond 1h", autocomplete: "off", spellcheck: false });
  input.setAttribute("aria-label", "Jump to");
  bar.append(input);
  const list = el("ul", "o-jr");
  list.setAttribute("role", "listbox");
  const foot = el("div", "o-jft");
  for (const t of ["↑↓ move", "↵ open", "esc close", "down · failing · logs <source> 1h"]) foot.append(el("span", "", t));
  dlg.append(bar, list, foot);
  document.body.append(dlg);

  let results = [];
  let sel = 0;
  const draw = () => {
    results = search(input.value);
    sel = Math.min(sel, Math.max(results.length - 1, 0));
    list.replaceChildren();
    if (!index) list.append(el("li", "o-jnone", "Asking every tool…"));
    else if (!results.length) list.append(el("li", "o-jnone", "Nothing matches. Try a name, down or failing."));
    results.forEach((it, n) => {
      const li = el("li", n === sel ? "on" : "");
      li.setAttribute("role", "option");
      li.append(el("span", `o-dot ${it.level === "bad" ? "bad" : it.level === "warn" ? "warn" : it.kind === "search" ? "mute" : ""}`));
      li.append(el("span", "n", it.name));
      li.append(el("span", "k", `${it.tool} · ${it.kind}`));
      li.append(el("span", "x", it.note));
      li.addEventListener("mousemove", () => {
        if (sel !== n) {
          sel = n;
          mark();
        }
      });
      li.addEventListener("click", (e) => go(it, e.metaKey || e.ctrlKey));
      list.append(li);
    });
  };
  const mark = () => {
    [...list.children].forEach((li, n) => li.classList.toggle("on", n === sel));
    list.children[sel]?.scrollIntoView({ block: "nearest" });
  };
  const go = (it, fresh) => {
    if (!it) return;
    if (fresh) open(it.href, "_blank", "noopener");
    else location.href = it.href;
  };
  const show = () => {
    if (dlg.open) return;
    input.value = "";
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
    if (e.key === "ArrowDown") sel = Math.min(sel + 1, results.length - 1);
    else if (e.key === "ArrowUp") sel = Math.max(sel - 1, 0);
    else if (e.key === "Enter") return go(results[sel], e.metaKey || e.ctrlKey);
    else return;
    e.preventDefault();
    mark();
  });
  dlg.addEventListener("click", (e) => {
    if (e.target === dlg) dlg.close();
  });
  jumpBtn.addEventListener("click", show);
  addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
      e.preventDefault();
      dlg.open ? dlg.close() : show();
    } else if (e.key === "/" && !dlg.open && !e.target.closest("input, textarea, select, [contenteditable]")) {
      e.preventDefault();
      show();
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

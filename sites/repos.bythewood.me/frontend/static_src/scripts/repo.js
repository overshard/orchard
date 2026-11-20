// The repository pages: go to file, the last commit column, line links and
// crumbs that fold on a phone.

const toast = (text, kind = "") => document.dispatchEvent(new CustomEvent("o-toast", { detail: { text, kind } }));
const typing = (t) => t.closest?.("input, textarea, select, [contenteditable]");

// ------------------------------------------------------------ go to file

// Letters of q in order, matched from the end so they land in the file name
// when they can, scored up for runs and for a start after a separator.
const fuzzy = (q, s) => {
  const lower = s.toLowerCase();
  const idx = [];
  let score = 0;
  let next = -2;
  for (let i = s.length - 1, j = q.length - 1; i >= 0 && j >= 0; i--) {
    if (lower[i] !== q[j]) continue;
    idx.unshift(i);
    score += i === next - 1 ? 5 : 1;
    if (i === 0 || /[/._-]/.test(s[i - 1])) score += 4;
    next = i;
    j--;
  }
  if (idx.length < q.length) return null;
  if (idx[0] >= s.lastIndexOf("/") + 1) score += 8;
  return { score: score - (idx[idx.length - 1] - idx[0]) * 0.15 - s.length * 0.01, idx };
};

const marked = (s, idx) => {
  const out = document.createDocumentFragment();
  const on = new Set(idx);
  let run = "";
  let runOn = false;
  const flush = () => {
    if (!run) return;
    if (runOn) {
      const m = document.createElement("mark");
      m.textContent = run;
      out.append(m);
    } else out.append(run);
    run = "";
  };
  for (let i = 0; i < s.length; i++) {
    if (on.has(i) !== runOn) {
      flush();
      runOn = on.has(i);
    }
    run += s[i];
  }
  flush();
  return out;
};

const finder = (panel) => {
  const input = panel.querySelector("[data-finder]");
  const ul = panel.querySelector(".finder ul");
  if (!input || !ul) return;
  let paths = null;
  let loading = null;
  let shown = [];
  let cur = 0;

  const load = () => {
    loading ??= fetch(panel.dataset.files, { credentials: "same-origin" })
      .then((r) => (r.ok ? r.json() : { paths: [] }))
      .then((d) => {
        paths = d.paths || [];
        draw();
      })
      .catch(() => {
        paths = [];
        draw();
      });
  };

  const draw = () => {
    const q = input.value.trim().toLowerCase().replace(/\s+/g, "");
    ul.replaceChildren();
    if (!q) {
      ul.hidden = true;
      return;
    }
    ul.hidden = false;
    if (!paths) {
      const li = document.createElement("li");
      li.className = "o-jnone";
      li.textContent = "Reading every path at this branch…";
      ul.append(li);
      return;
    }
    shown = [];
    for (const p of paths) {
      const m = fuzzy(q, p);
      if (m) shown.push({ p, m });
    }
    shown.sort((a, b) => b.m.score - a.m.score);
    shown = shown.slice(0, 12);
    cur = Math.min(cur, Math.max(0, shown.length - 1));
    if (!shown.length) {
      const li = document.createElement("li");
      li.className = "o-jnone";
      li.textContent = "No file matches. The letters only need to come in order, like lgn for login.go.";
      ul.append(li);
      return;
    }
    shown.forEach((s, i) => {
      const li = document.createElement("li");
      li.dataset.i = i;
      if (i === cur) li.className = "on";
      const n = document.createElement("span");
      n.className = "n o-mono";
      n.append(marked(s.p, s.m.idx));
      const k = document.createElement("span");
      k.className = "k";
      const dot = s.p.lastIndexOf(".");
      k.textContent = dot > s.p.lastIndexOf("/") ? s.p.slice(dot + 1) : "";
      li.append(n, k);
      ul.append(li);
    });
  };

  const open = (p, tab) => {
    const href = panel.dataset.blob + p.split("/").map(encodeURIComponent).join("/");
    if (tab) window.open(href, "_blank", "noopener");
    else location.href = href;
  };

  input.addEventListener("focus", load);
  input.addEventListener("input", () => {
    cur = 0;
    draw();
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown") cur = Math.min(shown.length - 1, cur + 1);
    else if (e.key === "ArrowUp") cur = Math.max(0, cur - 1);
    else if (e.key === "Enter" && shown[cur]) return open(shown[cur].p, e.metaKey || e.ctrlKey);
    else if (e.key === "Escape") {
      input.value = "";
      input.blur();
    } else return;
    e.preventDefault();
    draw();
  });
  ul.addEventListener("mousedown", (e) => {
    const li = e.target.closest("li[data-i]");
    if (!li) return;
    e.preventDefault();
    open(shown[+li.dataset.i].p, e.metaKey || e.ctrlKey);
  });
  input.addEventListener("blur", () => setTimeout(() => (ul.hidden = true), 100));
  input.addEventListener("focus", () => draw());

  addEventListener("keydown", (e) => {
    if (e.key !== "t" || e.metaKey || e.ctrlKey || e.altKey || typing(e.target)) return;
    if (document.querySelector("dialog[open]")) return;
    e.preventDefault();
    input.focus();
  });
};

// ------------------------------------------------------------ last commit

// Filled after the tree has painted, since walking history is the slow part.
const lastCommits = (panel) => {
  const url = panel.dataset.last;
  if (!url) return;
  const rows = panel.querySelectorAll("tr[data-name]");
  if (!rows.length) return;
  const repo = panel.dataset.files.split("/")[1];
  for (const tr of rows) tr.querySelector(".lcol").classList.add("wait");
  fetch(url, { credentials: "same-origin" })
    .then((r) => (r.ok ? r.json() : { entries: {} }))
    .catch(() => ({ entries: {} }))
    .then((d) => {
      for (const tr of rows) {
        const cell = tr.querySelector(".lcol");
        cell.classList.remove("wait");
        const c = d.entries?.[tr.dataset.name];
        if (!c) continue;
        const a = document.createElement("a");
        a.href = `/${repo}/commit/${c.sha}`;
        a.textContent = c.subject;
        a.title = c.short;
        cell.append(a);
        const when = tr.querySelector(".when");
        when.textContent = c.ago;
        when.title = new Date(c.when).toISOString().replace("T", " ").slice(0, 16) + " UTC";
      }
    });
};

// ------------------------------------------------------------ line links

const lineLinks = (panel) => {
  const code = panel.querySelector("[data-lines]");
  const out = panel.querySelector("[data-permalink]");
  if (!code || !out) return;
  const sha = panel.dataset.sha || "";
  const rev = panel.dataset.rev;
  let sel = null;
  let anchor = null;
  let pinned = false;

  const line = (n) => document.getElementById(`L${n}`)?.parentElement;
  const hash = () => (sel ? `#L${sel[0]}${sel[1] !== sel[0] ? `-L${sel[1]}` : ""}` : "");
  const link = () => {
    let path = location.pathname;
    if (pinned && sha) path = path.replace(`/blob/${rev}/`, `/blob/${sha}/`);
    return location.origin + path + hash();
  };
  const paint = () => {
    for (const el of code.querySelectorAll(".hl")) el.classList.remove("hl");
    if (sel) for (let n = sel[0]; n <= sel[1]; n++) line(n)?.classList.add("hl");
    out.textContent = link();
    history.replaceState(null, "", location.pathname + location.search + hash());
  };

  const m = /^#L(\d+)(?:-L(\d+))?$/.exec(location.hash);
  if (m) {
    const a = +m[1];
    const b = +(m[2] || m[1]);
    sel = [Math.min(a, b), Math.max(a, b)];
    anchor = sel[0];
    paint();
    line(sel[0])?.scrollIntoView({ block: "center" });
  } else out.textContent = link();

  code.addEventListener("click", (e) => {
    const a = e.target.closest('a[href^="#L"]');
    if (!a) return;
    e.preventDefault();
    const n = +a.getAttribute("href").slice(2);
    if (e.shiftKey && anchor) sel = [Math.min(anchor, n), Math.max(anchor, n)];
    else if (sel && sel[0] === n && sel[1] === n) sel = anchor = null;
    else {
      sel = [n, n];
      anchor = n;
    }
    if (e.shiftKey) getSelection()?.removeAllRanges();
    a.blur();
    paint();
  });

  const copy = () => {
    const text = link();
    const done = () => toast(pinned ? "Copied a link pinned to this commit, so it won't drift" : "Copied the link");
    const fail = () => {
      const r = document.createRange();
      r.selectNodeContents(out);
      getSelection().removeAllRanges();
      getSelection().addRange(r);
      toast("Copy was blocked, so the link is selected instead");
    };
    if (navigator.clipboard) navigator.clipboard.writeText(text).then(done, fail);
    else fail();
  };
  panel.querySelector("[data-copy-link]")?.addEventListener("click", copy);

  if (sha) {
    addEventListener("keydown", (e) => {
      if (e.key !== "y" || e.metaKey || e.ctrlKey || e.altKey || typing(e.target)) return;
      pinned = !pinned;
      out.classList.toggle("pinned", pinned);
      paint();
      toast(pinned ? `Pinned to ${sha.slice(0, 8)}` : `Back to ${rev}`);
    });
  }
};

// ------------------------------------------------------------ crumbs

for (const btn of document.querySelectorAll(".crumbs .fold button")) {
  btn.addEventListener("click", () => btn.closest(".crumbs").classList.add("open"));
}

for (const panel of document.querySelectorAll("section.files")) {
  finder(panel);
  lastCommits(panel);
}
for (const panel of document.querySelectorAll("section.blob")) lineLinks(panel);

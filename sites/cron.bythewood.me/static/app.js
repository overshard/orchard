// cron's own page behaviour. Everything works without it, the When box and the
// advanced fields both post, and the week just doesn't drag.
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

// The suite's toast lives in a module under a hashed name, so ask for one.
const toast = (text, opts = {}) => document.dispatchEvent(new CustomEvent("o-toast", { detail: { text, ...opts } }));

const node = (tag, cls, text) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
};

const post = async (url, form) => {
  const res = await fetch(url, { method: "POST", credentials: "same-origin", body: form ? new URLSearchParams(form) : undefined });
  const body = await res.json().catch(() => ({ error: "the answer didn't parse" }));
  if (!res.ok) throw new Error(body.error || `http ${res.status}`);
  return body;
};

// ------------------------------------------------------------ the job form

const form = document.querySelector("form.job");

if (form) {
  const when = form.elements.when;
  const out = $("[data-when-out]", form);
  const cal = $("[data-when-cal]", form);
  const adv = $("details.adv", form);
  const pick = () => form.querySelector('input[name="schedule"]:checked')?.value || "once";
  let timer;
  let asked = 0;

  const showFields = () => {
    for (const el of form.querySelectorAll("[data-for]")) el.hidden = el.dataset.for !== pick();
  };

  const draw = (p) => {
    out.replaceChildren();
    const head = node("div");
    head.append(node("span", "o-lab plain o-muted", p.kind === "once" ? "once" : "cron"));
    head.append(node("div", "cronout", p.kind === "once" ? p.at.replace("T", " ") : p.cron));
    head.append(node("div", "say", p.say));
    const next = node("div");
    next.append(node("span", "o-lab plain o-muted", `next ${p.next.length}`));
    for (const n of p.next) {
      const ev = node("div", "o-ev");
      ev.append(node("span", "t", n.text.replace(/ \d+:\d+[ap]m$/, "")));
      ev.append(node("span", "x o-mono", `${n.text.match(/\d+:\d+[ap]m$/)?.[0] || ""} · ${n.in}`));
      next.append(ev);
    }
    out.append(head, next);
    cal.replaceChildren();
    p.days.forEach((d, i) => {
      const c = node("div", i === 0 ? "today" : "");
      c.dataset.tip = `${d.day} · ${d.fires} ${d.fires === 1 ? "fire" : "fires"}`;
      c.append(node("b", "", d.day));
      if (d.fires > 6) c.append(node("span", "o-brass", `×${d.fires}`));
      else for (let k = 0; k < d.fires; k++) c.append(node("i", "dot"));
      cal.append(c);
    });
  };

  const refuse = (msg) => {
    out.replaceChildren();
    const n = node("div", "o-note bad");
    n.append(node("span", "o-lab o-bad", "not a schedule"), msg);
    out.append(n);
    cal.replaceChildren();
  };

  // A later answer can't be overwritten by an earlier one that came back slow.
  const preview = async (q, fill) => {
    const mine = ++asked;
    try {
      const res = await fetch("/api/preview?" + new URLSearchParams(q), { credentials: "same-origin" });
      const body = await res.json();
      if (mine !== asked) return;
      if (!res.ok) return refuse(body.error);
      draw(body);
      if (fill) {
        form.querySelector(`input[name="schedule"][value="${body.kind === "once" ? "once" : "repeat"}"]`).checked = true;
        if (body.kind === "once") form.elements.at.value = body.at;
        else form.elements.cron.value = body.cron;
        showFields();
      }
    } catch {
      if (mine === asked) out.replaceChildren();
    }
  };

  const fromAdvanced = () => {
    const mode = pick();
    const v = form.elements[mode === "repeat" ? "cron" : "at"].value.trim();
    if (!v) {
      out.replaceChildren();
      cal.replaceChildren();
      return;
    }
    preview({ [mode === "repeat" ? "cron" : "at"]: v }, false);
  };

  const update = () => (when.value.trim() ? preview({ when: when.value }, true) : fromAdvanced());

  when.addEventListener("input", () => {
    clearTimeout(timer);
    timer = setTimeout(update, 200);
  });
  adv.addEventListener("input", () => {
    // Typing in the exact fields means they're what counts.
    when.value = "";
    clearTimeout(timer);
    timer = setTimeout(fromAdvanced, 250);
  });
  adv.addEventListener("change", (e) => {
    if (e.target.name !== "schedule") return;
    showFields();
    when.value = "";
    fromAdvanced();
  });

  const ex = $("[data-examples]", form);
  ex.hidden = false;
  ex.addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    when.value = b.textContent;
    when.focus();
    update();
  });

  showFields();
  update();

  // The kind decides what the message box is for, so its label and the hint
  // under the kind follow the choice.
  const hint = form.querySelector("[data-kind-hint]");
  const label = form.elements.message?.closest("label")?.querySelector("span");
  const words = {
    notify: ["Sends the message as written.", "Message"],
    prompt: ["The model looks it up with web search and only pushes when the answer is worth it.", "What to look up"],
    quote: ["Pushes the first trade of the ticker, then the change at most every five minutes.", "Ticker"],
  };
  form.addEventListener("change", (e) => {
    if (e.target.name !== "kind" || !words[e.target.value]) return;
    [hint.textContent, label.textContent] = words[e.target.value];
  });
}

// ------------------------------------------------------------ try it

const tryBtn = $("[data-try]");

if (tryBtn) {
  const st = $("[data-try-st]");
  const bar = $("[data-try-bar]");
  const kv = $("[data-try-kv]");
  const push = $("[data-try-push]");
  const clk = $("[data-try-clk]");
  const name = $("h1")?.textContent.trim() || "cron";
  let busy = false;

  const pushCard = (dim, title, body, right) => {
    const p = node("div", dim ? "o-push dim" : "o-push");
    const h = node("div", "h");
    h.append(node("span", "", `ntfy · cron · ${name.toLowerCase()}`), node("span", "", right));
    p.append(h, node("b", "", title), body);
    if (!dim) {
      const acts = node("div", "acts");
      acts.append(node("span", "", "Open"), node("span", "", "Snooze 1h"));
      p.append(acts);
    }
    return p;
  };

  const row = (k, v) => {
    kv.append(node("dt", "", k), node("dd", "", v));
  };

  const run = async () => {
    if (busy) return;
    busy = true;
    tryBtn.disabled = true;
    st.textContent = "trying, nothing is sent";
    bar.className = "";
    void bar.offsetWidth;
    bar.classList.add("go");
    try {
      const t = await post(`/jobs/${tryBtn.dataset.try}/try`);
      clk.textContent = new Intl.DateTimeFormat("en-US", { timeZone: "America/New_York", hour: "numeric", minute: "2-digit" }).format(new Date()).replace(/\s?([AP]M)$/, (_, ap) => ap.toLowerCase());
      kv.replaceChildren();
      kv.hidden = false;
      row("took", t.ms < 1000 ? `${t.ms}ms` : `${(t.ms / 1000).toFixed(1)}s`);
      if (t.err) {
        st.textContent = "it failed · not sent";
        st.className = "o-mono o-bad small";
        row("error", t.err);
        push.replaceChildren(pushCard(true, "It would fail", t.err, "–"));
      } else if (t.push) {
        st.textContent = "would push · not sent";
        st.className = "o-mono o-brass small";
        row("title", t.title);
        push.replaceChildren(pushCard(false, t.title, t.body, "now"));
      } else {
        st.textContent = "would stay quiet · not sent";
        st.className = "o-mono o-muted small";
        row("found", t.body || "nothing worth a push");
        push.replaceChildren(pushCard(true, "No push", t.body || "Nothing worth a push this time.", "–"));
      }
      row("sent", "No, this was a try. Run now sends for real.");
    } catch (err) {
      st.textContent = err.message;
      st.className = "o-mono o-bad small";
    } finally {
      busy = false;
      tryBtn.disabled = false;
      bar.classList.remove("go");
      bar.classList.add("done");
    }
  };

  tryBtn.addEventListener("click", run);
  if (location.hash === "#try") {
    $("#try")?.scrollIntoView({ block: "start" });
    run();
  }
}

// ------------------------------------------------------------ the week

const wk = $("[data-week]");

if (wk) {
  let dragging = null;

  const settle = (day) => {
    const cards = $$(".card", day).sort((a, b) => a.dataset.min - b.dataset.min);
    for (const p of $$(".none", day)) p.remove();
    day.append(...cards);
    if (!cards.length) day.append(node("p", "none", "nothing"));
  };

  const move = async (card, from, to) => {
    const body = await post(`/jobs/${card.dataset.id}/move`, { day: to.dataset.day });
    to.append(card);
    settle(to);
    settle(from);
    return body;
  };

  wk.addEventListener("dragstart", (e) => {
    const c = e.target.closest?.(".card");
    if (!c?.classList.contains("once")) {
      e.preventDefault();
      return;
    }
    dragging = c;
    c.classList.add("drag");
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", c.dataset.id);
  });
  wk.addEventListener("dragend", () => {
    for (const x of $$(".drag, .over", wk)) x.classList.remove("drag", "over");
    dragging = null;
  });
  wk.addEventListener("dragover", (e) => {
    const d = e.target.closest("[data-day]");
    if (!d || !dragging) return;
    e.preventDefault();
    for (const x of $$(".over", wk)) if (x !== d) x.classList.remove("over");
    d.classList.add("over");
  });
  wk.addEventListener("drop", async (e) => {
    const to = e.target.closest("[data-day]");
    const card = dragging;
    if (!to || !card) return;
    e.preventDefault();
    to.classList.remove("over");
    const from = card.closest("[data-day]");
    if (from === to) return;
    try {
      const moved = await move(card, from, to);
      toast(`Moved ${card.dataset.name} to ${moved.text}`, {
        ms: 6000,
        undo: () =>
          move(card, to, from).catch((err) => toast(`Couldn't move it back, ${err.message}`, { kind: "bad" })),
      });
    } catch (err) {
      toast(`Couldn't move ${card.dataset.name}, ${err.message}`, { kind: "bad" });
    }
  });
}

// ------------------------------------------------------------ the jobs table

// The bulk form has one undo line, so it's written for the button pressed.
const bulk = $("#bulk");
if (bulk) {
  bulk.addEventListener("click", (e) => {
    const b = e.target.closest('button[name="do"]');
    if (!b) return;
    const n = bulk.querySelectorAll("input[data-bulk-id]").length;
    bulk.dataset.undo = `${b.value === "pause" ? "Pausing" : "Resuming"} ${n} ${n === 1 ? "job" : "jobs"}`;
  });
}

for (const f of document.querySelectorAll("form[data-autosubmit]")) {
  f.addEventListener("change", () => f.requestSubmit());
}

for (const f of document.querySelectorAll("form[data-confirm]")) {
  f.addEventListener("submit", (e) => {
    if (!confirm(f.dataset.confirm)) e.preventDefault();
  });
}

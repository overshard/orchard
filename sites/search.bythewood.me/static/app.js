const transcript = document.getElementById("transcript");
const form = document.getElementById("ask");
const input = document.getElementById("q");
const go = document.getElementById("go");
const archive = document.getElementById("archive");
const followhint = document.getElementById("followhint");
const sid = document.body.dataset.session;
const gauge = document.getElementById("gauge");
const segs = document.getElementById("segs");
const gtext = document.getElementById("gtext");
const capnote = document.getElementById("capnote");

const SEGMENTS = 8;
for (let i = 0; i < SEGMENTS; i++) segs.appendChild(el("span", "seg on"));
let turns = 0;
let stream = null;
// Following is the default after an answer, because a follow-up is the common
// next move. Starting fresh has to be one obvious click, not a subtle control
// in the corner.
let following = false;

// Each pipeline step gets a human label. The step names come off the server so
// a new one still shows, just without a friendly name.
const STEPS = {
  followup: "Reading the previous answer",
  plan: "Working out what to search for",
  search: "Searching the web",
  fetch: "Reading pages",
  write: "Writing the answer",
  check: "Checking every sentence",
  retry: "That did not hold up, searching again",
};

// Why each step takes the time it does. A pause with no explanation reads as a
// hang, and two of these pauses are deliberate.
const WHY = {
  search: "Searches are spaced about a second apart so the sources keep answering.",
  fetch: "Each page is downloaded and stripped down to its article text.",
  write: "The model is running locally on the GPU.",
  check: "Every sentence is checked against the passage it cites, which is one model call each.",
  retry: "Most of that answer was not backed by the pages found, so it is trying different searches.",
};

// Search capacity is shown before it runs out, because hitting a wall with no
// warning reads as the tool being broken rather than as a limit being reached.
// The engine is not named: what matters to a reader is how much is left.
function paintBudget(b) {
  if (!b) return;
  const lit = Math.ceil((SEGMENTS * b.left) / b.max);
  [...segs.children].forEach((s, i) => s.classList.toggle("on", i < lit));

  gauge.classList.toggle("low", b.questions <= 1 && !b.cooling && b.left > 0);
  gauge.classList.toggle("out", b.left === 0 || b.cooling);

  if (b.cooling) gtext.textContent = `easing ${b.resetIn}s`;
  else if (b.left === 0) gtext.textContent = `resting ${b.resetIn}s`;
  else gtext.textContent = `${b.questions} question${b.questions === 1 ? "" : "s"}`;

  if (b.cooling || b.left === 0) {
    capnote.textContent =
      "Searching is resting so the sources keep answering. Anything already read is still available, so a follow-up on this page still works.";
    capnote.hidden = false;
  } else if (b.questions <= 1) {
    capnote.textContent = "Close to the search allowance, so the next few questions may pause between searches.";
    capnote.hidden = false;
  } else {
    capnote.hidden = true;
  }
}

async function refreshBudget() {
  try {
    paintBudget(await (await fetch("/budget")).json());
  } catch {}
}
setInterval(refreshBudget, 5000);
refreshBudget();

function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
}

// While a question is running, keep the working panel in view. Once the answer
// lands, go to the top of that turn instead: scrolling to the bottom of the
// page lands on the sources and passages, which means the answer itself is the
// one thing you cannot see.
function scrollToBottom() {
  window.scrollTo({ top: document.body.scrollHeight, behavior: "smooth" });
}

function scrollToTurn(turn) {
  const headerH = document.querySelector(".bar").offsetHeight;
  const top = window.scrollY + turn.getBoundingClientRect().top - headerH - 16;
  window.scrollTo({ top, behavior: "smooth" });
}

form.addEventListener("submit", (e) => {
  e.preventDefault();
  const q = input.value.trim();
  if (!q || stream) return;
  ask(q);
});

document.getElementById("reset").addEventListener("click", () => newQuestion(true));

// newQuestion drops the conversation the model carries, but leaves what is on
// screen alone. Clearing the page would throw away answers worth keeping just
// to change what the next question is measured against.
async function newQuestion(clearScreen) {
  await fetch(`/reset?sid=${sid}`, { method: "POST" });
  following = false;
  if (clearScreen) {
    transcript.innerHTML = "";
    turns = 0;
  } else if (turns > 0) {
    transcript.appendChild(el("div", "brk", "new question"));
  }
  setMode();
  input.focus();
}

// setMode keeps the input honest about what the next question will do.
function setMode() {
  input.placeholder = following ? "Ask a follow-up" : "Ask a question";
  form.classList.toggle("followon", following);
  followhint.hidden = !following;
}

function ask(q) {
  input.value = "";
  go.disabled = true;
  input.disabled = true;

  const turn = el("article", "turn");
  turn.appendChild(el("div", "question", q));

  const status = el("div", "status");
  const head = el("div", "stephead");
  const spinner = el("span", "spinner");
  const label = el("span", "steptext", "Starting");
  const clock = el("span", "clock", "0s");
  head.append(spinner, label, clock);
  status.appendChild(head);
  const why = el("div", "why");
  status.appendChild(why);
  const trail = el("ul", "trail");
  status.appendChild(trail);
  turn.appendChild(status);

  const began = Date.now();
  const ticking = setInterval(() => {
    clock.textContent = `${Math.round((Date.now() - began) / 1000)}s`;
  }, 1000);

  transcript.appendChild(turn);
  scrollToTurn(turn);

  stream = new EventSource(`/stream?q=${encodeURIComponent(q)}&sid=${sid}`);
  let lastStep = "";

  stream.addEventListener("status", (ev) => {
    const d = JSON.parse(ev.data);
    label.textContent = STEPS[d.step] || d.step;
    why.textContent = WHY[d.step] || "";
    if (d.detail) {
      // One line per step, with later detail for the same step replacing the
      // previous line rather than stacking a line per fetched page.
      let li = lastStep === d.step ? trail.lastElementChild : null;
      if (!li) {
        li = el("li");
        trail.appendChild(li);
      }
      li.textContent = d.detail;
      lastStep = d.step;
    }
    // Only chase the status panel when it has been pushed out of sight.
    const box = status.getBoundingClientRect();
    if (box.bottom > window.innerHeight) scrollToBottom();
  });

  stream.addEventListener("answer", (ev) => {
    const d = JSON.parse(ev.data);
    status.remove();
    turn.appendChild(renderAnswer(d));
    if (d.pages !== undefined) {
      archive.textContent = `${d.pages} pages \u00b7 ${d.chunks} passages`;
    }
    paintBudget(d.budget);
    finish(turn);
  });

  stream.addEventListener("failed", (ev) => {
    const d = JSON.parse(ev.data);
    status.remove();
    turn.appendChild(el("p", "error", d.error));
    finish(turn);
  });

  stream.onerror = () => {
    if (!stream) return;
    status.remove();
    if (!turn.querySelector(".answer")) {
      turn.appendChild(el("p", "error", "The connection dropped before an answer arrived."));
    }
    finish(turn);
  };

  function finish(turn) {
    clearInterval(ticking);
    if (stream) stream.close();
    stream = null;
    go.disabled = false;
    input.disabled = false;
    turns++;
    following = true;
    setMode();
    input.focus({ preventScroll: true });
    scrollToTurn(turn);
  }
}

function renderAnswer(d) {
  const wrap = el("div", "answerwrap");

  const meta = el("div", "meta");
  meta.appendChild(el("span", "label", d.shape));
  meta.appendChild(el("span", "faint", d.elapsed));
  if (d.retried) meta.appendChild(el("span", "flag", "retried"));
  if (d.standalone && d.standalone !== d.question) {
    meta.appendChild(el("span", "chip", d.standalone));
  }
  (d.queries || []).forEach((q) => meta.appendChild(el("span", "chip", q)));
  wrap.appendChild(meta);

  const body = el("div", "answer prose");
  body.innerHTML = d.html;
  wrap.appendChild(body);

  (d.warnings || []).forEach((wtext) => wrap.appendChild(el("p", "warn", wtext)));

  const cites = d.citations || [];
  const checked = cites.filter((c) => c.Checked);
  const bad = checked.filter((c) => !c.Supported);

  if (checked.length) {
    const v = el("section", "validation");
    const head = el("div", "vhead");
    head.appendChild(el("span", "label", "verified"));
    const pct = Math.round((100 * (checked.length - bad.length)) / checked.length);
    const bar = el("span", bad.length ? "vbar bad" : "vbar");
    const fill = el("span", "vfill");
    bar.appendChild(fill);
    head.appendChild(bar);
    head.appendChild(el("span", bad.length ? "score bad" : "score",
      `${checked.length - bad.length}/${checked.length} verified`));
    v.appendChild(head);
    requestAnimationFrame(() => { fill.style.width = `${pct}%`; });

    if (bad.length) {
      v.appendChild(el("p", "faint explain",
        "Unsupported means the sentence was checked against the passage it cited and that passage does not state it. " +
        "The claim may still be true, but nothing read backs it, so treat it as the model's own words rather than something taken off a source."));
    }

    const list = el("ul", "checks");
    cites.forEach((c) => {
      const li = el("li", c.Supported ? "ok" : "bad");
      li.appendChild(el("span", "verdict", c.Supported ? (c.Repaired ? "re-cited" : "supported") : "unsupported"));
      const a = el("a", "cite", `[${c.Repaired || c.PassageID}]`);
      a.href = `#p${c.Repaired || c.PassageID}`;
      li.appendChild(a);
      const s = el("span", "sentence");
      s.appendChild(el("span", "stext", c.Sentence));
      if (c.Note) s.appendChild(el("span", "note", c.Note));
      li.appendChild(s);
      list.appendChild(li);
    });
    v.appendChild(list);
    wrap.appendChild(v);
  }

  if ((d.sources || []).length) {
    const sec = el("section");
    sec.appendChild(el("span", "label", "sources"));
    const ol = el("ol", "sources");
    d.sources.forEach((s) => {
      const li = el("li");
      const a = el("a", null, s.Title || s.URL);
      a.href = s.URL;
      a.target = "_blank";
      a.rel = "noreferrer noopener";
      li.appendChild(a);
      const sub = el("div", "faint");
      let t = host(s.URL);
      if (s.Published) t += ` · ${s.Published}`;
      sub.textContent = t;
      if (s.FromCache) {
        sub.appendChild(document.createTextNode(" · "));
        sub.appendChild(el("span", "cached", "cached"));
      }
      li.appendChild(sub);
      ol.appendChild(li);
    });
    sec.appendChild(ol);
    wrap.appendChild(sec);
  }

  if ((d.passages || []).length) {
    const det = el("details", "passages");
    const sum = el("summary", "label", `passages (${d.passages.length})`);
    det.appendChild(sum);
    det.appendChild(el("p", "faint explain",
      "The raw text the answer was written from. Sometimes this is more useful than the answer, especially for recipes."));
    d.passages.forEach((p) => {
      const box = el("div", "passage");
      box.id = `p${p.ID}`;
      const h = el("div", "phead");
      h.appendChild(el("span", "cite", `[${p.ID}]`));
      h.appendChild(el("span", "faint", `source ${p.Source}`));
      box.appendChild(h);
      box.appendChild(el("p", null, p.Text));
      det.appendChild(box);
    });
    wrap.appendChild(det);
  }

  const foot = el("div", "turnfoot");
  const fu = el("button", "act primary", "Ask a follow-up");
  fu.addEventListener("click", () => {
    following = true;
    setMode();
    input.focus();
    input.scrollIntoView({ block: "center", behavior: "smooth" });
  });
  const nq = el("button", "act", "New question");
  nq.addEventListener("click", () => {
    newQuestion(false);
    input.scrollIntoView({ block: "center", behavior: "smooth" });
  });
  foot.append(fu, nq);
  wrap.appendChild(foot);

  // A citation link should open the passages panel, not silently do nothing.
  wrap.querySelectorAll("a.cite").forEach((a) => {
    a.addEventListener("click", () => {
      const det = wrap.querySelector("details.passages");
      if (det) det.open = true;
    });
  });

  return wrap;
}

function host(u) {
  try {
    return new URL(u).hostname.replace(/^www\./, "");
  } catch {
    return u;
  }
}

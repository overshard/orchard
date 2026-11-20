// The explorer's two pieces that need a script. Everything else on the search
// page is links and a GET form, so it all works without this file.

// ------------------------------------------------------------------ brush

// Each bar is already a link to its own bucket. Dragging across several makes
// one zoom out of them instead.
const hist = document.querySelector("[data-hist]");
if (hist) {
  const n = +hist.dataset.n;
  const start = +hist.dataset.start;
  const bucket = +hist.dataset.bucket;
  const brush = hist.querySelector(".brush");
  let from = null;
  let to = null;
  let dragged = false;

  const at = (e) => {
    const r = hist.getBoundingClientRect();
    return Math.max(0, Math.min(n - 1, Math.floor(((e.clientX - r.left) / r.width) * n)));
  };
  const draw = () => {
    const lo = Math.min(from, to);
    const hi = Math.max(from, to);
    brush.hidden = false;
    brush.style.left = `${(lo / n) * 100}%`;
    brush.style.width = `${((hi - lo + 1) / n) * 100}%`;
  };

  hist.addEventListener("dragstart", (e) => e.preventDefault());
  hist.addEventListener("pointerdown", (e) => {
    if (e.pointerType !== "mouse" || e.button !== 0) return;
    from = to = at(e);
    dragged = false;
    hist.setPointerCapture(e.pointerId);
  });
  hist.addEventListener("pointermove", (e) => {
    if (from === null) return;
    const b = at(e);
    if (b !== to) {
      to = b;
      dragged = dragged || to !== from;
      draw();
    }
  });
  hist.addEventListener("pointerup", () => {
    if (from === null) return;
    const lo = Math.min(from, to);
    const hi = Math.max(from, to);
    from = to = null;
    if (!dragged) {
      brush.hidden = true;
      return;
    }
    const url = new URL(hist.dataset.base, location.href);
    url.searchParams.set("from", start + lo * bucket);
    url.searchParams.set("to", start + (hi + 1) * bucket);
    location.assign(url);
  });
  // The click that ends a drag lands on a bar, which would zoom to it alone.
  hist.addEventListener(
    "click",
    (e) => {
      if (dragged) {
        e.preventDefault();
        dragged = false;
      }
    },
    true,
  );
}

// ------------------------------------------------------------------ tail

// A short poll for lines newer than the last one seen. It stops while the tab
// is hidden, so a forgotten tab costs nothing, and #live in the URL starts it.
const tail = document.querySelector("[data-tail]");
const toggle = document.querySelector("[data-tail-toggle]");
if (tail && toggle) {
  const box = tail.querySelector("[data-tail-box]");
  const pill = tail.querySelector("[data-tail-pill]");
  const stateEl = tail.querySelector("[data-tail-state]");
  const countEl = tail.querySelector("[data-tail-count]");
  const pauseBtn = tail.querySelector("[data-tail-pause]");
  const lines = document.querySelector("[data-lines]");
  const pager = document.querySelector(".pager");
  const every = 2000;
  const keep = 500;

  let live = false;
  let running = true;
  let follow = true;
  let waiting = 0;
  let total = 0;
  let after = 0;
  let failures = 0;
  let timer = 0;
  let inflight = null;
  let programmatic = false;

  const ui = (note) => {
    const s = note || (!running ? "paused" : follow ? "following" : "holding");
    stateEl.textContent = s;
    stateEl.className = s === "following" ? "ok" : note ? "bad" : "warn";
    pauseBtn.textContent = running ? "Pause" : "Resume";
    countEl.textContent = total.toLocaleString();
    pill.hidden = follow || !waiting;
    pill.textContent = `${waiting} new line${waiting === 1 ? "" : "s"} ↓`;
    toggle.classList.toggle("on", live);
  };

  const toBottom = () => {
    programmatic = true;
    box.scrollTop = box.scrollHeight;
    requestAnimationFrame(() => {
      programmatic = false;
    });
  };

  const stop = () => {
    clearTimeout(timer);
    timer = 0;
    if (inflight) inflight.abort();
    inflight = null;
  };

  const schedule = (ms) => {
    clearTimeout(timer);
    if (live && running && !document.hidden) timer = setTimeout(poll, ms);
  };

  async function poll() {
    timer = 0;
    inflight = new AbortController();
    try {
      const res = await fetch(`${tail.dataset.tail}&after=${after}`, {
        signal: inflight.signal,
        credentials: "same-origin",
        redirect: "error",
        headers: { Accept: "application/json" },
      });
      if (res.status === 401) {
        live = false;
        ui("signed out, reload to sign in");
        return;
      }
      if (!res.ok) throw new Error(res.status);
      const data = await res.json();
      failures = 0;
      after = data.after;
      if (data.n) {
        const tpl = document.createElement("template");
        tpl.innerHTML = data.html;
        for (const el of tpl.content.querySelectorAll(".ln")) el.classList.add("new");
        box.append(tpl.content);
        while (box.children.length > keep) box.firstElementChild.remove();
        total += data.n;
        if (follow) toBottom();
        else waiting += data.n;
      }
      ui();
      schedule(every);
    } catch (err) {
      if (err.name === "AbortError") return;
      failures++;
      if (failures >= 4) {
        live = false;
        ui("stopped, reload to try again");
        return;
      }
      ui("retrying");
      schedule(every * 4);
    } finally {
      inflight = null;
    }
  }

  const start = () => {
    live = true;
    running = true;
    follow = true;
    waiting = 0;
    tail.hidden = false;
    lines.hidden = true;
    if (pager) pager.hidden = true;
    history.replaceState(null, "", "#live");
    ui();
    poll();
  };

  const end = () => {
    live = false;
    stop();
    tail.hidden = true;
    lines.hidden = false;
    if (pager) pager.hidden = false;
    history.replaceState(null, "", location.pathname + location.search);
    ui();
  };

  toggle.hidden = false;
  toggle.addEventListener("click", () => (live ? end() : start()));
  pauseBtn.addEventListener("click", () => {
    running = !running;
    if (running) poll();
    else stop();
    ui();
  });
  pill.addEventListener("click", () => {
    follow = true;
    waiting = 0;
    toBottom();
    ui();
  });
  box.addEventListener("scroll", () => {
    if (programmatic) return;
    const atEnd = box.scrollTop + box.clientHeight >= box.scrollHeight - 24;
    if (atEnd && !follow) waiting = 0;
    follow = atEnd;
    ui();
  });
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) stop();
    else if (live && running && !timer && !inflight) poll();
  });
  window.addEventListener("pagehide", stop);

  if (location.hash === "#live") start();
}

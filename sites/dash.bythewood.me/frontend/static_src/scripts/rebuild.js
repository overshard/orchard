// The BRIEFS readout reruns every desk now. Like the quick chat it is in the
// page for everybody, since the page can come out of Cloudflare's cache, and
// is only shown once /api/rebuild says the visitor is signed in.

const box = document.querySelector("[data-rebuild]");
const button = box?.querySelector("[data-rebuild-button]");
const label = box?.querySelector("[data-rebuild-label]");

let clock = null;
let poll = null;
let wake = null;

async function ask(init) {
  try {
    const r = await fetch("/api/rebuild", { cache: "no-store", ...init });
    const body = await r.json().catch(() => ({}));
    return { status: r.status, body };
  } catch {
    return { status: 0, body: {} };
  }
}

// A word the phone layout drops, then the figure it keeps.
function say(word, figure) {
  const w = document.createElement("span");
  if (figure) w.className = "rb-word";
  w.textContent = figure ? `${word} ` : word;
  label.replaceChildren(w);
  if (figure) label.append(figure);
}

function elapsed(seconds) {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

function stop() {
  clearInterval(clock);
  clearTimeout(poll);
  clearTimeout(wake);
}

function show(s) {
  stop();
  if (s.running) {
    box.dataset.state = "running";
    button.disabled = true;
    button.title = "EVERY BRIEF IS BEING WRITTEN";
    const since = Date.now() - (s.elapsed || 0) * 1000;
    const draw = () => say("RUN", elapsed(Math.max(0, Math.round((Date.now() - since) / 1000))));
    draw();
    clock = setInterval(draw, 1000);
    poll = setTimeout(refresh, 5000);
    return;
  }
  const last = s.finished ? `LAST RUN ${s.finished}` : "";
  if (s.ready_in > 0) {
    const failed = s.failed && s.failed.length > 0;
    box.dataset.state = failed ? "failed" : "done";
    button.disabled = true;
    button.title = failed ? `${s.failed.join(", ").toUpperCase()} DID NOT RUN` : last;
    say(failed ? "FAIL" : "DONE", s.finished || "");
    wake = setTimeout(() => show({ ...s, ready_in: 0 }), s.ready_in * 1000);
    return;
  }
  box.dataset.state = "ready";
  button.disabled = false;
  button.title = last ? `RERUN EVERY BRIEF NOW / ${last}` : "RERUN EVERY BRIEF NOW";
  say("RERUN");
}

async function refresh() {
  const { body } = await ask();
  if (body.signed_in !== true) {
    stop();
    box.hidden = true;
    return;
  }
  show(body);
}

async function press() {
  if (button.disabled) return;
  button.disabled = true;
  const { status, body } = await ask({
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  });
  if (status === 401) {
    stop();
    box.hidden = true;
    return;
  }
  if (status === 503) {
    stop();
    box.dataset.state = "off";
    button.title = "THE BRIEFS ARE OFF";
    say("OFF");
    return;
  }
  if (body.signed_in === true) {
    show(body);
    return;
  }
  // Nothing useful came back, so ask again instead of guessing.
  button.disabled = false;
  refresh();
}

if (box && button && label) {
  ask().then(({ body }) => {
    if (body.signed_in !== true) return;
    box.hidden = false;
    button.addEventListener("click", press);
    show(body);
  });
}

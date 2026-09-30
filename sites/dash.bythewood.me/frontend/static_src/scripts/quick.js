// A quick question to chat.bythewood.me. The page is the same for every visitor
// and can come out of Cloudflare's cache, so whether to offer it is asked after
// load, and nothing is bound or shown unless the answer is yes.

const dialog = document.querySelector("[data-quick]");
const opener = document.querySelector("[data-quick-open]");
const openButton = opener?.querySelector("[data-quick-button]");
const keyLabel = opener?.querySelector("[data-quick-key]");
const log = dialog?.querySelector("[data-quick-log]");
const form = dialog?.querySelector("[data-quick-form]");
const input = dialog?.querySelector("[data-quick-input]");
const sendButton = dialog?.querySelector("[data-quick-send]");
const statusLine = dialog?.querySelector("[data-quick-status]");
const chatLink = dialog?.querySelector("[data-quick-link]");
const newButton = dialog?.querySelector("[data-quick-new]");
const closeButton = dialog?.querySelector("[data-quick-close]");

const CHAT = "https://chat.bythewood.me/c/";

let convID = "";
let inflight = null;

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

async function signedIn() {
  try {
    const r = await fetch("/api/quick", { cache: "no-store" });
    if (!r.ok) return false;
    return (await r.json()).signed_in === true;
  } catch {
    return false;
  }
}

function status(text) {
  statusLine.textContent = text ? text.toUpperCase() : "";
}

// Closing the box ends the conversation, so the next open starts a new one. A
// turn still running when it closed is left alone and shown again if the box
// comes back before it finishes.
let fresh = false;

function open() {
  if (fresh && !inflight) clear();
  fresh = false;
  if (!dialog.open) dialog.showModal();
  input.focus();
}

function busy(on) {
  sendButton.disabled = on;
  newButton.disabled = on;
}

// The textarea grows with what is typed, up to the cap in the stylesheet.
function fit() {
  input.style.height = "auto";
  input.style.height = `${input.scrollHeight}px`;
}

function pin() {
  log.scrollTop = log.scrollHeight;
}

function exchange(question) {
  const q = el("p", "quick-q", question);
  const work = deck();
  const a = el("div", "quick-a");
  const blocks = el("div", "blocks");
  const tail = el("p", "tail");
  a.append(blocks, tail);
  log.append(q, work.root, a);
  pin();
  return { a, blocks, tail, work };
}

function failed(ui, text) {
  status("");
  ui.work.stop("failed");
  ui.a.append(el("p", "quick-err", text));
}

// ---------------------------------------------------------------- the work deck

// What chat is doing while it works, from the same step and tool events its own
// page draws. A turn runs for ten seconds to a minute and this is what there is
// to watch, so it reads like a tape deck: reels that turn while it works, a meter
// that jumps on every event, a counter of tokens written, and a log of each
// round, call and check as it happens.

const SVG = "http://www.w3.org/2000/svg";

function reel() {
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("class", "reel");
  svg.setAttribute("aria-hidden", "true");
  const parts = [
    ["circle", { cx: 12, cy: 12, r: 10.5 }],
    ["circle", { cx: 12, cy: 12, r: 3, class: "hub" }],
    ["path", { d: "M12 9 V2.5 M14.6 13.5 L20.2 16.75 M9.4 13.5 L3.8 16.75", class: "spoke" }],
  ];
  for (const [tag, attrs] of parts) {
    const node = document.createElementNS(SVG, tag);
    for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
    svg.append(node);
  }
  return svg;
}

const KIND = {
  prompt: "PROMPT",
  memory: "MEMORY",
  wikipedia: "WIKI",
  model: "MODEL",
  gate: "GATE",
  compact: "COMPACT",
  answer: "WRITE",
  title: "TITLE",
};

function clockText(ms) {
  const s = ms / 1000;
  const m = Math.floor(s / 60);
  return `${String(m).padStart(2, "0")}:${(s - m * 60).toFixed(1).padStart(4, "0")}`;
}

function short(text, n) {
  const t = String(text || "").replace(/\s+/g, " ").trim();
  return t.length > n ? `${t.slice(0, n - 1)}…` : t;
}

// A tool result as a line a person would write about it, since the raw JSON
// is mostly braces and field names at this width.
function preview(out) {
  const raw = String(out || "").trim();
  if (!raw.startsWith("{")) return raw;
  let r;
  try {
    r = JSON.parse(raw);
  } catch {
    // chat clips a long result before it sends it, so the JSON is often cut
    // short, and the titles are still there to be read.
    const titles = [...raw.matchAll(/"title":"((?:[^"\\]|\\.)*)"/g)].map((m) => m[1]);
    if (titles.length) return `results · ${titles.slice(0, 3).join(" · ")}`;
    const text = /"text":"((?:[^"\\]|\\.)*)/.exec(raw);
    if (text) return `read · ${text[1].replace(/\\n/g, " ")}`;
    const key = /^\{"(\w+)":/.exec(raw);
    return key ? `read ${key[1]}` : raw;
  }
  if (Array.isArray(r.results)) return `${r.results.length} results · ${r.results.slice(0, 3).map((x) => x.title).join(" · ")}`;
  if (r.found === false) return "no article under that name";
  if (r.found === true && r.title) return `read ${r.title}`;
  if (Array.isArray(r.quotes)) return r.quotes.map((q) => `${q.symbol} ${q.price}`).join(" · ");
  if (typeof r.text === "string") return `read ${r.chars || r.text.length} chars · ${r.text}`;
  if (r.remembered || r.now) return `kept · ${r.remembered || r.now}`;
  if (r.value !== undefined) return `= ${r.value}`;
  if (typeof r.section === "string") return `read ${r.section}`;
  const keys = Object.keys(r);
  return keys.length ? `read ${keys.slice(0, 4).join(", ")}` : raw;
}

// The counts a step carries, cut to what fits beside its name.
function metaText(kind, meta) {
  const m = String(meta || "");
  let x = /([\d,]+) tokens in, ([\d,]+) out/.exec(m);
  if (x) return `${Number(x[1].replace(/,/g, "")).toLocaleString()} IN · ${x[2]} OUT`;
  x = /([\d,]+) characters/.exec(m);
  if (x) return `${Number(x[1].replace(/,/g, "")).toLocaleString()} CHARS`;
  x = /^(\d+) of the stored facts/.exec(m);
  if (x) return `${x[1]} ${x[1] === "1" ? "FACT" : "FACTS"}`;
  return short(m, 28).toUpperCase();
}

function deck() {
  const root = el("section", "work");
  root.dataset.state = "running";
  root.setAttribute("aria-label", "What chat is doing");

  const head = el("div", "work-head");
  const reels = el("span", "reels");
  reels.append(reel(), el("span", "tape"), reel());
  const stage = el("span", "work-stage", "THINKING");
  stage.setAttribute("aria-live", "polite");
  const vu = el("span", "vu");
  const fill = el("span", "vu-fill");
  vu.append(fill);
  const counter = el("span", "work-count");
  const tape = el("span", "v", "0000");
  counter.append(el("span", "k", "TAPE"), tape);
  const clock = el("span", "work-clock", "00:00.0");
  head.append(reels, stage, vu, counter, clock);

  const lines = el("ol", "work-log");
  const scan = el("span", "work-scan");
  scan.setAttribute("aria-hidden", "true");
  const summary = el("button", "work-sum");
  summary.type = "button";
  summary.hidden = true;
  summary.addEventListener("click", () => {
    root.classList.toggle("open");
    pin();
  });
  root.append(head, lines, scan, summary);

  const started = performance.now();
  let written = 0;
  let tools = 0;
  let count = 0;
  let level = 0.2;
  let running = true;
  const pending = new Map();

  const tick = setInterval(() => {
    clock.textContent = clockText(performance.now() - started);
  }, 100);

  // The needle falls back between events and jitters a little on its own, so a
  // long wait on the model still reads as something happening.
  const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
  function meter() {
    if (!running) return;
    level = Math.max(0.12, level * 0.94);
    const jitter = still ? 0 : (Math.random() - 0.5) * 0.08;
    fill.style.transform = `scaleX(${Math.min(1, Math.max(0.05, level + jitter)).toFixed(3)})`;
    requestAnimationFrame(meter);
  }
  requestAnimationFrame(meter);
  const kick = (to = 0.95) => { level = Math.max(level, to); };

  function line(kind, label, text, meta) {
    const li = el("li", "w-line");
    li.dataset.kind = kind;
    li.append(
      el("span", "w-glyph"),
      el("span", "w-kind", label),
      el("span", "w-text", text || ""),
      el("span", "w-meta", meta || ""),
    );
    lines.append(li);
    count++;
    kick();
    pin();
    return li;
  }

  function detail(li, text) {
    text = preview(text);
    if (!text) return;
    let out = li.querySelector(".w-out");
    if (!out) {
      out = el("span", "w-out");
      li.append(out);
    }
    out.textContent = short(text, 140);
  }

  function tokens(meta) {
    const m = /(\d[\d,]*) out/.exec(meta || "");
    if (!m) return;
    written += Number(m[1].replace(/,/g, ""));
    tape.textContent = String(Math.min(written, 9999)).padStart(4, "0");
  }

  function step(s) {
    switch (s.kind) {
      case "tool": {
        // The call line went up when the tool started. This is what came back.
        const li = pending.get(s.label) || [...lines.children].reverse().find((n) => n.dataset.tool === s.label);
        if (li) detail(li, s.out);
        return;
      }
      case "model": {
        const li = line("model", KIND.model, s.label, metaText(s.kind, s.meta));
        li.dataset.bad = s.bad ? "yes" : "no";
        detail(li, s.out);
        tokens(s.meta);
        return;
      }
      case "gate": {
        const back = /^sent it back/i.test(s.out || "");
        const li = line("gate", KIND.gate, back ? "sent back" : "let it through", s.ms ? `${(s.ms / 1000).toFixed(1)}S` : "");
        li.dataset.verdict = back ? "back" : "through";
        if (back) {
          const nudge = (s.out || "").replace(/^sent it back:\s*/i, "");
          const q = /Search for "([^"]+)"/.exec(nudge);
          detail(li, q ? `search "${q[1]}"` : nudge.split(/(?<=\.)\s/)[0]);
        }
        return;
      }
      case "answer":
        tokens(s.meta);
        return;
      default: {
        const li = line(s.kind, KIND[s.kind] || s.kind.toUpperCase(), s.label, metaText(s.kind, s.meta));
        if (s.kind === "memory" || s.kind === "wikipedia") detail(li, s.out);
      }
    }
  }

  function toolStart(name, args) {
    tools++;
    const li = line("tool", name.toUpperCase(), args || "", "");
    li.dataset.tool = name;
    li.dataset.state = "run";
    const bar = el("span", "w-bar");
    bar.append(el("i"));
    li.append(bar);
    pending.set(name, li);
  }

  function toolDone(name, ms, ok) {
    const li = pending.get(name);
    if (!li) return;
    pending.delete(name);
    li.dataset.state = ok ? "ok" : "bad";
    li.querySelector(".w-meta").textContent = `${ok ? "OK" : "FAIL"} ${ms >= 1000 ? `${(ms / 1000).toFixed(1)}S` : `${ms}MS`}`;
    li.querySelector(".w-bar")?.remove();
    kick(1);
  }

  function setStage(text) {
    if (text) stage.textContent = text.toUpperCase();
  }

  // Parts rather than one string, so a phone can drop the token count and keep
  // the line to one row.
  function sum() {
    const took = clockText(performance.now() - started);
    const parts = [
      ["s-steps", `${count} STEPS`],
      ["s-calls", `${tools} ${tools === 1 ? "CALL" : "CALLS"}`],
      ["s-tok", `${String(written).padStart(4, "0")} TOK`],
      ["s-time", took],
    ];
    const text = el("span", "sum-text");
    for (const [cls, t] of parts) text.append(el("span", cls, t));
    summary.replaceChildren(text);
  }

  // Once the answer is coming the log folds to one line, since the answer is
  // what gets read, and the line opens it again.
  function writing() {
    if (root.dataset.state !== "running") return;
    root.dataset.state = "writing";
    setStage("writing");
    summary.hidden = false;
    sum();
  }

  function stop(how) {
    if (!running) return;
    running = false;
    clearInterval(tick);
    clock.textContent = clockText(performance.now() - started);
    root.dataset.state = how;
    setStage(how === "failed" ? "failed" : "done");
    for (const li of pending.values()) li.querySelector(".w-bar")?.remove();
    summary.hidden = false;
    sum();
    fill.style.transform = "scaleX(0)";
  }

  return { root, step, toolStart, toolDone, setStage, writing, stop, kick };
}

// chat's own event shapes. The html in them is chat's server rendered markdown,
// and tool, step, widget and image events are for its own page.
function handle(ev, ui) {
  switch (ev.kind) {
    case "status":
      ui.work.setStage(ev.text);
      ui.work.kick(0.5);
      break;
    case "step":
      if (ev.step) ui.work.step(ev.step);
      break;
    case "tool":
      ui.work.toolStart(ev.tool || "tool", ev.args || "");
      break;
    case "tool_done":
      ui.work.toolDone(ev.tool || "tool", ev.ms || 0, ev.ok === true);
      break;
    case "image":
      if (ev.image?.stage) ui.work.setStage(`picture ${ev.image.stage}`);
      break;
    case "block":
      status("");
      ui.work.writing();
      ui.blocks.insertAdjacentHTML("beforeend", ev.html || "");
      ui.tail.textContent = "";
      break;
    case "tail":
      status("");
      if (ev.text) ui.work.writing();
      ui.tail.textContent = ev.text || "";
      break;
    case "error":
      failed(ui, (ev.text || "that turn failed").toUpperCase());
      break;
    case "done":
      status("");
      ui.work.stop("done");
      if (ev.html) ui.blocks.innerHTML = ev.html;
      ui.tail.remove();
      if (ev.conversation_id) {
        convID = ev.conversation_id;
        chatLink.href = CHAT + encodeURIComponent(convID);
        chatLink.hidden = false;
        newButton.hidden = false;
      }
      break;
    default:
      return;
  }
  pin();
}

// Server sent events split on a blank line, and a chunk can end halfway
// through one, so whatever follows the last separator waits for the next read.
async function consume(resp, ui) {
  const reader = resp.body.getReader();
  const dec = new TextDecoder();
  let buf = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    const parts = buf.split("\n\n");
    buf = parts.pop();
    for (const part of parts) {
      const line = part.split("\n").find((l) => l.startsWith("data:"));
      if (!line) continue;
      let ev;
      try {
        ev = JSON.parse(line.slice(5).trim());
      } catch {
        continue;
      }
      handle(ev, ui);
    }
  }
}

async function send(text) {
  const message = text.trim();
  if (!message || inflight) return;
  const ui = exchange(message);
  input.value = "";
  fit();

  const ctl = new AbortController();
  inflight = ctl;
  busy(true);
  try {
    const resp = await fetch("/api/quick/send", {
      method: "POST",
      headers: { "content-type": "application/json" },
      // A new conversation has no id until chat stores the first turn, so the
      // run is keyed by one made up here, the way chat's own page does it.
      body: JSON.stringify({
        message,
        conversation_id: convID,
        run_id: convID || crypto.randomUUID(),
      }),
      signal: ctl.signal,
    });
    if (resp.status === 401) {
      failed(ui, "SIGNED OUT");
      opener.hidden = true;
      return;
    }
    if (!resp.ok || !resp.body) {
      failed(ui, `CHAT REFUSED THAT (${resp.status})`);
      return;
    }
    await consume(resp, ui);
  } catch (e) {
    if (e.name !== "AbortError") failed(ui, "CHAT IS NOT ANSWERING");
  } finally {
    ui.work.stop(ui.work.root.dataset.state === "failed" ? "failed" : "done");
    inflight = null;
    busy(false);
    status("");
    if (ui.tail.isConnected && !ui.tail.textContent.trim()) ui.tail.remove();
    pin();
  }
}

function clear() {
  if (inflight) return;
  convID = "";
  log.replaceChildren();
  chatLink.hidden = true;
  newButton.hidden = true;
  input.focus();
}

function keyName() {
  if (matchMedia("(pointer: coarse)").matches) return "OPEN";
  return /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘ K" : "CTRL K";
}

function enable() {
  opener.hidden = false;
  keyLabel.textContent = keyName();
  openButton.addEventListener("click", open);

  document.addEventListener("keydown", (e) => {
    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k") {
      e.preventDefault();
      open();
    }
  });

  form.addEventListener("submit", (e) => {
    e.preventDefault();
    send(input.value);
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      send(input.value);
    }
  });
  input.addEventListener("input", fit);
  newButton.addEventListener("click", clear);
  closeButton.addEventListener("click", () => dialog.close());
  dialog.addEventListener("close", () => {
    fresh = true;
  });

  // The box has no padding of its own, so a click whose target is the dialog
  // itself landed on the backdrop.
  dialog.addEventListener("click", (e) => {
    if (e.target === dialog) dialog.close();
  });
}

if (dialog && opener) {
  signedIn().then((yes) => {
    if (yes) enable();
  });
}

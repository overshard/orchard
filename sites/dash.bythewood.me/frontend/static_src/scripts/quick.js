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

function open() {
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
  const a = el("div", "quick-a");
  const blocks = el("div", "blocks");
  const tail = el("p", "tail");
  a.append(blocks, tail);
  log.append(q, a);
  pin();
  return { a, blocks, tail };
}

function failed(ui, text) {
  status("");
  ui.a.append(el("p", "quick-err", text));
}

// chat's own event shapes. The html in them is chat's server rendered markdown,
// and tool, step, widget and image events are for its own page.
function handle(ev, ui) {
  switch (ev.kind) {
    case "status":
      status(ev.text);
      break;
    case "block":
      status("");
      ui.blocks.insertAdjacentHTML("beforeend", ev.html || "");
      ui.tail.textContent = "";
      break;
    case "tail":
      status("");
      ui.tail.textContent = ev.text || "";
      break;
    case "error":
      failed(ui, (ev.text || "that turn failed").toUpperCase());
      break;
    case "done":
      status("");
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
  status("thinking");
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

// The browser half. It holds no conversation state of its own beyond the id:
// the server renders markdown and owns history, so a reload is authoritative
// and there is nothing here to drift out of step with the database.
(() => {
  const $ = (id) => document.getElementById(id);
  const thread = $("thread"), input = $("input"), form = $("ask");
  const send = $("send"), stop = $("stop"), welcome = $("welcome");
  const incognito = $("incognito"), incogFlag = $("incog-flag");
  const barTitle = $("bar-title"), modelDot = $("model-dot"), footState = $("foot-state");
  const side = $("side"), convs = $("convs"), scrim = $("scrim");

  // The width the stylesheet switches the sidebar to an overlay at. Kept in one
  // place because the two have to agree: a sidebar that is an overlay in CSS and
  // open by default in JS covers the conversation on every phone.
  const NARROW = 832;
  const isNarrow = () => window.innerWidth <= NARROW;

  // Focusing the box summons the on screen keyboard, so on a phone every
  // automatic focus costs half the screen and the reader did not ask for it.
  // A real keyboard is what a fine pointer and hover imply, and pressing a
  // shortcut proves one either way, so those focus unconditionally.
  const hasKeyboard = () => matchMedia("(hover: hover) and (pointer: fine)").matches;
  const refocus = () => { if (hasKeyboard()) input.focus(); };

  function showSide(open) {
    side.classList.toggle("closed", !open);
    scrim.hidden = !(open && isNarrow());
    // A drawer that opens behind an open keyboard is half a drawer.
    if (open && isNarrow()) document.activeElement?.blur();
  }
  const meter = $("meter"), meterNum = $("meter-num"), meterFill = $("meter-fill"),
        meterCap = $("meter-cap"), tps = $("tps");
  const tray = $("tray"), picker = $("picker"), drop = $("drop");
  let ctxSize = Number(meterCap.textContent) || 32768;

  const MAX_FILES = 10, MAX_BYTES = 20 * 1024 * 1024;

  let convID = "";
  let inflight = null;
  // Files chosen but not sent yet. They stay File objects until the turn goes,
  // so nothing is uploaded until there is a message to attach them to.
  let staged = [];

  // ---------------------------------------------------------------- helpers

  const atBottom = () => thread.scrollHeight - thread.scrollTop - thread.clientHeight < 80;
  function toBottom(force) {
    if (force || atBottom()) thread.scrollTop = thread.scrollHeight;
  }

  // The newest exchange is pinned with the question at the top of the viewport
  // and the answer growing underneath it, which is what every chat does and
  // what stops a long answer dragging the question off screen.
  //
  // It needs a spacer: at the bottom of the thread there is nothing below the
  // answer to scroll into, so without one the question cannot reach the top.
  // The spacer shrinks as the answer grows and reaches zero once the exchange
  // fills the screen on its own.
  let spacer = null, anchor = null;
  function makeRoom(node) {
    anchor = node;
    if (!spacer) {
      spacer = document.createElement("div");
      spacer.className = "spacer";
      thread.appendChild(spacer);
    } else {
      thread.appendChild(spacer);
    }
    sizeSpacer();
    scrollToAnchor(true);
  }
  function sizeSpacer() {
    if (!spacer || !anchor) return;
    const used = thread.scrollHeight - spacer.offsetHeight - anchor.offsetTop;
    const room = Math.max(0, thread.clientHeight - used - 24);
    spacer.style.height = room + "px";
  }
  // A few pixels of air above the question, since scrolling it flush to the
  // container edge tucks its first line under the bar.
  const PIN_GAP = 14;
  function scrollToAnchor(smooth) {
    if (!anchor) return;
    thread.scrollTo({ top: Math.max(0, anchor.offsetTop - PIN_GAP),
                      behavior: smooth ? "smooth" : "auto" });
  }
  function keepPinned() {
    if (!anchor) return;
    sizeSpacer();
    // Follow the text only while the reader is still down here. Scrolling up
    // to re-read something should not be yanked back.
    if (atBottom()) return;
    if (thread.scrollTop < anchor.offsetTop - PIN_GAP - 4) scrollToAnchor(false);
  }

  function bubble(role) {
    const node = $("tpl-msg").content.firstElementChild.cloneNode(true);
    node.classList.add(role === "user" ? "user" : "bot");
    node.querySelector(".who").textContent = role === "user" ? "You" : "Assistant";
    thread.appendChild(node);
    return node;
  }

  function toolChip(t) {
    const el = document.createElement("span");
    el.className = "tool" + (t.ok === false ? " bad" : t.running ? " run" : "");
    const ms = t.running ? "" : `<span class="ms">${fmtMs(t.ms)}</span>`;
    // A local snapshot answers as fast as anything and is as old as its file,
    // so the chip says when it was taken.
    const age = t.age ? `<span class="age" title="this data was taken ${esc(t.age)}">${esc(t.age)}</span>` : "";
    el.innerHTML = `<b>${esc(t.name)}</b>${t.args ? " " + esc(t.args) : ""}${age}${ms}`;
    if (t.err) el.title = t.err;
    return el;
  }

  // The record of what a turn did. It is open while the turn runs, so the work
  // is visible as it happens, and collapses to one line once the answer lands.
  // Nothing here is decoration: an answer off a local snapshot and one off the
  // model's memory look identical without it.
  const STEP_KINDS = {
    prompt: "prompt", memory: "memory", wikipedia: "wikipedia", model: "model",
    tool: "tool", gate: "gate", answer: "answer", title: "title", compact: "compact",
  };

  function workPanel(box) {
    if (box.dataset.built) return box;
    box.dataset.built = "1";
    box.innerHTML =
      `<button class="work-head" type="button" aria-expanded="true">` +
      `<span class="chev" aria-hidden="true"></span>` +
      `<span class="work-k">// WORK</span>` +
      `<span class="work-sum"></span></button>` +
      `<ol class="work-steps"></ol>`;
    const head = box.querySelector(".work-head");
    head.addEventListener("click", () => {
      const open = head.getAttribute("aria-expanded") === "true";
      head.setAttribute("aria-expanded", String(!open));
      box.querySelector(".work-steps").hidden = open;
    });
    return box;
  }

  function stepRow(s) {
    const li = document.createElement("li");
    li.className = "step" + (s.bad ? " bad" : "");
    li.dataset.kind = STEP_KINDS[s.kind] || "model";
    const ms = s.ms ? `<span class="ms">${fmtMs(s.ms)}</span>` : "";
    const io = (s.in || s.out || s.meta);
    li.innerHTML =
      `<button class="step-head" type="button" aria-expanded="false"${io ? "" : " disabled"}>` +
      `<span class="dot" aria-hidden="true"></span>` +
      `<span class="k">${esc(s.kind)}</span>` +
      `<span class="l">${esc(s.label || "")}</span>${ms}</button>` +
      (io ? `<div class="step-body" hidden>` +
        (s.meta ? `<div class="step-meta">${esc(s.meta)}</div>` : "") +
        (s.in ? `<div class="io"><span class="io-k">in</span><pre>${esc(s.in)}</pre></div>` : "") +
        (s.out ? `<div class="io"><span class="io-k">out</span><pre>${esc(s.out)}</pre></div>` : "") +
        `</div>` : "");
    if (io) {
      const head = li.querySelector(".step-head");
      head.addEventListener("click", () => {
        const open = head.getAttribute("aria-expanded") === "true";
        head.setAttribute("aria-expanded", String(!open));
        li.querySelector(".step-body").hidden = open;
      });
    }
    return li;
  }

  function addStep(box, s) {
    if (!box || !s) return;
    workPanel(box).hidden = false;
    box.querySelector(".work-steps").appendChild(stepRow(s));
    countWork(box);
  }

  function countWork(box) {
    const n = box.querySelectorAll(".step").length;
    box.querySelector(".work-sum").textContent = n + (n === 1 ? " step" : " steps");
  }

  // Collapsed once the answer is there, since by then the reader wants the
  // answer and the work is something to open if they care.
  function finishWork(box, steps) {
    if (!box) return;
    if (Array.isArray(steps) && steps.length) {
      workPanel(box).hidden = false;
      const list = box.querySelector(".work-steps");
      if (!list.children.length) list.replaceChildren(...steps.map(stepRow));
      countWork(box);
    }
    if (box.hidden || !box.dataset.built) return;
    const head = box.querySelector(".work-head");
    head.setAttribute("aria-expanded", "false");
    box.querySelector(".work-steps").hidden = true;
  }

  function sourceChip(s) {
    const el = document.createElement("a");
    el.className = "src";
    el.href = s.url;
    el.target = "_blank";
    el.rel = "noopener noreferrer";
    el.title = s.title || s.site;
    el.innerHTML = `<b>${s.n}</b>${esc(s.site || s.url)}`;
    return el;
  }

  function showSources(box, list) {
    if (!box || !Array.isArray(list) || !list.length) return;
    box.hidden = false;
    box.replaceChildren(...list.map(sourceChip));
  }

  // A tool that answers instantly has no ms field at all, since the server
  // omits a zero. Without this the chip reads NaN.
  function fmtMs(ms) {
    const n = Number(ms) || 0;
    return n < 1000 ? n + "ms" : (n / 1000).toFixed(1) + "s";
  }

  // 1024 and not 1000, because a context window is a power of two and 65536 has
  // to read as the 64k it was configured as rather than 66k.
  function kfmt(n) {
    n = Number(n) || 0;
    return n < 1024 ? String(n) : (n / 1024).toFixed(n < 10240 ? 1 : 0) + "k";
  }

  // The meter reports the whole window the model was handed, not the length of
  // the last message, since that is the number that decides when compaction
  // has to happen.
  function showStats(st) {
    if (!st) return;
    if (st.ctx) { ctxSize = st.ctx; meterCap.textContent = kfmt(st.ctx); }
    const used = Number(st.prompt_tokens) || 0;
    if (used) {
      const pct = Math.min(100, (used / ctxSize) * 100);
      meter.hidden = false;
      meterNum.textContent = kfmt(used);
      meterFill.style.width = pct.toFixed(1) + "%";
      meter.classList.toggle("warm", pct >= 55 && pct < 80);
      meter.classList.toggle("hot", pct >= 80);
      meter.title = `${used.toLocaleString()} of ${ctxSize.toLocaleString()} tokens used by the last turn`
        + (pct >= 55 ? ". Older turns get summarised past 55%." : "");
    }
    if (st.decode_tps) {
      tps.hidden = false;
      tps.innerHTML = `<b>${st.decode_tps}</b> tok/s`;
      tps.title = `Generated at ${st.decode_tps} tokens a second`
        + (st.prefill_tps ? `, prompt read at ${st.prefill_tps} a second` : "");
    }
  }

  // Both numbers describe the last turn of one thread, so they have to go when
  // the thread does. Nothing stores them per conversation, so an old one reopens
  // with an empty bar until it runs a turn.
  function clearStats() {
    meter.hidden = true;
    tps.hidden = true;
    meterNum.textContent = "0";
    meterFill.style.width = "0%";
    meter.classList.remove("warm", "hot");
    meter.title = "";
    tps.textContent = "";
    tps.title = "";
  }

  function esc(s) {
    return String(s ?? "").replace(/[&<>"']/g, (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  }

  function statusLine(text) {
    let el = thread.querySelector(".status");
    if (!el) {
      el = document.createElement("div");
      el.className = "status";
      el.innerHTML = `<i class="dot busy"></i><span class="txt"></span><span class="dots"></span>`;
      thread.insertBefore(el, spacer || null);
    }
    el.querySelector(".txt").textContent = text;
    return el;
  }
  const clearStatus = () => thread.querySelectorAll(".status").forEach((e) => e.remove());

  function errorLine(text) {
    const el = document.createElement("div");
    el.className = "err";
    el.textContent = text;
    thread.appendChild(el);
    toBottom(true);
  }

  function busy(on) {
    send.hidden = on; stop.hidden = !on; input.disabled = on;
    modelDot.classList.toggle("busy", on);
    modelDot.classList.toggle("on", !on);
    if (!on) refocus();
  }

  // ----------------------------------------------------------- attachments

  // Why a file was refused belongs next to the composer where the refusal
  // happened, and it goes back to the keybind hint on its own.
  let footTimer = null;
  const footRest = footState.textContent;
  function footNote(text) {
    footState.textContent = text;
    clearTimeout(footTimer);
    footTimer = setTimeout(() => { footState.textContent = footRest; }, 4000);
  }

  function addFiles(list) {
    for (const f of list) {
      if (staged.length >= MAX_FILES) { footNote(`${MAX_FILES} files is the limit.`); break; }
      if (f.size > MAX_BYTES) { footNote(`${f.name} is over 20 MB.`); continue; }
      // Same name and size twice is the same file, which is what a second drop
      // of the same thing produces.
      if (staged.some((s) => s.name === f.name && s.size === f.size)) continue;
      staged.push(f);
    }
    renderTray();
  }

  function renderTray() {
    tray.replaceChildren();
    tray.hidden = staged.length === 0;
    staged.forEach((f, i) => {
      const el = document.createElement("span");
      el.className = "chip";
      el.innerHTML = `<span class="nm">${esc(f.name)}</span><span class="sz">${bytes(f.size)}</span>`;
      const x = document.createElement("button");
      x.type = "button";
      x.textContent = "\u2715";
      x.title = "Remove";
      x.setAttribute("aria-label", `Remove ${f.name}`);
      x.addEventListener("click", () => { staged.splice(i, 1); renderTray(); refocus(); });
      el.appendChild(x);
      tray.appendChild(el);
    });
    sizeSpacer();
  }

  // A chip on a message that has already been sent. The file is gone by then,
  // so this only ever reports what happened to it.
  function fileChip(f) {
    const el = document.createElement("span");
    el.className = "chip" + (f.err ? " bad" : "");
    el.innerHTML = `<span class="nm">${esc(f.name)}</span><span class="sz">${bytes(f.size)}</span>`;
    if (f.err) el.title = f.err;
    return el;
  }

  function bytes(n) {
    if (n < 1024) return n + " B";
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KB";
    return (n / (1024 * 1024)).toFixed(1) + " MB";
  }

  $("attach").addEventListener("click", () => picker.click());
  picker.addEventListener("change", () => { addFiles(picker.files); picker.value = ""; });

  // dragenter and dragleave fire for every element the pointer crosses, so the
  // overlay is counted in rather than toggled or it flickers over the thread.
  let dragDepth = 0;
  const dragging = (e) => Array.from(e.dataTransfer?.types || []).includes("Files");
  window.addEventListener("dragenter", (e) => {
    if (!dragging(e)) return;
    e.preventDefault();
    if (++dragDepth === 1) drop.hidden = false;
  });
  window.addEventListener("dragover", (e) => { if (dragging(e)) e.preventDefault(); });
  window.addEventListener("dragleave", (e) => {
    if (!dragging(e)) return;
    if (--dragDepth <= 0) { dragDepth = 0; drop.hidden = true; }
  });
  window.addEventListener("drop", (e) => {
    if (!dragging(e)) return;
    e.preventDefault();
    dragDepth = 0;
    drop.hidden = true;
    addFiles(e.dataTransfer.files);
    refocus();
  });

  input.addEventListener("paste", (e) => {
    const files = Array.from(e.clipboardData?.files || []);
    if (!files.length) return;
    e.preventDefault();
    addFiles(files);
  });

  // ------------------------------------------------------------ memory

  const memSheet = $("mem-sheet"), memList = $("mem-list"), memMsg = $("mem-msg"),
        memSaid = $("mem-said"), memSend = $("mem-send"), memCount = $("mem-count");

  function memShow(view) {
    if (!view) return;
    memMsg.hidden = !(view.note || view.problem);
    memMsg.textContent = view.note || view.problem || "";
    memMsg.classList.toggle("bad", !!view.problem && !view.note);

    const facts = view.facts || [];
    memCount.textContent = facts.length
      ? `${facts.length} fact${facts.length === 1 ? "" : "s"}`
      : "";
    memList.replaceChildren();
    if (!facts.length) {
      memList.innerHTML = `<p class="mem-empty">Nothing yet. It picks things up as you talk, and you can tell it something above.</p>`;
      return;
    }
    for (const f of facts) {
      const el = document.createElement("div");
      el.className = "fact";
      el.innerHTML = `<span class="ft"></span>` +
        `<span class="fu">${f.used ? `used ${f.used}\u00d7` : "unused"}</span>`;
      el.querySelector(".ft").textContent = f.fact;
      const x = document.createElement("button");
      x.type = "button";
      x.className = "fx";
      x.textContent = "\u2715";
      x.title = "Forget this";
      x.setAttribute("aria-label", "Forget this");
      x.addEventListener("click", async () => {
        el.classList.add("going");
        memShow(await memCall("DELETE", "/api/memory/" + f.id));
      });
      el.appendChild(x);
      memList.appendChild(el);
    }
  }

  async function memCall(method, url, body) {
    try {
      const r = await fetch(url, {
        method,
        headers: body ? { "content-type": "application/json" } : undefined,
        body: body ? JSON.stringify(body) : undefined,
      });
      if (!r.ok) return { problem: "that did not go through (" + r.status + ")" };
      return await r.json();
    } catch (e) {
      return { problem: e.message || String(e) };
    }
  }

  async function memToggle(on) {
    const show = on ?? memSheet.hidden;
    memSheet.hidden = !show;
    if (show) {
      memShow({ facts: [] });
      memShow(await memCall("GET", "/api/memory"));
      if (hasKeyboard()) memSaid.focus();
    } else {
      refocus();
    }
  }

  $("mem-open").addEventListener("click", () => memToggle(true));
  $("mem-close").addEventListener("click", () => memToggle(false));
  memSheet.addEventListener("click", (e) => { if (e.target === memSheet) memToggle(false); });

  $("mem-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const said = memSaid.value.trim();
    if (!said) return;
    // A round trip here is a model call, so the wait is real and the button
    // has to say so rather than looking like nothing happened.
    memSend.disabled = true;
    memSend.textContent = "thinking";
    memMsg.hidden = false;
    memMsg.classList.remove("bad");
    memMsg.textContent = "Working out what should change...";
    const view = await memCall("POST", "/api/memory/teach", { said });
    memSend.disabled = false;
    memSend.textContent = "Tell it";
    if (!view.problem) memSaid.value = "";
    memShow(view);
    if (hasKeyboard()) memSaid.focus();
  });

  memSaid.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); $("mem-form").requestSubmit(); }
  });

  $("mem-forget").addEventListener("click", async () => {
    if (!confirm("Forget every one of these? This cannot be undone.")) return;
    memShow(await memCall("DELETE", "/api/memory"));
  });

  // ---------------------------------------------------------------- sending

  async function ask(text) {
    // A turn made only of files is a real question, so an empty box is only
    // empty when nothing is attached either.
    if ((!text.trim() && !staged.length) || inflight) return;
    welcome?.remove();
    const sending = staged;
    staged = [];
    renderTray();

    const mine = bubble("user");
    mine.querySelector(".body").textContent = text;
    if (!text.trim()) mine.querySelector(".body").remove();
    if (sending.length) {
      const fb = mine.querySelector(".files");
      fb.hidden = false;
      fb.replaceChildren(...sending.map((f) => fileChip({ name: f.name, size: f.size })));
    }

    const reply = bubble("bot");
    const body = reply.querySelector(".body");
    const toolbar = reply.querySelector(".tools");
    const workbox = reply.querySelector(".work");
    const srcbox = reply.querySelector(".sources");
    const wdgbox = reply.querySelector(".widgets");
    body.classList.add("typing");
    makeRoom(mine);

    // The body is built from finished blocks plus one trailing paragraph of
    // plain text. Nothing is re-rendered at the end, so there is no reflow to
    // read through.
    const blocks = document.createElement("div");
    const tail = document.createElement("p");
    tail.className = "tail";
    body.append(blocks, tail);

    const ctl = new AbortController();
    inflight = ctl;
    busy(true);
    statusLine("thinking");

    try {
      const convFor = incognito.checked ? "" : convID;
      // A form rather than JSON once there are files, and JSON when there are
      // none so the ordinary turn does not pay for multipart framing.
      let init;
      if (sending.length) {
        const fd = new FormData();
        fd.append("message", text);
        fd.append("conversation_id", String(convFor));
        fd.append("incognito", String(incognito.checked));
        for (const f of sending) fd.append("files", f, f.name);
        init = { method: "POST", body: fd, signal: ctl.signal };
      } else {
        init = {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            message: text,
            conversation_id: convFor,
            incognito: incognito.checked,
          }),
          signal: ctl.signal,
        };
      }
      const resp = await fetch("/api/send", init);
      if (!resp.ok || !resp.body) throw new Error("the server refused that (" + resp.status + ")");

      const reader = resp.body.getReader();
      const dec = new TextDecoder();
      let buf = "";
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        // Server sent events are separated by a blank line, and a chunk can
        // split one in half, so anything after the last separator is kept.
        const parts = buf.split("\n\n");
        buf = parts.pop();
        for (const part of parts) {
          const line = part.split("\n").find((l) => l.startsWith("data:"));
          if (!line) continue;
          let ev;
          try { ev = JSON.parse(line.slice(5).trim()); } catch { continue; }
          handle(ev, { body, toolbar, srcbox, blocks, tail, widgets: wdgbox,
                       work: workbox, files: mine.querySelector(".files") });
        }
      }
    } catch (e) {
      if (e.name !== "AbortError") errorLine(e.message || String(e));
    } finally {
      clearStatus();
      body.classList.remove("typing");
      if (tail.isConnected && !tail.textContent.trim()) tail.remove();
      if (!body.textContent.trim()) reply.remove();
      inflight = null;
      busy(false);
      sizeSpacer();
      // A turn is when search finds out it has been rate limited, so the bar
      // learns about it here rather than on the next reload.
      refreshStatus();
    }
  }

  function handle(ev, ui) {
    switch (ev.kind) {
      case "status":
        statusLine(ev.text);
        keepPinned();
        break;
      case "tool": {
        ui.toolbar.hidden = false;
        const chip = toolChip({ name: ev.tool, args: ev.args, running: true });
        chip.dataset.pending = ev.tool;
        ui.toolbar.appendChild(chip);
        statusLine(ev.tool.replace(/_/g, " "));
        keepPinned();
        break;
      }
      case "tool_done": {
        const pending = ui.toolbar.querySelector(`[data-pending="${CSS.escape(ev.tool)}"]`);
        if (pending) {
          pending.removeAttribute("data-pending");
          pending.classList.remove("run");
          if (!ev.ok) pending.classList.add("bad");
          pending.insertAdjacentHTML("beforeend", `<span class="ms">${fmtMs(ev.ms)}</span>`);
        }
        break;
      }
      case "widget":
        // Straight in, rather than waiting for the answer, so the chart is on
        // screen while the prose about it is still being written.
        if (ev.widget) window.Widgets.add(ui.widgets, ev.widget);
        keepPinned();
        break;
      case "block":
        clearStatus();
        // Append rather than replace, so nothing already on screen moves.
        ui.blocks.insertAdjacentHTML("beforeend", ev.html);
        ui.tail.textContent = "";
        keepPinned();
        break;
      case "tail":
        clearStatus();
        ui.tail.textContent = ev.text || "";
        keepPinned();
        break;
      case "step":
        addStep(ui.work, ev.step);
        break;
      case "error":
        clearStatus();
        errorLine(ev.text);
        break;
      case "done":
        clearStatus();
        showStats(ev.stats);
        ui.tail.remove();
        if (ev.conversation_id) {
          const isNew = !convID;
          convID = ev.conversation_id;
          if (isNew) refreshConversations();
        }
        if (Array.isArray(ev.tools) && ev.tools.length) {
          ui.toolbar.hidden = false;
          ui.toolbar.replaceChildren(...ev.tools.map(toolChip));
        }
        finishWork(ui.work, ev.steps);
        showSources(ui.srcbox, ev.sources);
        if (Array.isArray(ev.files) && ev.files.length && ui.files) {
          ui.files.hidden = false;
          ui.files.replaceChildren(...ev.files.map(fileChip));
        }
        keepPinned();
        break;
    }
  }

  // ---------------------------------------------------------------- history

  async function refreshConversations() {
    try {
      const r = await fetch("/api/conversations");
      const d = await r.json();
      convs.replaceChildren();
      if (!d.conversations || !d.conversations.length) {
        convs.innerHTML = `<p class="empty">Nothing yet. Ask something.</p>`;
      } else {
        for (const c of d.conversations) {
          const a = document.createElement("a");
          a.className = "conv" + (c.id === convID ? " active" : "");
          a.href = "/c/" + c.id;
          a.dataset.id = c.id;
          a.innerHTML = `<span class="conv-title">${esc(c.title)}</span>` +
            `<span class="conv-when">${esc(when(c.updated))}</span>` +
            `<button class="conv-del" data-del="${c.id}" title="Delete" aria-label="Delete conversation">&#10005;</button>`;
          convs.appendChild(a);
        }
      }
      if (!filter.hidden) applyFilter();
      const s = await (await fetch("/api/status")).json();
      $("stat-convs").textContent = s.conversations;
      $("stat-msgs").textContent = s.messages;
      if (s.ctx) { ctxSize = s.ctx; meterCap.textContent = kfmt(s.ctx); }
    } catch { /* the list is a convenience, not the conversation */ }
  }

  function when(iso) {
    const d = (Date.now() - new Date(iso)) / 1000;
    if (d < 60) return "just now";
    if (d < 3600) return Math.floor(d / 60) + "m ago";
    if (d < 86400) return Math.floor(d / 3600) + "h ago";
    return new Date(iso).toLocaleDateString(undefined, { day: "numeric", month: "short" });
  }

  async function openConversation(id) {
    const r = await fetch("/api/conversation/" + id);
    if (!r.ok) return;
    const d = await r.json();
    convID = id;
    thread.replaceChildren();
    spacer = null; anchor = null;
    barTitle.textContent = d.title || "Conversation";
    clearStats();
    for (const m of d.messages) {
      const node = bubble(m.role === "user" ? "user" : "bot");
      const body = node.querySelector(".body");
      if (m.role === "user") body.textContent = m.text;
      else body.innerHTML = m.html || esc(m.text);
      if (m.role === "user" && !m.text) body.remove();
      if (m.files && m.files.length) {
        const fb = node.querySelector(".files");
        fb.hidden = false;
        fb.replaceChildren(...m.files.map(fileChip));
      }
      if (m.tools && m.tools.length) {
        const tb = node.querySelector(".tools");
        tb.hidden = false;
        tb.replaceChildren(...m.tools.map(toolChip));
      }
      window.Widgets.render(node.querySelector(".widgets"), m.widgets);
      if (m.steps && m.steps.length) finishWork(node.querySelector(".work"), m.steps);
      showSources(node.querySelector(".sources"), m.sources);
    }
    document.querySelectorAll(".conv").forEach((el) =>
      el.classList.toggle("active", el.dataset.id === id));
    history.pushState({ id }, "", "/c/" + id);
    toBottom(true);
    if (isNarrow()) showSide(false);
  }

  // ---------------------------------------------------------------- wiring

  form.addEventListener("submit", (e) => {
    e.preventDefault();
    const t = input.value;
    input.value = "";
    input.style.height = "auto";
    ask(t);
  });

  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); form.requestSubmit(); }
  });
  input.addEventListener("input", () => {
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, 176) + "px";
  });

  stop.addEventListener("click", () => inflight?.abort());

  document.querySelectorAll(".chip").forEach((c) =>
    c.addEventListener("click", () => ask(c.dataset.ask)));

  $("new-chat").addEventListener("click", () => {
    convID = "";
    thread.replaceChildren();
    spacer = null; anchor = null;
    barTitle.textContent = "New conversation";
    clearStats();
    staged = [];
    renderTray();
    history.pushState({}, "", "/");
    document.querySelectorAll(".conv").forEach((el) => el.classList.remove("active"));
    refocus();
  });

  convs.addEventListener("click", async (e) => {
    const del = e.target.closest("[data-del]");
    if (del) {
      e.preventDefault(); e.stopPropagation();
      await fetch("/api/conversation/" + del.dataset.del, { method: "DELETE" });
      if (del.dataset.del === convID) $("new-chat").click();
      refreshConversations();
      return;
    }
    const a = e.target.closest(".conv");
    if (a) { e.preventDefault(); openConversation(a.dataset.id); }
  });

  $("wipe").addEventListener("click", async () => {
    if (!confirm("Delete every saved conversation? This cannot be undone.")) return;
    await fetch("/api/conversations", { method: "DELETE" });
    $("new-chat").click();
    refreshConversations();
  });

  incognito.addEventListener("change", () => {
    incogFlag.hidden = !incognito.checked;
    footState.textContent = incognito.checked
      ? "Incognito. This conversation is not being written down."
      : "Enter to send, shift and enter for a new line.";
    if (incognito.checked) $("new-chat").click();
  });

  $("side-close").addEventListener("click", () => showSide(false));
  $("side-open").addEventListener("click", () => showSide(true));
  scrim.addEventListener("click", () => showSide(false));

  // ------------------------------------------------------------- keyboard
  //
  // The bindings other chat apps use, so muscle memory carries over. Chrome
  // keeps Ctrl+Shift+I for its developer tools and a page cannot always take
  // it back, which is why incognito answers to Ctrl+Shift+P as well.
  const sheet = $("keys-sheet"), filter = $("filter");

  function toggleSheet(on) {
    sheet.hidden = on === undefined ? !sheet.hidden : !on;
    if (!sheet.hidden) $("keys-close").focus(); else refocus();
  }

  function toggleFilter(on) {
    filter.hidden = on === undefined ? !filter.hidden : !on;
    showSide(true);
    if (!filter.hidden) { filter.focus(); filter.select(); }
    else { filter.value = ""; applyFilter(); }
  }

  function applyFilter() {
    const q = filter.value.trim().toLowerCase();
    let shown = 0;
    document.querySelectorAll(".conv").forEach((el) => {
      const hit = !q || el.textContent.toLowerCase().includes(q);
      el.style.display = hit ? "" : "none";
      if (hit) shown++;
    });
    const none = convs.querySelector(".no-hits");
    if (!shown && q) {
      if (!none) {
        const p = document.createElement("p");
        p.className = "empty no-hits";
        p.textContent = "Nothing matches.";
        convs.appendChild(p);
      }
    } else if (none) none.remove();
  }
  filter.addEventListener("input", applyFilter);
  filter.addEventListener("keydown", (e) => {
    if (e.key === "Escape") { e.preventDefault(); toggleFilter(false); input.focus(); }
  });

  document.addEventListener("keydown", (e) => {
    const mod = e.ctrlKey || e.metaKey;
    const key = (e.key || "").toLowerCase();

    if (key === "escape") {
      if (!memSheet.hidden) { e.preventDefault(); memToggle(false); return; }
      if (isNarrow() && !side.classList.contains("closed")) { e.preventDefault(); showSide(false); return; }
      if (!sheet.hidden) { e.preventDefault(); toggleSheet(false); return; }
      if (e.shiftKey) { e.preventDefault(); input.focus(); return; }
      if (inflight) { e.preventDefault(); inflight.abort(); return; }
      return;
    }
    if (!mod) return;

    if (e.shiftKey && key === "o") { e.preventDefault(); $("new-chat").click(); input.focus(); return; }
    if (e.shiftKey && (key === "i" || key === "p")) {
      e.preventDefault();
      incognito.checked = !incognito.checked;
      incognito.dispatchEvent(new Event("change"));
      return;
    }
    if (e.shiftKey && key === "s") { e.preventDefault(); showSide(side.classList.contains("closed")); return; }
    if (e.shiftKey && (key === "backspace" || key === "delete")) {
      e.preventDefault();
      if (!convID) return;
      if (!confirm("Delete this conversation?")) return;
      fetch("/api/conversation/" + convID, { method: "DELETE" })
        .then(() => { $("new-chat").click(); refreshConversations(); });
      return;
    }
    if (!e.shiftKey && key === "k") { e.preventDefault(); toggleFilter(); return; }
    if (!e.shiftKey && key === "/") { e.preventDefault(); toggleSheet(); return; }
    if (e.shiftKey && key === "m") { e.preventDefault(); memToggle(); return; }
  });

  $("keys-open").addEventListener("click", () => toggleSheet(true));
  $("keys-close").addEventListener("click", () => toggleSheet(false));
  sheet.addEventListener("click", (e) => { if (e.target === sheet) toggleSheet(false); });

  window.addEventListener("resize", () => {
    sizeSpacer();
    // Rotating a phone or widening a window must not leave a scrim over a
    // sidebar that is now part of the layout again.
    if (!isNarrow()) scrim.hidden = true;
    else scrim.hidden = side.classList.contains("closed");
  });

  window.addEventListener("popstate", () => {
    const m = location.pathname.match(/^\/c\/([\w-]+)/);
    if (m) openConversation(m[1]); else $("new-chat").click();
  });

  // Is the model server up, and can anything be looked up? Asking /api/status
  // never wakes the weights. It runs again after every turn, since a turn is
  // when search discovers it has been rate limited.
  const searchDown = $("search-down");
  async function refreshStatus() {
    try {
      const s = await (await fetch("/api/status")).json();
      if (s.ctx) { ctxSize = s.ctx; meterCap.textContent = kfmt(s.ctx); }
      modelDot.classList.toggle("on", !!s.up);
      modelDot.title = s.up ? "The model answering" : "The model server is not answering";
      const left = s.search_left || {};
      // Down means a host refused us. Spent means we stopped ourselves. They
      // read the same to a reader mid conversation, so the flag covers both and
      // the tooltip says which.
      const spent = left.day <= 0 || left.hour <= 0 || left.minute <= 0;
      searchDown.hidden = !(s.search_down || spent);
      if (s.search_down) {
        searchDown.textContent = "NO SEARCH";
        searchDown.title = "Search refused this address and is being left alone for another " +
          (s.search_back_in || "while") + ". Nothing can be looked up until then.";
      } else if (spent) {
        searchDown.textContent = "SEARCH PAUSED";
        searchDown.title = "The search budget is spent for now, so searching is paused " +
          "rather than pushed. It frees up on its own.";
      } else if (left.day !== undefined) {
        searchDown.title = "";
        // Not a warning until it is low, just something the bar can answer.
        $("model-dot").title = (s.up ? "The model answering" : "The model server is not answering") +
          "\nSearches left today: " + left.day;
      }
    } catch { /* leave the bar as it was */ }
  }
  refreshStatus();

  // ---------------------------------------------------------------- viewport
  //
  // An open keyboard shrinks the visual viewport and leaves the layout one
  // alone, so a shell sized in dvh keeps its composer under the keyboard. The
  // meta tag handles this where interactive-widget is supported and this
  // covers the rest, iOS included.
  const vv = window.visualViewport;
  if (vv) {
    const fit = () => {
      document.documentElement.style.setProperty("--app-h", vv.height + "px");
      // Safari scrolls the window to reveal a focused field even when there is
      // nothing to scroll, which leaves the page sitting above its own top.
      if (window.scrollY !== 0) window.scrollTo(0, 0);
      sizeSpacer();
    };
    vv.addEventListener("resize", fit);
    vv.addEventListener("scroll", fit);
    fit();
  }

  // Dragging a drawer shut is how a phone expects to close one, and the button
  // that opens it is at the far end of the screen from a thumb.
  let swipe = null;
  side.addEventListener("touchstart", (e) => {
    if (!isNarrow() || side.classList.contains("closed") || e.touches.length !== 1) return;
    swipe = { x: e.touches[0].clientX, y: e.touches[0].clientY, live: false };
  }, { passive: true });
  side.addEventListener("touchmove", (e) => {
    if (!swipe) return;
    const dx = e.touches[0].clientX - swipe.x, dy = e.touches[0].clientY - swipe.y;
    // Only once the gesture is clearly sideways, or every scroll of the list
    // drags the drawer with it.
    if (!swipe.live) {
      if (Math.abs(dx) < 12 || Math.abs(dx) < Math.abs(dy)) return;
      swipe.live = true;
      side.style.transition = "none";
    }
    side.style.transform = "translateX(" + Math.min(0, dx) + "px)";
  }, { passive: true });
  const swipeEnd = (e) => {
    if (!swipe) return;
    const moved = swipe.live ? e.changedTouches[0].clientX - swipe.x : 0;
    swipe = null;
    side.style.transition = "";
    side.style.transform = "";
    if (moved < -side.offsetWidth / 3) showSide(false);
  };
  side.addEventListener("touchend", swipeEnd, { passive: true });
  side.addEventListener("touchcancel", swipeEnd, { passive: true });

  // Before anything else paints. The stylesheet makes the sidebar an overlay
  // under 52em, and open is the wrong default for an overlay: it covers the
  // conversation on every phone.
  if (isNarrow()) showSide(false);

  const first = location.pathname.match(/^\/c\/([\w-]+)/);
  if (first) openConversation(first[1]);
  refocus();
})();

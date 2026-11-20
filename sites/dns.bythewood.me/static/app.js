// dns's own page behaviour. Every form posts without it, the live log, copy
// buttons and the setup check just don't run.
const $ = (sel, root = document) => root.querySelector(sel);

// A form that ends something asks first, in the words on data-confirm.
for (const f of document.querySelectorAll("form[data-confirm]")) {
  f.addEventListener("submit", (e) => {
    if (!confirm(f.dataset.confirm)) e.preventDefault();
  });
}

// A select marked data-autosubmit sends its form on change, so the button
// beside it is only for when this script didn't load.
for (const sel of document.querySelectorAll("select[data-autosubmit]")) {
  sel.addEventListener("change", () => sel.form.requestSubmit());
}
for (const b of document.querySelectorAll("[data-nojs]")) b.hidden = true;

const toast = (text, kind = "") => document.dispatchEvent(new CustomEvent("o-toast", { detail: { text, kind, ms: 2500 } }));

document.addEventListener("click", async (e) => {
  const b = e.target.closest("[data-copy]");
  if (!b) return;
  try {
    await navigator.clipboard.writeText(b.dataset.copy);
    toast("Copied");
  } catch {
    toast("Couldn't copy, select it instead", "warn");
  }
});

// The log follows new lookups while it's on the first page, unless paused or
// the tab is hidden. Rows come rendered from the server so they match.
const tb = $("table[data-live]");
if (tb && tb.dataset.live) {
  let newest = Number(tb.dataset.newest) || 0;
  let on = true;
  const btn = $("[data-live-toggle]");
  const state = $("[data-live-state]");
  btn?.addEventListener("click", () => {
    on = !on;
    btn.textContent = on ? "Pause" : "Resume";
    if (state) {
      state.textContent = on ? "on" : "paused";
      state.className = on ? "ok" : "";
    }
  });
  const body = tb.tBodies[0];
  // A row picked with j and k or opened holds the tail, since new rows on top
  // would move it out from under the keys.
  const busy = () => body.querySelector("tr.o-drawer:not([hidden]), tr.o-cur");
  const tick = async () => {
    if (!on || document.hidden || busy()) {
      if (on && state) state.textContent = busy() ? "held" : "on";
      return;
    }
    if (state && on) state.textContent = "on";
    try {
      const res = await fetch(`${tb.dataset.live}&after=${newest}`, { credentials: "same-origin" });
      if (res.status !== 200) return;
      const n = Number(res.headers.get("X-Newest"));
      if (!n) return;
      newest = n;
      body.querySelector("tr.none")?.remove();
      body.insertAdjacentHTML("afterbegin", await res.text());
      while (body.rows.length > 600) body.deleteRow(-1);
    } catch {}
  };
  setInterval(tick, 3000);
}

// The setup check looks up a one time name under the probe zone. Only this
// resolver answers it, so seeing it here means the browser asked here.
const probe = $("[data-probe]");
if (probe) {
  const out = $("[data-probe-out]", probe);
  const go = $("[data-probe-go]", probe);
  const say = (text, cls = "small") => {
    out.textContent = text;
    out.className = cls;
  };
  go.addEventListener("click", async () => {
    const nonce = Array.from(crypto.getRandomValues(new Uint8Array(8)), (b) => b.toString(16).padStart(2, "0")).join("");
    go.disabled = true;
    say("Checking…");
    fetch(`https://${nonce}.${probe.dataset.zone}/`, { mode: "no-cors", cache: "no-store" }).catch(() => {});
    try {
      for (let i = 0; i < 8; i++) {
        await new Promise((r) => setTimeout(r, 750));
        const res = await fetch(`/probe/${nonce}`, { credentials: "same-origin" });
        if (!res.ok) throw new Error(`answered ${res.status}`);
        const j = await res.json();
        if (j.seen) return say(`Yes, this browser asked here as ${j.device} over ${j.proto}.`, "small o-ok");
      }
      say("No, this browser asked somewhere else. Set it up above, then check again.", "small o-bad");
    } catch (e) {
      say(`Couldn't finish the check (${e.message}), try again.`, "small o-bad");
    } finally {
      go.disabled = false;
    }
  });
}

// dns's own page behaviour. Every form posts without it, the live log, copy
// buttons and the setup check just don't run.
const $ = (sel, root = document) => root.querySelector(sel);

// A form that ends something asks first, in the words on data-confirm.
for (const f of document.querySelectorAll("form[data-confirm]")) {
  f.addEventListener("submit", (e) => {
    if (!confirm(f.dataset.confirm)) e.preventDefault();
  });
}

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
  const tick = async () => {
    if (!on || document.hidden || body.querySelector("tr.o-drawer.on, tr.on")) return;
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
  $("[data-probe-go]", probe).addEventListener("click", async () => {
    const nonce = Array.from(crypto.getRandomValues(new Uint8Array(8)), (b) => b.toString(16).padStart(2, "0")).join("");
    out.textContent = "Checking…";
    fetch(`https://${nonce}.${probe.dataset.zone}/`, { mode: "no-cors", cache: "no-store" }).catch(() => {});
    for (let i = 0; i < 8; i++) {
      await new Promise((r) => setTimeout(r, 750));
      const res = await fetch(`/probe/${nonce}`, { credentials: "same-origin" });
      const j = await res.json();
      if (j.seen) {
        out.innerHTML = "";
        out.append(`Yes, this browser asked here as ${j.device} over ${j.proto}.`);
        out.className = "small o-ok";
        return;
      }
    }
    out.textContent = "No, this browser asked somewhere else. Set it up above, then check again.";
    out.className = "small o-bad";
  });
}

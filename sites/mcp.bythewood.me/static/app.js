const toast = (text, kind) => document.dispatchEvent(new CustomEvent("o-toast", { detail: { text, kind } }));

async function copy(text, done) {
  try {
    await navigator.clipboard.writeText(text);
    toast(done);
  } catch {
    toast("The clipboard said no", "bad");
  }
}

// A call's drawer shows the call formatted or as the JSON-RPC that went over the wire.
document.addEventListener("change", (e) => {
  const r = e.target.closest('.o-drawer input[type="radio"][name^="view-"]');
  if (!r) return;
  const d = r.closest(".o-drawer");
  for (const el of d.querySelectorAll(".view-formatted")) el.hidden = r.value !== "formatted";
  for (const el of d.querySelectorAll(".view-raw")) el.hidden = r.value !== "raw";
});

document.addEventListener("click", (e) => {
  const b = e.target.closest("[data-copy-call]");
  if (b) {
    const d = b.closest("tr.o-drawer");
    const row = d.previousElementSibling;
    const part = (p) => d.querySelector(`[data-part="${p}"]`)?.textContent.trim() ?? "";
    if (d.querySelector('input[value="raw"]').checked) {
      copy(`// request\n${part("request")}\n\n// response\n${part("response")}\n`, "Copied the request and response");
      return;
    }
    const cell = (n) => row.cells[n]?.textContent.trim() ?? "";
    const args = cell(2) === "·" ? "no arguments" : cell(2);
    copy(`**${cell(1)}** ${args}, ${cell(5)}, ${cell(6)} at ${row.cells[0].dataset.tip}\n\n${part("text")}\n`, "Copied the call as markdown");
    return;
  }
  const c = e.target.closest("[data-copy]");
  if (c) copy(c.parentElement.querySelector("[data-copy-src]").textContent.trim(), "Copied");
});

// A link to one call, or a page holding only one, opens its drawer.
const rows = document.querySelectorAll("table[data-keys] tbody tr[id]");
const target = (location.hash && document.getElementById(location.hash.slice(1))) || (rows.length === 1 ? rows[0] : null);
if (target?.nextElementSibling?.classList.contains("o-drawer")) {
  target.click();
  target.scrollIntoView({ block: "nearest" });
}

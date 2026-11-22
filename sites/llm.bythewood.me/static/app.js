// A form that ends something asks first, in the words on data-confirm.
for (const f of document.querySelectorAll("form[data-confirm]")) {
  f.addEventListener("submit", (e) => {
    if (!confirm(f.dataset.confirm)) e.preventDefault();
  });
}

// A link to one call, or a page holding only one, opens its drawer.
const rows = document.querySelectorAll("table[data-keys] tbody tr[id]");
const target = (location.hash && document.getElementById(location.hash.slice(1))) || (rows.length === 1 ? rows[0] : null);
if (target?.nextElementSibling?.classList.contains("o-drawer")) {
  target.click();
  target.scrollIntoView({ block: "nearest" });
}

// The make a key action lands on the name box ready to type.
if (location.hash === "#new") document.getElementById("new")?.focus();

// Copy as markdown takes the call's row and its drawer as they read.
document.addEventListener("click", async (e) => {
  const b = e.target.closest("[data-copy-call]");
  if (!b) return;
  const d = b.closest("tr.o-drawer");
  const row = d?.previousElementSibling;
  if (!row) return;
  const cell = (n) => row.cells[n]?.textContent.trim() ?? "";
  const part = (p) => d.querySelector(`[data-part="${p}"]`)?.textContent.trim();
  let md = `**${cell(1)}** at ${cell(0)}, ${cell(3)} tokens, ${cell(5)}, ${cell(6)}\n\n## Asked\n\n${part("asked") || ""}\n`;
  if (part("error")) md += `\n## Error\n\n${part("error")}\n`;
  md += `\n## Reply\n\n${part("reply") || ""}\n`;
  try {
    await navigator.clipboard.writeText(md);
    document.dispatchEvent(new CustomEvent("o-toast", { detail: { text: "Copied the call as markdown" } }));
  } catch {
    document.dispatchEvent(new CustomEvent("o-toast", { detail: { text: "The clipboard said no", kind: "bad" } }));
  }
});

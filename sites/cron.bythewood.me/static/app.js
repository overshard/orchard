// Shows the field for the chosen schedule and previews when it would fire. The
// form works without this, both fields are just visible.
const form = document.querySelector("form.job");

if (form) {
  const preview = form.querySelector("[data-preview]");
  const pick = () => form.querySelector('input[name="schedule"]:checked')?.value || "once";
  let timer;

  const show = async () => {
    const mode = pick();
    for (const el of form.querySelectorAll("[data-for]")) el.hidden = el.dataset.for !== mode;
    const value = form.elements[mode === "repeat" ? "cron" : "at"].value.trim();
    if (!value) {
      preview.textContent = "";
      return;
    }
    const q = new URLSearchParams({ [mode === "repeat" ? "cron" : "at"]: value });
    try {
      const res = await fetch("/api/preview?" + q, { credentials: "same-origin" });
      const body = await res.json();
      preview.classList.toggle("bad", !res.ok);
      preview.textContent = res.ok ? "Fires " + body.next.map((n) => n.text).join(", then ") : body.error;
    } catch {
      preview.textContent = "";
    }
  };

  form.addEventListener("input", () => {
    clearTimeout(timer);
    timer = setTimeout(show, 250);
  });
  form.addEventListener("change", show);
  show();
}

for (const f of document.querySelectorAll("form[data-confirm]")) {
  f.addEventListener("submit", (e) => {
    if (!confirm(f.dataset.confirm)) e.preventDefault();
  });
}

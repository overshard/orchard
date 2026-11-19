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

// The kind decides what the message box is for, so its label and the hint
// under the kind follow the choice.
if (form) {
  const hint = form.querySelector("[data-kind-hint]");
  const label = form.elements.message?.closest("label")?.querySelector("span");
  const words = {
    notify: ["Sends the message as written.", "Message"],
    prompt: ["The model looks it up with web search and only pushes when the answer is worth it.", "What to look up"],
    quote: ["Pushes the first trade of the ticker, then the change at most every five minutes.", "Ticker"],
  };
  form.addEventListener("change", (e) => {
    if (e.target.name !== "kind" || !words[e.target.value]) return;
    [hint.textContent, label.textContent] = words[e.target.value];
  });
}

for (const f of document.querySelectorAll("form[data-confirm]")) {
  f.addEventListener("submit", (e) => {
    if (!confirm(f.dataset.confirm)) e.preventDefault();
  });
}

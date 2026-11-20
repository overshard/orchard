// The account page's tool cards take a dot and a word from the same merged
// state the header's tabs read, the last copy first so nothing blinks in.
export const toolCards = () => {
  const cards = document.querySelectorAll(".tool[data-tool]");
  if (!cards.length) return;

  const paint = (data) => {
    for (const card of cards) {
      const st = data?.tools?.find((t) => t.tool === card.dataset.tool);
      const slot = card.querySelector(".tool-state");
      if (!st || !slot) continue;
      const level = st.level || "";
      slot.className = `tool-state ${level}`;
      slot.querySelector("b").textContent = st.note || level || "no answer";
      slot.hidden = false;
      card.classList.toggle("is-warn", level === "warn");
      card.classList.toggle("is-bad", level === "bad");
    }
  };

  try {
    const kept = sessionStorage.getItem("orchard-index");
    if (kept) paint(JSON.parse(kept));
  } catch {}
  fetch("/suite/index.json", { credentials: "same-origin" })
    .then((r) => (r.ok ? r.json() : null))
    .then((d) => d && paint(d))
    .catch(() => {});
};

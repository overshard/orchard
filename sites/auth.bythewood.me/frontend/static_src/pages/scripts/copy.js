// The copy button on the recovery codes page. They are shown once, so the
// difference between copying them and retyping twelve characters ten times is
// the difference between saving them and not bothering.
export function copyButtons() {
  for (const button of document.querySelectorAll("[data-copy]")) {
    button.addEventListener("click", async () => {
      const target = document.querySelector(button.dataset.copy);
      if (!target) return;

      const text = target.innerText.trim();
      try {
        await navigator.clipboard.writeText(text);
      } catch {
        // Denied, or an insecure context. Selecting the text is still better
        // than a button that silently does nothing.
        const range = document.createRange();
        range.selectNodeContents(target);
        const sel = window.getSelection();
        sel.removeAllRanges();
        sel.addRange(range);
        return;
      }

      const was = button.textContent;
      button.textContent = "Copied";
      button.classList.add("is-done");
      setTimeout(() => {
        button.textContent = was;
        button.classList.remove("is-done");
      }, 1600);
    });
  }
}

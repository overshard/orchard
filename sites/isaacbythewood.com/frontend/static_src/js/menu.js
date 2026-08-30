// The full-screen menu overlay. Nothing mounts or unmounts, so it is one
// "is-open" class and the CSS holds the rest, including a delayed z-index
// transition that keeps the overlay above the page for the whole slide-out.

export const initMenu = () => {
  const button = document.querySelector(".menu-hamburger");
  const overlay = document.querySelector(".menu-overlay");
  if (!button || !overlay) return;

  const image = overlay.querySelector(".menu-overlayGridRight img[data-src]");

  const setOpen = (open) => {
    if (open && image && !image.src) {
      // srcset first, or the browser starts the fallback download before it
      // has any candidates to choose from.
      if (image.dataset.srcset) image.srcset = image.dataset.srcset;
      image.src = image.dataset.src;
    }
    overlay.classList.toggle("is-open", open);
    // The overlay is hidden by z-index alone, so without inert its links stay
    // in the tab order while it is closed.
    overlay.toggleAttribute("inert", !open);
    button.setAttribute("aria-expanded", open ? "true" : "false");
    // Cleared rather than set to "scroll", which would override the stylesheet
    // and force a scrollbar gutter on short pages.
    document.body.style.overflowY = open ? "hidden" : "";
  };

  button.addEventListener("click", () => {
    setOpen(!overlay.classList.contains("is-open"));
  });

  // Links navigate away, so they need no close handler. Escape is the only
  // other way out.
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && overlay.classList.contains("is-open")) {
      setOpen(false);
    }
  });
};

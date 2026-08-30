// The full-screen menu overlay.
//
// react-transition-group needed six classes to express this because it mounted
// and unmounted the subtree around a timer. Nothing mounts or unmounts here,
// so it is one "is-open" class and the CSS holds the rest, including keeping
// the overlay above the page for the full slide-out via a delayed z-index
// transition.

export const initMenu = () => {
  const button = document.querySelector(".menu-hamburger");
  const overlay = document.querySelector(".menu-overlay");
  if (!button || !overlay) return;

  const image = overlay.querySelector(".menu-overlayGridRight img[data-src]");

  const setOpen = (open) => {
    // First open pays for the panel image; nobody else does.
    if (open && image && !image.src) {
      // srcset first: setting src alone would let the browser start the
      // fallback download before it has any candidates to choose from.
      if (image.dataset.srcset) image.srcset = image.dataset.srcset;
      image.src = image.dataset.src;
    }
    overlay.classList.toggle("is-open", open);
    // Paired with the inert attribute in the template. The overlay is hidden by
    // z-index alone, so without this its nine links stay in the tab order while
    // it is closed.
    overlay.toggleAttribute("inert", !open);
    button.setAttribute("aria-expanded", open ? "true" : "false");
    // Cleared rather than set to "scroll", which would clobber whatever the
    // stylesheet asks for and force a scrollbar gutter on short pages.
    document.body.style.overflowY = open ? "hidden" : "";
  };

  button.addEventListener("click", () => {
    setOpen(!overlay.classList.contains("is-open"));
  });

  // Links navigate away, so there is no close handler on them: the next
  // document arrives with the menu shut. Escape is the only other way out,
  // and it did not exist in the React version.
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && overlay.classList.contains("is-open")) {
      setOpen(false);
    }
  });
};

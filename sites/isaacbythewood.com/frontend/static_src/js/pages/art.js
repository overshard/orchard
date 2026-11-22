// The art page: a lightbox over the acrylic pours, and three generative canvas
// pieces that play one at a time.

import { constellations } from "../canvas/constellations.js";
import { retrostars } from "../canvas/retrostars.js";
import { slimemold } from "../canvas/slimemold.js";

const FACTORIES = {
  constellations: (cvs) => constellations(cvs, { numStars: 50 }),
  retrostars: (cvs) => retrostars(cvs, { numStars: 50 }),
  slimemold: (cvs) => slimemold(cvs, {}),
};

const initCanvases = () => {
  const running = new Map();
  let active = null;

  const instanceFor = (name, cvs) => {
    // Built on first play, since slime mold alone allocates two Float32Arrays
    // over the whole grid plus 6000 agents.
    if (!running.has(name)) running.set(name, FACTORIES[name](cvs));
    return running.get(name);
  };

  const setActive = (name) => {
    if (active && active !== name) {
      const previous = running.get(active);
      if (previous) previous.stop();
      const button = document.querySelector(`[data-art-toggle="${active}"]`);
      if (button) {
        button.dataset.active = "false";
        button.textContent = "▶";
      }
    }
    active = name;
  };

  document.querySelectorAll("[data-art-toggle]").forEach((button) => {
    const name = button.dataset.artToggle;
    const cvs = document.querySelector(`[data-art-canvas="${name}"]`);
    if (!cvs || !FACTORIES[name]) return;

    const play = () => {
      setActive(name);
      instanceFor(name, cvs).start();
      button.dataset.active = "true";
      button.textContent = "⏸";
    };

    const pause = () => {
      const instance = running.get(name);
      if (instance) instance.stop();
      active = null;
      button.dataset.active = "false";
      button.textContent = "▶";
    };

    button.addEventListener("click", () => {
      if (button.dataset.active === "true") pause();
      else play();
    });

    if (button.dataset.autoplay === "true") play();
  });
};

// The overlay is a <dialog> opened with showModal, so the browser moves focus
// in, closes it on Escape and hands focus back to the card that opened it.
const initLightbox = () => {
  const overlay = document.querySelector(".art-lightboxOverlay");
  if (!overlay || typeof overlay.showModal !== "function") return;

  const image = overlay.querySelector(".art-lightboxImage");
  const loading = overlay.querySelector(".art-lightboxLoading");
  const close = overlay.querySelector(".art-lightboxClose");

  const revealed = () => {
    image.classList.add("art-show");
    loading.classList.add("art-hide");
  };

  const show = (src, alt) => {
    image.classList.remove("art-show");
    loading.classList.remove("art-hide");
    image.alt = alt || "";
    image.src = src;
    overlay.setAttribute("aria-label", alt || "Artwork");
    overlay.showModal();
    document.body.style.overflowY = "hidden";

    // A cached image can be complete before the load listener attaches, and
    // then the event never fires and the spinner sits over a decoded picture.
    if (image.complete && image.naturalWidth > 0) revealed();
  };

  image.addEventListener("load", revealed);
  image.addEventListener("error", revealed);

  document.querySelectorAll("[data-lightbox]").forEach((item) => {
    item.addEventListener("click", (e) => {
      // A modified click still opens the file in a new tab.
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
      e.preventDefault();
      show(item.getAttribute("href"), item.dataset.lightboxAlt);
    });
  });

  overlay.addEventListener("close", () => {
    image.classList.remove("art-show");
    loading.classList.remove("art-hide");
    image.removeAttribute("src");
    document.body.style.overflowY = "scroll";
  });

  // A click on the dimmed area lands on the dialog itself, outside the image.
  overlay.addEventListener("click", (e) => {
    if (e.target === overlay) overlay.close();
  });
  close.addEventListener("click", () => overlay.close());
};

const initCardFades = () => {
  // Anything already cached is marked at once, or it sits invisible waiting for
  // a load event that will not fire.
  document.querySelectorAll(".art-cardImage img").forEach((img) => {
    if (img.complete && img.naturalWidth > 0) {
      img.classList.add("art-imgLoaded");
      return;
    }
    img.addEventListener("load", () => img.classList.add("art-imgLoaded"), {
      once: true,
    });
    img.addEventListener("error", () => img.classList.add("art-imgLoaded"), {
      once: true,
    });
  });
};

export const initArt = () => {
  initCardFades();
  initLightbox();
  initCanvases();
};

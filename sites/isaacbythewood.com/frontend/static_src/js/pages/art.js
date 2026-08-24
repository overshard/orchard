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
    // Built on first play rather than on load. Slime mold alone allocates two
    // Float32Arrays over the whole grid and 6000 agents, and a visitor who
    // never presses play should not pay for that.
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

    // Constellations runs on arrival, matching the old default state.
    if (button.dataset.autoplay === "true") play();
  });
};

const initLightbox = () => {
  const overlay = document.querySelector(".art-lightboxOverlay");
  if (!overlay) return;

  const image = overlay.querySelector(".art-lightboxImage");
  const loading = overlay.querySelector(".art-lightboxLoading");
  const close = overlay.querySelector(".art-lightboxClose");

  const hide = () => {
    overlay.hidden = true;
    image.classList.remove("art-show");
    loading.classList.remove("art-hide");
    image.removeAttribute("src");
    document.body.style.overflowY = "scroll";
  };

  const show = (src, alt) => {
    image.classList.remove("art-show");
    loading.classList.remove("art-hide");
    image.alt = alt || "";
    image.src = src;
    overlay.hidden = false;
    document.body.style.overflowY = "hidden";

    // A cached image can already be complete before the load listener attaches,
    // in which case the event never fires and the spinner would sit there over
    // a fully decoded picture.
    if (image.complete && image.naturalWidth > 0) revealed();
  };

  const revealed = () => {
    image.classList.add("art-show");
    loading.classList.add("art-hide");
  };

  image.addEventListener("load", revealed);
  image.addEventListener("error", revealed);

  document.querySelectorAll("[data-lightbox]").forEach((item) => {
    item.addEventListener("click", () => {
      show(item.dataset.lightbox, item.dataset.lightboxAlt);
    });
  });

  overlay.addEventListener("click", hide);
  close.addEventListener("click", (e) => {
    e.stopPropagation();
    hide();
  });
  overlay
    .querySelector(".art-lightboxImageWrapper")
    .addEventListener("click", (e) => e.stopPropagation());

  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && !overlay.hidden) hide();
  });
};

const initCardFades = () => {
  // The cards fade in as their image decodes. Anything already cached is
  // marked immediately so it does not sit invisible.
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

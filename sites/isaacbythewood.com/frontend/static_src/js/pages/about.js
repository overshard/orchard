// The about page: nine skill words stacked down the left, each pinned to a
// vertical slot. Every four seconds two of them trade places, wiping out and
// back in rather than sliding, so the column never looks like it is shuffling.

import { enter, exit } from "../transition.js";

const SWAP_MS = 4000;
const TRANSITION_MS = 1000;
const SLOT_HEIGHT_VH = 11;

const CLASSES = {
  enter: {
    from: "about-wordEnter",
    active: "about-wordEnterActive",
    done: "about-wordEnterDone",
  },
  exit: {
    from: "about-wordExit",
    active: "about-wordExitActive",
    clear: ["about-wordEnterDone", "about-wordAppearDone"],
  },
};

export const initAbout = () => {
  const container = document.querySelector(".about-words");
  if (!container) return;

  const words = Array.from(container.querySelectorAll(".about-word"));
  if (words.length < 2) return;

  // Slot index is the word's position down the column. Go renders the initial
  // slots as inline styles so the column is laid out correctly before any JS
  // runs; from here the two are kept in sync.
  const slots = new Map(words.map((el, i) => [el, i]));

  const place = (el) => {
    el.style.top = `${slots.get(el) * SLOT_HEIGHT_VH}vh`;
  };

  words.forEach((el) => el.classList.add("about-wordAppearDone"));

  window.setInterval(() => {
    let a = Math.floor(Math.random() * words.length);
    let b = Math.floor(Math.random() * words.length);
    while (a === b) b = Math.floor(Math.random() * words.length);

    const first = words[a];
    const second = words[b];

    const swap = () => {
      const slotA = slots.get(first);
      slots.set(first, slots.get(second));
      slots.set(second, slotA);
      place(first);
      place(second);
    };

    let done = 0;
    const afterExit = () => {
      done += 1;
      // Both have to be gone before either moves, otherwise the one that
      // finishes first jumps while the other is still visible in its old slot.
      if (done < 2) return;
      swap();
      enter(first, CLASSES.enter, TRANSITION_MS);
      enter(second, CLASSES.enter, TRANSITION_MS);
    };

    exit(first, CLASSES.exit, TRANSITION_MS, afterExit);
    exit(second, CLASSES.exit, TRANSITION_MS, afterExit);
  }, SWAP_MS);
};

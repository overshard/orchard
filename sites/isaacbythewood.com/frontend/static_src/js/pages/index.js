// The home page: one huge washed-out word behind the hero, cycling every two
// seconds. Each change is an old word falling out downward while the new one
// drops in from above.

import { enter, exit } from "../transition.js";

const WORDS = ["AI Agents", "Automation", "DevOps", "Architecture"];
const CYCLE_MS = 2000;
const TRANSITION_MS = 1000;

const CLASSES = {
  appear: {
    from: "index-wordAppear",
    active: "index-wordAppearActive",
    done: "index-wordAppearDone",
  },
  enter: {
    from: "index-wordEnter",
    active: "index-wordEnterActive",
    done: "index-wordEnterDone",
  },
  exit: {
    from: "index-wordExit",
    active: "index-wordExitActive",
    clear: ["index-wordEnterDone", "index-wordAppearDone"],
  },
};

export const initHome = () => {
  const container = document.querySelector(".index-words");
  const hero = document.querySelector(".index-hero");
  const image = document.querySelector(".index-imageWrapper img");

  // The hero animation is held until the image is there, so the text does not
  // fade up over an empty rectangle. Both paths release the loader, including
  // the failure path: a broken image must not strand the page behind it.
  if (image) {
    const ready = () => {
      if (hero) hero.style.animationPlayState = "running";
      window.dispatchEvent(new Event("loaderReady"));
    };
    if (image.complete) ready();
    else {
      image.addEventListener("load", ready, { once: true });
      image.addEventListener("error", ready, { once: true });
    }
  }

  if (!container) return;

  const makeWord = (text) => {
    const el = document.createElement("h3");
    el.className = "index-word";
    el.textContent = text;
    return el;
  };

  let index = 0;
  let current = makeWord(WORDS[index]);
  container.appendChild(current);
  enter(current, CLASSES.appear, TRANSITION_MS);

  window.setInterval(() => {
    const outgoing = current;
    index = (index + 1) % WORDS.length;

    const incoming = makeWord(WORDS[index]);
    container.appendChild(incoming);
    current = incoming;
    enter(incoming, CLASSES.enter, TRANSITION_MS);

    exit(outgoing, CLASSES.exit, TRANSITION_MS, (el) => el.remove());
  }, CYCLE_MS);
};

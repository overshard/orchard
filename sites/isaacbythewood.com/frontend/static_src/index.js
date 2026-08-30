// Entry point. Vite bundles this into one hashed .js and one hashed .css, and
// the Go server resolves both names out of dist/.vite/manifest.json.
//
// There is no router and no hydration. Each page is a real document served by
// Go, so this file's only job is to attach the behaviour that page needs,
// chosen by the data-page attribute Go writes onto <body>.

import "./css/globals.css";
import "./css/components/page.css";
import "./css/components/grid.css";
import "./css/components/loader.css";
import "./css/components/cursor.css";
import "./css/components/menu.css";
import "./css/components/sidebar.css";
import "./css/components/canvas.css";
import "./css/pages/index.css";
import "./css/pages/about.css";
import "./css/pages/code.css";
import "./css/pages/art.css";
import "./css/pages/contact.css";

import { initCursor } from "./js/cursor.js";
import { initLoader } from "./js/loader.js";
import { initMenu } from "./js/menu.js";

import { initHome } from "./js/pages/index.js";
import { initAbout } from "./js/pages/about.js";
import { initArt } from "./js/pages/art.js";
import { initContact } from "./js/pages/contact.js";

const PAGES = {
  index: initHome,
  about: initAbout,
  art: initArt,
  contact: initContact,
};

// Each init is isolated, because they are independent and a throw in one used
// to take the rest with it. initLoader in particular is what lifts the opening
// curtain, so a failure in initCursor above it left the home page as an opaque
// full-screen sheet with no content and no recovery.
const run = (name, fn) => {
  try {
    fn();
  } catch (err) {
    console.error(name + " failed", err);
  }
};

const start = () => {
  // Loader first: it is the one whose failure is visible as a blank page, so it
  // should not be downstream of anything else.
  run("loader", initLoader);
  run("cursor", initCursor);
  run("menu", initMenu);

  const page = document.body.dataset.page;
  if (PAGES[page]) run(page, PAGES[page]);
};

// The bundle is a module, so it is deferred and the DOM is already parsed by
// the time it runs. The readyState check is only for the dev-server case where
// it can be injected earlier.
if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", start);
} else {
  start();
}

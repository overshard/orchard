// The six columns that fold up off the screen on load.
//
// It plays once per visit, not once per page. The rewrite briefly got this
// wrong in the other direction: under Next.js the loader mounted once for the
// whole session, and with real navigation it started running on every document,
// which looked deliberate but is not what it is for. It is cover for a cold
// first paint. Once the assets are cached there is nothing left to cover, so
// the second page onward gets the white fade in globals.css instead.
//
// On the home page it waits for the hero image, because folding away to reveal
// a blank rectangle is worse than a slightly longer hold. Everywhere else the
// CSS has already started the animation and this file has nothing to do.

export const initLoader = () => {
  // A returning visitor has no curtain to release, so there is nothing here to
  // arm. The home page still gates its hero on the image either way; that
  // lives in pages/index.js and does not depend on this.
  if (document.documentElement.dataset.returning) return;

  const loader = document.querySelector(".loader");
  if (!loader || loader.dataset.wait !== "true") return;

  // One attribute drives both the paused columns and the visible dots, so
  // releasing is a single removal rather than a walk over six elements.
  const release = () => loader.removeAttribute("data-wait");

  // The home page fires this once its hero has loaded, or failed to: a broken
  // image must still release the overlay or the page stays behind it forever.
  window.addEventListener("loaderReady", release, { once: true });

  // Belt and braces. If the hero somehow fires neither event, do not strand
  // the visitor behind a curtain.
  window.setTimeout(release, 5000);
};

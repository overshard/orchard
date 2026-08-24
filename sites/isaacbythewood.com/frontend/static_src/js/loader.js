// The six columns that fold up off the screen on load.
//
// This got better in the rewrite rather than worse. Under Next.js the loader
// mounted once for the whole session, so it played on first paint and never
// again; every later route change was a bare cross-fade. With real navigation
// it runs on every page, which is what it always looked like it was meant to
// do.
//
// On the home page it waits for the hero image, because folding away to reveal
// a blank rectangle is worse than a slightly longer hold. Everywhere else the
// CSS has already started the animation and this file has nothing to do.

export const initLoader = () => {
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

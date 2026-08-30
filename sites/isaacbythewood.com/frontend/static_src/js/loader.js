// The six columns that fold up off the screen on load. It plays once per build
// rather than once per page: the head script in base.html owns that decision and
// keys it on the bundle hash, so a deploy is a cold load and gets the opening.

export const initLoader = () => {
  if (document.documentElement.dataset.returning) return;

  const loader = document.querySelector(".loader");
  if (!loader || loader.dataset.wait !== "true") return;

  // The one attribute drives both the paused columns and the dots in the CSS.
  const release = () => loader.removeAttribute("data-wait");

  // The home page fires this once its hero has loaded or failed to. A broken
  // image still has to release the overlay or the page stays behind it.
  window.addEventListener("loaderReady", release, { once: true });

  // Belt and braces, in case the hero fires neither event.
  window.setTimeout(release, 5000);
};

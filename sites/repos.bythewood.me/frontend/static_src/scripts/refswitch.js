// The ref switcher is a <details>, so it opens and closes with no JavaScript at
// all. This adds the one behaviour markup cannot: closing it when a click lands
// outside, which is what every other dropdown on the web does and what its
// absence makes feel broken.
document.addEventListener("click", (event) => {
  document.querySelectorAll("details.refswitch[open]").forEach((el) => {
    if (!el.contains(event.target)) el.open = false;
  });
});

// Escape closes it too, for the same reason.
document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  document.querySelectorAll("details.refswitch[open]").forEach((el) => {
    el.open = false;
  });
});

// The ref switcher is a <details> and opens and closes without JavaScript. All
// this adds is the one thing markup cannot, closing on a click outside it.
document.addEventListener("click", (event) => {
  document.querySelectorAll("details.refswitch[open]").forEach((el) => {
    if (!el.contains(event.target)) el.open = false;
  });
});

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  document.querySelectorAll("details.refswitch[open]").forEach((el) => {
    el.open = false;
  });
});

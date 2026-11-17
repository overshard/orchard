// A form with data-confirm asks before it submits, without an inline handler.
document.addEventListener("submit", (e) => {
  const msg = e.target.dataset && e.target.dataset.confirm;
  if (msg && !window.confirm(msg)) {
    e.preventDefault();
  }
});

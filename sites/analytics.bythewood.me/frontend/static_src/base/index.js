import "./scripts/guest.js";

import "./styles/analytics.scss";

// A form that ends something asks first, in words that say what it ends.
document.addEventListener("submit", (e) => {
  const msg = e.target.dataset && e.target.dataset.confirm;
  if (msg && !window.confirm(msg)) e.preventDefault();
});

// One menu open at a time, and a click outside closes it.
document.addEventListener("click", (e) => {
  for (const d of document.querySelectorAll("details.menu[open]")) {
    if (!d.contains(e.target)) d.open = false;
  }
});

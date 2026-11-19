import { copyButtons } from "./scripts/copy.js";
import { otp } from "./scripts/otp.js";

import "./styles/pages.scss";

copyButtons();
otp();

// A form that ends something asks first, in words that say what it ends.
for (const f of document.querySelectorAll("form[data-confirm]")) {
  f.addEventListener("submit", (e) => {
    if (!confirm(f.dataset.confirm)) e.preventDefault();
  });
}

// pass's own script. A form that ends something asks first, in the words on
// data-confirm.
for (const f of document.querySelectorAll("form[data-confirm]")) {
  f.addEventListener("submit", (e) => {
    if (!confirm(f.dataset.confirm)) e.preventDefault();
  });
}

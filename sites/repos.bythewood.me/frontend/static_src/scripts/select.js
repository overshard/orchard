// A field with data-select selects its whole value on click, for copying.
document.addEventListener("click", (e) => {
  if (e.target.matches && e.target.matches("input[data-select]")) {
    e.target.select();
  }
});

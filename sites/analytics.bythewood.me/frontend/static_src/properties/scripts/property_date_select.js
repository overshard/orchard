// The custom window submits on Show, with the dates swapped if they're the
// wrong way round.
document.addEventListener("DOMContentLoaded", function () {
  const form = document.getElementById("custom-dates");
  if (!form) return;
  form.addEventListener("submit", function () {
    const a = form.querySelector("#date-start");
    const b = form.querySelector("#date-end");
    if (a.value && b.value && a.value > b.value) [a.value, b.value] = [b.value, a.value];
  });
});

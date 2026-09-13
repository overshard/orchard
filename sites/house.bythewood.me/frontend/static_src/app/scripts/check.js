// The address form. Geocoding happens before the redirect and it is paced, so the
// browser can sit for a few seconds with nothing to show for it, and a form whose
// button still looks live gets pressed twice.

const form = document.querySelector('form[action="/check"]');

if (form) {
  form.addEventListener("submit", () => {
    const button = form.querySelector('button[type="submit"]');
    if (!button) return;
    button.disabled = true;
    button.textContent = "Looking it up";
  });
}

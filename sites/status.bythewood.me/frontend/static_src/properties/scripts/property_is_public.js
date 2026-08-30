// Reloads after the post, so the rest of the dashboard re-renders against the
// new visibility.
document.addEventListener("DOMContentLoaded", function () {
  const wrap = document.getElementById("is-public-form");
  if (!wrap) return;
  const propertyId = wrap.dataset.propertyId;
  if (!propertyId) return;
  const checkbox = wrap.querySelector("#is-public-switch");
  if (!checkbox) return;

  checkbox.addEventListener("change", async () => {
    checkbox.disabled = true;
    try {
      await fetch(`/properties/${propertyId}/public`, {
        method: "POST",
        credentials: "same-origin",
      });
      window.location.reload();
    } catch (err) {
      console.error("toggle public failed", err);
      checkbox.disabled = false;
    }
  });
});

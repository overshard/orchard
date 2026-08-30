// The form's `action` already holds /properties/{id}/cards, so it is posted to
// directly rather than rebuilt from window.location.
document.addEventListener("DOMContentLoaded", function () {
  const form = document.getElementById("custom-card-form");
  if (!form) return;

  form.addEventListener("change", function () {
    const data = [];
    for (const [key, value] of new FormData(form).entries()) {
      data.push({ event: key, value: value === "on" });
    }
    fetch(form.action, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    })
      .then((r) => {
        if (!r.ok) throw new Error(`${form.action} returned HTTP ${r.status}`);
        window.location.reload();
      })
      .catch((err) => {
        console.error("custom cards save failed:", err);
      });
  });
});

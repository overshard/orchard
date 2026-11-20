// The card and goal boxes sit in one table and belong to two forms by their
// form attribute, so a change is caught on the table and sent to its own form.
document.addEventListener("DOMContentLoaded", function () {
  const menu = document.getElementById("event-menu");
  if (!menu) return;

  menu.addEventListener("change", function (e) {
    const box = e.target;
    if (!box.form) return;
    if (box.form.id === "goal-form") {
      box.form.requestSubmit();
      return;
    }
    const form = box.form;
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

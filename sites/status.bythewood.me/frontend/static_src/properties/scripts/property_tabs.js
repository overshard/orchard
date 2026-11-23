// A signed in property page splits into views. The view lives in the hash so
// a reload or a shared link stays on it.
document.addEventListener("DOMContentLoaded", () => {
  const views = document.querySelectorAll(".view[data-view]");
  const buttons = document.querySelectorAll(".view-tabs [data-view]");
  if (!buttons.length) return;

  const show = (name) => {
    if (![...views].some((v) => v.dataset.view === name)) name = "uptime";
    views.forEach((v) => (v.hidden = v.dataset.view !== name));
    buttons.forEach((b) => {
      b.classList.toggle("on", b.dataset.view === name);
      b.setAttribute("aria-current", b.dataset.view === name ? "true" : "false");
    });
    history.replaceState(null, "", name === "uptime" ? location.pathname + location.search : `#${name}`);
  };
  buttons.forEach((b) => b.addEventListener("click", () => show(b.dataset.view)));
  show(location.hash.slice(1));

  const filters = document.querySelectorAll(".fixf [data-type]");
  filters.forEach((f) =>
    f.addEventListener("click", () => {
      filters.forEach((x) => x.setAttribute("aria-pressed", x === f ? "true" : "false"));
      document.querySelectorAll(".issue[data-type]").forEach((i) => {
        i.hidden = f.dataset.type !== "" && i.dataset.type !== f.dataset.type;
      });
    }),
  );
});

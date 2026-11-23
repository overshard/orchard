// The views and the tabbed panels switch in place. The view lives in the hash
// so a reload or a range change stays on it, and each panel's tab is
// remembered, since clicking a row reloads the page with a filter.
const KEY = "analytics.tabs";

function load() {
  try {
    return JSON.parse(localStorage.getItem(KEY)) || {};
  } catch {
    return {};
  }
}

function save(tabs) {
  try {
    localStorage.setItem(KEY, JSON.stringify(tabs));
  } catch {
    // Private windows can refuse storage, and the tabs still work without it.
  }
}

document.addEventListener("DOMContentLoaded", () => {
  const views = document.querySelectorAll(".view[data-view]");
  const buttons = document.querySelectorAll(".view-tabs [data-view]");
  if (!views.length) return;

  const show = (name) => {
    if (![...views].some((v) => v.dataset.view === name)) name = "traffic";
    views.forEach((v) => (v.hidden = v.dataset.view !== name));
    buttons.forEach((b) => {
      b.classList.toggle("on", b.dataset.view === name);
      b.setAttribute("aria-current", b.dataset.view === name ? "true" : "false");
    });
    history.replaceState(null, "", name === "traffic" ? location.pathname + location.search : `#${name}`);
  };
  buttons.forEach((b) => b.addEventListener("click", () => show(b.dataset.view)));
  show(location.hash.slice(1));

  document.querySelectorAll("a[data-keep-view]").forEach((a) =>
    a.addEventListener("click", () => {
      if (location.hash) a.href = a.href.split("#")[0] + location.hash;
    }),
  );

  const tabs = load();
  document.querySelectorAll(".cards[data-card]").forEach((card) => {
    const pick = (id) => {
      const btn = card.querySelector(`.ctabs [data-tab="${id}"]`);
      if (!btn) return;
      card.querySelectorAll(".ctabs [data-tab]").forEach((b) => b.setAttribute("aria-selected", b === btn ? "true" : "false"));
      card.querySelectorAll(".pane[data-pane]").forEach((p) => (p.hidden = p.dataset.pane !== id));
    };
    card.querySelectorAll(".ctabs [data-tab]").forEach((b) =>
      b.addEventListener("click", () => {
        pick(b.dataset.tab);
        tabs[card.dataset.card] = b.dataset.tab;
        save(tabs);
      }),
    );
    if (tabs[card.dataset.card]) pick(tabs[card.dataset.card]);
    // A country filter means its regions are what's wanted under Locations.
    if (card.querySelector('.pane[data-pane="regions"] .o-hbar')) pick("regions");
  });
});

// Browser notifications for the notices the server puts on every frame. The
// server names each event the same way on every poll, so all this keeps is which
// ids it has already seen, and the ones on the page when it loaded count as seen.

const root = document.querySelector("[data-notify]");
const button = root?.querySelector("[data-notify-button]");
const label = root?.querySelector("[data-notify-label]");
const menu = root?.querySelector("[data-notify-menu]");
const boxes = menu ? [...menu.querySelectorAll("input[type=checkbox]")] : [];

const KINDS = "dash-notify";
const SHOWN = "dash-notify-shown";
const KNOWN = "dash-notify-known";
const GROUP = {
  market: "Markets",
  weather: "Severe weather",
  earnings: "Earnings",
  news: "Headlines",
  live: "TheBurntPeanut",
};

// Chrome on Android has the API but throws on the constructor, since it only
// shows a notification from a service worker, so it gets no switch at all.
const supported = "Notification" in window && !/Android/i.test(navigator.userAgent);

function kinds() {
  try {
    return new Set(JSON.parse(localStorage.getItem(KINDS) || "[]"));
  } catch {
    return new Set();
  }
}

function saveKinds(set) {
  try {
    localStorage.setItem(KINDS, JSON.stringify([...set]));
    localStorage.setItem(KNOWN, JSON.stringify(Object.keys(GROUP)));
  } catch {
    // A private window can refuse storage, and then the choice lasts a page.
  }
}

// A kind added to the menu after someone switched notifications on starts on
// for them, as it would have if it had been there when they pressed it.
function adopt() {
  const chosen = kinds();
  if (!chosen.size) return;
  let known;
  try {
    known = JSON.parse(localStorage.getItem(KNOWN) || "null");
  } catch {
    known = null;
  }
  // Anyone who switched on before this key existed chose from these four.
  known ??= ["market", "weather", "earnings", "news"];
  const added = Object.keys(GROUP).filter((k) => !known.includes(k));
  if (!added.length) return;
  for (const k of added) chosen.add(k);
  saveKinds(chosen);
}

function paint() {
  const perm = Notification.permission;
  const chosen = kinds();
  const state = perm === "denied" ? "blocked" : perm === "granted" && chosen.size ? "on" : "off";
  root.dataset.state = state;
  label.textContent = state.toUpperCase();
  button.title =
    state === "blocked"
      ? "Notifications are blocked for this site in the browser's settings"
      : "Browser notifications for severe weather, big market moves, earnings, headlines and TheBurntPeanut going live";
  for (const box of boxes) box.checked = chosen.has(box.value);
}

function openMenu(open) {
  menu.hidden = !open;
  button.setAttribute("aria-expanded", String(open));
}

if (root && supported) {
  root.hidden = false;
  adopt();
  paint();

  // A browser only asks for permission from inside a press, which is why this
  // is a switch and not something the page does on load.
  button.addEventListener("click", async () => {
    if (Notification.permission === "default") await Notification.requestPermission();
    if (Notification.permission !== "granted") {
      paint();
      return;
    }
    // Off turns everything on and shows what that means, and after that a
    // press is the menu.
    if (!kinds().size && menu.hidden) {
      saveKinds(new Set(Object.keys(GROUP)));
      paint();
      openMenu(true);
      return;
    }
    openMenu(menu.hidden);
  });

  for (const box of boxes) {
    box.addEventListener("change", () => {
      const chosen = kinds();
      if (box.checked) chosen.add(box.value);
      else chosen.delete(box.value);
      saveKinds(chosen);
      paint();
    });
  }

  document.addEventListener("click", (e) => {
    if (!menu.hidden && !root.contains(e.target)) openMenu(false);
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && !menu.hidden) {
      openMenu(false);
      button.focus();
    }
  });
  window.addEventListener("storage", (e) => {
    if (e.key === KINDS) paint();
  });
}

// Two tabs on one browser get the same frame. Whichever takes the lock first
// shows it and writes the ids down, and the other finds them there.
function claim(list) {
  const take = () => {
    let shown = [];
    try {
      shown = JSON.parse(localStorage.getItem(SHOWN) || "[]");
    } catch {
      shown = [];
    }
    const mine = list.filter((n) => !shown.includes(n.id));
    try {
      localStorage.setItem(SHOWN, JSON.stringify([...shown, ...mine.map((n) => n.id)].slice(-300)));
    } catch {
      // Without storage each tab shows its own, which the tag then collapses.
    }
    return mine;
  };
  return navigator.locks ? navigator.locks.request("dash-notify", take) : Promise.resolve(take());
}

function show(list) {
  const groups = new Map();
  for (const n of list) {
    if (!groups.has(n.kind)) groups.set(n.kind, []);
    groups.get(n.kind).push(n);
  }
  for (const [kind, items] of groups) {
    const one = items.length === 1 ? items[0] : null;
    try {
      const note = new Notification(one ? one.title : GROUP[kind] || "dash", {
        body: one ? one.body : items.map((n) => n.title).join("\n"),
        // One per kind in the tray, and a new one still sounds.
        tag: "dash-" + kind,
        renotify: true,
      });
      note.onclick = () => {
        window.focus();
        if (one?.url) window.open(one.url, "_blank", "noopener");
        note.close();
      };
    } catch {
      // A browser that has the API and refuses the constructor.
    }
  }
}

let seen = null;

export function notices(list) {
  const all = Array.isArray(list) ? list : [];
  if (seen === null || seen.size > 5000) {
    seen = new Set(all.map((n) => n.id));
    return;
  }
  const fresh = all.filter((n) => !seen.has(n.id));
  for (const n of fresh) seen.add(n.id);
  if (!fresh.length || !supported || Notification.permission !== "granted") return;

  // The page is already saying it to someone looking at it, apart from a
  // warning, which is worth a second telling.
  const looking = !document.hidden && document.hasFocus();
  const chosen = kinds();
  const wanted = fresh.filter((n) => chosen.has(n.kind) && (!looking || n.kind === "weather"));
  if (wanted.length) claim(wanted).then(show);
}

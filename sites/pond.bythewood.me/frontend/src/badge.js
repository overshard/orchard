// The number on an installed pond's home screen icon, the count of things
// waiting. Off until it's turned on in settings, and there's no push and no
// service worker, so it only changes while the pond is open.
const can = typeof navigator !== "undefined" && "setAppBadge" in navigator;
let on = false, last = -1;
try { on = localStorage.getItem("pond-badge") === "1"; } catch (e) { /* private window */ }

export function badgeCan() { return can; }
export function badgeOn() { return on; }

export function setBadgeOn(v) {
  on = v;
  try { localStorage.setItem("pond-badge", v ? "1" : "0"); } catch (e) { /* private window */ }
  if (!v) clear();
  last = -1;
}

function clear() {
  if (can) navigator.clearAppBadge().catch(() => {});
}

export function badge(n) {
  if (!can || !on || n === last) return;
  last = n;
  if (n > 0) navigator.setAppBadge(n).catch(() => {});
  else clear();
}

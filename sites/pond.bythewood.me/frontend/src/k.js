import { $, post } from "./api.js";

// The key arrives in the fragment, which no server sees. It comes out of the
// address bar and the history before anything else happens.
const key = decodeURIComponent(location.hash.slice(1));
history.replaceState(null, "", location.pathname);

function restore() {
  $("k-out").textContent = "Opening the pond.";
  post("/api/key/restore", { key }).then((d) => {
    $("k-out").textContent = "That's " + d.name + ". Welcome back.";
    setTimeout(() => { location.replace("/"); }, 900);
  }, (err) => { $("k-out").textContent = err.message; });
}

// A browser that already has a frog only swaps on a press, so a link
// somebody sent can't quietly take its frog away.
const swap = $("k-swap");
if (!key) {
  if (swap) swap.hidden = true;
  $("k-out").textContent = "There's no key on this link. Type the six words instead.";
} else if (swap) {
  $("k-go").addEventListener("click", () => { swap.hidden = true; restore(); }, { once: true });
} else restore();

import { $, post } from "./api.js";

// The key arrives in the fragment, which no server sees. It comes out of the
// address bar and the history before anything else happens.
const key = decodeURIComponent(location.hash.slice(1));
history.replaceState(null, "", location.pathname);

if (!key) $("k-out").textContent = "There's no key on this link. Type the six words instead.";
else post("/api/key/restore", { key }).then((d) => {
  $("k-out").textContent = "That's " + d.name + ". Welcome back.";
  setTimeout(() => { location.replace("/"); }, 900);
}, (err) => { $("k-out").textContent = err.message; });

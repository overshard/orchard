import qrcode from "qrcode-generator";
import { $, post } from "./api.js";

const qr = $("qr");
if (qr) {
  const ctx = qr.getContext("2d"), q = qrcode(0, "M");
  q.addData(qr.dataset.url); q.make();
  const n = q.getModuleCount(), m = Math.floor(qr.width / n), off = Math.floor((qr.width - n * m) / 2);
  ctx.fillStyle = "#fff6dc"; ctx.fillRect(0, 0, qr.width, qr.height);
  ctx.fillStyle = "#0a0908";
  for (let y = 0; y < n; y++) for (let x = 0; x < n; x++) if (q.isDark(y, x)) ctx.fillRect(off + x * m, off + y * m, m, m);
}

const copy = $("key-copy");
if (copy) copy.addEventListener("click", () => {
  const done = (t) => { copy.textContent = t; setTimeout(() => { copy.textContent = "Copy"; }, 1600); };
  try { navigator.clipboard.writeText($("key-line").textContent).then(() => done("Copied"), () => done("Select it")); } catch (e) { done("Select it"); }
});

$("key-show").addEventListener("change", (e) => { $("key-in").type = e.target.checked ? "text" : "password"; });

$("restore").addEventListener("submit", (e) => {
  e.preventDefault();
  const out = $("key-result");
  out.textContent = "Looking.";
  post("/api/key/restore", { key: $("key-in").value }).then((d) => {
    out.textContent = "That's " + d.name + ". Welcome back.";
    setTimeout(() => { location.href = "/"; }, 900);
  }, (err) => { out.textContent = err.message; });
});

const lock = $("key-new");
if (lock) lock.addEventListener("click", () => {
  if (!confirm("Change the lock? The six words you have now will stop working.")) return;
  post("/api/key/rotate").then(() => location.reload(), (err) => { $("key-result").textContent = err.message; });
});

import qrcode from "qrcode-generator";
import jsQR from "jsqr";
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

function restore() {
  const out = $("key-result");
  out.textContent = "Looking.";
  post("/api/key/restore", { key: $("key-in").value }).then((d) => {
    out.textContent = "That's " + d.name + ". Welcome back.";
    setTimeout(() => { location.href = "/"; }, 900);
  }, (err) => { out.textContent = err.message; });
}

$("restore").addEventListener("submit", (e) => { e.preventDefault(); restore(); });

// Chrome saves through the credentials API, and others offer to save when a
// form with a password field is submitted, so the form posts nowhere.
const save = $("save");
if (save) save.addEventListener("submit", (e) => {
  e.preventDefault();
  const out = $("save-result"), id = $("save-name").value, password = $("save-key").value;
  if (window.PasswordCredential) {
    navigator.credentials.store(new window.PasswordCredential({ id, password, name: id })).then(
      () => { out.textContent = "Your browser should have offered to save it."; },
      () => { out.textContent = "Your browser said no. Copy the words instead."; });
  } else out.textContent = "If your password manager didn't offer to save it, copy the words instead.";
});

// A pond key's QR code holds the address of /k with the words after the #.
// Scanning one here reads the words out and never opens that address.
const pad = document.createElement("canvas"), pctx = pad.getContext("2d", { willReadFrequently: true });
let stream = null;

function readQR(source, w, h) {
  const scale = Math.min(1, 900 / Math.max(w, h));
  pad.width = Math.round(w * scale); pad.height = Math.round(h * scale);
  pctx.drawImage(source, 0, 0, pad.width, pad.height);
  const hit = jsQR(pctx.getImageData(0, 0, pad.width, pad.height).data, pad.width, pad.height);
  return hit && hit.data ? hit.data : "";
}

function scanned(text) {
  let key = text.includes("#") ? text.slice(text.indexOf("#") + 1) : text;
  try { key = decodeURIComponent(key); } catch (e) { /* typed as it is */ }
  $("key-in").value = key;
  restore();
}

function stopCamera() {
  if (stream) stream.getTracks().forEach((t) => t.stop());
  stream = null;
  $("scan-view").hidden = true;
  $("scan-cam").textContent = "Scan a QR code";
}

$("scan-cam").addEventListener("click", () => {
  if (stream) { stopCamera(); return; }
  const out = $("key-result"), video = $("scan-video");
  if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) { out.textContent = "This browser won't give a page the camera. Use a saved picture or type the words."; return; }
  navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" } }).then((s) => {
    stream = s;
    video.srcObject = s; video.play();
    $("scan-view").hidden = false;
    $("scan-cam").textContent = "Stop the camera";
    out.textContent = "Point it at the QR code.";
    (function look() {
      if (!stream) return;
      if (video.videoWidth) {
        const text = readQR(video, video.videoWidth, video.videoHeight);
        if (text) { stopCamera(); scanned(text); return; }
      }
      setTimeout(look, 160);
    })();
  }, () => { out.textContent = "No camera, or it wasn't allowed. Use a saved picture or type the words."; });
});

$("scan-file").addEventListener("change", (e) => {
  const file = e.target.files[0], out = $("key-result");
  if (!file) return;
  createImageBitmap(file).then((img) => {
    const text = readQR(img, img.width, img.height);
    if (text) scanned(text); else out.textContent = "Couldn't find a QR code in that picture.";
  }, () => { out.textContent = "That file isn't a picture this browser can read."; });
  e.target.value = "";
});

const lock = $("key-new");
if (lock) lock.addEventListener("click", () => {
  if (!confirm("Change the lock? The six words you have now will stop working.")) return;
  post("/api/key/rotate").then(() => location.reload(), (err) => { $("key-result").textContent = err.message; });
});

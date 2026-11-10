// Blips for the games and a click for the keys, made in code like the croak.
// Sound and buzz are both off until turned on in settings, and the choice
// stays in this browser only.
let ac = null;
const on = { sound: read("pond-sound"), buzz: read("pond-buzz") };

function read(k) { try { return localStorage.getItem(k) === "1"; } catch (e) { return false; } }

export function soundOn(kind) { return on[kind]; }

export function setSound(kind, v) {
  on[kind] = v;
  try { localStorage.setItem("pond-" + kind, v ? "1" : "0"); } catch (e) { /* private window */ }
}

function tone(type, f0, f1, len, vol) {
  try {
    ac = ac || new (window.AudioContext || window.webkitAudioContext)();
    if (ac.state === "suspended") ac.resume();
    const t = ac.currentTime + 0.005, o = ac.createOscillator(), g = ac.createGain();
    o.type = type;
    o.frequency.setValueAtTime(f0, t);
    o.frequency.exponentialRampToValueAtTime(f1, t + len);
    g.gain.setValueAtTime(vol, t);
    g.gain.exponentialRampToValueAtTime(0.001, t + len);
    o.connect(g); g.connect(ac.destination);
    o.start(t); o.stop(t + len + 0.02);
  } catch (e) { /* no audio here */ }
}

const SFX = {
  catch: () => tone("square", 880, 1480, 0.08, 0.06),
  hop: () => tone("square", 330, 520, 0.05, 0.04),
  bonk: () => tone("sawtooth", 160, 60, 0.18, 0.09),
  click: () => tone("triangle", 1800, 900, 0.025, 0.05),
  clunk: () => tone("triangle", 140, 70, 0.06, 0.12),
  cast: () => tone("triangle", 900, 260, 0.14, 0.04),
  plop: () => tone("sine", 520, 140, 0.09, 0.08),
  bite: () => tone("square", 1250, 1000, 0.06, 0.05),
  reel: () => tone("triangle", 2600, 2200, 0.012, 0.02),
  snap: () => tone("sawtooth", 520, 70, 0.2, 0.08),
  land: () => { tone("square", 660, 990, 0.07, 0.05); setTimeout(() => tone("square", 990, 1480, 0.09, 0.05), 70); },
};

export function sfx(kind) {
  if (on.sound && SFX[kind]) SFX[kind]();
  if ((kind === "bonk" || kind === "snap") && on.buzz && navigator.vibrate) navigator.vibrate(40);
}

// A key goes down with a click, and the main key with a clunk.
document.addEventListener("pointerdown", (e) => {
  if (!on.sound) return;
  const k = e.target.closest("button, .chip, .disk, a.game, .tabs button");
  if (!k || k.disabled) return;
  sfx(k.classList.contains("primary") || k.classList.contains("disk") ? "clunk" : "click");
});

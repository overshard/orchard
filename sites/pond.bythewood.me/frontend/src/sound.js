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

// A streak walks the blip up a major scale, a semitone or two a catch.
const SCALE = [0, 2, 4, 5, 7, 9, 11, 12, 14, 16, 17, 19];
let bend = 1, lastKey = "";

function tone(type, f0, f1, len, vol) {
  f0 *= bend; f1 *= bend;
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
  close: () => tone("triangle", 1500, 2100, 0.05, 0.05),
  kick: () => tone("sine", 300, 180, 0.04, 0.05),
  boing: () => tone("square", 240, 720, 0.09, 0.05),
  warn: () => tone("square", 640, 640, 0.1, 0.035),
  land: () => {
    const b = bend;
    tone("square", 660, 990, 0.07, 0.05);
    setTimeout(() => { bend = b; tone("square", 990, 1480, 0.09, 0.05); bend = 1; }, 70);
  },
  whip: () => tone("triangle", 2600, 700, 0.05, 0.035),
  gulp: () => {
    const b = bend;
    tone("sine", 360, 150, 0.1, 0.1);
    setTimeout(() => { bend = b; tone("square", 700, 1180, 0.06, 0.04); bend = 1; }, 90);
  },
  lick: () => tone("sine", 1100, 2100, 0.035, 0.03),
  pip: () => tone("square", 1250, 760, 0.03, 0.022),
  dew: () => tone("sine", 1500, 2300, 0.05, 0.035),
  thump: () => tone("sine", 150, 55, 0.16, 0.12),
  zap: () => tone("sawtooth", 2600, 1900, 0.03, 0.015),
  chomp: () => tone("square", 420, 180, 0.08, 0.06),
  band: () => {
    tone("square", 520, 520, 0.06, 0.04);
    setTimeout(() => tone("square", 780, 780, 0.06, 0.04), 60);
    setTimeout(() => tone("triangle", 1040, 1560, 0.16, 0.05), 120);
  },
  twang: () => { tone("square", 190, 95, 0.16, 0.05); tone("triangle", 720, 340, 0.09, 0.04); },
  creak: () => tone("triangle", 260, 300, 0.03, 0.025),
  reed: () => tone("sawtooth", 1500, 520, 0.05, 0.03),
  wood: () => { tone("square", 320, 130, 0.08, 0.05); tone("triangle", 900, 600, 0.03, 0.03); },
  mud: () => tone("sine", 210, 70, 0.12, 0.1),
  ice: () => { tone("triangle", 2600, 3400, 0.05, 0.035); setTimeout(() => tone("triangle", 3100, 2500, 0.06, 0.025), 40); },
  stone: () => { tone("triangle", 1000, 760, 0.04, 0.05); tone("sine", 150, 60, 0.12, 0.1); },
  splash: () => { tone("sine", 900, 180, 0.14, 0.06); tone("triangle", 1500, 500, 0.08, 0.025); },
  puff: () => { tone("sine", 110, 320, 0.22, 0.1); tone("triangle", 600, 220, 0.18, 0.03); },
  pop: () => tone("square", 700, 1300, 0.05, 0.05),
  perk: () => {
    tone("triangle", 660, 660, 0.07, 0.05);
    setTimeout(() => tone("triangle", 880, 880, 0.07, 0.05), 70);
    setTimeout(() => tone("triangle", 1320, 1320, 0.12, 0.05), 140);
  },
};

// The same blip twice running is bent a little, so a row of them isn't one note.
export function sfx(kind, streak) {
  if (on.sound && SFX[kind]) {
    const key = kind + (streak || 0);
    bend = Math.pow(2, SCALE[Math.min(SCALE.length - 1, Math.max(0, (streak || 1) - 1))] / 12);
    if (key === lastKey) bend *= 1 + (Math.random() - 0.5) * 0.07;
    lastKey = key;
    SFX[kind]();
    bend = 1;
  }
  if ((kind === "bonk" || kind === "snap") && on.buzz && navigator.vibrate) navigator.vibrate(40);
}

// slurp is a gulp pitched by the size of what went down, a squeak for a
// speck and a low glug for a boat.
export function slurp(s) {
  if (!on.sound) return;
  const f = 1500 / Math.pow(s, 0.42);
  tone("sine", f, f * 0.4, 0.05 + Math.min(0.22, s / 300), 0.07);
  tone("triangle", f * 1.5, f * 0.9, 0.035, 0.02);
}

// chime is a soft bell some semitones over G4, for a reed a pebble rings.
export function chime(semis, vol) {
  if (!on.sound) return;
  const f = 392 * Math.pow(2, semis / 12), v = vol || 0.03;
  tone("triangle", f, f, 0.2, v);
  tone("sine", f * 2, f * 2, 0.09, v * 0.4);
}

// A key goes down with a click, and the main key with a clunk.
document.addEventListener("pointerdown", (e) => {
  if (!on.sound) return;
  const k = e.target.closest("button, .chip, .disk, a.game, .tabs button");
  if (!k || k.disabled) return;
  sfx(k.classList.contains("primary") || k.classList.contains("disk") ? "clunk" : "click");
});

// The audio clock runs whether sound is on or not, so a game that keeps time
// with its own song can read it. It's null until the browser lets it run.
export function audioClock() {
  try {
    ac = ac || new (window.AudioContext || window.webkitAudioContext)();
    if (ac.state === "suspended") ac.resume();
    return ac.state === "running" ? ac : null;
  } catch (e) { return null; }
}

// croakAt is a short croak at a set pitch, played at a time on the audio clock.
// croakAt plays a croak at when, held for dur when it's given, and hands back
// a stop for a held one let go early.
export function croakAt(pitch, when, vol, dur) {
  if (!on.sound || !ac) return () => {};
  try {
    const len = dur || 0.13, o = ac.createOscillator(), f = ac.createBiquadFilter(), env = ac.createGain(), lfo = ac.createOscillator(), lg = ac.createGain(), am = ac.createGain();
    o.type = "sawtooth";
    o.frequency.setValueAtTime(pitch * 0.94, when);
    o.frequency.linearRampToValueAtTime(pitch * 1.04, when + Math.min(len, 0.13));
    // A phone speaker barely plays a low croak, so its upper harmonics carry it.
    const low = pitch < 220;
    f.type = "bandpass"; f.frequency.value = pitch * (low ? 3.4 : 1.7); f.Q.value = low ? 1.1 : 2.5;
    lfo.type = "square"; lfo.frequency.value = 38; lg.gain.value = 0.45; am.gain.value = 0.55;
    lfo.connect(lg); lg.connect(am.gain);
    env.gain.setValueAtTime(0, when);
    env.gain.linearRampToValueAtTime(vol || 0.2, when + 0.012);
    env.gain.setValueAtTime(vol || 0.2, when + len - 0.03);
    env.gain.linearRampToValueAtTime(0, when + len);
    o.connect(f); f.connect(am); am.connect(env); env.connect(ac.destination);
    o.start(when); lfo.start(when); o.stop(when + len + 0.02); lfo.stop(when + len + 0.02);
    return () => {
      const t = ac.currentTime;
      if (t >= when + len) return;
      env.gain.cancelScheduledValues(t); env.gain.setValueAtTime(env.gain.value, t); env.gain.linearRampToValueAtTime(0, t + 0.03);
    };
  } catch (e) { return () => {}; }
}

let rainSrc = null, rainGain = null;

// rain is a bed of filtered noise at a level from 0 to 1, and 0 stops it.
export function rain(level) {
  if (!on.sound || !ac || level <= 0) {
    if (rainSrc) { try { rainSrc.stop(); } catch (e) { /* already stopped */ } }
    rainSrc = null;
    return;
  }
  try {
    if (!rainSrc) {
      const buf = ac.createBuffer(1, ac.sampleRate * 2, ac.sampleRate), d = buf.getChannelData(0);
      for (let i = 0; i < d.length; i++) d[i] = Math.random() * 2 - 1;
      const f = ac.createBiquadFilter();
      f.type = "bandpass"; f.frequency.value = 2400; f.Q.value = 0.5;
      rainSrc = ac.createBufferSource(); rainSrc.buffer = buf; rainSrc.loop = true;
      rainGain = ac.createGain(); rainGain.gain.value = 0;
      rainSrc.connect(f); f.connect(rainGain); rainGain.connect(ac.destination);
      rainSrc.start();
    }
    rainGain.gain.setTargetAtTime(0.012 + level * 0.06, ac.currentTime, 0.4);
  } catch (e) { /* no audio here */ }
}

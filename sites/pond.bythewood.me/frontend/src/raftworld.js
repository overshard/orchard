// Raft run's river and everything on it. Nothing in here touches the page,
// so a script can play whole runs through it with bun.
export const W = 180, H = 270, FY = H - 56, STEP = 1 / 240;

// The frog steers the leaf itself and a held finger or key takes over until
// it's let go. With SELF false the leaf only drifts unless it's steered.
export const SELF = true;

// A finger moves the leaf faster than the frog does, so taking over is worth it.
const STEER = 150, KEYS = 120, PILOT = 72;
// The frog is hit a few pixels inside its leaf, and NEAR is how close a miss
// gets a CLOSE. A rock closer to dead centre than HEAD tips the leaf over.
export const BODY = 3, NEAR = 4, HEAD = 2.5;
// Every ten seconds or so a whirlpool turns up ahead. It drags the leaf in,
// its eye is a bump and getting past inside its ring pays SPUN.
export const WHIRL = 10, RING = 20, EYE = 5, PULL = 75;
export const PAY = { weir: 1, pebble: 2, fly: 1, shell: 3, spun: 3 };
// How far behind a rock its eddy curls, and how much of a bend the current
// carries the leaf round by itself.
export const EDDY = 30;
const CARRY = 0.8, LOOK = 120;
// Ten metres is more river than a point of score, so a run's tens of
// metres never come to more than its score.
export const METRE = 20;

// The river narrows and quickens over the first stretch and then holds.
export function centre(y) { return W / 2 + Math.sin(y / 170) * 30 + Math.sin(y / 53) * 9; }
export function half(y) { return 40 + 34 * Math.exp(-y / 1500); }
export function speed(t) { return 118 - 62 * Math.exp(-t / 7); }

// The current runs full speed down the middle and three quarters at the banks.
export function flow(x, y) {
  const k = Math.min(1, Math.abs(x - centre(y)) / half(y));
  return 1 - 0.25 * k * k;
}

export function world(o = {}) {
  const R = o.rand || Math.random;
  const pilot = Object.assign({ steer: PILOT, dodge: 0.92, read: 0.92, notice: 0.45, ride: 0.6 }, o.pilot);
  const w = {
    R, pilot, self: o.self === undefined ? SELF : o.self,
    t: 0, acc: 0, dist: 0, v: speed(0), fx: centre(56), aim: centre(56), mine: false, turn: 0,
    things: [], nextAt: 230, since: 0, lives: 3, safe: 0, dead: 0, tipped: false, why: "",
    row: 0, chain: 0, chainT: 0, glow: 0, eddy: 0, whirlAt: 8 + R() * 2,
    bonus: 0, weirs: 0, pebbles: 0, flies: 0, shells: 0, whirls: 0, clean: -1, events: [],
  };
  // A weir inside two seconds, with a line of pebbles to show the way through.
  const wy = 150, gap = 40, gx = centre(wy) + (R() < 0.5 ? -1 : 1) * 14 - gap / 2;
  w.things.push({ k: "weir", y: wy, gap, gx, read: true, off: 0 });
  for (let i = 1; i <= 4; i++) w.things.push({ k: "pebble", y: 56 + i * 20, x: w.fx + (gx + gap / 2 - w.fx) * i / 4, seen: true });
  while (w.nextAt < w.dist + H + 40) spawn(w);
  return w;
}

export function score(w) { return Math.floor(w.dist / 150) + w.bonus; }
export function metres(w) { return Math.floor(w.dist / METRE); }
export function over(w) { return w.dead > (w.tipped ? 1.1 : 0.45); }
export function leafY(w) { return w.dist + (H - FY); }

function spawn(w) {
  const R = w.R, p = w.pilot, y = w.nextAt, c = centre(y), h = half(y), r = R();
  if (w.since >= 3 || r < 0.3) {
    const gap = 26 + 14 * Math.exp(-w.dist / 1200);
    // A weir the frog misreads it aims a little off, and sometimes into the reeds.
    w.things.push({ k: "weir", y, gap, gx: c - h + 8 + R() * (h * 2 - gap - 16), read: R() < p.read, off: (R() < 0.5 ? -1 : 1) * gap * (0.3 + R() * 0.35) });
    w.since = 0;
  } else {
    const n = r < 0.75 ? 1 : 2;
    for (let i = 0; i < n; i++) w.things.push({ k: "rock", y: y + i * 18, x: c - h + 10 + R() * (h * 2 - 20), r: 5 + Math.floor(R() * 3), dodge: R() < p.dodge });
    const q = R();
    if (q < 0.3) w.things.push({ k: "pebble", y: y + 40, x: c - h + 12 + R() * (h * 2 - 24), seen: R() < p.notice });
    else if (q < 0.55) {
      // A row of fireflies drifting across the water on a slant.
      const x0 = c - h + 16 + R() * (h * 2 - 32), dx = (R() < 0.5 ? -1 : 1) * (4 + R() * 4), seen = R() < p.notice;
      for (let i = 0; i < 4; i++) {
        const x = Math.max(c - h + 10, Math.min(c + h - 10, x0 + dx * i));
        w.things.push({ k: "fly", y: y + 34 + i * 16, x, x0: x, ph: R() * 6, seen });
      }
    } else if (q < 0.65) w.things.push({ k: "shell", y: y + 40, x: c - h + 14 + R() * (h * 2 - 28), vx: (R() < 0.5 ? -1 : 1) * (6 + R() * 6), seen: R() < p.notice });
    w.since++;
  }
  w.nextAt += 70 + R() * 40;
}

function bump(w, why) {
  if (w.safe > 0) return false;
  if (w.clean < 0) w.clean = score(w);
  w.lives--; w.safe = 1.2; w.why = why; w.row = 0;
  if (w.lives <= 0) w.dead = 0.01;
  w.events.push({ k: "bump", x: w.fx });
  return true;
}

function tip(w) {
  if (w.clean < 0) w.clean = score(w);
  w.tipped = true; w.dead = 0.01; w.why = "TIPPED ON A ROCK"; w.row = 0;
  w.events.push({ k: "tip", x: w.fx });
}

function toward(w, x, most) {
  const d = x - w.fx, m = Math.sign(d) * Math.min(Math.abs(d), most);
  w.fx += m;
  return m;
}

// Where the frog wants the leaf. The nearest weir, whirlpool or catch it has
// noticed sets the line, and then it steps round any rock it has seen in it.
export function pilotAim(w) {
  const fy = leafY(w), c = centre(fy + 40), h = half(fy + 40);
  const lo = Math.max(c - h, centre(fy) - half(fy)) + 9, hi = Math.min(c + h, centre(fy) + half(fy)) - 9;
  let aim = c + Math.sin(w.t * 0.7) * h * 0.25;
  const ahead = w.things.filter((t) => !t.done && t.y > fy - (t.k === "rock" ? t.r + BODY + 1 : 4) && t.y < fy + LOOK).sort((a, b) => a.y - b.y);
  const next = ahead.find((t) => t.k === "whirl" || t.k === "weir");
  const got = ahead.find((t) => (t.k === "pebble" || t.k === "fly" || t.k === "shell") && t.seen);
  if (got && (!next || (got.y < next.y && next.y - fy > 50))) aim = got.x;
  else if (next && next.k === "whirl") aim = next.x + (w.fx < next.x ? -1 : 1) * (next.ride ? RING - 5 : RING + 10);
  else if (next) aim = next.gx + next.gap / 2 + (next.read ? 0 : next.off);
  // The nearest rock goes last so it has the final say. A rock too close to
  // get round the far side of keeps the leaf on the side it's on.
  for (const t of ahead.filter((t) => t.k === "rock" && t.dodge && t.y - fy < 80 && (!next || t.y < next.y)).reverse()) {
    const clear = t.r + BODY + 5, l = t.x - clear, r = t.x + clear;
    const left = w.fx < t.x, time = Math.max(0, t.y - fy) / Math.max(1, w.v);
    const cross = (left ? r - w.fx : w.fx - l) / w.pilot.steer;
    if (cross < time * 0.8 && (left ? r <= hi : l >= lo)) {
      if (aim > l && aim < r) aim = l < lo ? r : r > hi ? l : Math.abs(w.fx - l) < Math.abs(w.fx - r) ? l : r;
    } else aim = left ? Math.min(aim, Math.max(l, lo)) : Math.max(aim, Math.min(r, hi));
  }
  return Math.max(lo, Math.min(hi, aim));
}

// run steps the river on by dt at a fixed step. hand is a held finger's x,
// or null, and which arrow keys are down.
export function run(w, dt, hand) {
  w.acc += Math.min(dt, 0.05);
  while (w.acc >= STEP) {
    w.acc -= STEP;
    tick(w, STEP, hand || { want: null });
  }
}

function tick(w, dt, hand) {
  w.t += dt;
  if (w.dead) { w.dead += dt; return; }
  w.safe = Math.max(0, w.safe - dt);
  w.glow = Math.max(0, w.glow - dt);
  w.eddy = Math.max(0, w.eddy - dt);
  if (w.chainT > 0 && (w.chainT -= dt) <= 0) w.chain = 0;
  const fy0 = leafY(w);
  let m = flow(w.fx, fy0);
  for (const t of w.things) {
    if (t.k !== "rock" || fy0 - t.y < t.r || fy0 - t.y > t.r + EDDY || Math.abs(w.fx - t.x) > t.r + 6) continue;
    m *= 0.8; w.eddy = 0.2;
    toward(w, t.x, 18 * dt);
  }
  w.v = speed(w.t) * m;
  w.dist += w.v * dt;
  const fy = leafY(w);
  w.fx += (centre(fy) - centre(fy0)) * CARRY;
  w.mine = hand.want !== null || !!hand.left || !!hand.right;
  let moved = 0;
  if (w.mine) {
    if (hand.want !== null) moved += toward(w, hand.want, STEER * dt);
    if (hand.left) { w.fx -= KEYS * dt; moved -= KEYS * dt; }
    if (hand.right) { w.fx += KEYS * dt; moved += KEYS * dt; }
  } else if (w.self) {
    w.aim = pilotAim(w);
    moved = toward(w, w.aim, w.pilot.steer * dt);
  }
  w.turn += (Math.sign(moved) - w.turn) * Math.min(1, dt * 8);
  while (w.nextAt < w.dist + H + 40) spawn(w);
  if (w.t >= w.whirlAt) {
    const y = w.nextAt + 10, wc = centre(y);
    w.things.push({ k: "whirl", y, x: wc + (w.R() * 2 - 1) * half(y) / 3, ride: w.R() < w.pilot.ride });
    w.nextAt += 80;
    w.whirlAt = w.t + WHIRL - 1 + w.R() * 2;
    w.events.push({ k: "warn" });
  }
  const c = centre(fy), h = half(fy);
  for (const t of w.things) {
    if (t.k === "fly" && !t.done) t.x = t.x0 + Math.sin(w.t * 1.6 + t.ph) * 5;
    if (t.k === "shell" && !t.done) {
      t.x += t.vx * dt;
      const sc = centre(t.y), sh = half(t.y) - 10;
      if (Math.abs(t.x - sc) > sh) { t.x = sc + Math.sign(t.x - sc) * sh; t.vx = -t.vx; }
    }
    if (t.k !== "whirl" || t.done || Math.abs(fy - t.y) > RING + 10 || Math.abs(w.fx - t.x) > RING + 12) continue;
    toward(w, t.x, PULL * dt);
  }
  if (Math.abs(w.fx - c) > h - 5) { bump(w, "ON THE BANK"); w.fx = c + Math.sign(w.fx - c) * (h - 9); }
  for (const t of w.things) {
    if (t.done) continue;
    if (t.k === "weir" && fy >= t.y) {
      t.done = true;
      const room = Math.min(w.fx - BODY - t.gx, t.gx + t.gap - w.fx - BODY);
      if (room > 0) {
        w.bonus += PAY.weir; w.weirs++;
        w.events.push({ k: "weir", x: w.fx, n: ++w.row, close: room < NEAR });
      } else bump(w, "IN THE REEDS");
    } else if (t.k === "rock" && Math.abs(fy - t.y) < t.r + BODY && Math.abs(w.fx - t.x) < t.r + BODY) {
      t.done = true;
      if (w.safe > 0) continue;
      if (Math.abs(w.fx - t.x) < HEAD) tip(w);
      else bump(w, "ON A ROCK");
    } else if (t.k === "rock" && fy > t.y + t.r + BODY) {
      t.done = true;
      if (Math.abs(w.fx - t.x) < t.r + BODY + NEAR && w.safe === 0) w.events.push({ k: "close", x: w.fx });
    } else if (t.k === "whirl" && fy >= t.y) {
      t.done = true;
      const d = Math.abs(w.fx - t.x);
      if (d < EYE) bump(w, "IN THE WHIRLPOOL");
      else if (d < RING && w.safe === 0) { w.bonus += PAY.spun; w.whirls++; w.events.push({ k: "spun", x: w.fx, n: ++w.row }); }
    } else if ((t.k === "pebble" || t.k === "fly" || t.k === "shell") && Math.abs(fy - t.y) < 8 && Math.abs(w.fx - t.x) < 9) {
      t.done = true;
      w.bonus += PAY[t.k];
      if (t.k === "pebble") w.pebbles++;
      else if (t.k === "shell") w.shells++;
      else { w.flies++; w.glow = 0.5; w.chainT = 1.6; w.chain++; }
      w.events.push({ k: t.k, x: t.x, n: t.k === "fly" ? w.chain : ++w.row });
    }
  }
  w.things = w.things.filter((t) => t.y > w.dist - 20);
}

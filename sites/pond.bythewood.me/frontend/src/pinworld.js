// Lily pinball's table and every ball on it. Nothing in here touches the
// page, so a script can play whole games through it with bun.
export const W = 200, H = 320, CX = 94, BR = 5, STEP = 1 / 480;
export const GRAV = 420, FAST = 650, BALLS = 3, SAVE = 6, GULP = 0.8, MOST_BALLS = 5;
export const PAY = { pad: 10, sling: 5, fly: 25, set: 200, mouth: 250 };

// The flippers turn about their pivots, resting under level and flipping
// over it. TURN is held down so a flipper's tip never steps past a ball.
export const PIVOTS = [[54, 268], [134, 268]], FLEN = 34, FR = 3.5, TURN = 20;
export const REST = [0.5, Math.PI - 0.5], UP = [-0.45, Math.PI + 0.45];

// The lane the frog plunges up on the right, and its one way gate at the top.
export const LANE = { x0: 182, x1: 197, floor: 306, gate: [182, 74, 196, 50] };
export const MOUTH = { x: CX, y: 42, r: 7 }, FROG = { x: CX, y: 22, r: 17 };
export const PADS = [{ x: 66, y: 104, r: 12 }, { x: 122, y: 104, r: 12 }, { x: CX, y: 136, r: 12 }];
export const FLIES = [30, 62, 94, 126, 158].map((x) => ({ x, y: 70 })), FLY_R = 7;

const mirror = (s) => [2 * CX - s[0], s[1], 2 * CX - s[2], s[3]];
const LSLING = [21, 198, 46, 242];
// Each cattail's face kicks, and the post over it splits the outlane off.
export const SLINGS = [LSLING, mirror(LSLING)];
const LBACK = [[21, 198, 52, 262], [46, 242, 52, 262]];
export const POSTS = [{ x: 21, y: 196, r: 3 }, { x: 2 * CX - 21, y: 196, r: 3 }];
export const WALLS = [
  [6, 330, 6, 196], [6, 196, 11, 174], [11, 174, 6, 150], [6, 150, 6, 54], [6, 54, 14, 32], [14, 32, 28, 16], [28, 16, 48, 8], [48, 8, 140, 8],
  [140, 8, 164, 12], [164, 12, 182, 24], [182, 24, 193, 40], [193, 40, 197, 58], [197, 58, 197, 330],
  [LANE.x0, 330, LANE.x0, 196], [LANE.x0, 196, LANE.x0 - 5, 174], [LANE.x0 - 5, 174, LANE.x0, 150], [LANE.x0, 150, LANE.x0, LANE.gate[1]],
  ...LBACK, ...LBACK.map(mirror),
  // The bank either side of the drain, under the pivots.
  [52, 262, 54, 268], [2 * CX - 52, 262, 2 * CX - 54, 268],
];
const WALL_R = 1.5, WALL_E = 0.45, FLIP_E = 0.25, SLING_E = 0.4, SLING_KICK = 110, PAD_KICK = 90, SPIN = 0.15;

// The frog commits to a save this often as a ball comes down to it.
// LEAD is how far ahead of the ball it flips, in seconds.
export const FROG_SAVE = 0.94;
const LEAD = 0.03, AUTO = 0.18, COOL = 0.2;

let ids = 0;

function ball(x, y, vx, vy, age) {
  return { id: ++ids, x, y, vx, vy, age: age || 0, born: age || 0, lane: false, still: 0, zone: false, skill: true, near: 99 };
}

export function world(rand, save) {
  const w = {
    rand: rand || Math.random, save: save === undefined ? FROG_SAVE : save,
    balls: [], left: BALLS, saveT: 0, saved: false, held: null, queue: [], qT: 0,
    ang: REST.slice(), w: [0, 0], auto: [0, 0], cool: [0, 0], hold: [false, false], up: [false, false],
    lit: [0, 0, 0, 0, 0], score: 0, t: 0, acc: 0, over: false, events: [],
    counts: { lights: 0, mouth: 0, multi: 0, long: 0, pads: 0, slings: 0 },
  };
  w.balls.push(inLane(w));
  return w;
}

function inLane(w, age) {
  const b = ball((LANE.x0 + LANE.x1) / 2, LANE.floor - BR, 0, 0, age);
  b.lane = true; b.wait = 0.5 + w.rand() * 0.3;
  return b;
}

export function flipper(w, i) {
  const [px, py] = PIVOTS[i], a = w.ang[i];
  return [px, py, px + FLEN * Math.cos(a), py + FLEN * Math.sin(a)];
}

// run moves the table on by dt. hold is which flippers the player has.
export function run(w, dt, hold) {
  if (w.over) return;
  w.hold = hold || [false, false];
  w.acc += dt;
  while (w.acc >= STEP) { w.acc -= STEP; tick(w, STEP); }
}

function tick(w, h) {
  if (w.over) return;
  w.t += h;
  w.saveT = Math.max(0, w.saveT - h);
  frogPlays(w, h);
  for (let i = 0; i < 2; i++) {
    const up = w.hold[i] || w.auto[i] > 0, want = up ? UP[i] : REST[i], old = w.ang[i], d = want - old, mv = TURN * h;
    w.ang[i] = Math.abs(d) < mv ? want : old + Math.sign(d) * mv;
    w.w[i] = (w.ang[i] - old) / h;
    if (up && !w.up[i]) w.events.push({ k: "flip", i, mine: w.hold[i] });
    w.up[i] = up;
  }
  if (w.held) {
    w.held.t -= h;
    if (w.held.t <= 0) {
      const a = Math.PI / 2 + (w.rand() - 0.5) * 1.8, v = 180 + w.rand() * 80;
      const b = ball(MOUTH.x, MOUTH.y + MOUTH.r + 2, Math.cos(a) * v, Math.sin(a) * v, w.held.age);
      w.balls.push(b); w.held = null;
      w.events.push({ k: "spit", x: b.x, y: b.y });
    }
  }
  if (w.queue.length && !w.held) {
    w.qT -= h;
    if (w.qT <= 0) {
      w.queue.shift();
      w.qT = 0.35;
      const a = Math.PI / 2 + (w.rand() - 0.5) * 1.2;
      const b = ball(MOUTH.x, MOUTH.y + MOUTH.r + 2, Math.cos(a) * 200, Math.sin(a) * 200);
      w.balls.push(b);
      w.events.push({ k: "drop", x: b.x, y: b.y });
    }
  }
  for (const b of w.balls) move(w, b, h);
  for (let i = 0; i < w.balls.length; i++) for (let j = i + 1; j < w.balls.length; j++) knock(w.balls[i], w.balls[j]);
  const was = w.balls.length;
  w.balls = w.balls.filter((b) => !b.gone);
  if (w.balls.length < was || !w.balls.length) lost(w);
}

// The frog flips on time and hard, and only misses because it decided to.
function frogPlays(w, h) {
  for (let i = 0; i < 2; i++) { w.auto[i] -= h; w.cool[i] -= h; }
  for (const b of w.balls) {
    if (b.lane) continue;
    if (b.vy > 0 && b.y > 230 && !b.zone) { b.zone = true; b.skill = w.rand() < w.save; }
    if (b.y < 220) b.zone = false;
    if (!b.skill || b.vy < -20) continue;
    const i = b.x < CX ? 0 : 1, [px, py] = PIVOTS[i], along = i ? px - b.x : b.x - px;
    if (along < 4 || along > FLEN + 3 || w.cool[i] > 0) continue;
    // Flip just before the ball meets the resting flipper, so it's hit on the way up.
    const meet = py + along * Math.tan(REST[0]) - (FR + BR) / Math.cos(REST[0]);
    const soon = (meet - b.y) / Math.max(b.vy, 30);
    if (soon < LEAD && soon > -0.05) { w.auto[i] = AUTO; w.cool[i] = COOL; }
  }
}

function move(w, b, h) {
  b.age += h;
  if (b.lane && b.wait > 0) {
    b.y = LANE.floor - BR; b.vx = 0; b.vy = 0;
    b.wait -= h;
    if (b.wait <= 0) {
      b.vy = -(590 + w.rand() * 40);
      if (!w.saved) w.saveT = SAVE;
      w.events.push({ k: "plunge", x: b.x, y: b.y });
    }
    return;
  }
  b.vy += GRAV * h;
  b.x += b.vx * h; b.y += b.vy * h;
  for (const q of WALLS) seg(w, b, q, WALL_R + BR, WALL_E);
  // The gate only turns back a ball coming from the field.
  const g = LANE.gate;
  if (b.lane && side(b, g) > WALL_R + BR + 1) b.lane = false;
  if (!b.lane) seg(w, b, g, WALL_R + BR, WALL_E);
  if (b.x > LANE.x0 && b.y > LANE.floor - BR && b.x < LANE.x1) {
    b.lane = true; b.wait = 0.5; b.y = LANE.floor - BR; b.vx = b.vy = 0;
    w.events.push({ k: "lane" });
    return;
  }
  SLINGS.forEach((q, i) => {
    const t = seg(w, b, q, WALL_R + BR, SLING_E, null, 0, true);
    if (t > 0.12 && t < 0.88 && b.hitV > 60) {
      const [nx, ny] = normal(q);
      b.vx += nx * SLING_KICK; b.vy += ny * SLING_KICK;
      w.score += PAY.sling; w.counts.slings++;
      w.events.push({ k: "sling", i, x: b.x, y: b.y, id: b.id });
    }
  });
  for (const p of POSTS) round(w, b, p, 0, WALL_E);
  for (let i = 0; i < 2; i++) {
    const f = flipper(w, i);
    seg(w, b, f, FR + BR, FLIP_E, PIVOTS[i], w.w[i]);
    // Only a flipper that swung counts toward a near miss.
    if (w.ang[i] !== REST[i]) b.near = Math.min(b.near, Math.hypot(b.x - f[2], b.y - f[3]) - FR - BR);
  }
  PADS.forEach((p, i) => {
    if (round(w, b, p, PAD_KICK, 1)) {
      w.score += PAY.pad; w.counts.pads++;
      w.events.push({ k: "pad", i, x: p.x, y: p.y, id: b.id });
    }
  });
  if (!w.held && b.age - b.born > 0.4 && Math.hypot(b.x - MOUTH.x, b.y - MOUTH.y) < MOUTH.r) {
    b.gone = true;
    w.held = { t: GULP, age: b.age };
    w.score += PAY.mouth; w.counts.mouth++;
    w.events.push({ k: "gulp", x: MOUTH.x, y: MOUTH.y });
    return;
  }
  round(w, b, FROG, 0, WALL_E);
  FLIES.forEach((f, i) => {
    if (w.lit[i] || Math.hypot(b.x - f.x, b.y - f.y) > FLY_R) return;
    w.lit[i] = 1; w.score += PAY.fly;
    const n = w.lit.reduce((a, v) => a + v, 0);
    w.events.push({ k: "fly", i, n, x: f.x, y: f.y });
    if (n === FLIES.length) setLit(w);
  });
  const v = Math.hypot(b.vx, b.vy);
  if (v > FAST) { b.vx *= FAST / v; b.vy *= FAST / v; }
  // A ball sat still on a post or a resting flipper gets a nudge off it.
  b.still = v < 12 && !w.hold[b.x < CX ? 0 : 1] ? b.still + h : 0;
  if (b.still > 1.2) { b.vx += (w.rand() < 0.5 ? -1 : 1) * 60; b.vy -= 40; b.still = 0; }
  if (b.y > H + 10 || b.x < -10 || b.x > W + 10 || b.y < -20) {
    b.gone = true;
    // Anywhere but the drain is a bug, and a script counts it.
    b.escaped = b.y <= H + 10;
    if (b.escaped) w.escaped = (w.escaped || 0) + 1;
    w.counts.long = Math.max(w.counts.long, Math.floor(b.age));
    w.events.push({ k: "drain", id: b.id, x: b.x, skill: b.skill, close: b.near < 3 && b.x > PIVOTS[0][0] && b.x < PIVOTS[1][0] });
    if (w.saveT > 0 && !w.saved) {
      w.saved = true; w.saveT = 0;
      w.balls.push(inLane(w, b.age));
      w.events.push({ k: "saved" });
    }
  }
  if (b.y < 240) b.near = 99;
}

function setLit(w) {
  w.lit = [0, 0, 0, 0, 0];
  w.score += PAY.set; w.counts.lights++;
  const room = MOST_BALLS - w.balls.length - (w.held ? 1 : 0) - w.queue.length;
  if (room > 0) {
    w.counts.multi++;
    for (let i = 0; i < Math.min(2, room); i++) w.queue.push(1);
    w.qT = 0.5;
  }
  w.events.push({ k: "set", multi: room > 0 });
}

// When the last ball on the table goes and nothing is coming, the next one
// goes in the lane, or the game is over.
function lost(w) {
  if (w.balls.length || w.held || w.queue.length) return;
  if (w.left > 1) {
    w.left--;
    w.saved = false;
    w.balls.push(inLane(w));
    w.events.push({ k: "next" });
  } else {
    w.left = 0; w.over = true;
    w.events.push({ k: "over" });
  }
}

function side(b, q) {
  const [nx, ny] = normal(q);
  return (b.x - q[0]) * nx + (b.y - q[1]) * ny;
}

// A segment's normal, to the left going from its first end to its second.
function normal(q) {
  const dx = q[2] - q[0], dy = q[3] - q[1], l = Math.hypot(dx, dy);
  return [dy / l, -dx / l];
}

// seg pushes a ball out of a segment of thickness rad and bounces it. piv and
// spin are a flipper's pivot and how fast it's turning, so the flipper's own
// speed where it meets the ball goes into the ball. It says where along the
// segment it met, or 0 for not at all.
function seg(w, b, q, rad, e, piv, spin, quiet) {
  const [ax, ay, bx, by] = q, dx = bx - ax, dy = by - ay, l2 = dx * dx + dy * dy;
  let t = ((b.x - ax) * dx + (b.y - ay) * dy) / l2;
  t = Math.max(0, Math.min(1, t));
  const cx = ax + dx * t, cy = ay + dy * t;
  let nx = b.x - cx, ny = b.y - cy;
  const d = Math.hypot(nx, ny);
  if (d >= rad || d === 0) return 0;
  nx /= d; ny /= d;
  b.x = cx + nx * rad; b.y = cy + ny * rad;
  let svx = 0, svy = 0;
  if (piv) { svx = -spin * (cy - piv[1]); svy = spin * (cx - piv[0]); }
  const rvx = b.vx - svx, rvy = b.vy - svy, vn = rvx * nx + rvy * ny;
  b.hitV = -vn;
  if (vn < 0) {
    // A slow touch rolls along instead of bouncing, so a ball can cradle.
    const k = vn < -40 ? 1 + e : 1;
    b.vx = svx + rvx - k * vn * nx; b.vy = svy + rvy - k * vn * ny;
    if (vn < -40) turn(w, b, SPIN * 0.3);
    if (!quiet && vn < -150) w.events.push({ k: "knock", x: cx, y: cy, v: -vn });
  }
  return Math.max(t, 1e-6);
}

// round bounces a ball off a circle, with a kick out from its middle.
function round(w, b, p, kick, e) {
  let nx = b.x - p.x, ny = b.y - p.y;
  const d = Math.hypot(nx, ny), rad = p.r + BR;
  if (d >= rad || d === 0) return false;
  nx /= d; ny /= d;
  b.x = p.x + nx * rad; b.y = p.y + ny * rad;
  const vn = b.vx * nx + b.vy * ny;
  if (vn < 0) { b.vx -= (1 + e) * vn * nx; b.vy -= (1 + e) * vn * ny; }
  if (!kick) return true;
  b.vx += nx * kick; b.vy += ny * kick;
  turn(w, b, SPIN);
  return true;
}

// No two bounces are quite alike, so no line round the table is a sure thing.
function turn(w, b, most) {
  const a = (w.rand() - 0.5) * 2 * most, c = Math.cos(a), s = Math.sin(a), vx = b.vx;
  b.vx = vx * c - b.vy * s; b.vy = vx * s + b.vy * c;
}

function knock(a, b) {
  if (a.lane || b.lane) return;
  const dx = b.x - a.x, dy = b.y - a.y, d = Math.hypot(dx, dy);
  if (d >= 2 * BR || d === 0) return;
  const nx = dx / d, ny = dy / d, pen = (2 * BR - d) / 2;
  a.x -= nx * pen; a.y -= ny * pen; b.x += nx * pen; b.y += ny * pen;
  const vn = (b.vx - a.vx) * nx + (b.vy - a.vy) * ny;
  if (vn >= 0) return;
  const j = -0.9 * vn;
  a.vx -= nx * j; a.vy -= ny * j; b.vx += nx * j; b.vy += ny * j;
}

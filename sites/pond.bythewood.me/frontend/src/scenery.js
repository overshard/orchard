import { reduced } from "./api.js";
import { drawFurn } from "./icons.js";

// What a finished set puts on the water. Each is drawn in 4px cells from
// its own top left corner, w and h cells big, and at is where on the pond
// it stands so it clears the frog, the furniture and both top corners.
function cell(ctx, x, y, c, w, h) {
  ctx.fillStyle = c;
  ctx.fillRect(x * 4, y * 4, (w || 1) * 4, (h || 1) * 4);
}

function rows(ctx, art, col) {
  art.forEach((row, r) => {
    for (let c = 0; c < row.length; c++) if (row.charAt(c) !== ".") cell(ctx, c, r, col[row.charAt(c)]);
  });
}

const RING = [[1, 1], [4, 0], [7, 0], [9, 1], [0, 3], [10, 3], [3, 4], [7, 4]];
const MOSS = ["..gggggggg..", ".gggggggggg.", "gggggggggggg", "gggggggggggg", ".gggggggggg.", "..gggggggg.."];
const BRIDGE = ["....rrrrrr....", "..rr.p..p.rr..", ".r.p.dddd.p.r.", "rpdddooooddd.r", "pddoo....ooddp", "doo........ood"];
const CROW = ["..yyy..", ".yyyyy.", "..www..", "..wkw..", "ooooooo", "..rrr..", "..rbr..", "..rrr..", "...o...", "...o...", "...o..."];
const VANE = ["...y...", "..yyy..", "...s...", "y..s..y", "yyyyyyy", "y..s..y", "...s...", "...s...", "...s...", "..sss.."];
const BRIDGE_COL = { r: "#8a5a2b", p: "#6b4a2a", d: "#b08a5a", o: "#5a3f24" };
const CROW_COL = { y: "#e3c57a", w: "#f6dcb0", k: "#1a1a1a", o: "#8a5a2b", r: "#c9412d", b: "#3d8fd6" };
const VANE_COL = { y: "#ffe27a", s: "#9aa0a6" };

export const SCENERY = {
  s_ring: {
    w: 12, h: 6, at: [270, 64],
    draw(ctx) {
      rows(ctx, MOSS, { g: "#1f5a2c" });
      RING.forEach(([x, y], i) => {
        cell(ctx, x, y, i % 3 ? "#e2412d" : "#f2a23a", 2, 1);
        cell(ctx, x + (i % 2), y + 1, "#fff6dc");
      });
    },
  },
  s_lantern: {
    w: 6, h: 13, at: [60, 64],
    draw(ctx, t, day) {
      cell(ctx, 2, 8, "#6b4a2a", 2, 5);
      cell(ctx, 1, 12.5, "rgba(127,214,194,0.3)", 4, 0.5);
      drawFurn(ctx, "f_lantern", 0, 0, 4, !!(day && day.night));
    },
  },
  s_bridge: {
    w: 14, h: 6, at: [0, 84],
    draw(ctx) { rows(ctx, BRIDGE, BRIDGE_COL); },
  },
  s_scarecrow: {
    w: 7, h: 11, at: [284, 92],
    draw(ctx) { rows(ctx, CROW, CROW_COL); },
  },
  s_vane: {
    w: 7, h: 10, at: [226, 8],
    // The arrow comes about now and then, quicker in a wind.
    draw(ctx, t, day) {
      const back = !reduced && Math.sin(t * (0.15 + 0.2 * ((day && day.wind) || 0))) < 0;
      rows(ctx, VANE.map((row, r) => r >= 3 && r <= 5 && back ? [...row].reverse().join("") : row), VANE_COL);
      cell(ctx, back ? 0 : 6, 4, "#e2412d");
    },
  },
  s_chime: {
    w: 5, h: 9, at: [124, 0],
    draw(ctx, t, day) {
      cell(ctx, 2, 0, "#6d747a", 1, 2);
      cell(ctx, 0, 2, "#8a5a2b", 5, 1);
      [5, 6, 4].forEach((len, i) => {
        const sway = reduced ? 0 : Math.round(Math.sin(t * 1.7 + i * 1.3) * (0.5 + ((day && day.wind) || 0))) / 4;
        cell(ctx, i * 2 + sway, 3, i === 1 ? "#cfe3ea" : "#9fd8e8", 1, len);
      });
    },
  },
};

// Every finished set's scenery, on the water behind the frog.
export function drawScenery(ctx, sets, day, t) {
  (sets || []).forEach((s) => {
    const sc = s.done && SCENERY[s.scene];
    if (!sc) return;
    ctx.save();
    ctx.translate(sc.at[0], sc.at[1]);
    sc.draw(ctx, t, day);
    ctx.restore();
  });
}

// After dark the second lantern is lit over the night like the first.
export function drawSceneryNight(ctx, sets, day) {
  const sc = SCENERY.s_lantern;
  if (!day.night || !(sets || []).some((s) => s.done && s.scene === "s_lantern")) return;
  const [x, y] = sc.at;
  [[36, 0.1], [24, 0.12], [14, 0.16]].forEach(([r, a]) => {
    ctx.fillStyle = "rgba(255,226,122," + a + ")";
    ctx.fillRect(x + 12 - r, y + 16 - r, r * 2, r * 2);
  });
  drawFurn(ctx, "f_lantern", x, y, 4, true);
}

// One piece of scenery on a canvas of its own, for the shelf and a reveal.
export function sceneryCanvas(key, s) {
  const sc = SCENERY[key], cv = document.createElement("canvas"), k = s || 1;
  cv.width = sc.w * 4 * k; cv.height = sc.h * 4 * k;
  const ctx = cv.getContext("2d");
  ctx.scale(k, k);
  sc.draw(ctx, 0, { night: true });
  return cv;
}

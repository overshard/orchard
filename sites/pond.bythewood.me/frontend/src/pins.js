// A badge is an enamel pin, a twelve by twelve disc with a gold rim and an
// eight by eight picture in the middle. The letters are one palette for all.
const INK = { w: "#fff6dc", y: "#ffe27a", k: "#14110c", g: "#56b45f", l: "#9fe08f", r: "#e2412d", p: "#f59ab3", b: "#9fd8e8", o: "#c9974f", s: "#c3c8cd" };
const DISC = ["...rrrrrr...", "..rffffffr..", ".rffffffffr.", "rffffffffffr", "rffffffffffr", "rffffffffffr", "rffffffffffr", "rffffffffffr", "rffffffffffr", ".rffffffffr.", "..rffffffr..", "...rrrrrr..."];
// The enamel behind the picture says which part of the game a pin is from.
const ENAMEL = { play: "#1f5f6b", frogs: "#1f5a2c", finds: "#4b3578", works: "#6b4526", town: "#8a2f48", days: "#27507a" };

const PINS = {
  first_fish: ["play", "........", "...ww...", ".wwwww.w", "wkwwwwww", "wwwwwwww", ".wwwww.w", "...ww...", "........"],
  fish_10: ["play", "........", ".www.www", "wyyywyyy", "wyyywyyy", "wyyywyyy", "wwwwwwww", "........", "........"],
  fish_big: ["play", "...yy...", "..yyyy.y", ".yyyyyyy", "ykyyyyyy", "yyyyyyyy", ".yyyyyyy", "..yyyy.y", "...yy..."],
  record: ["play", "wyyyyyyw", "wyyyyyyw", ".yyyyyy.", "..yyyy..", "...yy...", "...yy...", "..yyyy..", ".yyyyyy."],
  podium: ["play", "........", "...yy...", "...yy...", ".ssyy...", ".ssyyoo.", ".ssyyoo.", ".ssyyoo.", "........"],
  dive_win: ["play", "..rr.rr.", "..rr.rr.", "...yyy..", "..yyyyy.", "..yykyy.", "..yykyy.", "..yyyyy.", "...yyy.."],
  every_disk: ["play", "wwwwwww.", "wsswwwww", "wsswwwww", "wwwwwwww", "wkkkkkkw", "wkkkkkkw", "wkkkkkkw", "wwwwwwww"],
  hatch: ["frogs", "..wwww..", ".wwwwww.", ".wwwwww.", "wkwkwkww", "wwkwkwkw", "wwwwwwww", ".wwwwww.", "..wwww.."],
  grown: ["frogs", ".ll..ll.", "lwkllwkl", "llllllll", "llllllll", "lkllllkl", ".lkkkkl.", "..llll..", "........"],
  elder: ["frogs", ".ww..ww.", "lwkllwkl", "llllllll", "lkllllkl", ".lkkkkl.", "..wwww..", "..wwww..", "...ww..."],
  chorus: ["frogs", "..wwwww.", "..w...w.", "..w...w.", "..w...w.", ".ww..ww.", "www.www.", "ww..ww..", "........"],
  heart_1: ["frogs", "........", ".pp.pp..", "ppppppp.", "ppppppp.", ".ppppp..", "..ppp...", "...p....", "........"],
  heart_5: ["frogs", ".rr..rr.", "rwrrrrrr", "rwrrrrrr", "rrrrrrrr", ".rrrrrr.", "..rrrr..", "...rr...", "........"],
  job_10: ["works", ".sssss..", ".sssss..", "...o....", "...o....", "...o....", "...o....", "...o....", "........"],
  job_50: ["works", "..s..s..", ".ssssss.", "sss..sss", ".s....s.", ".s....s.", "sss..sss", ".ssssss.", "..s..s.."],
  job_99: ["works", "...y....", "...y....", "yyyyyyy.", ".yyyyy..", "..yyy...", ".yy.yy..", ".y...y..", "........"],
  pet: ["works", "..w..w..", "..w..w..", "w......w", "w.wwww.w", "..wwww..", ".wwwwww.", ".wwwwww.", "..w..w.."],
  hats_10: ["finds", "........", "..wwww..", "..wwww..", "..wwww..", "..rrrr..", "..wwww..", "wwwwwwww", "........"],
  hats_30: ["finds", "...y....", "...r....", "..rwr...", "..wrw...", ".rwrwr..", ".wrwrw..", "rwrwrwr.", "........"],
  hat_gold: ["finds", "........", "y..yy..y", "y.yyyy.y", "yyyyyyyy", "yyryyryy", "yyyyyyyy", "yyyyyyyy", "........"],
  trinkets_10: ["finds", "..yy....", ".yyky...", ".yyyyoo.", "..yy....", "yyyyyyy.", "yyyyyyy.", ".yyyyy..", "........"],
  rocks_5: ["finds", "........", ".bbbbbb.", "bbwbbbbb", ".bwbbbb.", "..bbbb..", "...bb...", "........", "........"],
  keepsakes_5: ["days", ".r.rr.r.", "wwwwwwww", "wwwwwwww", "wkkwkkww", "wwwwwwww", "wkkwkkww", "wwwwwwww", "........"],
  set_1: ["finds", "www.yyy.", "www.yyy.", "www.yyy.", "........", "ppp.bbb.", "ppp.bbb.", "ppp.bbb.", "........"],
  sets_all: ["finds", "........", "ww.yy.pp", "ww.yy.pp", "........", "bb.gg.rr", "bb.gg.rr", "........", "........"],
  lily_lord: ["finds", "...p....", "..ppp...", ".pwpwp..", "..ppp...", "gggggggg", "gggggg.g", ".gggggg.", "........"],
  bar: ["works", "........", "........", "...ww...", "..wwww..", ".ss.sss.", "ssssssss", "ssssssss", "........"],
  garden: ["works", "..yy....", ".ywwy...", "..yy....", "...g..g.", "..gg.gg.", "...ggg..", "...g....", "oooooooo"],
  cook_1: ["works", "..w..w..", ".w..w...", "..w..w..", "wwwwwwww", "wwwwwwww", ".wwwwww.", "..wwww..", "........"],
  cook_10: ["works", "w.w..ww.", "w.w.wwww", "w.w.wwww", "www..ww.", ".w...ww.", ".w...ww.", ".w...ww.", ".w...ww."],
  builder: ["works", "...ww...", "..wwww..", ".wwwwww.", "wwwwwwww", ".wkkwww.", ".wkkwkw.", ".wkkwww.", "........"],
  lodger: ["works", "..oooo..", ".oyyyyo.", "oyyyyyyo", "oyykkyyo", "oyykkyyo", "oyyyyyyo", "oyyyyyyo", "........"],
  friend_1: ["town", "wwwwwww.", "wwpwpww.", "wpppppw.", "wwpppww.", "wwwpwww.", "wwwwwww.", ".ww.....", ".w......"],
  friends_all: ["town", "........", ".ww..ww.", ".ww..ww.", "...ww...", "wwwwwwww", "wwwwwwww", "ww.ww.ww", "........"],
  best_friend: ["town", ".r....r.", "..r..r..", "...rr...", "wwwrrwww", "wwwrrwww", "wwwrrwww", "wwwrrwww", "........"],
  stall: ["town", "rwrwrwrw", "rwrwrwrw", "........", "o......o", "o.wwww.o", "o.wwww.o", "oooooooo", "........"],
  share: ["town", "......yy", ".....yy.", "...y.y..", "..yyy...", ".yy.....", "yy......", "wwwwwwww", "........"],
  furniture: ["town", "........", ".w.w....", "wwwwww..", "wwwwwwww", "wwwwww.w", "wwwwwww.", ".wwww...", "wwwwww.."],
  days_30: ["days", "...y....", ".y.y.y..", "..yyy...", "yyyyyyy.", "..yyy...", ".y.y.y..", "...y....", "........"],
  days_100: ["days", "wwwwwww.", ".w...w..", "..wyw...", "...y....", "..w.w...", ".w.y.w..", "wyyyyyw.", "wwwwwww."],
  sites: ["days", "w..w..w.", ".w.w.w..", "..www...", "...w....", "...w....", "...w....", "..www...", ".wwwww.."],
  key: ["days", "..yyy...", ".y...y..", ".y...y..", "..yyy...", "...y....", "...yy...", "...y....", "...yy..."],
  sweep_clear: ["play", "..gggg..", ".gllllg.", "gllllg..", "gllll...", "gllllg..", ".gllllg.", "..gggg..", "........"],
  drop_five: ["play", "...ss...", "..ssss..", "...ss...", "........", "y.w..w.y", "y.wssw.y", "y.wssw.y", "yyyyyyyy"],
  crickets_100: ["works", "......w.", ".....w..", "..yyyw..", ".yyyyyy.", "yykyyyy.", ".w.w.w..", "w..w..w.", "........"],
  first_firefly: ["works", "..w..w..", "...ww...", "..wkkw..", "..kkkk..", "..kyyk..", ".yyyyyy.", "y.yyyy.y", "...yy..."],
  mastered: ["works", "...y....", "...y....", "yyywyyy.", ".yywyy..", "..yyy...", ".yy.yy..", "y.....y.", "........"],
};

// A pin at s pixels a cell. One not earned yet is only its shape in the dark.
export function pinCanvas(kind, got, s) {
  const cv = document.createElement("canvas"), ctx = cv.getContext("2d"), pin = PINS[kind] || PINS.key;
  cv.width = cv.height = 12 * s;
  DISC.forEach((row, r) => {
    for (let c = 0; c < 12; c++) {
      const ch = row.charAt(c);
      if (ch === ".") continue;
      ctx.fillStyle = got ? (ch === "r" ? "#d9a520" : ENAMEL[pin[0]]) : (ch === "r" ? "#2f2820" : "#1a1612");
      ctx.fillRect(c * s, r * s, s, s);
    }
  });
  if (got) {
    ctx.fillStyle = "rgba(255,255,255,0.14)";
    ctx.fillRect(3 * s, s, 4 * s, s);
  }
  for (let r = 0; r < 8; r++) for (let c = 0; c < 8; c++) {
    const ch = pin[r + 1].charAt(c);
    if (ch === ".") continue;
    ctx.fillStyle = got ? INK[ch] : "#3a3228";
    ctx.fillRect((c + 2) * s, (r + 2) * s, s, s);
  }
  return cv;
}

export function hasPin(kind) { return !!PINS[kind]; }

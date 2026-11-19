// Twelve by twelve icons in letters off P, or off a few colours laid over it.
// The dark edge round every filled cell is added in code, and a capital with
// nothing above it is drawn lighter, so every icon is lit the same way.
const EDGE = "#0a0d16";
const P = {
  g: "#3f8a4a", G: "#6fd08c", l: "#a8e8a0", y: "#c98a2a", Y: "#ffcf6a", w: "#fff6d0", W: "#ffffff", k: "#111522",
  b: "#8a5a32", B: "#c48a4a", s: "#7f8a99", S: "#b9c3cf", r: "#a8424a", R: "#e2687a", p: "#f6b8cc",
  u: "#2f7fa8", U: "#6fc0e0", v: "#6a4ab0", V: "#a88aff", o: "#c0602a", O: "#ff9a4a", d: "#4a2e1a", D: "#6a4428", n: "#2a3a5a",
  a: "#a87a12", A: "#f2c230", t: "#2f8f8a", T: "#7fd6c2", e: "#3a4250", h: "#b8a27a", H: "#efe0b8",
  i: "#8fb8d0", I: "#d8f0fa", q: "#8a2a1a", Q: "#e2412d", z: "#24502c", m: "#8a3a63", M: "#e07ab0",
  f: "#d8781a", F: "#ffd23a", L: "#e0d4ff",
};

export const ICONS = {
  // The places and the tabs.
  i_pond: [["............", "............", "....pp......", "...pRRp.....", "....pp......", "..gggggg.g..", ".gGGGGGG.gg.", ".gGlGGGGGGg.", ".gGGGGGGGGg.", "..gggggggg..", "............", "............"]],
  i_frogs: [["............", "..ww....ww..", ".wkkw..wkkw.", ".wkkwGGwkkw.", "..wwGGGGww..", ".GGGGGGGGGG.", ".GGlGGGGGGG.", ".GGkkkkkkGG.", ".GGGGGGGGGG.", "..GGgggGGG..", ".GG..gg..GG.", "............"]],
  i_play: [["............", "....ssss....", "...sSSSSs...", "...s.YY.s...", "...sYwwYs...", "...sYwYYs...", "...sYYYYs...", "...sYYYYs...", "...sSSSSs...", "....ssss....", "............", "............"]],
  i_make: [["............", "..sss.......", ".sSSSs......", ".sSSSSs.....", "..ssbbs.....", "....bb......", "....bb...G..", "....bb..GgG.", "....bb...g..", "....bb...g..", "............", "............"]],
  i_town: [["............", ".....r......", "....rRr.....", "...rRRRr....", "..rRRRRRr...", "..wwYYwwB...", "..wwYYwwB...", "..BBYYBBB...", "..BBYYBBB...", "............", "............", "............"]],
  i_bag: [["............", "....bbbb....", "...b....b...", "..BBBBBBBB..", ".BBBBBBBBBB.", ".BBBBYYBBBB.", ".BBBBYYBBBB.", ".BBBBBBBBBB.", ".BbbbbbbbbB.", "..bbbbbbbb..", "............", "............"]],
  i_folk: [["............", "...BBBBBB...", "..BOOOOOOB..", "..BOwOOwOB..", "..BOkOOkOB..", "..BOOOOOOB..", "..BOORROOB..", "...BBBBBB...", "..VVVVVVVV..", ".VVVVVVVVVV.", ".VVVVVVVVVV.", "............"]],
  i_fish: [["............", "............", "..........u.", "....uuuu.uu.", "..uuUUUUuuu.", ".uUWkUUUUuu.", ".uUUUUUUUuu.", "..uuUUUUuuu.", "....uuuu.uu.", "..........u.", "............", "............"]],
  i_works: [["...U..R.....", "..U..R..Y...", "...U..R.Y...", "............", "..UURRYYYY..", ".DDDDDDDDDD.", ".DBBBBBBBBD.", ".DBBdddBBBD.", ".DBBBBBBBBD.", "..DDDDDDDD..", "............", "............"]],
  i_market: [["............", ".nnnnnnnnnn.", ".n........n.", ".n......G.n.", ".n.....G..n.", ".n.G..G...n.", ".n..GG....n.", ".nG.......n.", ".nnnnnnnnnn.", "....b..b....", "...bb..bb...", "............"]],
  i_bank: [["............", "............", ".....SS.....", "....SWSs....", "...SSSss.S..", "..BBSSsBBSs.", ".BBBBBBBBBB.", ".BbBBbBBbBB.", ".bbbbbbbbbb.", "............", "............", "............"]],
  i_shops: [["............", ".RwRwRwRwRw.", ".RwRwRwRwRw.", "..rwrwrwrw..", "..B......B..", "..BHHHHUUB..", "..BHHHHUUB..", "..BhhhhUUB..", ".DDDDDDDDDD.", "............", "............", "............"]],
  i_stall: [["............", ".RwRwRwRwRw.", ".rwrwrwrwrw.", ".B........B.", ".B..GG.YY.B.", ".B..GG.YY.B.", ".BBBBBBBBBB.", ".bDDDDDDDDb.", ".b........b.", ".b........b.", "............", "............"]],
  i_wizard: [["......V.....", ".....VV.....", ".....VVv....", "....VVYv....", "....VVVvv...", "...VVYVVv...", "...VVVVVvv..", "..vvvvvvvvv.", ".VVVVVVVVVV.", "............", "............", "............"]],
  i_set: [["............", ".www..YYY...", ".wWw..YwY...", ".www..YYY...", "............", ".ppp..UUU...", ".pWp..UWU...", ".ppp..UUU...", "............", "............", "............", "............"]],
  i_pin: [["............", "....AAAA....", "...AYYYYa...", "..AYGGGGYa..", "..AYGwGGYa..", "..AYGGGGYa..", "..AYGGGGYa..", "...aYYYYa...", "....aaaa....", "............", "............", "............"]],
  i_star: [["............", ".....Y......", ".....Y......", "....YwY.....", ".YYYYwYYYY..", "..YYYYYYY...", "...YYYYY....", "...YY.YY....", "..YY...YY...", "............", "............", "............"]],
  i_cam: [["............", "............", "...SSS......", ".SSSSSSSSSS.", ".SeeeeeeeeS.", ".SeeTTTTeeS.", ".SeTkkkkTeS.", ".SeeTTTTeeS.", ".SSSSSSSSSS.", "............", "............", "............"]],
  i_rest: [["............", "......YYYY..", "........Y...", ".......Y....", "......YYYY..", "..IIII......", "....I.......", "...I........", "..IIII......", "............", "............", "............"]],
  i_gear: [["............", ".....YY.....", "..Y.YYYY.Y..", "...YYYYYY...", "..YYY..YYY..", ".YYY....YYY.", ".YYY....YYY.", "..YYY..YYY..", "...YYYYYY...", "..Y.YYYY.Y..", ".....YY.....", "............"]],
  i_site: [["............", "....YYYY....", "..YY.YY.YY..", ".Y...YY...Y.", ".YYYYYYYYYY.", ".Y..Y..Y..Y.", ".YYYYYYYYYY.", ".Y...YY...Y.", "..YY.YY.YY..", "....YYYY....", "............", "............"]],
  i_hood: [["............", ".....V......", "....VVV.....", "...VVVVV....", "...VVAVV....", "..VVkkkVV...", "..VVkkkVV...", ".VVVVVVVVV..", ".vAAAAAAAv..", ".v.......v..", "............", "............"]],

  // New keys the Next strip and the tabs use.
  sprout: [["............", "...GG.GG....", "..GlG.GlG...", "...GGGGG....", ".....G......", ".....G......", "..DDDDDDDD..", ".DdDdDdDdDd.", ".dDdDdDdDdD.", "............", "............", "............"]],
  bowl: [["............", "...W..W.....", "....W..W....", "............", "..ssssssss..", ".sSSSSSSSSs.", "..sSSSSSSs..", "..sSSSSSSs..", "...ssssss...", "...OoOOoO...", "....O..O....", "............"]],
  basket: [["............", "....bbbb....", "...b....b...", "..b......b..", ".BYBYBYBYBY.", ".YBYBYBYBYB.", "..BYBYBYBY..", "..YBYBYBYB..", "...bbbbbb...", "............", "............", "............"]],
  burrow: [["............", "....GGGG....", "..GGlGGGGG..", ".GGGGGGGGGG.", ".GGGddddGGG.", ".GGdYYYYdGG.", ".GGdYwYYdGG.", ".GGdYYYYdGG.", ".gggggggggg.", "............", "............", "............"]],
  spin: [["............", "....RYRY....", "...YRYRYR...", "..RYRYRYRY..", "..YRYwwRYR..", "..RYRwwYRY..", "..YRYRYRYR..", "...RYRYRY...", "....YRYR....", ".....bb.....", "....bbbb....", "............"]],
  heart: [["............", "............", "..RR...RR...", ".RpRR.RRRR..", ".RpRRRRRRR..", ".RRRRRRRRR..", "..RRRRRRR...", "...RRRRR....", "....RRR.....", ".....R......", "............", "............"]],
  log: [["............", "............", "..DDDDDDDD..", ".DdddddddDD.", ".DdBBBdddDdD", ".DdBdBdddDdD", ".DdBBBdddDD.", "..DDDDDDDD..", "............", "............", "............", "............"]],
  key: [["............", "............", "..YYY.......", ".Y...Y......", ".Y...YYYYYY.", ".Y...Y..Y.Y.", "..YYY...Y...", "............", "............", "............", "............", "............"]],
  egg: [["............", "..lll.lll...", ".lGGl.lGGl..", ".lGkl.lGkl..", "..lll.lll...", "....lll.....", "...lGGl.....", "...lGkl.....", "....lll.....", "............", "............", "............"]],

  // The games, g_ and the game's key.
  g_fish: [["............", "........b...", ".......b.W..", "......b..W..", ".....b...W..", "....b...RWR.", "...b....RRR.", "..b......R..", ".b..........", "UUUUUUUUUUUU", "uuuuuuuuuuuu", "............"]],
  g_dive: [["............", "...UU.......", "..U..U......", "......GG....", ".....GGGG...", "....GlGGG...", "....GGGG....", "...uuGGuuu..", "..uUUUUUUUu.", "..uuuuuuuuu.", "............", "............"]],
  g_swarm: [["............", ".k.....k....", "kWk...kWk...", ".k..k..k....", "...kWk...k..", "....k...kWk.", ".k.......k..", "kWk..k......", ".k..kWk.....", ".....k......", "............", "............"]],
  g_lash: [["............", "..ww........", ".wkkw.......", ".wkkwGG.....", "..wwGGGRRRR.", "..GGGGG...R.", "..GGGGG...RR", "...GGG...k..", "............", "............", "............", "............"]],
  g_whirl: [["............", "...uuuuu....", "..u.....u...", ".u..UUU..u..", ".u.U...U.u..", ".u.U.W.U.u..", ".u.U..U..u..", ".u..UU...u..", "..u.....u...", "...uuuuu....", "............", "............"]],
  g_sling: [["............", ".D.......D..", ".D.......D..", ".DD.....DD..", "..DD...DD...", "...DDDDD....", "....DD......", "....DD...S..", "....DD..SSS.", "....DD...S..", "............", "............"]],
  g_sweep: [["............", ".SSS.SSS.SSS", ".SSS.SSS.SkS", "............", ".SSS.SkS.SSS", ".SSS.SSS.SSS", "............", ".SkS.SSS.SSS", ".SSS.SSS.SSS", "............", "............", "............"]],
  g_putt: [["............", "......R.....", "......RR....", "......RRR...", "......b.....", "......b.....", "..ggggbgggg.", ".gGGGGkGGGGg", ".gGlGGGGGGGg", "..gggggggg..", "............", "............"]],
  g_race: [["............", "..bWkWkW....", "..bkWkWk....", "..bWkWkW....", "..bkWkWk....", "..b.........", "..b.........", "..b.........", ".bbb........", "............", "............", "............"]],
  g_cross: [["............", "............", ".SSSSSSSSSS.", ".e.ee.ee.e..", ".SSSSSSSSSS.", "............", "....GGGG....", "...GwGGwG...", "...GGGGGG...", "UUUUUUUUUUUU", "uuuuuuuuuuuu", "............"]],
  g_raft: [["............", "............", "....GG......", "...GGGG.....", "...GwwG.....", ".BBBBBBBBB..", ".bBbBbBbBb..", "..........U.", ".UUU..UUU...", "UuuuUUuuuUU.", "............", "............"]],
  g_leap: [["............", ".....GG.....", "....GwwG....", "....GGGG....", "....G..G....", "............", "...gGGGg....", "..........gG", "..........gG", "..gGGGg.....", "............", "............"]],
  g_school: [["............", "............", "..SS........", ".SkSss......", "..SS..SS....", ".....SkSss..", "......SS....", "..SS........", ".SkSss......", "..SS........", "............", "............"]],
  g_stack: [["............", "....gGGGg...", "............", "...gGGGGg...", "............", "..gGGGGGGg..", "............", ".gGGGGGGGGg.", "............", "UUUUUUUUUUUU", "............", "............"]],
  g_skip: [["............", "............", "......SS....", ".....SWSs...", ".....ssss...", "............", "............", "..U...U...U.", ".U.U.U.U.U.U", "............", "............", "............"]],
  g_drop: [["............", ".....SS.....", "....SWSs....", ".....ss.....", "............", "..G.....G...", "..G..G..G...", "..G..G..G...", "..g..G..g...", ".....g......", "............", "............"]],
  g_chorus: [["............", ".......YY...", ".......Y.Y..", ".......Y....", ".....YYY....", "....YYYY....", ".....YY.....", "............", "..DDDDDDDD..", ".DdddddddDD.", "..DDDDDDDD..", "............"]],
  g_shell: [["............", "............", "............", "...BBB......", "..BBBBB.....", ".BBbBbBB....", ".bbbbbbb....", "........SSS.", ".......SWSSs", "......ssssss", "............", "............"]],
  g_jar: [["............", "...BBBBBB...", "....SSSS....", "...S....S...", "..S.lk.lk.S.", "..S.ll.ll.S.", "..S..lk...S.", "..S..ll...S.", "..S.......S.", "...SSSSSSS..", "............", "............"]],
  g_flick: [["............", "..ww........", ".wkkw.......", ".wkkwGG.....", "..wwGGGRRRRk", "..GGGGG...kW", "..GGGGG....k", "...GGG......", "............", "............", "............", "............"]],

  // Rocks.
  peb: [["............", "............", "............", "....ssss....", "...sSSSSs...", "..sSWSSSSs..", "..sSSSSSSs..", "...ssssss...", "............", "............", "............", "............"]],
  agate: [["............", "....oooo....", "...oOHHOo...", "..oOHOOHOo..", "..oHOooOHo..", "..oHOooOHo..", "..oOHOOHOo..", "...oOHHOo...", "....oooo....", "............", "............", "............"]],
  quartz: [["............", ".....L......", "....LLc.....", "....LLc.....", "...LLLc..L..", "...LLLccLLc.", "..LLLLccLLc.", "..LLLcccLcc.", "...hhhhhhhh.", "............", "............", "............"], { L: "#e6e0f2", c: "#a79fc0", h: "#6f6890" }],
  geode: [["............", "....cccc....", "...cVVVVc...", "..cVLLLLVc..", "..cVLWLLVc..", "..cVLLLLVc..", "..cVVLLVVc..", "...cVVVVc...", "....cccc....", "............", "............", "............"], { c: "#5b534a" }],
  amethyst: [["............", ".....V......", "....VLv.....", "...VVLvv....", "...VVLvv....", "..VVVLvvv...", "..VVVLvvv...", "..VVVLvvv...", "...vvvvv....", "............", "............", "............"]],
  calcite: [["............", ".....SS.....", "....SWWs....", "...SWWSss...", "..SWWSSsss..", "...SWSSss...", "....SSss....", ".....ss.....", "............", "............", "............", "............"]],
  pyrite: [["............", "............", "..AAAAA.....", "..AYYYAa....", "..AYwYAa....", "..AYYYAAAAA.", "..aaaaaAYYa.", ".......AYYa.", ".......aaaa.", "............", "............", "............"]],
  banded: [["............", "....oooo....", "...HHHHHH...", "..OOOOOOOO..", "..oooooooo..", "..HHHHHHHH..", "..OOOOOOOO..", "...oooooo...", "....HHHH....", "............", "............", "............"]],
  fossil: [["............", "...hhhhhh...", "..hHHHHHHh..", ".hHHccccHHh.", ".hHcHHHHcHh.", ".hHcHccHcHh.", ".hHcHHcccHh.", ".hHHcHHHHHh.", "..hHHHHHHh..", "...hhhhhh...", "............", "............"], { h: "#8a7a5a", H: "#e3d6b0", c: "#8a7a5a" }],
  wood: [["............", "............", ".DDDDDDDDDD.", ".DBBhBBBBBD.", ".DBhhhBBhBD.", ".DBBhBBhhBD.", ".DBBBBBBhBD.", ".DDDDDDDDDD.", "............", "............", "............", "............"]],
  moonstone: [["............", "....iiii....", "...iIIUUi...", "..iIIIIUUi..", "..iIWIIIUi..", "..iIIIIIIi..", "...iIIIIi...", "....iiii....", "............", "............", "............", "............"]],

  // Garden, larder and wheel things.
  cricket: [["............", "..........b.", "...BBBB..b..", "..BBkBBBBb..", ".BBBBBBBBB..", "..BBBBBBBBb.", "..b.b..b..b.", ".b..b..b...b", "............", "............", "............", "............"]],
  hat: [["............", "............", "....RRRR....", "...RRpRRR...", "...RRRRRR...", "...YYYYYY...", ".RRRRRRRRRR.", "..rrrrrrrr..", "............", "............", "............", "............"]],
  sticks: [["............", "..........b.", ".........bB.", "...b....bB..", "...Bb..bB...", "....BbbB....", "....bBBb....", "...bB..Bb...", "..bB....B...", ".bB.........", "............", "............"]],
  reeds: [["............", "..D...D.....", "..D..DdD....", ".DdD..D..D..", "..D...g.DdD.", "..g...g..D..", "..g..gg..g..", "..gg.g..gg..", "...gggggg...", "............", "............", "............"]],
  clay: [["............", "............", "....oooo....", "..ooOOOOoo..", ".oOOOwOOOOo.", ".oOOOOOOOOo.", ".ooOOOOOOoo.", "..oooooooo..", "............", "............", "............", "............"]],
  duckweed: [["............", "..GG........", ".GlGG...GG..", ".GGGG..GlGG.", "..GG...GGGG.", ".......gGG..", "...GG.......", "..GlGG..GG..", "..GGGG.GlGG.", "...gG..GGGG.", "............", "............"]],
  watercress: [["............", "...GG..GG...", "..GlGG.GlGG.", "..GGGgGGGG..", "...GG.gGG...", ".GG..g......", "GlGGgg......", ".GGG.g......", ".....g......", ".....g......", "............", "............"]],
  mayfly: [["............", "..II...II...", "..IWI.IWI...", "...IIhII....", ".....hh.....", ".....hh.....", "......h.....", "......h.....", ".....h.h....", "....h...h...", "............", "............"], { h: "#c9b25a" }],
  lotus: [["............", ".....pp.....", "....pMMp....", "..p.pMMp.p..", "..pMpMMpMp..", "..pMMMMMMp..", "...pMMMMp...", "..GGGGGGGG..", ".GgGGGGGGgG.", "..gggggggg..", "............", "............"]],
  marigold: [["............", "....F.F.....", "...FFFFF....", "..FFfFfFF...", "...FfffF....", "..FFfFfFF...", "...FFFFF....", "....FgF.....", ".....g..G...", ".....gGGG...", ".....g......", "............"]],
  firefly: [["............", "...S..S.....", "....SS......", "...eeee.....", "..eeEEee....", "..eYYYYe....", "..YwYYYY....", "...YYYY.....", "....YY......", "............", "............", "............"], { E: "#5a4a38", e: "#3a2e22" }],
  morel: [["............", ".....hh.....", "....hDhh....", "...hDhDhh...", "...hhDhDh...", "...hDhhDh...", "...hhDhhh...", "....hhhh....", ".....HH.....", ".....HH.....", "....HHHH....", "............"], { h: "#9a7448", D: "#5a3f24", H: "#e9dcc0" }],
  snail: [["............", "............", "...BBBB.....", "..BHHHHB....", "..BHBBHB....", "..BHBHHB..h.", "..BHHHB...h.", "...BBBhhhhh.", ".hhhhhhhhhh.", "............", "............", "............"], { h: "#cfc7a8" }],
  cap: [["............", "............", "...cccccc...", "..cSSSSSSc..", ".cSSRRRSSSc.", ".cSRRRRRSSc.", ".cSSRRRSSSc.", "..cSSSSSSc..", "...cccccc...", "............", "............", "............"], { c: "#6d747a" }],
  feather: [["............", ".........Ss.", "........SSs.", ".......SSs..", "......SSs...", ".....SSs....", "....SSs.....", "...SSs......", "..eSs.......", ".e..........", "e...........", "............"]],
  smallfry: [["............", "............", "..SSS..S....", ".SWSSSSS....", "..SSS..S....", "............", "......SSS..S", ".....SWSSSSS", "......SSS..S", "............", "............", "............"], { S: "#9fb7c0", W: "#dfe9ec" }],
  fillet: [["............", "............", "....oooo....", "..ooppppo...", ".oppPPPppo..", ".opwPPpPPpo.", ".opPPwPPPpo.", "..opPPPppo..", "...ooopoo...", "............", "............", "............"], { o: "#c9744f", P: "#f2b8a0", p: "#e8a088" }],

  // Drops, dyes and patterns are filled in below from one shape each.

  f_stones: [["............", "............", "............", "....SSSS....", "...SSWSSs...", "...ssssss...", "......SSSs..", "..SSS.SSSss.", ".SSSSsssss..", ".ssssss.....", "............", "............"]],
  f_reeds: [["............", "...D....D...", "...D...DdD..", "..DdD...D...", "...D..D.g...", "...g.DdDg...", "...g..D.g...", "...g..g.g...", "..gg..g.gg..", ".gggggggggg.", "............", "............"]],
  bait_worms: [["............", "..R...R.....", ".R.R.R.R....", "...R...R....", "..SSSSSSSS..", ".SeeeeeeeeS.", ".SeDDDDDDeS.", ".SeDdDDdDeS.", ".SeeeeeeeeS.", "..SSSSSSSS..", "............", "............"]],
  bait_lure: [["............", ".......S....", "..YYYY.S....", ".YYwYYYS....", ".YYkYYYYs...", ".aYYYYYa....", "..aaaa.S....", ".......S.S..", "........SS..", "............", "............", "............"]],

  // Trinkets.
  t_acorn: [["............", ".....d......", "...DDDDD....", "..DdDdDdD...", "..DDDDDDD...", "..BBBBBBB...", "..BHBBBBB...", "...BBBBB....", "....BBB.....", ".....B......", "............", "............"], { H: "#e3c57a" }],
  t_pinecone: [["............", ".....G......", "....DbD.....", "...DbDbD....", "...bDbDb....", "..DbDbDbD...", "..bDbDbDb...", "...DbDbD....", "....bDb.....", ".....D......", "............", "............"]],
  t_marble: [["............", "............", "....uuuu....", "...uUUUUu...", "..uUWRUUUu..", "..uUURRUUu..", "..uUUURRUu..", "..uUUUUUUu..", "...uuuuuu...", "............", "............", "............"]],
  t_button: [["............", "............", "....HHHH....", "...HHHHHH...", "..HHhHHhHH..", "..HHHHHHHH..", "..HHhHHhHH..", "...HHHHHH...", "....hhhh....", "............", "............", "............"]],
  t_boat: [["............", ".....W......", ".....WW.....", ".....WWW....", ".....WWWW...", ".....b......", "..SSSSSSSS..", "...SSSSSS...", "TTTTTTTTTTTT", "............", "............", "............"]],
  t_duck: [["............", "....FFF.....", "...FFkFF....", "...FFFFFOO..", "....FFF.....", ".F..FFFF....", ".FFFFFFFFF..", ".FFFFFFFFF..", "..fffffff...", "............", "............", "............"]],
  t_cone: [["............", ".....O......", ".....O......", "....OOO.....", "....www.....", "...OOOOO....", "...OOOOO....", "..wwwwwww...", "..OOOOOOO...", ".eeeeeeeee..", "............", "............"]],
  t_chanterelle: [["............", "............", "..FFF..FFF..", ".FffFFFFffF.", "..FFffffFF..", "...FFFFFF...", "....FFFF....", "....FFFF....", ".....FF.....", "....FFFF....", "............", "............"]],
  t_clover: [["............", "...GG.GG....", "..GlGGGlG...", "..GGGgGGG...", "...GgggG....", "..GGGgGGG...", "..GlGGGlG...", "...GG.GG....", ".....g......", "......g.....", "............", "............"]],
  t_earring: [["............", ".....YY.....", "....Y..Y....", ".....YY.....", ".....A......", "....AUA.....", "...AUUUA....", "...AUWUA....", "....AUA.....", ".....A......", "............", "............"]],
  t_globe: [["............", "...IIIIII...", "..IuuuWuuI..", "..IuWuuuuI..", "..IuuGGuuI..", "..IuGGGGuI..", "...IIIIII...", "..BBBBBBBB..", "..bbbbbbbb..", "............", "............", "............"]],
  t_gnome: [["............", ".....Q......", "....QQ......", "....QQQ.....", "...QQQQQ....", "...HHHHH....", "...HkHkH....", "...wwwww....", "..UwwwwwU...", "..UUUUUUU...", "...d...d....", "............"]],
  t_agaric: [["............", "....QQQQ....", "..QQwQQQQQ..", ".QQQQQQwQQQ.", ".QwQQQQQQwQ.", ".qqqqqqqqqq.", "....HHHH....", "....HHHH....", "....HHHH....", "...HHHHHH...", "............", "............"]],
  t_robot: [["............", ".....Q......", ".....S......", "...SSSSS....", "...SYSYS....", "...SSSSS....", "..eSSSSSe...", "..eSUUSSe...", "...SSSSS....", "...S...S....", "............", "............"]],
  t_disco: [["............", ".....e......", "....SWSS....", "...SsSsSs...", "..SsWsSsSs..", "..sSsSsWsS..", "..SsSsSsSs..", "...sSsSsS...", "....SsSs....", "............", "............", "............"]],
  t_jar: [["............", "...BBBBBB...", "....SSSS....", "...S....S...", "..S..Y...S..", "..S...Y..S..", "..S.Y....S..", "..S....Y.S..", "..S..Y...S..", "...SSSSSS...", "............", "............"]],
  t_goldmorel: [["............", ".....AA.....", "....AaAA....", "...AaAaAA...", "...AAaAaA...", "...AaAAaA...", "...AAaAAA...", "....AAAA....", ".....HH.....", ".....HH.....", "....HHHH....", "............"]],
  t_pearl: [["............", "............", "............", "....WWW.....", "...WWWWW....", "...WWWWh....", "....hhh.....", "..HHHHHHHH..", ".BHHHHHHHHB.", "..BBBBBBBB..", "............", "............"], { W: "#f6f0e6", h: "#cfc7b8" }],
  t_raft: [["............", ".....W......", ".....WW.....", ".....WWW....", ".....b......", ".BBBBBBBBBB.", ".bBbBbBbBbB.", ".BBBBBBBBBB.", "TTTTTTTTTTTT", "............", "............", "............"]],
  t_basket: [["............", "....bbbb....", "...b....b...", "..b......b..", ".BYBYBYBYBY.", ".YBYBYBYBYB.", "..BYBYBYBY..", "..YBYBYBYB..", "...bbbbbb...", "............", "............", "............"]],
  t_pot: [["............", "............", "....oooo....", ".....OO.....", "....OOOO....", "...OOOOOO...", "..OOOOOOOO..", "..OwOOOOOO..", "...OOOOOO...", "....oooo....", "............", "............"]],
  t_flute: [["............", ".........hH.", "........hHh.", ".......hkh..", "......hHh...", ".....hkh....", "....hHh.....", "...hkh......", "..hHh.......", ".hHh........", "............", "............"], { h: "#8a7a3a", H: "#d6c88f" }],
  t_statue: [["............", "...ww..ww...", "..wkkwwkkw..", "..OOOOOOOO..", ".OOOOOOOOOO.", ".OOoooooOOO.", ".OOOOOOOOOO.", "..OO....OO..", ".SSSSSSSSSS.", ".ssssssssss.", "............", "............"]],
  t_bee: [["............", "....II.II...", "....IIIII...", ".....III....", "...AAAAAA...", "..AkAkkAkA..", "..AAkAAkAA..", "...AkkAkA...", "....AAAA....", "............", "............", "............"]],
  t_ladybird: [["............", ".....kk.....", "....kkkk....", "...QQkkQQ...", "..QQkQQkQQ..", "..QQQkkQQQ..", "..QkQQQQkQ..", "..QQQkkQQQ..", "...QQQQQQ...", "....k..k....", "............", "............"]],
  t_hedgehog: [["............", "....D.D.D...", "...DDDDDDD..", "..DdDDdDDdD.", ".HDDDDDDDDD.", "HkHDDdDDDdD.", "HHHHDDDDDD..", ".HH.H...H...", "............", "............", "............", "............"], { H: "#e3c57a" }],
  t_dragonfly: [["............", ".....kk.....", "IIII.uu.IIII", ".III.uu.III.", "IIII.uu.IIII", ".....uu.....", ".....uu.....", ".....uu.....", ".....uu.....", "......u.....", "............", "............"]],
  t_newt: [["............", "............", ".OO.........", "OkOO........", "OOOOFOOO....", ".OOOOOOOOO..", ".O..O..O.OO.", "..........O.", "...........O", "............", "............", "............"]],
  t_crayfish: [["............", ".QQ......QQ.", ".Q.Q....Q.Q.", "..QQ....QQ..", "....QkkQ....", "....QQQQ....", "....QqqQ....", "....QQQQ....", ".....QQ.....", "....QQQQ....", "............", "............"]],
  t_mouse: [["............", "..hh........", ".hHHh.......", ".hHhh.......", "..hhhhhh....", ".khhhhhhh...", "..hhhhhhh...", "...h...h.p..", ".........p..", "........p...", "............", "............"], { h: "#a8865a", H: "#e9dcc0" }],

  // Keepsakes.
  k_frost: [["............", ".....I......", "...I.I.I....", "....III.....", ".IIIIWIIII..", "....III.....", "...I.I.I....", ".....I......", "............", "............", "............", "............"]],
  k_web: [["............", ".I...I...I..", "..I..I..I...", "...IIIII....", ".IIIWIWIII..", "...IIIII....", "..I..I..I...", ".I...I...I..", "............", "............", "............", "............"]],
  k_storm: [["............", "..SSSS......", ".SSSSSSSS...", "SSSSSSSSSS..", ".ssssssss...", "....YY......", "...YY.......", "....YY......", ".....Y......", "............", "............", "............"]],
  k_shell: [["............", "............", ".....H......", "....HOH.....", "...HOHOH....", "..HOHOHOH...", "..HOHOHOH...", "...HHHHH....", "....OOO.....", "............", "............", "............"]],
  k_snow: [["............", "............", "....WWWW....", "...WWWWWW...", "..WWWWWWIW..", "..WWWWWIIW..", "..WWWWIIIW..", "...WIIIII...", "....IIII....", "............", "............", "............"]],
  k_lily: [["............", "............", "....p..p....", "...pMppMp...", "....pFFp....", "...pMppMp...", ".GGGGGGGGGG.", ".gGlGGGGGGg.", "..gggggggg..", "............", "............", "............"]],
  k_penny: [["............", "....oooo....", "...oOOOOo...", "..oOOwOOOo..", "..oOOOOOOo..", "..oOOOOOOo..", "...oOOOOo...", "....oooo....", ".uuuuuuuuuu.", "..uuuuuuuu..", "............", "............"]],
  k_kite: [["............", ".....Q......", "....QYQ.....", "...QYYYQ....", "..QQQQQQQ...", "...QYYYQ....", "....QYQ.....", ".....Q......", "......w.w...", ".......w....", "............", "............"]],
  k_wing: [["............", ".II.........", ".IWII.......", "..IWWII.....", "...IWWWII...", "....IWWWWu..", ".....IIIIu..", "........uu..", "............", "............", "............", "............"]],
  k_amber: [["............", "............", "....yyyy....", "...yFFFFy...", "..yFFFFFFy..", "..yFFdFFFy..", "..yFFFFFFy..", "...yFFFFy...", "....yyyy....", "............", "............", "............"], { F: "#f2a23a", d: "#3a2a12" }],
  k_heron: [["............", "......SS....", ".....SkSS...", "......SSee..", "......S.....", ".....SS.....", "....SSSS....", "...SSSSSs...", "....SSSs....", ".....e.e....", ".....e.e....", "............"]],
  k_moon: [["............", "....WWW.....", "...WWWWW....", "..WWWIWWW...", "..WWWWWWW...", "..WIWWWWW...", "...WWWWW....", "....WWW.....", "TTTTTTTTTTTT", "............", "............", "............"]],
  k_blossom: [["............", "...p..p.....", "..pMppMp....", "...pFFp.....", "..pMppMp....", "...p.Dp.....", ".....D......", ".....DD.....", ".....D......", "............", "............", "............"]],
  k_daisy: [["............", "....W.W.....", "..W.WWW.W...", "...WWFWW....", "..WWFFFWW...", "...WWFWW....", "..W.WWW.W...", "....WgW.....", ".....g......", "....gg......", "............", "............"]],
  k_maple: [["............", ".....Q......", "..Q..Q..Q...", "..QQQQQQQ...", "...QQqQQ....", ".QQQQqQQQQ..", "..QQQqQQQ...", "....QqQ.....", ".....q......", ".....q......", "............", "............"]],
  k_holly: [["............", "..G......G..", ".GGG....GGG.", ".GlGG..GGlG.", "..GGGGGGGG..", "...GGQQGG...", "....QQwQ....", ".....QQ.....", "............", "............", "............", "............"]],
  k_spawn: [["............", "............", "..III.III...", ".IIkIIIkII..", ".IIIIIIIII..", "..III.III.I.", "....III..IkI", "...IIkII..I.", "....III.....", "............", "............", "............"]],
  k_days7: [["............", "....ssss....", "...sSSSSs...", "..sSWSSSSs..", "..sSSSSSSs..", "..sSSSSSSs..", "...ssssss...", "............", "............", "............", "............", "............"]],
  k_days30: [["............", ".HHHHHHHHHH.", ".HH.......H.", ".H....G...H.", ".H...GGG..H.", ".H....b...H.", ".H....b...H.", ".H...bbb..H.", ".HHHHHHHHHH.", "............", "............", "............"]],
  k_days100: [["............", "....hhhh....", "....hDDh....", "...I....I...", "..I......I..", "..ITTTTTTI..", "..ITtTTTTI..", "..ITTTtTTI..", "..ITTTTTTI..", "...IIIIII...", "............", "............"], { h: "#cfe3ea" }],
  k_days365: [["............", "...DDDDDD...", "..DHHHHHHD..", ".DHBBBBBBHD.", ".DHBDDDDBHD.", ".DHBDBBDBHD.", ".DHBDDDDBHD.", ".DHBBBBBBHD.", "..DHHHHHHD..", "...DDDDDD...", "............", "............"], { H: "#e3c57a" }],

  // Mementos.
  m_float: [["............", ".....b......", ".....b......", "....QQQ.....", "...QQQQQ....", "...WWWWW....", "....WWW.....", ".....b......", ".....b......", "UUUUUUUUUUUU", "............", "............"]],
  m_notebook: [["............", "..dmmmmmm...", "..dMMMMMM...", "..dMwwwwM...", "..dMMMMMM...", "..dMppppM...", "..dMMMMMM...", "..dMMMMMM...", "..dmmmmmm...", "............", "............", "............"]],
  m_gizmo: [["............", "..b.....b...", "...b...b....", "....b.b.....", ".....W......", "....b.b.....", "...b...b.SS.", "..b.....bSS.", "........sss.", "............", "............", "............"]],
  m_bag: [["............", "....DDDD....", "...D....D...", "..GGGGGGGG..", ".GgggggggG..", ".GGGGYYGGG..", ".GGGGYYGGG..", ".GGGGGGGGG..", "..gggggggg..", "............", "............", "............"]],
  m_trowel: [["............", ".........bb.", "........bb..", ".......bb...", "......ss....", ".....sSs....", "....sSSs....", "...sSSs.....", "..sSSs......", "..sss.......", "............", "............"]],
  m_mud: [["............", "...BBBBBB...", "...BBBBBB...", "..I......I..", "..IDDDDDDI..", "..IDdDDDDI..", "..IDDDdDDI..", "..IDDDDDDI..", "...IIIIII...", "............", "............", "............"]],
  m_spoon: [["............", "..BBB.......", ".BHHHB......", ".BHHHB......", ".BHHHB......", "..BBBb......", ".....b......", "......b.....", ".......b....", "........b...", "............", "............"], { H: "#e3c57a" }],
  m_leaf: [["............", "........GG..", "......GGGG..", "....GGGlGG..", "...GGGlGGG..", "..GGGlGGGG..", "..GGlGGGG...", "..GlGGGG....", "..lGGG......", ".D..........", "............", "............"]],
  m_medal: [["............", "..Q.....U...", "...Q...U....", "....Q.U.....", ".....A......", "....AAAA....", "...AAYYAA...", "...AYwYYA...", "...AAYYAA...", "....AAAA....", "............", "............"]],
  m_poem: [["............", "..hhhhhhhh..", ".hHHHHHHHHh.", "..HddddddH..", "..HHHHHHHH..", "..HdddddHH..", "..HHHHHHHH..", "..HddddHHH..", ".hHHHHHHHHh.", "..hhhhhhhh..", "............", "............"]],
  m_lantern: [["............", ".....ee.....", "....e..e....", "...eeeeee...", "...SYYYwS...", "...SYYwYS...", "...SYYYYS...", "...SYYYYS...", "...eeeeee...", "....eeee....", "............", "............"]],
  m_shiny: [["............", ".....Y......", ".....Y......", "....YwY..I..", ".YYYwWwYYY..", "....YwY.....", ".....Y......", "..I..Y......", "............", "............", "............", "............"]],

  mastery: [["............", "..AAAAAAAA..", ".AAYYYYYYAA.", ".A.AYwYYA.A.", "..AAYYYYAA..", "....AYYA....", ".....AA.....", ".....AA.....", "....aaaa....", "...AAAAAA...", "............", "............"]],
};

// One shape each for things that only differ by colour. c is the main colour
// and d its shade.
const DROP = ["............", ".....SS.....", ".....ss.....", "....cccc....", "...cCCCCc...", "...cWCCCc...", "...cCCCCc...", "...cCCCCc...", "....cccc....", "............", "............", "............"];
const DYE = ["............", ".....SS.....", ".....ss.....", "....CCCC....", "...CCCCCC...", "...CWCCCC...", "...CWCCCC...", "...CCCCCC...", "....dddd....", "............", "............", "............"];
const DYES = { blue: "#3d8fd6", gold: "#f2c230", berry: "#e2567a", olive: "#8a9a3a", brown: "#8a5a2b", grey: "#9aa0a6" };
const DROPS = { red: "#e2412d", pale: "#9fd8e8" };
Object.entries(DYES).forEach(([k, c]) => { ICONS["dye_" + k] = [DYE, { C: c, d: darker(c) }]; });
Object.entries(DROPS).forEach(([k, c]) => { ICONS["drops_" + k] = [DROP.map((r) => r.replace(/c/g, "d")), { C: c, d: darker(c) }]; });

// A pattern is a stamp, its face cut in the pattern it prints.
const STAMP_TOP = ["............", "....DD......", "....DD......", "..BBBBBB....", "..BBBBBB...."];
const STAMP_FACE = {
  spots: [".GGGGGGGG...", ".GgGGGgGG...", ".GGGgGGGg...", ".gGGGGgGG..."],
  bands: [".GGGGGGGG...", ".gggggggg...", ".GGGGGGGG...", ".gggggggg..."],
  speckled: [".GgGgGGgG...", ".GGgGgGGg...", ".gGGgGgGG...", ".GgGGgGGg..."],
  mask: [".GGGGGGGG...", ".gggGGggg...", ".gggGGggg...", ".GGGGGGGG..."],
  stripe: [".GGGHHGGG...", ".gggHHggg...", ".GGGHHGGG...", ".gggHHggg..."],
};
Object.entries(STAMP_FACE).forEach(([k, face]) => { ICONS["stamp_" + k] = [[...STAMP_TOP, ...face, "............", "............", "............"]]; });

// The six stamps a frog's page can be pressed with, st_ and the stamp.
ICONS.st_heart = ICONS.heart;
ICONS.st_star = ICONS.i_star;
ICONS.st_pebble = ICONS.peb;
ICONS.st_fly = [["............", "..II....II..", ".IWII..IIWI.", ".IIIIeeIIII.", "..IIReeRII..", "....eeee....", "...eeeeee...", "....eeee....", "...e.ee.e...", "....e..e....", "............", "............"]];
ICONS.st_wave = [["............", "............", "..TT....TT..", ".TWTT..TWTT.", "TT..TTTT..TT", "............", "..UU....UU..", ".UWUU..UWUU.", "UU..UUUU..UU", "............", "............", "............"]];
ICONS.st_leaf = [["............", "..........z.", "......GGGz..", "....GGGGGz..", "...GGlGGzG..", "..GGlGGzGG..", "..GlGGzGGG..", "..GGGzGGG...", "...GzGGG....", "..z.GGG.....", ".z..........", "............"]];

// A frame is a picture in a border, and each kind changes the border.
const PIC = ["..OOOO.O..", "..OOOOOO..", "rrrOOOOO..", "rrrrrOOO..", "gggggggggg"];
function framed(edge, inner) {
  return ["............", edge.repeat(12).slice(0, 12), edge + inner.repeat(10) + edge, ...PIC.map((r) => edge + inner + r.slice(1, 9) + inner + edge), edge + inner.repeat(10) + edge, edge.repeat(12).slice(0, 12), "............", "............"];
}
ICONS.pc_gilt = [framed("A", "Y")];
ICONS.pc_lily = [framed("G", "g").map((r, i) => (i === 1 ? "GGGpGGGGGpGG" : r))];
ICONS.pc_night = [framed("n", "e").map((r, i) => (i === 2 ? "neYeeeeeeYen" : r))];
ICONS.pc_stamp = [["............", "..w.w.w.w...", ".wwwwwwwww..", "..wGGGGGw...", ".wwGkGGkGww.", "..wGGGGGw...", ".wwGlllGww..", "..wGGGGGw...", ".wwwwwwwww..", "..w.w.w.w...", "............", "............"]];

// A job's tool is one shape a job, its working part in the tier's metal: a
// twig or plain wood, then iron, then brass. Each is tool_<job>_<tier>.
const TOOL_SHAPES = {
  bugs: ["............", "...AAAAA....", "..A.a.a.A...", "..Aa.a.aA...", "..A.a.a.A...", "...AAAAA....", ".....B......", "......B.....", ".......B....", "........B...", ".........B..", "............"],
  pebbles: ["............", "............", "............", ".B........B.", ".BAAAAAAAAB.", "..ATTTTTTA..", "..AtTtTTtA..", "...AAAAAA...", "............", "............", "............", "............"],
  garden: ["............", "...BBB......", "..B...B.....", ".AAAAAA...A.", ".AAAAAAA.A..", ".AAAAAAAA...", ".aAAAAAA....", ".aaaaaa.....", "............", "............", "............", "............"],
  sticks: ["............", "...AAA......", "..AAAAB.....", "..AAa.B.....", "......B.....", "......B.....", "......B.....", "......B.....", "......b.....", "............", "............", "............"],
  reeds: ["............", "....AAAA....", "...A....A...", ".........A..", ".........A..", "........A...", ".......A....", "......B.....", ".....B......", "....B.......", "............", "............"],
  mud: [".....BB.....", "....B..B....", ".....BB.....", "......B.....", "......B.....", ".....AAA....", ".....AAA....", ".....AAA....", ".....aAa....", "......a.....", "............", "............"],
  craft: ["............", "..AAAAAA....", "..AAAAAA....", "..aaBaaa....", "....B.......", "....B.......", "....B.......", "....B.......", "....b.......", "............", "............", "............"],
};
const TOOL_METAL = [["#c9974f", "#8a6230"], ["#cfd3d6", "#7f8a99"], ["#f2c230", "#a87a12"]];
Object.entries(TOOL_SHAPES).forEach(([job, rows]) => TOOL_METAL.forEach(([A, a], i) => {
  ICONS["tool_" + job + "_" + (i + 1)] = [rows, { A, a }];
}));

function darker(hex) {
  const n = parseInt(hex.slice(1), 16), f = (v) => Math.round(v * 0.65);
  return "#" + [n >> 16, (n >> 8) & 255, n & 255].map((v) => f(v).toString(16).padStart(2, "0")).join("");
}
function lighter(hex) {
  const n = parseInt(hex.slice(1), 16), f = (v) => Math.round(v + (255 - v) * 0.25);
  return "#" + [n >> 16, (n >> 8) & 255, n & 255].map((v) => f(v).toString(16).padStart(2, "0")).join("");
}

// The finished colour of each cell, the edge and the light worked out once
// per icon and kept, since scenes draw icons every frame.
const cache = {};
function cellsOf(rows, pal) {
  const at = (x, y) => x >= 0 && y >= 0 && x < 12 && y < 12 && rows[y].charAt(x) !== "." && rows[y].charAt(x) !== "";
  const out = [];
  for (let y = 0; y < 12; y++) for (let x = 0; x < 12; x++) {
    let c = null;
    if (at(x, y)) {
      const ch = rows[y].charAt(x);
      c = (pal && pal[ch]) || P[ch];
      if (c && ch !== ch.toLowerCase() && !at(x, y - 1)) c = lighter(c);
    } else if (at(x - 1, y) || at(x + 1, y) || at(x, y - 1) || at(x, y + 1)) c = EDGE;
    out.push(c);
  }
  return out;
}
export function iconCells(key) {
  if (!ICONS[key]) key = "peb";
  if (!cache[key]) cache[key] = cellsOf(ICONS[key][0], ICONS[key][1]);
  return cache[key];
}

// The two colours an ingredient is mostly made of, for the dishes.
function mainColours(key) {
  const n = {};
  ICONS[key || "peb"][0].join("").split("").forEach((ch) => { if (ch !== ".") n[ch] = (n[ch] || 0) + 1; });
  const pal = ICONS[key][1] || {}, top = Object.keys(n).sort((a, b) => n[b] - n[a]);
  const col = (ch) => (ch && (pal[ch] || P[ch])) || "#9aa0a6";
  return [col(top[0]), col(top[1] || top[0])];
}

// A dish is drawn as a bowl, a plate or a skewer in its two ingredients'
// colours, with sparkles on the better ones.
const DISH_SHAPES = {
  bowl: ["............", "............", "....s..s....", "..xxyyxyxy..", ".SSSSSSSSSS.", ".sSSSSSSSSs.", "..sSSSSSSs..", "...ssssss...", "............", "............", "............", "............"],
  plate: ["............", "............", "....xxyy....", "...xyxyxyx..", "...yxyxyxy..", "....yyxx....", ".SSSSSSSSSS.", "..ssssssss..", "............", "............", "............", "............"],
  stick: ["............", ".........B..", "........xx..", ".......xxx..", "......yyy...", ".....yyy....", "....xxx.....", "...xxx......", "..B.........", ".B..........", "............", "............"],
};

export function drawDish(ctx, d, x, y, s) {
  const shape = /skewer/i.test(d.name) ? "stick" : /soup|stew|tea|broth|jam/i.test(d.name) ? "bowl" : "plate";
  const [xa] = mainColours(ICONS[d.a] ? d.a : "peb"), [ya, yb] = mainColours(ICONS[d.b] ? d.b : "peb");
  const cells = cellsOf(DISH_SHAPES[shape], { x: xa, y: ya === xa ? yb : ya, s: "#7f8a99", S: "#d5d8dc", B: "#c48a4a" });
  cells.forEach((c, i) => { if (c) { ctx.fillStyle = c; ctx.fillRect(x + (i % 12) * s, y + Math.floor(i / 12) * s, s, s); } });
  if (d.tier >= 3) {
    ctx.fillStyle = "#fff6d0";
    ctx.fillRect(x, y + s, s, s); ctx.fillRect(x + 11 * s, y, s, s);
    if (d.tier === 4) ctx.fillRect(x + 10 * s, y + 9 * s, s, s);
  }
}

export function dishCanvas(d, s) {
  const cv = document.createElement("canvas");
  cv.width = cv.height = 12 * s;
  drawDish(cv.getContext("2d"), d, 0, 0, s);
  return cv;
}

// The wheel names its slices in three letters.
export const SLICE_ICON = { P25: "peb", P40: "peb", CRI: "cricket", AGA: "agate", QUA: "quartz", GEO: "geode", HAT: "hat", DYE: "dye_blue" };
export const SLICE_NAME = { P25: "25 pebbles", P40: "40 pebbles", CRI: "Cricket", AGA: "River agate", QUA: "Smoky quartz", GEO: "Geode", HAT: "Hat of the day", DYE: "Dye of the day" };

export function drawIcon(ctx, key, x, y, s) {
  iconCells(key).forEach((c, i) => { if (c) { ctx.fillStyle = c; ctx.fillRect(x + (i % 12) * s, y + Math.floor(i / 12) * s, s, s); } });
}

export function iconCanvas(key, s) {
  const cv = document.createElement("canvas");
  cv.width = cv.height = 12 * s;
  drawIcon(cv.getContext("2d"), key, 0, 0, s);
  return cv;
}

// Tansy's furniture, drawn bigger than an icon on the pond itself.
export const FURN = {
  f_lilypad: [["....gggggg....", "..gGGGGGGGGg..", ".gGGGGGpGGGGg.", "gGGGGGpPpGGGGg", ".gGGGGGpGGGGg.", "..gggggg.ggg.."],
    { g: "#1f5a2c", G: "#56b45f", p: "#f59ab3", P: "#fff6dc" }],
  f_teacup: [["..wwwwwwww...", ".wbbbbbbbbw..", ".wccccckccwhh", ".wppppkpppw.h", "..wccckccw.h.", "...wcccwwhh..", "....wwww....."],
    { w: "#e9e3d6", b: "#7fd6c2", c: "#fff6dc", p: "#e2567a", k: "#6d747a", h: "#e9e3d6" }],
  f_toadstool: [["...rrrr...", ".rrwrrrrr.", "rrrrrrrwrr", "rwrrrrrrrr", ".rrrrwrrr.", "...cccc...", "...cccc...", "...ccdc...", "..cccccc.."],
    { r: "#e2412d", w: "#fff6dc", c: "#e9dcc0", d: "#c9b896" }],
  f_lantern: [["..kk..", ".k..k.", ".kkkk.", "wjjjjw", "wjyyjw", "wjyyjw", "wjjjjw", ".wwww."],
    { k: "#6d747a", w: "#cfe3ea", j: "#9fd8e8", y: "#ffe27a" }],
};

export function drawFurn(ctx, key, x, y, s, lit) {
  const [rows, col] = FURN[key];
  rows.forEach((row, r) => {
    for (let c = 0; c < row.length; c++) {
      const ch = row.charAt(c);
      if (ch === ".") continue;
      ctx.fillStyle = ch === "y" && !lit ? "#c9b25a" : col[ch];
      ctx.fillRect(x + c * s, y + r * s, s, s);
    }
  });
}

// Stones and bulrushes are drawn by hand on the pond, so the shop has icons.
export function furnCanvas(key) {
  if (!FURN[key]) return iconCanvas(key, 2);
  const rows = FURN[key][0], cv = document.createElement("canvas");
  cv.width = rows[0].length * 2; cv.height = rows.length * 2;
  drawFurn(cv.getContext("2d"), key, 0, 0, 2, true);
  return cv;
}

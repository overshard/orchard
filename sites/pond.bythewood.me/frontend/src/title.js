import { state, titled } from "./api.js";
import { DISKS } from "./play.js";

// The tab says where your frog is, with the count of things waiting in front
// like a mail tab, and calls after you when the tab is left.
const AT = {
  play: "at the arcade",
  dive: "diving",
  race: "in today's race",
  wheel: "at the wheel",
  bag: "rummaging in the bag",
  collect: "admiring the collection",
  reeds: "in the reeds",
  job: "at work",
  garden: "in the garden",
  bank: "at the bank",
  market: "at the pond market",
  neighbours: "visiting the neighbours",
  shops: "out shopping",
  stall: "minding the stall",
  wizard: "at the wizard's",
};
const POND = { egg: "is still an egg", tadpole: "swimming laps", froglet: "on the pad", frog: "on the pad", elder: "dozing on the pad" };
const CALL = { tadpole: "Blub?", froglet: "Ribbit?", frog: "Ribbit?", elder: "Ribbit?" };
DISKS.forEach((d) => { if (d.game) AT[d.key] = "playing " + d.name; });

let place = "pond", waiting = 0;

function draw() {
  const f = state.pond && state.pond.frog;
  if (!f) return;
  const name = titled(f.name);
  let line;
  if (document.hidden && CALL[f.stage]) line = CALL[f.stage] + " " + name;
  else if (place === "settings") line = "Settings";
  else line = name + " " + (AT[place] || POND[f.stage] || "on the pad");
  document.title = (waiting > 0 ? "(" + waiting + ") " : "") + line + " · pond";
}

export function titlePlace(p) { place = p; draw(); }
export function titleWaiting(n) { waiting = n; draw(); }

document.addEventListener("visibilitychange", draw);

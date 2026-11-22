// gate's own page behaviour. Every form posts without it, the test, the rule
// builder's live count and the countdown just don't run.
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
const num = (n) => Number(n).toLocaleString();

const test = async (fields) => {
  const res = await fetch("/rules/test", { method: "POST", credentials: "same-origin", body: new URLSearchParams(fields) });
  if (!res.ok) throw new Error(`http ${res.status}`);
  return res.json();
};

const bars = (title, rows, kind) => {
  if (!rows || !rows.length) return "";
  const top = Math.max(...rows.map((r) => r.n));
  return `<div><h4>${title}</h4>` + rows.map((r) =>
    `<div class="o-hbar" data-tip="${esc(r.k)}"><span class="k o-mono">${esc(r.k)}</span><span class="bar"><i class="${kind}" style="width:${Math.max(2, (r.n / top) * 100).toFixed(0)}%"></i></span><span class="v">${num(r.n)}</span></div>`).join("") + "</div>";
};

// ------------------------------------------------------------ the rule test

const form = $("[data-rule-form]");
const panel = $("[data-test]");

if (form && panel) {
  const out = $("[data-test-out]", panel);
  const pm = $("[data-test-pm]", panel);
  const lock = $("[data-lockout]", form);
  const lockList = $("[data-lockout-list]", form);
  const watching = form.elements.watching;
  let timer, asked = 0, last = null;

  const action = () => (form.querySelector('input[name="action"]:checked') || {}).value || "block";

  const showLock = () => {
    if (last) lockList.innerHTML = last.lockout.map((x) => `<code>${esc(x)}</code>`).join(", ");
    lock.hidden = watching.checked || !lockList.textContent.trim();
  };

  const draw = (r) => {
    if (r.error) {
      pm.innerHTML = `<span>doesn't parse</span>`;
      out.innerHTML = `<div class="o-note bad"><span class="o-lab plain o-bad">won't save</span><span>${esc(r.error)}</span></div>`;
      return;
    }
    const kind = action() === "block" ? "bad" : action() === "allow" ? "ok" : "warn";
    const share = r.total ? (100 * r.matched) / r.total : 0;
    pm.innerHTML = `<span>caught<b class="${r.matched ? kind : ""}">${num(r.matched)}</b></span><span>of<b>${num(r.total)}</b></span>` +
      `<span>share<b class="${share > 20 ? "bad" : ""}">${share.toFixed(1)}%</b></span><span>addresses<b>${num(r.addresses)}</b></span><span>runs<b>${r.runs_at}${r.runs_at === 1 ? "st" : r.runs_at === 2 ? "nd" : r.runs_at === 3 ? "rd" : "th"}</b></span>`;
    const w = [];
    if (r.lockout.length) w.push(["bad", "signs you out", `Turned on, this refuses signing in to orchard at ${r.lockout.join(", ")}.`]);
    if (r.session) w.push(["bad", "signed in", `${num(r.session)} of these carried an orchard session, so it catches you or a tab you left open.`]);
    if (r.solved) w.push(["warn", "solved", `${num(r.solved)} of these passed with a solved challenge, so they're probably people.`]);
    if (r.already_n) w.push(["", "overlap", `${num(r.already_n)} of these were already refused by ${r.already.map((a) => `${a.k} (${a.n})`).join(", ")}.`]);
    if (action() === "limit") w.push(["", "limit", `${num(r.over_rate)} of these would have gone over the rate and been refused.`]);
    if (r.headers) w.push(["warn", "headers", "The hour doesn't keep headers, so a header line never matches here. It will on real requests."]);
    if (!r.total) w.push(["", "empty", "Nothing checked since gate last started, so there's nothing to test against yet."]);
    else if (r.minutes < 60) w.push(["", "short", `The ring holds the last ${r.minutes} ${r.minutes === 1 ? "minute" : "minutes"}, since gate started then.`]);
    if (r.matched && !w.some((x) => x[0])) w.push(["ok", "clear", "Nothing it catches was signed in, solved a challenge or signs in to orchard."]);
    const top = Math.max(1, ...r.by_minute);
    const cls = { bad: "o-bad", warn: "o-warn", ok: "o-ok", "": "o-muted" };
    out.innerHTML =
      `<div class="gmin">${r.by_minute.map((n, i) => `<i class="${n ? "m " + kind : ""}" style="height:${n ? Math.max(6, (n / top) * 100) : 4}%" data-tip="${60 - i} minutes ago: ${n} caught"></i>`).join("")}</div>` +
      `<div class="gmin-ax"><span>an hour ago</span><span>30m</span><span>now</span></div>` +
      `<div class="gfac">${bars("hosts", r.hosts, kind)}${bars("networks", r.networks, kind)}${bars("paths", r.paths, kind)}${bars("user agents", r.uas, kind)}</div>` +
      `<div class="gwarn">${w.map((x) => `<div class="${x[0]}"><span class="o-lab plain ${cls[x[0]]}">${x[1]}</span><span>${esc(x[2])}</span></div>`).join("")}</div>`;
  };

  const run = async () => {
    const n = ++asked;
    try {
      const r = await test(new FormData(form));
      if (n !== asked) return;
      last = r;
      draw(r);
      showLock();
    } catch (e) {
      if (n === asked) pm.innerHTML = `<span>test failed, ${esc(e.message)}</span>`;
    }
  };
  const soon = () => { clearTimeout(timer); timer = setTimeout(run, 250); };

  form.addEventListener("input", soon);
  form.addEventListener("change", (e) => { if (e.target === watching) showLock(); else soon(); });
  showLock();
  run();
}

// ------------------------------------------------------------ rule from an event

for (const b of $$("[data-build]")) {
  const go = $("[data-build-go]", b);
  const say = $("[data-build-out]", b);
  let timer, asked = 0;
  const when = () => $$('input[type="checkbox"]:checked', b).map((i) => i.value).join("\n");
  const count = async () => {
    const n = ++asked;
    const w = when();
    go.href = "/rules/new?when=" + encodeURIComponent(w);
    if (!w) { say.textContent = "Tick at least one line."; return; }
    try {
      const r = await test({ name: "draft", when: w, action: "block" });
      if (n !== asked) return;
      say.textContent = r.error ? r.error : `${num(r.matched)} in the last hour from ${num(r.addresses)} ${r.addresses === 1 ? "address" : "addresses"}` + (r.lockout.length ? ", and it catches signing in" : "");
    } catch (e) { say.textContent = ""; }
  };
  b.addEventListener("change", () => { clearTimeout(timer); timer = setTimeout(count, 150); });
  // Count only once the drawer opens, since a page holds sixty of them.
  const row = b.closest("tr");
  new MutationObserver(() => { if (!row.hidden && !say.textContent) count(); }).observe(row, { attributes: true, attributeFilter: ["hidden", "class"] });
}

// ------------------------------------------------------------ mode countdown

const left = (until) => {
  const s = until - Date.now() / 1000;
  if (s < 60) return "under a minute";
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
  return h ? `${h}h ${m}m` : `${m}m`;
};
const ticking = $$("[data-until]").filter((el) => Number(el.dataset.until) > 0);
if (ticking.length) {
  setInterval(() => {
    for (const el of ticking) {
      const u = Number(el.dataset.until);
      if (u <= Date.now() / 1000) { location.reload(); return; }
      el.textContent = left(u);
    }
  }, 20000);
}

// ------------------------------------------------------------ event drawers

// The time in each event row is a real button for its drawer, so Tab reaches
// it. The suite's row click and j/k toggle the drawer too, so the button
// follows the drawer's hidden attribute.
for (const b of $$("button.evx[aria-controls]")) {
  const d = document.getElementById(b.getAttribute("aria-controls"));
  if (!d) continue;
  const sync = () => b.setAttribute("aria-expanded", String(!d.hidden));
  new MutationObserver(sync).observe(d, { attributes: true, attributeFilter: ["hidden"] });
  b.addEventListener("click", () => {
    d.hidden = !d.hidden;
    d.previousElementSibling?.setAttribute("aria-expanded", String(!d.hidden));
  });
  sync();
}

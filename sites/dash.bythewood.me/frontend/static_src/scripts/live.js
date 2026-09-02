// The page is rendered once by Go and then patched from the state that arrives
// over /events. Both sides render the same JSON, so the markup built here has
// to match templates/home.html and partials.html.

const live = document.querySelector("[data-live]");
const liveLabel = document.querySelector("[data-live-label]");
const guardLine = document.querySelector("[data-guard-line]");

function connection(state, label) {
  if (!live) return;
  live.dataset.state = state;
  if (liveLabel) liveLabel.textContent = label;
}

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

// Every link on this page points somewhere else, so they all get the same
// treatment rather than each caller remembering.
function link(href, className, text) {
  const a = el("a", className, text);
  a.href = href;
  a.rel = "noopener noreferrer";
  a.target = "_blank";
  return a;
}

// Every frame carries the whole state, so without this a market poll every
// thirty seconds rebuilt the DOM of the weather, the earnings and the store
// listings too. Rebuilding a panel empties it for a moment before refilling it,
// which is the blank flash: the panel collapses to no height and comes back.
const rendered = new Map();

function changed(key, value) {
  const next = JSON.stringify(value ?? null);
  if (rendered.get(key) === next) return false;
  rendered.set(key, next);
  return true;
}

// setText writes only when the value actually differs, so an unchanged figure
// never touches the DOM and the browser never re-lays it out.
function setText(node, value) {
  if (!node) return false;
  const next = value === undefined || value === null ? "" : String(value);
  if (node.textContent === next) return false;
  node.textContent = next;
  return true;
}

function setAttr(node, name, value) {
  if (!node) return;
  const next = value === undefined || value === null ? "" : String(value);
  if (node.getAttribute(name) !== next) node.setAttribute(name, next);
}

const SVG = "http://www.w3.org/2000/svg";

function svg(tag, attrs) {
  const node = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  return node;
}

function sparkNode(spark) {
  const root = svg("svg", {
    class: "spark",
    viewBox: "0 0 100 32",
    preserveAspectRatio: "none",
    "aria-hidden": "true",
    focusable: "false",
  });
  if (!spark || !spark.line) return root;

  root.append(svg("path", { class: "spark-area", d: spark.area }));
  if (spark.has_base) {
    root.append(
      svg("line", {
        class: "spark-base",
        x1: 0,
        x2: 100,
        y1: spark.baseline,
        y2: spark.baseline,
      }),
    );
  }
  root.append(svg("path", { class: "spark-line", d: spark.line }));
  if (spark.closed) {
    root.append(
      svg("rect", { class: "spark-dead", x: spark.span, y: 0, width: spark.dead_width, height: 32 }),
      svg("line", { class: "spark-shut", x1: spark.span, x2: spark.span, y1: 0, y2: 32 }),
    );
  } else if (spark.partial) {
    root.append(
      svg("line", { class: "spark-now", x1: spark.span, x2: spark.span, y1: 0, y2: 32 }),
    );
  }
  return root;
}

function cardNode(card) {
  const article = el("article", "card");
  article.dataset.dir = card.direction || "flat";
  article.dataset.key = card.key;

  const header = el("header");
  header.append(el("h3", null, card.label));
  header.append(el("span", "sym", card.note || card.symbol));
  article.append(header);

  if (card.unavailable) {
    article.append(el("p", "price dim", "NO SIGNAL"));
    const move = el("p", "move");
    move.append(el("span", "pts", "—"));
    article.append(move);
  } else {
    const price = el("p", "price", card.price);
    price.dataset.price = "";
    article.append(price);

    const move = el("p", "move");
    move.append(el("span", "glyph"));
    move.append(el("span", "pts", card.change));
    move.append(el("span", "pct", card.percent));
    article.append(move);
  }

  article.append(sparkNode(card.spark));
  return article;
}

function flash(node) {
  node.classList.remove("tick");
  // Reading the layout restarts the animation on a node that is still mid
  // flash from the previous poll.
  void node.offsetWidth;
  node.classList.add("tick");
  node.addEventListener("animationend", () => node.classList.remove("tick"), {
    once: true,
  });
}

// patchCard writes the figures onto a card that is already on the page, so a
// card whose price did not move is left completely alone.
function patchCard(card, data) {
  card.dataset.dir = data.direction || "flat";

  setText(card.querySelector("h3"), data.label);
  setText(card.querySelector(".sym"), data.note || data.symbol);

  const price = card.querySelector("[data-price]");
  if (price && setText(price, data.price)) flash(price);

  setText(card.querySelector(".pts"), data.change);
  setText(card.querySelector(".pct"), data.percent);

  const spark = data.spark || {};
  setAttr(card.querySelector(".spark-area"), "d", spark.area);
  setAttr(card.querySelector(".spark-line"), "d", spark.line);

  const base = card.querySelector(".spark-base");
  if (base && spark.has_base) {
    setAttr(base, "y1", spark.baseline);
    setAttr(base, "y2", spark.baseline);
  }

  // The cursor appears when a session opens and goes when it fills the card, and
  // it swaps for the closed marking when that card's market shuts while the rest
  // of the strip keeps going, so all three have to be added and removed rather
  // than only moved.
  const svgRoot = card.querySelector(".spark");
  if (!svgRoot) return;

  const marker = (cls, make) => {
    let node = card.querySelector("." + cls);
    if (!node) {
      node = make();
      svgRoot.append(node);
    }
    return node;
  };
  const drop = (cls) => card.querySelector("." + cls)?.remove();

  if (spark.closed) {
    drop("spark-now");
    const dead = marker("spark-dead", () =>
      svg("rect", { class: "spark-dead", y: 0, height: 32 }),
    );
    setAttr(dead, "x", spark.span);
    setAttr(dead, "width", spark.dead_width);

    const shut = marker("spark-shut", () =>
      svg("line", { class: "spark-shut", y1: 0, y2: 32 }),
    );
    setAttr(shut, "x1", spark.span);
    setAttr(shut, "x2", spark.span);
    return;
  }

  drop("spark-dead");
  drop("spark-shut");

  if (spark.partial) {
    const now = marker("spark-now", () =>
      svg("line", { class: "spark-now", y1: 0, y2: 32 }),
    );
    setAttr(now, "x1", spark.span);
    setAttr(now, "x2", spark.span);
  } else {
    drop("spark-now");
  }
}

// The cards are only rebuilt when the set of them changes, which happens at a
// session boundary when the strip swaps to futures and at no other time.
function patchCards(container, cards) {
  const existing = Array.from(container.children);
  const sameSet =
    existing.length === cards.length &&
    cards.every((c, i) => existing[i].dataset.key === c.key) &&
    cards.every((c, i) => Boolean(c.unavailable) === !existing[i].querySelector("[data-price]"));

  if (!sameSet) {
    container.replaceChildren(...cards.map(cardNode));
    return;
  }
  cards.forEach((c, i) => {
    if (!c.unavailable) patchCard(existing[i], c);
  });
}

// The tab carries the market, so a dash left open in another window still says
// what it is doing. The base wording comes off the body rather than being kept
// here as a second copy of a string the server already renders.
function setTabTitle(ticker) {
  const base = document.body.dataset.titleBase;
  if (!base) return;
  const next = ticker ? `${ticker} · ${base}` : base;
  if (document.title !== next) document.title = next;
}

function renderMarket(market) {
  if (!market) return;

  setTabTitle(market.ticker);

  const cards = document.querySelector("[data-market-cards]");
  if (cards && market.cards) patchCards(cards, market.cards);

  const session = document.querySelector(".markets .session");
  if (session) session.dataset.session = market.session || "";

  const badge = document.querySelector("[data-market-session]");
  if (badge) badge.textContent = market.session || "";

  const phase = document.querySelector("[data-market-phase]");
  if (phase) phase.textContent = market.phase || "";

  const drawdown = document.querySelector("[data-market-drawdown]");
  if (drawdown) drawdown.textContent = market.drawdown || "—";
}

function storyNode(story, i) {
  const li = el("li");
  li.append(el("span", "rank", String(i + 1).padStart(2, "0")));

  const body = el("div", "body");
  body.append(link(story.url, "title", story.title));

  const meta = el("div", "meta");
  if (story.host) meta.append(el("span", "host", story.host));

  const pts = el("span", "pts", String(story.points));
  pts.append(el("span", "unit", "PTS"));
  meta.append(pts);

  const cmt = link(story.comments, "cmt", String(story.count));
  cmt.append(el("span", "unit", "CMT"));
  meta.append(cmt);

  if (story.age) meta.append(el("span", "age", story.age));

  body.append(meta);
  li.append(body);
  return li;
}

function renderStories(selector, stories) {
  const host = document.querySelector(selector);
  if (!host) return;

  const list = el("ol", "stories");
  if (!stories || stories.length === 0) {
    const li = el("li", "empty");
    li.append(el("span", "rank", "--"));
    li.append(el("div", "body", "AWAITING FEED"));
    list.append(li);
  } else {
    list.append(...stories.map(storyNode));
  }
  host.replaceChildren(list);
}

function renderWeather(weather) {
  const host = document.querySelector("[data-weather]");
  if (!host || !weather) return;

  if (weather.unavailable) {
    host.replaceChildren(el("p", "dim", "NO SIGNAL"));
    return;
  }

  const temp = el("span", "temp", weather.temperature);
  temp.append(el("span", "deg", "°"));

  const cond = el("span", "cond");
  cond.append(el("span", "cond-label", weather.condition));
  cond.append(el("span", "cond-feels", `FEELS ${weather.feels}°`));

  const row = el("div", "temp-row");
  row.append(temp, cond);

  const range = el("span", "range");
  range.append(el("span", "hi", `${weather.high}°`));
  range.append(el("span", "lo", `${weather.low}°`));
  row.append(range);

  const now = el("div", "now-row");
  now.append(labelled("RAIN", weather.rain));
  now.append(labelled("WIND", weather.wind));

  host.replaceChildren(row, now);
  renderHours(weather);
  renderDaylight(weather);
}

// The next eight hours, which is the half of a forecast anyone acts on and the
// content that stops this panel padding four numbers out to fill its band.
function labelled(key, value) {
  const span = el("span");
  span.append(el("b", null, key));
  span.append(document.createTextNode(` ${value ?? "—"}`));
  return span;
}

// The next eight hours, each showing the chance of rain as a bar and again in
// figures. A curve was tried and is the wrong shape for a value that sits near
// zero most days: it drew a flat line under an empty box, and nothing on it said
// what was being measured.
function renderHours(weather) {
  const host = document.querySelector("[data-hours]");
  if (!host) return;

  const hours = weather.hours;
  if (!hours || hours.length === 0) {
    host.replaceChildren();
    return;
  }

  const head = el("div", "hours-head");
  head.append(el("span", "k", `NEXT ${hours.length} HOURS`));
  head.append(el("span", "v", "CHANCE OF RAIN"));

  // The bars share one plot so they share a floor and a half way rule. Eight
  // separate boxes gave eight nine pixel ticks and nothing to read against.
  const plot = el("div", "hour-plot");
  const cols = el("div", "hour-cols");

  for (const h of hours) {
    const wet = (h.rain || 0) >= 50 ? "yes" : "no";

    const bar = el("span", "hbar");
    bar.dataset.wet = wet;
    const fill = el("i");
    fill.style.height = `${h.rain || 0}%`;
    bar.append(fill);
    plot.append(bar);

    const col = el("div", "hour");
    col.dataset.warm = String(h.warm || 0);
    col.dataset.wet = wet;
    col.append(el("span", "hp", `${h.rain || 0}%`));
    col.append(el("span", "hv", `${h.temp}°`));
    col.append(el("span", "ht", h.label));
    cols.append(col);
  }

  host.replaceChildren(head, plot, cols);
}

function renderDaylight(weather) {
  const host = document.querySelector("[data-daylight]");
  if (!host) return;

  const known = Boolean(weather.sunrise && weather.sunset);
  host.hidden = !known;
  if (!known) return;

  const ends = host.querySelectorAll(".t");
  setText(ends[0], weather.sunrise);
  setText(ends[1], weather.sunset);

  const fill = host.querySelector(".track i");
  if (fill) fill.style.width = `${weather.day_percent || 0}%`;
}

// The air readout lives beside the weather but comes from its own polls, so it
// is patched separately and survives a weather frame that arrived without it.
function renderAir(weather, air) {
  const host = document.querySelector("[data-air]");
  if (!host) return;

  const gauge = (key, value, band, fill, level, known, note) => {
    const box = el("div", "gauge");
    box.dataset.level = String(level || 0);
    box.append(el("span", "gk", key));

    if (!known) {
      box.append(el("span", "gv dim", "—"));
      box.append(el("span", "gbar"));
      box.append(el("span", "gb", "NO SIGNAL"));
      return box;
    }

    box.append(el("span", "gv", String(value)));
    const bar = el("span", "gbar");
    const inner = el("i");
    inner.style.width = `${fill || 0}%`;
    bar.append(inner);
    box.append(bar);
    box.append(el("span", "gb", band || ""));
    if (note) box.append(el("span", "gn", note));
    return box;
  };

  host.replaceChildren(
    gauge("UV", (weather && weather.uv) || "—", weather && weather.uv_state,
      weather && weather.uv_fill, weather && weather.uv_level, Boolean(weather)),
    gauge("AQI", air && air.aqi, air && air.aqi_state,
      air && air.aqi_fill, air && air.aqi_level, Boolean(air && air.known)),
    gauge("POLLEN", air && air.pollen, air && air.pollen_state,
      air && air.pollen_fill, air && air.pollen_level,
      Boolean(air && air.pollen_known)),
  );
}

function renderSystems(systems) {
  const host = document.querySelector("[data-systems]");
  if (!host || !systems || !systems.rows) return;

  const existing = Array.from(host.children);
  const sameSet =
    existing.length === systems.rows.length &&
    systems.rows.every((r, i) => existing[i].querySelector(".name")?.textContent === r.label);

  if (sameSet) {
    systems.rows.forEach((r, i) => {
      const li = existing[i];
      li.dataset.state = r.state;
      setText(li.querySelector(".latency"), r.response || "—");

      const traffic = li.querySelector(".traffic");
      if (traffic) {
        traffic.dataset.level = String(r.level || 0);
        traffic.dataset.trend = r.trend || "";
      }

      const errors = li.querySelector(".errors");
      if (errors) {
        setText(errors, r.know_error ? `${r.errors}E` : "—");
        errors.dataset.any = r.know_error && r.errors > 0 ? "yes" : "no";
      }
    });
    renderSystemsSummary(systems);
    return;
  }

  host.replaceChildren(
    ...systems.rows.map((row) => {
      const li = el("li");
      li.dataset.state = row.state;
      li.append(el("span", "pip"));

      // Caddy has no public hostname of its own, so its row is a label rather
      // than a link.
      li.append(row.url ? link(row.url, "name", row.label) : el("span", "name", row.label));

      const traffic = el("span", "traffic");
      traffic.dataset.level = String(row.level || 0);
      traffic.dataset.trend = row.trend || "";
      if (row.know_traf) traffic.title = `${row.requests} requests`;
      for (let i = 0; i < 4; i++) traffic.append(el("i"));
      li.append(traffic);

      li.append(el("span", "bar"));
      const response = el("span", "latency", row.response || "—");
      response.title = "95th percentile response time over 24h";
      li.append(response);

      const errors = el("span", "errors", row.know_error ? `${row.errors}E` : "—");
      errors.dataset.any = row.know_error && row.errors > 0 ? "yes" : "no";
      li.append(errors);
      return li;
    }),
  );

  renderSystemsSummary(systems);
}

function renderSystemsSummary(systems) {
  setText(document.querySelector("[data-systems-summary]"), `${systems.up}/${systems.total} UP`);

  // The window is zero whenever logging did not answer, and a footnote reading
  // "last 0h" is worse than no footnote.
  setText(
    document.querySelector("[data-systems-note]"),
    systems.window
      ? `${systems.requests} REQ / ${systems.errors} ERR / P95 / LAST ${systems.window}H`
      : "",
  );
}

// The status line names any upstream currently shut off, so a panel that has
// stopped moving says why instead of just looking stale.
function renderGuard(guarded) {
  if (!guardLine) return;
  guardLine.textContent =
    guarded && guarded.length
      ? `GUARD OPEN: ${guarded.join(" ").toUpperCase()}`
      : "GUARD NOMINAL";
}

function renderFeeds(feeds) {
  const host = document.querySelector("[data-feeds]");
  if (!host || !feeds) return;

  const existing = Array.from(host.children);
  const sameSet =
    existing.length === feeds.length &&
    feeds.every((f, i) => existing[i].querySelector(".name")?.textContent === f.name);

  if (sameSet) {
    feeds.forEach((f, i) => {
      const li = existing[i];
      li.dataset.state = f.state;
      setText(li.querySelector(".age"), f.age);

      const load = li.querySelector(".load");
      if (load) {
        setText(load, `${f.used}/${f.budget}`);
        load.dataset.hot = f.load >= 50 ? "yes" : "no";
      }
    });
    return;
  }

  host.replaceChildren(
    ...feeds.map((feed) => {
      const li = el("li");
      li.dataset.state = feed.state;
      li.append(el("span", "pip"));
      li.append(el("span", "name", feed.name));
      li.append(el("span", "bar"));
      li.append(el("span", "age", feed.age));

      const load = el("span", "load", `${feed.used}/${feed.budget}`);
      load.dataset.hot = feed.load >= 50 ? "yes" : "no";
      li.append(load);
      return li;
    }),
  );
}

function renderWire(wire) {
  const host = document.querySelector("[data-wire]");
  if (!host) return;

  if (!wire || wire.length === 0) {
    const li = el("li", "empty", "AWAITING WIRE");
    host.replaceChildren(li);
    return;
  }

  host.replaceChildren(
    ...wire.map((h) => {
      const li = el("li");
      li.append(el("span", "tag", h.source));
      li.append(link(h.url, null, h.title));
      li.append(el("span", "age", h.age));
      return li;
    }),
  );
}

function renderConditions(signal) {
  if (!signal) return;

  const headline = document.querySelector("[data-signal-headline]");
  if (headline) {
    headline.textContent = signal.headline || "";
    headline.dataset.level = signal.level || "calm";
  }

  const host = document.querySelector("[data-conditions]");
  if (!host || !signal.conditions) return;

  host.replaceChildren(
    ...signal.conditions.map((c) => {
      const li = el("li");
      li.dataset.state = c.state || "calm";
      li.append(el("span", "k", c.label));
      li.append(el("span", "v", c.value));
      li.append(el("span", "n", c.note));

      const meter = el("span", "meter");
      const fill = el("i", "fill");
      fill.style.width = `${c.fill || 0}%`;
      meter.append(fill);
      for (const at of c.ticks || []) {
        const tick = el("i", "tick");
        tick.style.left = `${at}%`;
        meter.append(tick);
      }
      li.append(meter);
      return li;
    }),
  );
}

function renderRates(rates) {
  const host = document.querySelector("[data-rates]");
  if (!host || !rates || !rates.rows) return;

  host.replaceChildren(
    ...rates.rows.map((r) => {
      const li = el("li");
      li.dataset.dir = r.direction || "flat";
      li.append(el("span", "k", r.label));
      li.append(el("span", "v", r.unavailable ? "—" : r.yield));

      const scale = el("span", "scale");
      const fill = el("i");
      fill.style.width = `${r.fill || 0}%`;
      scale.append(fill);
      li.append(scale);

      li.append(el("span", "d", r.change || ""));
      return li;
    }),
  );

  const curve = document.querySelector("[data-curve]");
  if (curve) {
    curve.textContent = rates.curve || "—";
    const wrap = curve.closest("[data-curve-state]");
    if (wrap) wrap.dataset.curveState = rates.curve_state || "";
  }
  setText(document.querySelector("[data-curve-shape]"), rates.shape || "");
}

function renderSectors(sectors) {
  const host = document.querySelector("[data-sectors]");
  if (!host || !sectors) return;

  host.replaceChildren(
    ...sectors.map((c) => {
      const cell = el("div", "cell");
      cell.dataset.dir = c.direction || "flat";
      cell.dataset.heat = String(c.heat || 0);
      if (c.benchmark) cell.dataset.benchmark = "yes";
      cell.append(el("span", "s", c.label));
      cell.append(el("span", "p", c.unavailable ? "—" : c.percent));
      return cell;
    }),
  );
}

function renderEarnings(rows) {
  const host = document.querySelector("[data-earnings]");
  if (!host) return;

  if (!rows || rows.length === 0) {
    host.replaceChildren(el("li", "empty", "NOTHING MAJOR SCHEDULED"));
    return;
  }

  host.replaceChildren(
    ...rows.map((r) => {
      const li = el("li");
      li.append(el("span", "tkr", r.symbol));
      li.append(el("span", "co", r.name));
      li.append(el("span", "cap", r.cap));

      const day = el("span", "day", r.day);
      if (r.when) {
        day.append(document.createTextNode(" "));
        day.append(el("span", "when", r.when));
      }
      li.append(day);
      return li;
    }),
  );
}

// Absent entirely when nothing is active, because a permanent "all clear" row
// trains you to stop seeing the space it sits in.
function renderAlerts(alerts) {
  const host = document.querySelector("[data-alerts]");
  if (!host) return;

  if (!alerts || alerts.length === 0) {
    host.replaceChildren();
    return;
  }

  host.replaceChildren(
    ...alerts.map((a) => {
      const div = el("div", "alert");
      div.dataset.severity = a.severity || "";
      div.append(el("span", "ev", a.event));
      div.append(el("span", "hl", a.headline || ""));
      if (a.until) div.append(el("span", "til", `UNTIL ${a.until}`));
      return div;
    }),
  );
}

function renderSteam(games) {
  const host = document.querySelector("[data-steam]");
  if (!host) return;

  if (!games || games.length === 0) {
    host.replaceChildren(el("li", "empty", "AWAITING STORE"));
    return;
  }

  host.replaceChildren(
    ...games.map((g) => {
      const li = el("li");
      li.append(link(g.url, null, g.name));

      const price = el("span", "pr");
      if (g.discount > 0) {
        price.append(el("span", "disc", `-${g.discount}%`));
        price.append(document.createTextNode(" "));
      }
      price.append(document.createTextNode(g.price));
      li.append(price);

      if (g.reviewed) {
        const rating = el("span", "rating");
        rating.dataset.band = reviewBand(g.rating);
        rating.title = g.verdict || "";
        rating.append(el("span", "pct", `${g.rating}%`));
        const track = el("span", "track");
        const bar = el("i");
        bar.style.width = `${g.rating}%`;
        track.append(bar);
        rating.append(track);
        rating.append(el("span", "n", g.reviews || ""));
        li.append(rating);
      }

      const meta = el("span", "meta");
      for (const tag of g.tags || []) meta.append(el("span", "tag", tag));
      if (g.players) meta.append(el("span", "players", `${g.players} PLAYING`));
      li.append(meta);

      return li;
    }),
  );
}

// Valve's own review bands, kept in step with the band function the server
// template uses so a row rendered here and a row rendered there match.
function reviewBand(pct) {
  if (pct >= 80) return "good";
  if (pct >= 70) return "mixed";
  if (pct >= 40) return "poor";
  return "bad";
}

function renderOutlook(outlook) {
  const host = document.querySelector("[data-outlook]");
  if (!host) return;

  const days = outlook && outlook.days;
  if (!days || days.length === 0) {
    host.replaceChildren(el("li", "empty", "AWAITING FORECAST"));
    return;
  }

  host.replaceChildren(
    ...days.map((d) => {
      const li = el("li");
      li.dataset.verdict = d.verdict;

      const head = el("div", "head");
      head.append(el("span", "d", `${d.day} ${d.date}`));

      const temps = el("span", "temps");
      temps.append(document.createTextNode(`${d.high}°`));
      temps.append(el("i", null, "/"));
      temps.append(document.createTextNode(`${d.low}°`));
      head.append(temps);

      head.append(el("span", "v", d.verdict));
      li.append(head);

      const factors = el("div", "factors");
      for (const f of d.factors || []) {
        const factor = el("div", "factor");
        factor.dataset.score = String(f.score);
        factor.append(el("span", "fk", f.label));
        factor.append(el("span", "fbar"));
        factor.append(el("span", "fv", f.value));
        factors.append(factor);
      }
      li.append(factors);

      return li;
    }),
  );
}

function renderStreaming(titles) {
  const host = document.querySelector("[data-streaming]");
  if (!host) return;

  if (!titles || titles.length === 0) {
    host.replaceChildren(el("li", "empty", "AWAITING LISTINGS"));
    return;
  }

  host.replaceChildren(
    ...titles.map((t) => {
      const li = el("li");
      li.append(t.url ? link(t.url, "name", t.name) : el("span", "name", t.name));
      li.append(el("span", "svc", t.provider));

      const rating = el("span", "rating");
      rating.dataset.band = t.score_band || "";
      rating.append(el("span", "pct", `${t.score}%`));
      const track = el("span", "track");
      const bar = el("i");
      bar.style.width = `${t.score}%`;
      track.append(bar);
      rating.append(track);
      rating.append(el("span", "n", t.score_from || ""));
      li.append(rating);

      const meta = el("span", "meta");
      meta.append(el("span", "kind", t.year ? `${t.kind} ${t.year}` : t.kind));

      const imdb = el("span", "score imdb", `IMDB ${t.imdb}`);
      imdb.dataset.grade = t.imdb_state || "";
      meta.append(imdb);

      if (t.tomato) {
        const rt = el("span", "score rt", `RT ${t.tomato}`);
        rt.dataset.grade = t.tomato_state || "";
        meta.append(rt);
      }
      li.append(meta);

      return li;
    }),
  );
}

// Every frame carries the whole state, so without the changed() gate a market
// poll every thirty seconds rebuilds the weather, the earnings and the store
// listings too, and a panel that is rebuilt collapses to no height for a moment
// before it refills. Each renderer is handed only its own slice, so comparing
// that slice is enough to know whether the panel can be left alone.
function render(state) {
  if (changed("market", state.market)) renderMarket(state.market);
  if (changed("signal", state.signal)) renderConditions(state.signal);
  if (changed("rates", state.rates)) renderRates(state.rates);
  if (changed("sectors", state.sectors)) renderSectors(state.sectors);
  if (changed("earnings", state.earnings)) renderEarnings(state.earnings);
  if (changed("alerts", state.alerts)) renderAlerts(state.alerts);
  if (changed("outlook", state.outlook)) renderOutlook(state.outlook);
  if (changed("steam", state.steam)) renderSteam(state.steam);
  if (changed("wire", state.wire)) renderWire(state.wire);
  if (changed("feeds", state.feeds)) renderFeeds(state.feeds);
  if (changed("hn", state.hn)) renderStories("[data-hn]", state.hn);
  if (changed("lobsters", state.lobsters)) renderStories("[data-lobsters]", state.lobsters);
  if (changed("weather", state.weather)) renderWeather(state.weather);
  // Two figures in this bank come from the weather poll and three from the air
  // poll, so it has to redraw when either moves.
  if (changed("air", [state.weather, state.air])) renderAir(state.weather, state.air);
  if (changed("systems", state.systems)) renderSystems(state.systems);
  if (changed("guarded", state.guarded)) renderGuard(state.guarded);
}

// EventSource reconnects on its own, but only for a clean disconnect. An error
// that closes the stream is ours to retry, so the backoff doubles up to a
// minute rather than hammering a server that may be mid-deploy.
let backoff = 1000;
const maxBackoff = 60000;

function connect() {
  const source = new EventSource("/events");

  source.onopen = () => {
    backoff = 1000;
    connection("live", "LIVE");
  };

  source.onmessage = (event) => {
    try {
      render(JSON.parse(event.data));
      connection("live", "LIVE");
    } catch (err) {
      // A frame that will not parse is not a reason to tear down a working
      // connection, so this keeps the last good render and waits for the next.
      console.error("dash: bad frame", err);
    }
  };

  source.onerror = () => {
    source.close();
    connection("down", "RETRY");
    setTimeout(connect, backoff);
    backoff = Math.min(backoff * 2, maxBackoff);
  };
}

connect();

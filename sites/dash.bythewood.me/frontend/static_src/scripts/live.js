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

function renderMarket(market) {
  if (!market) return;

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

function readoutCell(key, value) {
  const cell = el("div");
  cell.append(el("dt", null, key));
  cell.append(el("dd", null, value));
  return cell;
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

  const grid = el("dl", "grid-readout");
  grid.append(
    readoutCell("HIGH", `${weather.high}°`),
    readoutCell("LOW", `${weather.low}°`),
    readoutCell("RAIN", weather.rain),
    readoutCell("WIND", weather.wind),
  );

  const children = [row, grid];

  if (weather.sunrise && weather.sunset) {
    const bar = el("div", "daylight");
    bar.append(el("span", "t", weather.sunrise));

    const track = el("span", "track");
    const fill = el("i");
    fill.style.width = `${weather.day_percent || 0}%`;
    track.append(fill);
    bar.append(track);

    bar.append(el("span", "t", weather.sunset));
    children.push(bar);
  }

  host.replaceChildren(...children);
}

// The air readout lives beside the weather but comes from its own polls, so it
// is patched separately and survives a weather frame that arrived without it.
function renderAir(weather, air) {
  const host = document.querySelector("[data-air]");
  if (!host) return;

  const cell = (key, value, sub) => {
    const div = el("div");
    div.append(el("dt", null, key));
    const dd = el("dd", null, value);
    if (sub) {
      dd.append(document.createTextNode(" "));
      dd.append(el("span", "sub", sub));
    }
    div.append(dd);
    return div;
  };

  const uv = cell("UV", (weather && weather.uv) || "—", weather && weather.uv_state);
  const aqi = air && air.known
    ? cell("AQI", String(air.aqi), air.aqi_state)
    : cell("AQI", "—");

  const pollen = air && air.pollen_known
    ? cell("POLLEN", air.pollen, [air.pollen_state, air.pollen_top].filter(Boolean).join(" · "))
    : cell("POLLEN", "—");
  pollen.className = "wide";

  host.replaceChildren(uv, aqi, pollen);
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
      setText(li.querySelector(".latency"), r.latency || "—");

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
      li.append(el("span", "latency", row.latency || "—"));

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
      ? `${systems.requests} REQ / ${systems.errors} ERR / LAST ${systems.window}H`
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

      const meta = el("span", "meta");
      for (const tag of g.tags || []) meta.append(el("span", "tag", tag));
      if (g.players) meta.append(el("span", "players", `${g.players} PLAYING`));
      li.append(meta);

      return li;
    }),
  );
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
      head.append(el("span", "v", d.verdict));
      li.append(head);

      const facts = el("div", "facts");
      facts.append(el("span", "t", `${d.high}°/${d.low}° ${d.temp}`));
      facts.append(el("span", null, d.rain));
      facts.append(el("span", null, d.ground));
      facts.append(el("span", null, d.wind));
      li.append(facts);

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
      if (t.hot) li.dataset.hot = "yes";
      li.append(t.url ? link(t.url, "name", t.name) : el("span", "name", t.name));
      li.append(el("span", "svc", t.provider));

      const meta = el("span", "meta");
      meta.append(el("span", "kind", t.year ? `${t.kind} ${t.year}` : t.kind));
      meta.append(el("span", "score imdb", `IMDB ${t.imdb}`));
      if (t.tomato) meta.append(el("span", "score rt", `RT ${t.tomato}`));
      li.append(meta);

      return li;
    }),
  );
}

function render(state) {
  renderMarket(state.market);
  renderConditions(state.signal);
  renderRates(state.rates);
  renderSectors(state.sectors);
  renderEarnings(state.earnings);
  renderAlerts(state.alerts);
  renderOutlook(state.outlook);
  renderSteam(state.steam);
  renderWire(state.wire);
  renderFeeds(state.feeds);
  renderStories("[data-hn]", state.hn);
  renderStories("[data-lobsters]", state.lobsters);
  renderWeather(state.weather);
  renderAir(state.weather, state.air);
  renderSystems(state.systems);
  renderGuard(state.guarded);
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

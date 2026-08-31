// The clock ticks without the server, since a second hand over a stream would
// be a frame a second for something the browser already knows. UTC sits beside
// local because every timestamp the server logs is UTC.
const utc = document.querySelector("[data-clock-utc]");
const zone = document.querySelector("[data-clock-zone]");
const local = document.querySelector("[data-clock-local]");
const date = document.querySelector("[data-clock-date]");

if (utc && local && date) {
  const hms = (tz) =>
    new Intl.DateTimeFormat("en-GB", {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
      ...(tz ? { timeZone: tz } : {}),
    });

  const utcFmt = hms("UTC");
  const localFmt = hms(null);
  const dateFmt = new Intl.DateTimeFormat("en-GB", {
    weekday: "short",
    day: "2-digit",
    month: "short",
  });

  // "LOCAL" on its own does not say local to what, and the answer differs per
  // viewer, so the label carries the browser's actual zone abbreviation. Intl
  // gives the short name for today, which is what handles daylight saving
  // without a table.
  if (zone) {
    const now = new Date();
    const parts = new Intl.DateTimeFormat("en-US", {
      timeZoneName: "short",
    }).formatToParts(now);
    const named = parts.find((p) => p.type === "timeZoneName");
    zone.textContent = named ? named.value.toUpperCase() : "LOCAL";
    zone.title = Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  }

  const tick = () => {
    const now = new Date();
    utc.textContent = utcFmt.format(now);
    local.textContent = localFmt.format(now);
    date.textContent = dateFmt.format(now).toUpperCase();
  };

  tick();
  setInterval(tick, 1000);
}

// The chart theme every dashboard on this site draws with.
//
// This file is duplicated byte for byte in analytics, logging and status the
// same way shipper.go and session.go are. A colour that drifts between two of
// these dashboards is visible, and there is no shared bundle to put it in.
//
// The series colours are punchier than the UI palette on purpose. Chrome can be
// muted because nothing depends on telling two borders apart, and a doughnut
// slice does. Same hues as the rest of the site, more chroma.
//
// The order is validated rather than chosen by eye. Every adjacent pair clears
// the normal vision floor and sits in the 6 to 8 band for deuteranopia and
// protanopia, which is only acceptable because every chart here carries a
// legend, so colour is never the only thing telling two series apart.
// Reordering these means checking them again.

export const series = ["#57b378", "#d8a83e", "#dc6a4b", "#63a9c9"];

// Anything past the fourth category folds into this rather than getting a
// generated hue, since a fifth and sixth colour out of these five is not
// distinguishable from the ones already used.
export const other = "#7d7469";

export const palette = [...series, other];

// Status is a separate job from identity, so these are keyed by name and never
// handed out in order.
export const status = {
  good: series[0],
  warn: series[1],
  bad: series[2],
  info: series[3],
  muted: other,
};

export const ink = {
  ticks: "#9a9188",
  grid: "rgba(107, 158, 120, 0.14)",
  border: "rgba(107, 158, 120, 0.3)",
  legend: "#b3aba2",
  title: "#ede8e0",
  body: "#ddd7cd",
  surface: "rgba(9, 8, 6, 0.96)",
};

export const fontStack =
  "'Monaspace Argon', ui-monospace, 'Cascadia Code', Consolas, 'SF Mono', Menlo, monospace";

export const tooltipStyle = {
  backgroundColor: ink.surface,
  borderColor: "rgba(107, 158, 120, 0.5)",
  borderWidth: 1,
  titleColor: ink.title,
  bodyColor: ink.body,
  titleFont: { family: fontStack, size: 11 },
  bodyFont: { family: fontStack, size: 11 },
  padding: 10,
  displayColors: true,
  boxWidth: 8,
  boxHeight: 8,
};

// Applied once per bundle. Chart.js reads these at construction, so it has to
// run before any chart is built.
export function applyDefaults(Chart) {
  Chart.defaults.color = ink.ticks;
  Chart.defaults.borderColor = ink.grid;
  Chart.defaults.font.family = fontStack;
  Chart.defaults.font.size = 11;
}

// Folds a ranked list down to the colours that are actually distinguishable,
// summing the tail into one "other" row.
export function foldToPalette(rows, limit = series.length) {
  if (rows.length <= limit + 1) return rows;
  const head = rows.slice(0, limit);
  const tail = rows.slice(limit);
  const sum = tail.reduce((n, r) => n + (r.count || 0), 0);
  if (sum <= 0) return head;
  return [...head, { label: "other", count: sum }];
}

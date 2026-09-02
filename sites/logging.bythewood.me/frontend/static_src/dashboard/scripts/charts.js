import Chart from "chart.js/auto";
import {
  applyDefaults,
  fontStack,
  ink,
  palette,
  status,
  tooltipStyle,
} from "./chart_theme.js";

// Three charts, all reading inline <script type="application/json"> blocks the
// server rendered. Level and status colours are keyed by name rather than taken
// in order, so ERROR is red whatever order the server sent them in.
const levelColors = {
  ERROR: status.bad,
  WARN: status.warn,
  INFO: status.good,
  DEBUG: status.muted,
};

const statusColors = {
  "2xx": status.good,
  "3xx": status.info,
  "4xx": status.warn,
  "5xx": status.bad,
};

const fallback = palette;

applyDefaults(Chart);


// read returns null for a missing or unparseable block, so one bad chart does
// not take the others down with it.
function read(id) {
  const el = document.getElementById(id);
  if (!el) return null;
  try {
    return JSON.parse(el.textContent);
  } catch (err) {
    console.error("chart data", id, err);
    return null;
  }
}

function withAlpha(hex, alpha) {
  const n = parseInt(hex.slice(1), 16);
  const r = (n >> 16) & 255;
  const g = (n >> 8) & 255;
  const b = n & 255;
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

function colorsFor(labels, table) {
  return labels.map(
    (label, i) => table[label] || fallback[i % fallback.length],
  );
}

const volume = read("chart-volume");
const volumeCanvas = document.getElementById("volume-chart");
if (volume && volumeCanvas) {
  const labels = volume.map((p) => p.label);
  new Chart(volumeCanvas, {
    type: "line",
    data: {
      labels,
      datasets: [
        {
          label: "errors",
          data: volume.map((p) => p.errors),
          borderColor: terracotta,
          backgroundColor: withAlpha(terracotta, 0.35),
          borderWidth: 1.5,
          fill: "origin",
          pointRadius: 0,
          pointHoverRadius: 3,
          tension: 0.25,
          stack: "records",
        },
        {
          label: "other records",
          data: volume.map((p) => Math.max(0, p.count - p.errors)),
          borderColor: green,
          backgroundColor: withAlpha(green, 0.18),
          borderWidth: 1.5,
          fill: "-1",
          pointRadius: 0,
          pointHoverRadius: 3,
          tension: 0.25,
          stack: "records",
        },
      ],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      // Nearest x rather than nearest point, so hovering anywhere in a column
      // shows both series for that bucket.
      interaction: { mode: "index", intersect: false },
      animation: false,
      plugins: {
        tooltip: tooltipStyle,
        legend: {
          display: true,
          position: "bottom",
          labels: { boxWidth: 8, boxHeight: 8, padding: 12 },
        },
      },
      scales: {
        x: {
          stacked: true,
          grid: { display: false },
          ticks: {
            maxRotation: 0,
            autoSkip: true,
            // autoSkip alone still crowds the labels at a year of daily
            // buckets on a phone.
            maxTicksLimit: 10,
          },
        },
        y: {
          stacked: true,
          beginAtZero: true,
          grid: { color: ink.grid },
          ticks: { precision: 0 },
        },
      },
    },
  });
}

function doughnut(canvasId, dataId, table) {
  const data = read(dataId);
  const canvas = document.getElementById(canvasId);
  if (!data || !canvas || data.length === 0) return;

  const labels = data.map((d) => d.label);
  const colors = colorsFor(labels, table || {});

  new Chart(canvas, {
    type: "doughnut",
    data: {
      labels,
      datasets: [
        {
          data: data.map((d) => d.count),
          backgroundColor: colors.map((c) => withAlpha(c, 0.75)),
          borderColor: colors,
          borderWidth: 1,
        },
      ],
    },
    options: {
      responsive: true,
      // false, so the doughnut fills the fixed-height panel body the CSS gives
      // it rather than ballooning to whatever its own width implies.
      maintainAspectRatio: false,
      cutout: "62%",
      animation: { animateRotate: false },
      plugins: {
        tooltip: tooltipStyle,
        legend: {
          position: "bottom",
          labels: {
            boxWidth: 8,
            boxHeight: 8,
            padding: 8,
            color: ink.legend,
            font: { family: fontStack, size: 10 },
          },
        },
      },
    },
  });
}

doughnut("levels-chart", "chart-levels", levelColors);
doughnut("status-chart", "chart-status", statusColors);

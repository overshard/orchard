import Chart from "chart.js/auto";

// Three charts, all reading inline <script type="application/json"> blocks the
// server rendered. Level and status colours are keyed by name rather than taken
// in order, so ERROR is terracotta whatever order the server sent them in.
const green = "#6b9e78";
const greenBright = "#7db88c";
const amber = "#c9a84c";
const terracotta = "#c47055";
const slate = "#7eaab8";
const grey = "#847c72";

const levelColors = {
  ERROR: terracotta,
  WARN: amber,
  INFO: green,
  DEBUG: grey,
};

const statusColors = {
  "2xx": green,
  "3xx": slate,
  "4xx": amber,
  "5xx": terracotta,
};

const fallback = [green, amber, terracotta, slate, grey, greenBright];

const fontStack =
  "'Monaspace Argon', ui-monospace, 'Cascadia Code', Consolas, monospace";

Chart.defaults.color = "rgba(221, 215, 205, 0.55)";
Chart.defaults.borderColor = "rgba(107, 158, 120, 0.08)";
Chart.defaults.font.family = fontStack;
Chart.defaults.font.size = 11;

const tooltipStyle = {
  backgroundColor: "rgba(9, 8, 6, 0.95)",
  borderColor: "rgba(107, 158, 120, 0.3)",
  borderWidth: 1,
  titleColor: "#ede8e0",
  bodyColor: "#ddd7cd",
  padding: 10,
  titleFont: { family: fontStack, size: 12 },
  bodyFont: { family: fontStack, size: 11 },
  cornerRadius: 4,
};

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
          grid: { color: "rgba(107, 158, 120, 0.06)" },
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
            color: "rgba(221, 215, 205, 0.7)",
            font: { family: fontStack, size: 10 },
          },
        },
      },
    },
  });
}

doughnut("levels-chart", "chart-levels", levelColors);
doughnut("status-chart", "chart-status", statusColors);

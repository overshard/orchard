import Chart from "chart.js/auto";
import {
  applyDefaults,
  fontStack,
  foldToPalette,
  ink,
  palette,
  series,
  status,
  tooltipStyle,
} from "./chart_theme.js";

const paletteBorders = palette;

applyDefaults(Chart);

// Labels are padded to a common width so the four doughnut legends line up in
// the sidebar, which only works because the legend font is monospace.
let maxLabelLength = 0;
if (document.getElementById("chart-total-events-by-browser-data")) {
  const browserData = JSON.parse(document.getElementById("chart-total-events-by-browser-data").innerHTML);
  const deviceData = JSON.parse(document.getElementById("chart-total-events-by-device-data").innerHTML);
  const screenSizeData = JSON.parse(document.getElementById("chart-total-events-by-screen-size-data").innerHTML);
  const platformData = JSON.parse(document.getElementById("chart-total-events-by-platform-data").innerHTML);
  const allData = [...browserData, ...deviceData, ...screenSizeData, ...platformData];
  for (let i = 0; i < allData.length; i++) {
    if (allData[i].label.length > maxLabelLength) {
      maxLabelLength = allData[i].label.length;
    }
  }
}

function padLabels(data) {
  for (let i = 0; i < data.length; i++) {
    data[i].label = data[i].label + " ".repeat(Math.max(0, maxLabelLength - data[i].label.length));
  }
  return data;
}

const doughnutOptions = {
  responsive: true,
  aspectRatio: 1.8,
  animation: { animateRotate: false },
  cutout: "60%",
  plugins: {
    tooltip: tooltipStyle,
    legend: {
      position: "right",
      labels: {
        boxWidth: 8,
        boxHeight: 8,
        padding: 8,
        color: ink.legend,
        font: { family: fontStack, size: 11 },
      },
    },
  },
};

// The placeholder keeps the card's footprint about the same, so a chart with no
// data does not reflow the ones beside it.
function renderEmpty(canvas) {
  const placeholder = document.createElement("div");
  placeholder.className = "chart-empty";
  placeholder.textContent = "no data yet";
  Object.assign(placeholder.style, {
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    height: "100%",
    minHeight: "120px",
    color: ink.ticks,
    fontFamily: fontStack,
    fontSize: "11px",
    letterSpacing: "0.05em",
    textTransform: "uppercase",
  });
  canvas.replaceWith(placeholder);
}

function hasData(rows) {
  return rows.some((d) => (d.count || 0) > 0);
}

function renderDoughnut(canvasId, dataId) {
  document.addEventListener("DOMContentLoaded", function () {
    const canvas = document.getElementById(canvasId);
    if (!canvas) return;
    const raw = JSON.parse(document.getElementById(dataId).innerHTML);
    if (!hasData(raw)) {
      renderEmpty(canvas);
      return;
    }
    const data = padLabels(foldToPalette(raw));
    new Chart(canvas.getContext("2d"), {
      type: "doughnut",
      data: {
        labels: data.map((d) => d.label),
        datasets: [
          {
            data: data.map((d) => d.count),
            backgroundColor: palette,
            borderColor: "rgba(14, 13, 10, 0.9)",
            borderWidth: 2,
          },
        ],
      },
      options: doughnutOptions,
    });
  });
}

document.addEventListener("DOMContentLoaded", function () {
  const canvas = document.getElementById("chart-total-events");
  if (!canvas) return;
  const data = JSON.parse(document.getElementById("chart-total-events-data").innerHTML);
  if (!hasData(data)) {
    renderEmpty(canvas);
    return;
  }
  const ctx = canvas.getContext("2d");

  const gradient = ctx.createLinearGradient(0, 0, 0, 320);
  gradient.addColorStop(0, "rgba(87, 179, 120, 0.35)");
  gradient.addColorStop(1, "rgba(87, 179, 120, 0.01)");

  new Chart(ctx, {
    type: "line",
    data: {
      labels: data.map((d) => d.label),
      datasets: [
        {
          label: "events",
          data: data.map((d) => d.count),
          backgroundColor: gradient,
          borderColor: series[0],
          pointBackgroundColor: series[0],
          pointBorderColor: "rgba(14, 13, 10, 1)",
          pointRadius: 3,
          pointHoverRadius: 5,
          borderWidth: 2,
          tension: 0.25,
          fill: true,
        },
      ],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: { duration: 0 },
      plugins: {
        tooltip: { ...tooltipStyle, mode: "index", intersect: false },
        legend: {
          display: false,
        },
      },
      scales: {
        x: {
          ticks: {
            autoSkip: true,
            maxTicksLimit: 10,
            maxRotation: 0,
            color: ink.ticks,
            font: { family: fontStack, size: 10 },
          },
          grid: { color: ink.grid, drawTicks: false },
          border: { color: ink.border },
        },
        y: {
          beginAtZero: true,
          ticks: {
            color: ink.ticks,
            font: { family: fontStack, size: 10 },
          },
          grid: { color: ink.grid, drawTicks: false },
          border: { display: false },
        },
      },
    },
  });
});

renderDoughnut("chart-total-events-by-browser", "chart-total-events-by-browser-data");
renderDoughnut("chart-total-events-by-device", "chart-total-events-by-device-data");
renderDoughnut("chart-total-events-by-screen-size", "chart-total-events-by-screen-size-data");
renderDoughnut("chart-total-events-by-platform", "chart-total-events-by-platform-data");

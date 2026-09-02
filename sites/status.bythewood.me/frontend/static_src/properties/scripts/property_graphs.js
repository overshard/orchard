import Chart from "chart.js/auto";
import {
  applyDefaults,
  fontStack,
  ink,
  palette,
  series,
  status,
  tooltipStyle,
} from "./chart_theme.js";

const accent = {
  green: status.good,
  greenBright: series[0],
  greenFill: "rgba(87, 179, 120, 0.35)",
  amber: status.warn,
  amberFill: "rgba(216, 168, 62, 0.35)",
  terracotta: status.bad,
  terracottaFill: "rgba(220, 106, 75, 0.35)",
  slate: status.info,
  slateFill: "rgba(99, 169, 201, 0.3)",
  grid: ink.grid,
  ticks: ink.ticks,
};

// Fills for the ranked bars, in the validated order, with the neutral last for
// anything folded into "other".
const backgroundColors = [
  accent.greenFill,
  accent.amberFill,
  accent.terracottaFill,
  accent.slateFill,
  "rgba(125, 116, 105, 0.35)",
];

const borderColors = palette;

applyDefaults(Chart);

const tickFont = { size: 11, family: fontStack };
const legendLabel = { boxWidth: 10, boxHeight: 10, font: tickFont, color: ink.legend };

document.addEventListener("DOMContentLoaded", function () {
  const canvas = document.getElementById("chart-response-times");
  if (!canvas) return;
  const data = JSON.parse(
    document.getElementById("chart-status-response-times-data").innerHTML
  );
  const ctx = canvas.getContext("2d");

  const series = [
    { key: "total", label: "Total",   color: accent.green,      width: 2,   tension: 0.25 },
    { key: "dns",   label: "DNS",     color: accent.terracotta, width: 1.5, tension: 0.2  },
    { key: "tcp",   label: "TCP",     color: accent.amber,      width: 1.5, tension: 0.2  },
    { key: "tls",   label: "TLS",     color: accent.slate,      width: 1.5, tension: 0.2  },
    { key: "ttfb",  label: "TTFB",    color: "#7d7469",         width: 1.5, tension: 0.2  },
  ];

  const chart = new Chart(ctx, {
    type: "line",
    data: {
      labels: data.map((d) => {
        const date = new Date(d.label);
        return `${date.getHours() % 12 || 12}:${date.getMinutes() < 10 ? "0" : ""}${date.getMinutes()} ${date.getHours() >= 12 ? "PM" : "AM"}`;
      }),
      datasets: series.map((s) => ({
        label: s.label,
        // Older rows have null phase timings, and chart.js draws a null as a
        // gap in the line, which is what we want here.
        data: data.map((d) => (d[s.key] == null ? null : d[s.key])),
        borderColor: s.color,
        backgroundColor: s.color,
        borderWidth: s.width,
        pointRadius: 0,
        pointHoverRadius: 4,
        pointHoverBackgroundColor: s.color,
        tension: s.tension,
        fill: false,
        spanGaps: false,
      })),
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: { duration: 0 },
      interaction: { mode: "index", intersect: false },
      plugins: {
        tooltip: {
          ...tooltipStyle,
          mode: "index",
          intersect: false,
          titleFont: tickFont,
          bodyFont: tickFont,
          callbacks: {
            label: (item) =>
              ` ${item.dataset.label}: ${item.parsed.y == null ? "–" : item.parsed.y + " ms"}`,
          },
        },
        legend: { position: "top", labels: legendLabel },
      },
      scales: {
        x: {
          grid: { color: accent.grid },
          border: { display: false },
          ticks: { autoSkip: true, maxRotation: 25, font: tickFont, color: accent.ticks },
        },
        y: {
          grid: { color: accent.grid },
          border: { display: false },
          ticks: {
            beginAtZero: true,
            font: tickFont,
            color: accent.ticks,
            callback: (value) => `${value} ms`,
          },
        },
      },
    },
  });
  chart.canvas.parentNode.style.width = "100%";
  chart.canvas.parentNode.style.height = "300px";
});

function buildDoughnut(canvasId, dataId) {
  const canvas = document.getElementById(canvasId);
  if (!canvas) return;
  const data = JSON.parse(document.getElementById(dataId).innerHTML);
  const ctx = canvas.getContext("2d");

  // Keyed by name, so 200 and Uptime are always green and a failure is always
  // terracotta whatever order the server sent the rows in.
  const paint = (label) => {
    const name = String(label || "").toLowerCase();
    if (name === "uptime" || name === "200") return [accent.greenFill, accent.green];
    if (name === "downtime") return [accent.terracottaFill, accent.terracotta];
    return null;
  };

  const bg = data.map((d, i) => {
    const p = paint(d.label);
    return p ? p[0] : backgroundColors[i % backgroundColors.length];
  });
  const bd = data.map((d, i) => {
    const p = paint(d.label);
    return p ? p[1] : borderColors[i % borderColors.length];
  });

  new Chart(ctx, {
    type: "doughnut",
    data: {
      labels: data.map((d) => String(d.label)),
      datasets: [
        {
          data: data.map((d) => d.count),
          backgroundColor: bg,
          borderColor: bd,
          borderWidth: 1.5,
        },
      ],
    },
    options: {
      responsive: true,
      aspectRatio: 2,
      animation: { animateRotate: false },
      cutout: "62%",
      // Bottom rather than right: these two panels are a third of a row wide,
      // and a right hand legend leaves so little room that Chart.js truncates
      // "Downtime" to "Downt".
      plugins: {
        legend: { position: "bottom", labels: legendLabel },
        tooltip: {
          ...tooltipStyle,
        },
      },
    },
  });
}

document.addEventListener("DOMContentLoaded", () => buildDoughnut("chart-status-codes", "chart-status-codes-data"));
document.addEventListener("DOMContentLoaded", () => buildDoughnut("chart-uptime", "chart-uptime-data"));

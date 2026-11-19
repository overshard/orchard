// Margin pins on a home page hero. Each [data-co] inside the staged window
// gets a numbered pin in the gutter beside it and a hairline back to it.
const draw = () => {
  for (const cw of document.querySelectorAll(".o-cw")) {
    const ov = cw.querySelector(".o-ov");
    const win = cw.querySelector(".o-win");
    if (!ov || !win) continue;
    const base = cw.getBoundingClientRect();
    const rel = (el) => {
      const r = el.getBoundingClientRect();
      return { x: r.left - base.left, y: r.top - base.top, w: r.width, h: r.height };
    };
    const w = rel(win);
    let px = w.x + w.w + 18;
    const left = px > cw.clientWidth - 12;
    if (left) px = w.x - 18;
    if (left && px < 12) {
      ov.replaceChildren();
      continue;
    }
    let lines = "";
    let pins = "";
    [...win.querySelectorAll("[data-co]")].forEach((el, i) => {
      const r = rel(el);
      const cy = r.y + Math.min(r.h / 2, 14);
      const sx = left ? r.x - 2 : r.x + r.w + 2;
      lines += `<line x1="${sx}" y1="${cy}" x2="${left ? px + 10 : px - 10}" y2="${cy}" stroke="rgba(238,240,242,.4)" stroke-width="1"/>`;
      pins += `<span class="anc" style="left:${sx}px;top:${cy}px;--d:${i * 0.6}s"></span><span class="pin" style="left:${px}px;top:${cy}px">${i + 1}</span>`;
    });
    ov.innerHTML = `<svg aria-hidden="true">${lines}</svg>${pins}`;
  }
};

if (document.querySelector(".o-cw")) {
  draw();
  addEventListener("resize", draw);
  document.fonts?.ready.then(draw);
}

// On a narrow screen the tool tabs scroll, so start with this tool in view.
const on = document.querySelector(".o-tools a.on");
if (on) {
  const bar = on.closest(".o-bar");
  bar.scrollLeft = on.offsetLeft - bar.clientWidth / 2 + on.offsetWidth / 2;
}

// A table row marked data-href opens on a click anywhere in it, and the link
// inside it stays the real target for keyboards and new tabs.
for (const tr of document.querySelectorAll("tr[data-href]")) {
  tr.classList.add("link");
  tr.addEventListener("click", (e) => {
    if (e.target.closest("a, button, form, input, select, textarea")) return;
    if (getSelection()?.toString()) return;
    location.href = tr.dataset.href;
  });
}

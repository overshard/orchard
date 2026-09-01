// The decoration on the sign in pages: a drifting star field with faint lines
// between near neighbours, and the occasional shooting star.
//
// Canvas rather than DOM nodes, because this draws a couple of hundred points
// every frame and CSS starts dropping frames somewhere around a hundred and
// fifty. It is decorative, so the canvas carries aria-hidden in the template and
// nothing here is reachable by keyboard.

const NEAR = 150; // px between two stars before a line is drawn between them
const MAX_LINKS = 3; // per star, so a dense corner does not turn into a mesh

export function starfield(canvas) {
  const ctx = canvas.getContext("2d", { alpha: true });
  if (!ctx) return;

  const still = window.matchMedia("(prefers-reduced-motion: reduce)");

  let stars = [];
  let shooting = null;
  let frame = null;
  let w = 0;
  let h = 0;
  const pointer = { x: 0.5, y: 0.5, tx: 0.5, ty: 0.5 };

  // Density by area rather than a fixed count, or the panel is crowded on a
  // phone and empty on a wide monitor.
  function populate() {
    const target = Math.min(300, Math.round((w * h) / 6200));
    stars = [];
    for (let i = 0; i < target; i++) {
      stars.push({
        x: Math.random() * w,
        y: Math.random() * h,
        // Three rough depths. Far stars are dimmer, smaller and slower, which
        // is the whole of the parallax.
        z: 0.35 + Math.random() * 0.65,
        r: 0.6 + Math.random() * 1.6,
        vx: (Math.random() - 0.5) * 0.05,
        vy: -0.04 - Math.random() * 0.07,
        tw: Math.random() * Math.PI * 2,
      });
    }
  }

  function resize() {
    const rect = canvas.getBoundingClientRect();
    // Guard against a zero-sized parent, which happens while the panel is still
    // being laid out and would otherwise divide by zero in populate().
    if (rect.width < 2 || rect.height < 2) return;

    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    w = rect.width;
    h = rect.height;
    canvas.width = Math.round(w * dpr);
    canvas.height = Math.round(h * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    populate();
  }

  function maybeShoot() {
    if (shooting || Math.random() > 0.004) return;
    shooting = {
      x: Math.random() * w * 0.7,
      y: Math.random() * h * 0.4,
      len: 90 + Math.random() * 120,
      life: 0,
      span: 34 + Math.random() * 18,
    };
  }

  function draw(animate) {
    ctx.clearRect(0, 0, w, h);

    // Eased, so moving the mouse glides the field instead of snapping it.
    pointer.x += (pointer.tx - pointer.x) * 0.05;
    pointer.y += (pointer.ty - pointer.y) * 0.05;
    const shiftX = (pointer.x - 0.5) * 26;
    const shiftY = (pointer.y - 0.5) * 18;

    const placed = stars.map((s) => {
      if (animate) {
        s.x += s.vx * s.z;
        s.y += s.vy * s.z;
        s.tw += 0.01 + s.z * 0.015;
        // Wrap rather than respawn, so density stays flat over time.
        if (s.y < -4) s.y = h + 4;
        if (s.x < -4) s.x = w + 4;
        if (s.x > w + 4) s.x = -4;
      }
      return { s, x: s.x + shiftX * s.z, y: s.y + shiftY * s.z };
    });

    // Lines first, so a star always sits on top of the threads it anchors.
    ctx.lineWidth = 0.7;
    for (let i = 0; i < placed.length; i++) {
      let links = 0;
      for (let j = i + 1; j < placed.length && links < MAX_LINKS; j++) {
        const dx = placed[i].x - placed[j].x;
        const dy = placed[i].y - placed[j].y;
        const d = Math.hypot(dx, dy);
        if (d > NEAR) continue;
        links++;
        const fade = (1 - d / NEAR) * 0.55 * (0.5 + placed[i].s.z * 0.5);
        ctx.strokeStyle = `rgba(125, 184, 140, ${fade.toFixed(3)})`;
        ctx.beginPath();
        ctx.moveTo(placed[i].x, placed[i].y);
        ctx.lineTo(placed[j].x, placed[j].y);
        ctx.stroke();
      }
    }

    for (const p of placed) {
      const twinkle = animate ? 0.78 + Math.sin(p.s.tw) * 0.22 : 1;
      // Depth is halved into the alpha rather than driving it outright. A far
      // star at its own z disappears entirely under a bright light, and radius
      // and speed still carry the parallax on their own.
      ctx.globalAlpha = Math.min(1, (0.5 + p.s.z * 0.5) * twinkle);
      ctx.fillStyle = p.s.z > 0.8 ? "#e6f2e9" : "#c2d3c7";
      ctx.beginPath();
      ctx.arc(p.x, p.y, p.s.r * p.s.z, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.globalAlpha = 1;

    if (!animate) return;

    maybeShoot();
    if (shooting) {
      shooting.life++;
      const t = shooting.life / shooting.span;
      const x = shooting.x + t * shooting.len * 1.7;
      const y = shooting.y + t * shooting.len;
      // Fades in and back out rather than popping, so it reads as a streak.
      const alpha = Math.sin(Math.PI * t) * 0.7;
      const grad = ctx.createLinearGradient(x, y, x - 46, y - 27);
      grad.addColorStop(0, `rgba(219, 234, 223, ${alpha.toFixed(3)})`);
      grad.addColorStop(1, "rgba(219, 234, 223, 0)");
      ctx.strokeStyle = grad;
      ctx.lineWidth = 1.1;
      ctx.beginPath();
      ctx.moveTo(x, y);
      ctx.lineTo(x - 46, y - 27);
      ctx.stroke();
      if (shooting.life > shooting.span) shooting = null;
    }
  }

  function loop() {
    draw(true);
    frame = requestAnimationFrame(loop);
  }

  function start() {
    stop();
    if (still.matches) {
      // One frame and no timer: the field is there to look at, it just does not
      // move for anybody who asked for that.
      draw(false);
      return;
    }
    frame = requestAnimationFrame(loop);
  }

  function stop() {
    if (frame !== null) cancelAnimationFrame(frame);
    frame = null;
  }

  const observer = new ResizeObserver(() => {
    resize();
    if (still.matches) draw(false);
  });
  observer.observe(canvas);

  window.addEventListener("pointermove", (e) => {
    pointer.tx = e.clientX / window.innerWidth;
    pointer.ty = e.clientY / window.innerHeight;
  });

  // A background tab still runs rAF in some browsers and always burns battery
  // in the rest, and nobody is looking at it.
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) stop();
    else start();
  });

  still.addEventListener("change", start);

  resize();
  start();
}

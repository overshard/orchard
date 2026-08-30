// Drifting points that draw a line to every neighbour within 150px, brighter
// the closer they are, so constellations build and dissolve on their own.

export const constellations = (cvs, { numStars = 50 } = {}) => {
  const ctx = cvs.getContext("2d");
  const starDistance = 150;

  const resize = () => {
    cvs.width = cvs.offsetWidth;
    cvs.height = cvs.offsetHeight;
  };
  resize();
  window.addEventListener("resize", resize);

  let stars = [];
  for (let i = 0; i < numStars; i++) {
    stars.push({
      loc: [cvs.width * Math.random(), cvs.height * Math.random()],
      dir: [Math.random() > 0.5 ? 1 : -1, Math.random() > 0.5 ? 1 : -1],
    });
  }

  let frame = null;
  let running = false;

  const draw = () => {
    ctx.clearRect(0, 0, cvs.width, cvs.height);

    stars.forEach((star) => {
      ctx.beginPath();
      ctx.arc(star.loc[0], star.loc[1], 2, 0, 2 * Math.PI);
      ctx.fillStyle = "rgb(255, 255, 255)";
      ctx.fill();
      ctx.closePath();

      stars.forEach((closeStar) => {
        const distance = Math.hypot(
          star.loc[0] - closeStar.loc[0],
          star.loc[1] - closeStar.loc[1]
        );
        if (distance >= starDistance) return;
        ctx.beginPath();
        ctx.moveTo(star.loc[0], star.loc[1]);
        ctx.lineTo(closeStar.loc[0], closeStar.loc[1]);
        ctx.strokeStyle = `rgba(255, 255, 255, ${
          (starDistance - distance) / starDistance
        })`;
        ctx.stroke();
        ctx.closePath();
      });
    });

    stars.forEach((star) => {
      if (star.loc[0] < 0) star.dir[0] = 1;
      else if (star.loc[0] > cvs.width) star.dir[0] = -1;
      if (star.loc[1] < 0) star.dir[1] = 1;
      else if (star.loc[1] > cvs.height) star.dir[1] = -1;

      star.loc[0] += star.dir[0] * 0.5;
      star.loc[1] += star.dir[1] * 0.5;
    });

    if (running) frame = window.requestAnimationFrame(draw);
  };

  return {
    start() {
      if (running) return;
      running = true;
      frame = window.requestAnimationFrame(draw);
    },
    stop() {
      running = false;
      if (frame) window.cancelAnimationFrame(frame);
      frame = null;
    },
  };
};

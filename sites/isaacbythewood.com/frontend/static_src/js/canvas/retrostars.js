// Three star fields at different depths, offset against the pointer for
// parallax, twinkling on a four-step cycle. Pixel-art sized: everything is a
// 5px square, nothing is anti-aliased.

const COLORS = ["white", "yellow", "red", "green", "blue"];

export const retrostars = (cvs, { numStars = 50 } = {}) => {
  const ctx = cvs.getContext("2d");

  const resize = () => {
    cvs.width = cvs.offsetWidth;
    cvs.height = cvs.offsetHeight;
  };
  resize();
  window.addEventListener("resize", resize);

  // Never React state, even in the original: the draw loop reads these
  // directly, and storing them in state would have re-rendered the component
  // on every single mousemove.
  const mouse = { x: 640, y: 400 };
  let starSize = 0;

  window.addEventListener(
    "mousemove",
    (e) => {
      mouse.x = e.clientX;
      mouse.y = e.clientY;
    },
    { passive: true }
  );

  const offsetStar = (maxOffset) => ({
    x: -Math.floor((mouse.x / window.innerWidth) * maxOffset),
    y: -Math.floor((mouse.y / window.innerHeight) * maxOffset),
  });

  const makeStar = () => ({
    loc: [cvs.width * Math.random(), cvs.height * Math.random()],
    color: COLORS[Math.floor(Math.random() * COLORS.length)],
  });

  const smallStars = [];
  const mediumStars = [];
  const largeStars = [];
  for (let i = 0; i < numStars; i++) {
    smallStars.push(makeStar());
    if (mediumStars.length < i / 4) mediumStars.push(makeStar());
    if (largeStars.length < i / 8) largeStars.push(makeStar());
  }

  const square = (color, x, y, w = 5, h = 5) => {
    ctx.beginPath();
    ctx.fillStyle = color;
    ctx.fillRect(x, y, w, h);
    ctx.closePath();
  };

  // A plus sign of 5px squares: the "twinkling" state for medium and large
  // stars.
  const plus = (color, x, y, arm = 5) => {
    square(color, x, y);
    square(color, x - arm, y);
    square(color, x, y - arm);
    square(color, x + arm, y);
    square(color, x, y + arm);
  };

  let frame = null;
  let running = false;
  let twinkle = null;

  const draw = () => {
    ctx.clearRect(0, 0, cvs.width, cvs.height);

    const smallOffset = offsetStar(25);
    const mediumOffset = offsetStar(75);
    const largeOffset = offsetStar(125);

    smallStars.forEach(({ loc, color }) => {
      square(color, loc[0] + smallOffset.x, loc[1] + smallOffset.y);
    });

    mediumStars.forEach(({ loc, color }) => {
      const x = loc[0] + mediumOffset.x;
      const y = loc[1] + mediumOffset.y;
      if (starSize === 0 || starSize === 2) square(color, x, y);
      else plus(color, x, y);
    });

    largeStars.forEach(({ loc, color }) => {
      const x = loc[0] + largeOffset.x;
      const y = loc[1] + largeOffset.y;
      if (starSize === 0) {
        square(color, x, y);
      } else if (starSize === 1 || starSize === 3) {
        plus(color, x, y);
      } else {
        // Peak twinkle: a filled block with arms and a hole punched out of the
        // middle, which reads as a bright flare rather than a bigger dot.
        square(color, x - 5, y - 5, 15, 15);
        square(color, x - 10, y);
        square(color, x, y - 10);
        square(color, x + 10, y);
        square(color, x, y + 10);
        square("black", x, y);
      }
    });

    if (running) frame = window.requestAnimationFrame(draw);
  };

  return {
    start() {
      if (running) return;
      running = true;
      twinkle = window.setInterval(() => {
        starSize = (starSize + 1) % 4;
      }, 500);
      frame = window.requestAnimationFrame(draw);
    },
    stop() {
      running = false;
      if (frame) window.cancelAnimationFrame(frame);
      if (twinkle) window.clearInterval(twinkle);
      frame = null;
      twinkle = null;
    },
  };
};

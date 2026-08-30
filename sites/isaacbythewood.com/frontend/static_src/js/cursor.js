// The custom cursor, a dot tracking the pointer and a ring that grows over
// anything interactive. Positions are written in a requestAnimationFrame loop
// rather than in the mousemove handler, so a burst costs one layout per frame.

export const initCursor = () => {
  const dot = document.querySelector(".cursor");
  const circle = document.querySelector(".cursor-circle");
  if (!dot || !circle) return;

  const pos = { x: 0, y: 0 };

  document.addEventListener(
    "mousemove",
    (e) => {
      pos.x = e.clientX;
      pos.y = e.clientY;
    },
    { passive: true }
  );

  document.addEventListener(
    "mouseover",
    (e) => {
      const target = e.target;
      const interactive =
        target.tagName === "BUTTON" ||
        target.tagName === "A" ||
        !!target.closest("a, button") ||
        (target.classList && target.classList.contains("mouse-activate"));
      circle.classList.toggle("is-active", interactive);
    },
    { passive: true }
  );

  const animate = () => {
    const transform = `translate3d(${pos.x}px, ${pos.y}px, 0)`;
    dot.style.transform = transform;
    circle.style.transform = transform;
    window.requestAnimationFrame(animate);
  };

  window.requestAnimationFrame(animate);
};

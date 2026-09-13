// Swiping the photo set, because every other way of seeing the second photo on a
// phone means opening the listing and coming back.
//
// The images are all in the DOM and all but the first are hidden and lazy, so the
// browser fetches one only when it is swiped to. Nothing is preloaded and nothing
// is a background image, which keeps a grid of twenty houses to twenty requests.

function show(frames, index) {
  const imgs = frames.querySelectorAll("img");
  if (!imgs.length) return;
  const next = (index + imgs.length) % imgs.length;

  imgs.forEach((img, i) => {
    img.hidden = i !== next;
  });

  const dots = frames.parentElement.querySelectorAll(".dots span");
  dots.forEach((dot, i) => dot.classList.toggle("on", i === next));

  frames.dataset.at = String(next);
}

function at(frames) {
  return Number(frames.dataset.at || 0);
}

document.querySelectorAll(".frames").forEach((frames) => {
  if (frames.querySelectorAll("img").length < 2) return;

  // A tap is a tap and a drag is a drag. 30px is past the wobble of a thumb
  // pressing a link and well short of a deliberate swipe.
  let startX = null;
  let startY = null;
  const threshold = 30;

  frames.addEventListener(
    "touchstart",
    (e) => {
      startX = e.touches[0].clientX;
      startY = e.touches[0].clientY;
    },
    { passive: true },
  );

  frames.addEventListener("touchend", (e) => {
    if (startX === null) return;
    const dx = e.changedTouches[0].clientX - startX;
    const dy = e.changedTouches[0].clientY - startY;
    startX = null;

    // A mostly vertical movement is the page being scrolled, not a swipe.
    if (Math.abs(dx) < threshold || Math.abs(dy) > Math.abs(dx)) return;

    // The anchor would otherwise open the listing on the way up from a swipe.
    e.preventDefault();
    show(frames, at(frames) + (dx < 0 ? 1 : -1));
  });

  // On a mouse there is no swipe, so hovering across the photo cycles it, which
  // is what a desktop visitor expects from a listing grid.
  frames.addEventListener("mousemove", (e) => {
    const imgs = frames.querySelectorAll("img");
    const rect = frames.getBoundingClientRect();
    const fraction = (e.clientX - rect.left) / rect.width;
    show(frames, Math.min(imgs.length - 1, Math.floor(fraction * imgs.length)));
  });

  frames.addEventListener("mouseleave", () => show(frames, 0));
});

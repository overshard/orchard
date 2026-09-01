// Six boxes for the six digit code, which is what every code entry worth using
// does now: one character per field, focus follows typing, and it submits itself
// the moment the last one is filled.
//
// Progressive enhancement rather than markup. The template renders an ordinary
// single field and hides the boxes, so a browser with no JavaScript still has a
// working sign in, and this swaps them over on load.

const LEN = 6;

export function otp() {
  const form = document.querySelector("[data-otp-form]");
  if (!form) return;

  const wrap = form.querySelector("[data-otp]");
  const fallback = form.querySelector("[data-otp-fallback]");
  if (!wrap || !fallback) return;

  const boxes = Array.from(wrap.querySelectorAll("input"));
  if (boxes.length !== LEN) return;

  // type=hidden rather than hidden=true, so a `required` field the browser
  // cannot see never blocks the submit with a validation message pointing at
  // nothing.
  fallback.type = "hidden";
  fallback.required = false;
  wrap.hidden = false;

  let submitted = false;

  const value = () => boxes.map((b) => b.value).join("");

  function maybeSubmit() {
    if (submitted || value().length !== LEN) return;
    submitted = true;
    fallback.value = value();
    for (const b of boxes) b.blur();
    form.requestSubmit();
  }

  function fill(from, digits) {
    let i = from;
    for (const d of digits) {
      if (i >= LEN) break;
      boxes[i].value = d;
      i++;
    }
    // Land on the first empty box, or the last one when the code is complete.
    const next = boxes.findIndex((b) => b.value === "");
    boxes[next === -1 ? LEN - 1 : next].focus();
    maybeSubmit();
  }

  boxes.forEach((box, i) => {
    box.addEventListener("input", () => {
      // A phone's autofill drops the whole code into the first box, so this is
      // the paste path as much as the typing one.
      const digits = box.value.replace(/\D/g, "");
      box.value = "";
      if (!digits) return;
      fill(i, digits);
    });

    box.addEventListener("keydown", (e) => {
      if (e.key === "Backspace" && box.value === "" && i > 0) {
        e.preventDefault();
        boxes[i - 1].value = "";
        boxes[i - 1].focus();
        return;
      }
      if (e.key === "ArrowLeft" && i > 0) {
        e.preventDefault();
        boxes[i - 1].focus();
      }
      if (e.key === "ArrowRight" && i < LEN - 1) {
        e.preventDefault();
        boxes[i + 1].focus();
      }
    });

    box.addEventListener("paste", (e) => {
      e.preventDefault();
      const digits = (e.clipboardData || window.clipboardData)
        .getData("text")
        .replace(/\D/g, "");
      if (digits) fill(i, digits);
    });

    // Selecting the contents means typing over a digit replaces it rather than
    // being ignored, since maxlength is already reached.
    box.addEventListener("focus", () => box.select());
  });

  boxes[0].focus();
}

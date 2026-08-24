// A minimal stand-in for react-transition-group's CSSTransition.
//
// Two pages animate words in and out using the enter / enter-active /
// enter-done / exit / exit-active class sequence, and their CSS is written
// against exactly those states. Reproducing the state machine keeps the
// stylesheets working unchanged; hand-rolling the animations instead would
// have meant rewriting both pages' CSS as well as their JS.
//
// The forced reflow is not superstition. Adding the "from" class and the
// "active" class in the same frame means the browser only ever computes the
// end state, and no transition runs at all.

const nextFrame = (fn) => requestAnimationFrame(() => requestAnimationFrame(fn));

/** Play the enter sequence on an element. Classes: {from, active, done}. */
export const enter = (el, classes, duration) => {
  el.classList.add(classes.from);
  void el.offsetWidth;

  nextFrame(() => {
    el.classList.add(classes.active);
    window.setTimeout(() => {
      el.classList.remove(classes.from, classes.active);
      if (classes.done) el.classList.add(classes.done);
    }, duration);
  });
};

/** Play the exit sequence, then run onDone (usually removing the element). */
export const exit = (el, classes, duration, onDone) => {
  if (classes.clear) el.classList.remove(...classes.clear);
  el.classList.add(classes.from);
  void el.offsetWidth;

  nextFrame(() => {
    el.classList.add(classes.active);
    window.setTimeout(() => {
      el.classList.remove(classes.from, classes.active);
      if (onDone) onDone(el);
    }, duration);
  });
};

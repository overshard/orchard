// The report polls while the background assessment runs and says what it is
// waiting on rather than showing a bare spinner. Polling rather than a stream
// because it is one small answer a few times a minute, and a held-open connection
// for that is more machinery than the question deserves.

const panel = document.querySelector(".working");

if (panel) {
  const id = panel.dataset.id;
  const list = document.getElementById("working-list");
  const head = document.getElementById("working-head");
  const sub = document.getElementById("working-sub");

  const every = 3000;
  const giveUpAfter = 20 * 60 * 1000;
  const started = Date.now();

  const roughly = (seconds) => {
    if (seconds <= 30) return "under a minute left";
    const mins = Math.round(seconds / 60);
    return mins <= 1 ? "about a minute left" : `about ${mins} minutes left`;
  };

  // A run that died with the server still has its row saying it never finished,
  // so the page has to be able to say so rather than sit on a hopeful message.
  const stalled = () => {
    if (head) head.textContent = "This one stopped partway";
    if (sub) {
      sub.textContent =
        "Something interrupted it. Nothing is lost and nothing needs clicking: " +
        "it picks the rest up by itself within a few minutes.";
    }
  };

  const render = (body) => {
    const left = body.total - body.done;

    if (head) {
      head.textContent = left === 0 ? "Working out the drives" : `${body.done} of ${body.total} answered`;
    }
    if (sub) {
      const bits = [];
      if (body.seconds > 0 && left > 0) bits.push(roughly(body.seconds));
      if (body.cached > 0) bits.push(`${body.cached} we already had from an address near here`);
      sub.textContent = bits.length
        ? bits.join(", ") + ". Nothing needs clicking and you can leave and come back."
        : "Nothing needs clicking and you can leave and come back.";
    }
    if (!list) return;

    list.innerHTML = "";
    for (const s of body.sources || []) {
      const li = document.createElement("li");
      li.className = s.Done ? (s.Cached ? "had" : "got") : s.Failed ? "failed" : "waiting";

      const mark = document.createElement("span");
      mark.className = "mark";
      mark.textContent = s.Done ? "✓" : s.Failed ? "!" : "·";

      const what = document.createElement("span");
      what.className = "what";
      what.textContent = s.Name;

      const who = document.createElement("span");
      who.className = "who";
      who.textContent = s.Note
        ? s.Note
        : s.Done
          ? s.Cached
            ? "already saved from an address near here"
            : `from ${s.Who}`
          : `asking ${s.Who}`;

      li.append(mark, what, who);
      list.append(li);
    }
  };

  const tick = async () => {
    if (Date.now() - started > giveUpAfter) {
      stalled();
      return;
    }
    try {
      const res = await fetch(`/listing/${id}/status`);
      const body = await res.json();

      // Wait for the run to end rather than for the facts row to appear. The row
      // is written partway through, and reloading on it took the panel away while
      // the slow sources were still being asked for.
      if (!body.running) {
        if (body.ready) {
          window.location.reload();
          return;
        }
        stalled();
        return;
      }
      render(body);
    } catch (err) {
      // A failed poll is not worth reporting. The next one is three seconds away.
    }
    setTimeout(tick, every);
  };

  tick();
}

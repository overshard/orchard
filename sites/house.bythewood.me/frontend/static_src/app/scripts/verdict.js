// Thumbs up and down, posted without leaving the grid. On a phone a round trip
// to a detail page and back to rate a house is the difference between rating
// twenty and rating three.

async function save(id, body) {
  const res = await fetch(`/listing/${id}/verdict`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });
  if (!res.ok) throw new Error(`verdict ${res.status}`);
  return res.json();
}

function paint(scope, rating) {
  scope.querySelectorAll(".thumb").forEach((b) => {
    b.classList.toggle("on", Number(b.dataset.rating) === rating && rating !== 0);
  });
}

document.querySelectorAll(".verdict, .verdict-form").forEach((scope) => {
  const id = scope.dataset.id;

  scope.querySelectorAll(".thumb").forEach((button) => {
    button.addEventListener("click", async () => {
      // A second press on the rating already set clears it, so a misfire is
      // undone with the same thumb that caused it.
      const pressed = Number(button.dataset.rating);
      const already = button.classList.contains("on");
      const rating = already ? 0 : pressed;

      paint(scope, rating);
      try {
        await save(id, new URLSearchParams({ rating: String(rating) }));
      } catch (err) {
        // Put the buttons back rather than leave the page claiming something
        // that was never written.
        paint(scope, already ? pressed : 0);
        console.error(err);
      }
    });
  });
});

// Delete, which is not the same as a thumbs down. Down hides a house and keeps
// what we worked out about it, and this throws the lot away, so it asks once
// rather than trusting a thumb that missed.
document.querySelectorAll("[data-delete]").forEach((button) => {
  const scope = button.closest(".verdict, .verdict-form");
  if (!scope) return;

  let armed = false;
  const original = button.innerHTML;

  const disarm = () => {
    armed = false;
    button.innerHTML = original;
    button.classList.remove("armed");
  };

  button.addEventListener("click", async () => {
    if (!armed) {
      armed = true;
      button.classList.add("armed");
      button.textContent = "Sure?";
      setTimeout(() => armed && disarm(), 5000);
      return;
    }

    button.disabled = true;
    try {
      const res = await fetch(`/listing/${scope.dataset.id}/delete`, { method: "POST" });
      if (!res.ok) throw new Error(`delete ${res.status}`);

      // On the report there is nothing left to look at, so go back to the grid.
      // On a card, take the card away and leave the rest of the grid alone.
      const card = button.closest(".card");
      if (card) {
        card.remove();
      } else {
        window.location.href = "/";
      }
    } catch (err) {
      button.disabled = false;
      disarm();
      console.error(err);
    }
  });
});

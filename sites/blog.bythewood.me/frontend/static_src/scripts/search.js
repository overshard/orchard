// Live search for any "id_search" field, against /search/live/?q=<query>.
// Arrow keys navigate the results and Enter follows the selected link.

document.addEventListener("DOMContentLoaded", function () {
  const searchEl = document.getElementById("id_search");
  if (!searchEl) return;

  let activeIndex = -1;

  const getItems = () => {
    const container = searchEl.parentElement.querySelector("#id_search_results ul");
    return container ? Array.from(container.querySelectorAll("a")) : [];
  };

  const setActive = (index) => {
    const items = getItems();
    items.forEach((item) => item.classList.remove("active"));
    activeIndex = index;
    if (index >= 0 && index < items.length) {
      items[index].classList.add("active");
      items[index].scrollIntoView({ block: "nearest" });
    }
  };

  const clearResults = () => {
    const resultsEl = searchEl.parentElement.querySelector("#id_search_results");
    if (resultsEl) {
      searchEl.parentElement.removeChild(resultsEl);
    }
    activeIndex = -1;
  };

  searchEl.addEventListener("keydown", function (e) {
    const items = getItems();
    if (!items.length) return;

    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive(activeIndex < items.length - 1 ? activeIndex + 1 : 0);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(activeIndex > 0 ? activeIndex - 1 : items.length - 1);
    } else if (e.key === "Enter" && activeIndex >= 0 && activeIndex < items.length) {
      e.preventDefault();
      window.location.href = items[activeIndex].href;
    }
  });

  searchEl.addEventListener("keyup", function (e) {
    if (["ArrowDown", "ArrowUp", "Enter"].includes(e.key)) return;

    const query = searchEl.value;
    if (query.length === 0) {
      clearResults();
      return;
    }

    const url = "/search/live/?q=" + encodeURIComponent(query);
    fetch(url)
      .then((response) => response.json())
      .then((data) => {
        clearResults();
        if (!data.length) return;

        const resultsEl = document.createElement("div");
        resultsEl.id = "id_search_results";
        searchEl.parentElement.appendChild(resultsEl);

        const resultsDiv = document.createElement("ul");
        resultsDiv.classList.add("list-group", "rounded-bottom", "position-absolute", "mt-2");
        resultsDiv.style.zIndex = "1100";
        resultsDiv.style.width = searchEl.offsetWidth + "px";
        resultsDiv.style.backgroundColor = "#13120e";
        resultsDiv.style.border = "1px solid rgba(107,158,120,0.15)";
        resultsEl.appendChild(resultsDiv);

        // Rows are built from text nodes, so a query with regex metacharacters
        // ("c++") cannot throw and post text is never parsed as HTML.
        const escapeRegExp = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
        const appendHighlighted = (parent, text, strongClass) => {
          const re = new RegExp(escapeRegExp(query), "gi");
          let last = 0;
          let m;
          while ((m = re.exec(text)) !== null) {
            parent.appendChild(document.createTextNode(text.slice(last, m.index)));
            const strong = document.createElement("strong");
            if (strongClass) strong.className = strongClass;
            strong.textContent = m[0];
            parent.appendChild(strong);
            last = m.index + m[0].length;
          }
          parent.appendChild(document.createTextNode(text.slice(last)));
        };

        data.forEach((result) => {
          const a = document.createElement("a");
          a.classList.add("list-group-item", "list-group-item-action");
          const titleSpan = document.createElement("span");
          titleSpan.className = "fw-bold";
          appendHighlighted(titleSpan, result.title, "");
          const descSpan = document.createElement("span");
          descSpan.className = "text-muted";
          appendHighlighted(descSpan, result.description, "fw-bolder");
          a.appendChild(titleSpan);
          a.appendChild(document.createElement("br"));
          a.appendChild(descSpan);
          a.href = result.url;
          resultsDiv.appendChild(a);
        });
      });
  });

  searchEl.addEventListener("blur", function (e) {
    const searchResults = searchEl.parentElement.querySelector("#id_search_results");
    if (searchResults && !searchResults.contains(e.relatedTarget)) {
      clearResults();
    }
  });
});

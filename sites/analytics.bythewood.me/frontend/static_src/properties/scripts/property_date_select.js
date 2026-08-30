document.addEventListener("DOMContentLoaded", function () {
  const dateRange = document.getElementById("date-range");
  const dateStart = document.getElementById("date-start");
  const dateEnd = document.getElementById("date-end");
  if (!dateRange) return;

  const url = new URL(window.location.href);
  const date_range = url.searchParams.get("date_range");

  if (date_range) {
    dateRange.value = date_range;
  }

  dateRange.addEventListener("change", function () {
    const date_range = parseInt(dateRange.value);
    let dateEndValue = new Date();
    let dateStartValue = new Date(dateEndValue.getTime() - date_range * 86400000);

    dateEndValue = dateEndValue.toISOString().split("T")[0];
    dateStartValue = dateStartValue.toISOString().split("T")[0];

    dateEnd.value = dateEndValue;
    dateStart.value = dateStartValue;

    const form = dateRange.closest("form");
    form.submit();
  });

  dateStart.addEventListener("change", function () {
    if (dateStart.value && dateEnd.value) {
      if (new Date(dateStart.value) > new Date(dateEnd.value)) {
        const temp = dateStart.value;
        dateStart.value = dateEnd.value;
        dateEnd.value = temp;
      }
      dateRange.value = "custom";
      const form = dateStart.closest("form");
      form.submit();
    }
  });
  dateEnd.addEventListener("change", function () {
    if (dateStart.value && dateEnd.value) {
      if (new Date(dateStart.value) > new Date(dateEnd.value)) {
        const temp = dateStart.value;
        dateStart.value = dateEnd.value;
        dateEnd.value = temp;
      }
      dateRange.value = "custom";
      const form = dateEnd.closest("form");
      form.submit();
    }
  });
});

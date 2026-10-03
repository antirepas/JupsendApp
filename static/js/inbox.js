(function () {
  var root = document.getElementById("inbox-fm");
  if (!root) return;

  var threads = root.querySelectorAll(".inbox-thread");
  threads.forEach(function (el) {
    el.addEventListener("keydown", function (e) {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        el.click();
      }
    });
  });

  var reading = root.querySelector(".inbox-reading");
  if (reading && root.getAttribute("data-selected") && Number(root.getAttribute("data-selected")) > 0) {
    var messages = reading.querySelector(".inbox-messages");
    if (messages) {
      messages.scrollTop = messages.scrollHeight;
    }
  }
})();

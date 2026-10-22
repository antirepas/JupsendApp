(function () {
  function resizeFrames(scope) {
    (scope || document).querySelectorAll(".inbox-msg-frame").forEach(function (frame) {
      function apply() {
        try {
          var doc = frame.contentDocument || (frame.contentWindow && frame.contentWindow.document);
          if (!doc || !doc.body) return;
          var h = Math.max(doc.body.scrollHeight, doc.documentElement.scrollHeight, 80);
          frame.style.height = Math.min(Math.max(h + 12, 80), 640) + "px";
        } catch (e) {}
      }
      apply();
      frame.addEventListener("load", apply);
    });
  }

  function bindComposer() {
    var form = document.querySelector(".inbox-composer-form");
    if (!form) return;
    var body = form.querySelector("#inbox-reply-body");
    if (!body) return;
    body.addEventListener("keydown", function (e) {
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
        e.preventDefault();
        form.requestSubmit();
      }
    });
  }

  var threadPage = document.getElementById("inbox-thread-page");
  if (threadPage) {
    var stream = document.getElementById("inbox-messages");
    if (stream) stream.scrollTop = stream.scrollHeight;
    resizeFrames(threadPage);
    setTimeout(function () { resizeFrames(threadPage); }, 80);
    bindComposer();
    var reply = document.getElementById("inbox-reply-body");
    if (reply) reply.focus({ preventScroll: true });
  }
})();

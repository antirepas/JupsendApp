(function () {
  var root = document.getElementById("inbox-fm");
  if (!root) return;

  var reading = document.getElementById("inbox-reading");
  var folder = root.getAttribute("data-folder") || "all";
  var loadToken = 0;

  function resizeFrames(scope) {
    (scope || document).querySelectorAll(".inbox-msg-frame").forEach(function (frame) {
      try {
        var doc = frame.contentDocument || (frame.contentWindow && frame.contentWindow.document);
        if (!doc || !doc.body) return;
        var h = Math.max(doc.body.scrollHeight, doc.documentElement.scrollHeight, 120);
        frame.style.height = Math.min(Math.max(h + 16, 120), 520) + "px";
      } catch (e) {}
    });
  }

  function scrollMessages() {
    if (!reading) return;
    var messages = reading.querySelector(".inbox-messages");
    if (messages) messages.scrollTop = messages.scrollHeight;
    resizeFrames(reading);
    setTimeout(function () { resizeFrames(reading); }, 60);
  }

  function focusComposer() {
    if (!reading) return;
    var body = reading.querySelector("#inbox-reply-body");
    if (body) body.focus({ preventScroll: true });
  }

  function bindComposer(scope) {
    var form = (scope || document).querySelector(".inbox-composer-form");
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

  function afterPaneReady() {
    scrollMessages();
    focusComposer();
    bindComposer(reading);
  }

  function setActiveThread(contactId) {
    root.querySelectorAll(".inbox-thread").forEach(function (el) {
      var active = String(el.getAttribute("data-contact-id")) === String(contactId);
      el.classList.toggle("is-active", active);
      el.setAttribute("aria-selected", active ? "true" : "false");
      if (active) el.classList.remove("is-unread");
    });
    root.setAttribute("data-selected", String(contactId || 0));
  }

  function paneURL(contactId) {
    var url = "/inbox/threads/" + encodeURIComponent(contactId) + "/pane";
    if (folder && folder !== "all") url += "?folder=" + encodeURIComponent(folder);
    return url;
  }

  function pageURL(contactId) {
    var params = new URLSearchParams();
    if (folder && folder !== "all") params.set("folder", folder);
    if (contactId) params.set("contact", String(contactId));
    var q = new URLSearchParams(window.location.search).get("q");
    if (q) params.set("q", q);
    var qs = params.toString();
    return "/inbox" + (qs ? "?" + qs : "");
  }

  function updateBadge(unread) {
    if (unread == null || unread === "") return;
    var n = Number(unread);
    document.querySelectorAll('a[href="/inbox"] .nav-badge').forEach(function (badge) {
      if (!Number.isFinite(n) || n <= 0) {
        badge.remove();
        return;
      }
      badge.textContent = String(n);
    });
    var unreadCount = root.querySelector('.tpl-fm-nav-item[href="/inbox?folder=unread"] .tpl-fm-nav-count');
    if (unreadCount && Number.isFinite(n)) unreadCount.textContent = String(n);
  }

  function loadPane(contactId, pushHistory) {
    if (!reading || !contactId) return;
    var token = ++loadToken;
    setActiveThread(contactId);
    reading.classList.add("is-loading");
    fetch(paneURL(contactId), {
      headers: { Accept: "text/html", "X-Requested-With": "inbox-pane" },
      credentials: "same-origin",
    })
      .then(function (res) {
        if (!res.ok) throw new Error("Failed to load thread");
        updateBadge(res.headers.get("X-Inbox-Unread"));
        return res.text();
      })
      .then(function (html) {
        if (token !== loadToken) return;
        reading.innerHTML = html;
        reading.classList.remove("is-loading");
        afterPaneReady();
        if (pushHistory) {
          history.pushState({ inboxContact: Number(contactId) }, "", pageURL(contactId));
        }
      })
      .catch(function () {
        if (token !== loadToken) return;
        reading.classList.remove("is-loading");
        window.location.href = pageURL(contactId);
      });
  }

  root.querySelectorAll(".inbox-thread").forEach(function (el) {
    el.addEventListener("click", function (e) {
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return;
      e.preventDefault();
      var id = el.getAttribute("data-contact-id");
      if (!id) return;
      loadPane(id, true);
    });
    el.addEventListener("keydown", function (e) {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        el.click();
      }
    });
  });

  window.addEventListener("popstate", function () {
    var params = new URLSearchParams(window.location.search);
    var contact = params.get("contact");
    if (contact) loadPane(contact, false);
  });

  afterPaneReady();
})();

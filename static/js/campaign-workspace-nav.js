(function () {
  "use strict";

  var arrows = document.querySelectorAll("a.camp-ws-file-arrow");
  if (!arrows.length) return;

  var dirty = false;
  var pendingURL = "";

  function isDirty() {
    if (typeof window.__templateIsDirty === "function" && window.__templateIsDirty()) return true;
    if (typeof window.__workflowIsDirty === "function" && window.__workflowIsDirty()) return true;
    if (typeof window.__sheetIsDirty === "function" && window.__sheetIsDirty()) return true;
    return dirty;
  }

  function markDirty() {
    dirty = true;
  }

  function markClean() {
    dirty = false;
    if (typeof window.__templateMarkClean === "function") window.__templateMarkClean();
    if (typeof window.__workflowMarkClean === "function") window.__workflowMarkClean();
    if (typeof window.__sheetMarkClean === "function") window.__sheetMarkClean();
  }

  var tplForm = document.getElementById("template-form");
  if (document.getElementById("canvas")) {
    document.getElementById("canvas").addEventListener("mousedown", markDirty, { once: false });
  }
  if (document.getElementById("lib-sheet")) {
    document.getElementById("lib-sheet").addEventListener("input", markDirty);
    document.getElementById("lib-sheet").addEventListener("change", markDirty);
  }

  var modal = document.getElementById("camp-ws-leave-modal");
  if (!modal) {
    modal = document.createElement("dialog");
    modal.id = "camp-ws-leave-modal";
    modal.className = "camp-ws-modal";
    modal.innerHTML =
      '<div class="camp-ws-modal-card">' +
      "<h3 class=\"font-semibold text-lg text-slate-900\">Save your progress?</h3>" +
      '<p class="text-sm text-slate-500 mt-1 mb-4">You have unsaved changes on this page.</p>' +
      '<div class="flex flex-wrap justify-end gap-2">' +
      '<button type="button" class="btn-secondary" data-leave="cancel">Stay</button>' +
      '<button type="button" class="btn-secondary" data-leave="discard">Leave without saving</button>' +
      '<button type="button" class="btn-primary" data-leave="save">Save &amp; continue</button>' +
      "</div></div>";
    document.body.appendChild(modal);
  }

  function go(url) {
    pendingURL = "";
    markClean();
    location.href = url;
  }

  async function saveTemplateViaFetch() {
    if (!tplForm) return false;
    if (typeof window.__templateSyncBody === "function") window.__templateSyncBody();
    var fd = new FormData(tplForm);
    var res = await fetch(tplForm.action, {
      method: "POST",
      body: fd,
      credentials: "same-origin",
      redirect: "manual",
    });
    if (res.type === "opaqueredirect" || (res.status >= 200 && res.status < 400)) {
      markClean();
      return true;
    }
    return false;
  }

  async function trySaveThenGo(url) {
    if (tplForm) {
      try {
        var ok = await saveTemplateViaFetch();
        if (!ok) {
          alert("Could not save. Stay on this page and try Save again.");
          return;
        }
        go(url);
        return;
      } catch (err) {
        alert("Could not save. Stay on this page and try Save again.");
        return;
      }
    }
    var saveBtn = document.getElementById("btn-save");
    if (saveBtn) {
      saveBtn.click();
      setTimeout(function () {
        go(url);
      }, 700);
      return;
    }
    go(url);
  }

  modal.addEventListener("click", function (e) {
    var btn = e.target.closest("[data-leave]");
    if (!btn) return;
    var action = btn.getAttribute("data-leave");
    if (action === "cancel") {
      modal.close();
      pendingURL = "";
      return;
    }
    if (action === "discard") {
      modal.close();
      go(pendingURL);
      return;
    }
    if (action === "save") {
      modal.close();
      trySaveThenGo(pendingURL);
    }
  });

  arrows.forEach(function (a) {
    a.addEventListener("click", function (e) {
      var href = a.getAttribute("href");
      if (!href || href === "#") return;
      if (!isDirty()) return;
      e.preventDefault();
      pendingURL = href;
      if (typeof modal.showModal === "function") modal.showModal();
      else if (confirm("You have unsaved changes. Leave without saving?")) go(href);
    });
  });
})();

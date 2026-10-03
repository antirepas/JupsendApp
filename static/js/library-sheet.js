(function () {
  "use strict";

  var root = document.getElementById("lib-sheet");
  if (!root) return;
  var listID = root.getAttribute("data-list-id");
  var table = document.getElementById("lib-sheet-table");
  var toastEl = document.getElementById("lib-sheet-toast");
  var delBtn = document.getElementById("lib-sheet-del-rows");
  var editing = null;

  function toast(msg, isError) {
    if (!toastEl) return;
    toastEl.textContent = msg;
    toastEl.hidden = false;
    toastEl.classList.toggle("is-error", !!isError);
    clearTimeout(toastEl._t);
    toastEl._t = setTimeout(function () {
      toastEl.hidden = true;
    }, 2500);
  }

  function postJSON(url, body) {
    return fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(body),
    }).then(function (r) {
      return r.json().then(function (data) {
        if (!r.ok || !data.ok) throw new Error((data && data.error) || "Request failed");
        return data;
      });
    });
  }

  function selectedIDs() {
    return Array.prototype.slice
      .call(table.querySelectorAll(".lib-sheet-row-check:checked"))
      .map(function (cb) {
        return Number(cb.closest("tr").getAttribute("data-contact-id"));
      })
      .filter(Boolean);
  }

  function syncDel() {
    if (delBtn) delBtn.disabled = selectedIDs().length === 0;
  }

  function startEdit(cell) {
    if (!cell || editing || !cell.classList.contains("lib-sheet-cell")) return;
    editing = cell;
    var old = cell.textContent;
    var input = document.createElement("input");
    input.className = "lib-sheet-input";
    input.value = old;
    cell.textContent = "";
    cell.appendChild(input);
    input.focus();
    input.select();

    function finish(save) {
      if (!editing) return;
      var value = input.value;
      editing = null;
      cell.textContent = save ? value : old;
      if (!save || value === old) return;
      var tr = cell.closest("tr");
      postJSON("/library/sheets/" + listID + "/cell", {
        contact_id: Number(tr.getAttribute("data-contact-id")),
        column: cell.getAttribute("data-col"),
        value: value,
      }).catch(function (err) {
        cell.textContent = old;
        toast(err.message || "Save failed", true);
      });
    }

    input.addEventListener("keydown", function (e) {
      if (e.key === "Enter") {
        e.preventDefault();
        finish(true);
      } else if (e.key === "Escape") {
        e.preventDefault();
        finish(false);
      }
      e.stopPropagation();
    });
    input.addEventListener("blur", function () {
      finish(true);
    });
  }

  table.addEventListener("click", function (e) {
    var cell = e.target.closest(".lib-sheet-cell");
    if (cell) startEdit(cell);
  });

  table.addEventListener("change", function (e) {
    if (e.target.classList.contains("lib-sheet-row-check") || e.target.id === "lib-sheet-select-all") {
      if (e.target.id === "lib-sheet-select-all") {
        var on = e.target.checked;
        table.querySelectorAll(".lib-sheet-row-check").forEach(function (cb) {
          cb.checked = on;
        });
      }
      syncDel();
    }
  });

  var addBtn = document.getElementById("lib-sheet-add-row");
  if (addBtn) {
    addBtn.addEventListener("click", function () {
      postJSON("/library/sheets/" + listID + "/rows", { email: "" })
        .then(function () {
          location.reload();
        })
        .catch(function (err) {
          toast(err.message || "Add failed", true);
        });
    });
  }

  function showImport() {
    var panel = document.getElementById("lib-sheet-import");
    if (!panel) return;
    panel.classList.remove("hidden");
    var ta = panel.querySelector("textarea[name=paste]");
    if (ta) ta.focus();
  }
  var importToggle = document.getElementById("lib-sheet-toggle-import");
  if (importToggle) {
    importToggle.addEventListener("click", function () {
      var panel = document.getElementById("lib-sheet-import");
      if (!panel) return;
      if (panel.classList.contains("hidden")) showImport();
      else panel.classList.add("hidden");
    });
  }
  var emptyImport = document.getElementById("lib-sheet-empty-import");
  if (emptyImport) emptyImport.addEventListener("click", showImport);

  // Auto-open import when flash messages are present (after import redirect).
  if (root.querySelector(".alert-success, .alert-error")) {
    var panel = document.getElementById("lib-sheet-import");
    if (panel) panel.classList.remove("hidden");
  }

  if (delBtn) {
    delBtn.addEventListener("click", function () {
      var ids = selectedIDs();
      if (!ids.length || !confirm("Remove " + ids.length + " row(s) from this sheet?")) return;
      postJSON("/library/sheets/" + listID + "/rows/delete", { contact_ids: ids })
        .then(function () {
          location.reload();
        })
        .catch(function (err) {
          toast(err.message || "Remove failed", true);
        });
    });
  }

  root.addEventListener("paste", function (e) {
    if (editing) return;
    var text = (e.clipboardData || window.clipboardData).getData("text");
    if (!text || text.indexOf("\t") < 0 && text.indexOf("\n") < 0) return;
    e.preventDefault();
    var lines = text.replace(/\r/g, "").split("\n").filter(function (l) {
      return l.trim() !== "";
    });
    if (!lines.length) return;
    var matrix = lines.map(function (l) {
      return l.split("\t");
    });
    var headers = [];
    table.querySelectorAll("thead th[data-col]").forEach(function (th) {
      headers.push(th.getAttribute("data-col"));
    });
    // If first row looks like headers matching columns, skip it
    var first = matrix[0].map(function (c) {
      return String(c).trim().toLowerCase();
    });
    if (first.indexOf("email") >= 0) {
      headers = matrix[0].map(function (c) {
        return String(c).trim();
      });
      matrix = matrix.slice(1);
    }
    postJSON("/library/sheets/" + listID + "/paste", { headers: headers, rows: matrix })
      .then(function (data) {
        toast("Pasted " + (data.count || 0) + " rows");
        location.reload();
      })
      .catch(function (err) {
        toast(err.message || "Paste failed", true);
      });
  });

  syncDel();
})();

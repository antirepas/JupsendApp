(function () {
  "use strict";

  var root = document.getElementById("tpl-fm");
  if (!root) return;

  var CLIP_KEY = "jupsend.tpl.clipboard";
  var folderKey = root.getAttribute("data-folder") || "all";
  var list = document.getElementById("tpl-fm-list");
  var toastEl = document.getElementById("tpl-fm-toast");
  var selectionEl = document.getElementById("tpl-fm-selection");
  var clipboardEl = document.getElementById("tpl-fm-clipboard");
  var selectAll = document.getElementById("tpl-fm-select-all");
  var selected = new Set();
  var lastIndex = -1; // selection anchor
  var focusIndex = -1; // keyboard / range end
  var renaming = false;

  function rows() {
    return Array.prototype.slice.call(root.querySelectorAll(".tpl-fm-row"));
  }

  function toast(msg, isError) {
    if (!toastEl) return;
    toastEl.textContent = msg;
    toastEl.hidden = false;
    toastEl.classList.toggle("is-error", !!isError);
    clearTimeout(toastEl._t);
    toastEl._t = setTimeout(function () {
      toastEl.hidden = true;
    }, 2800);
  }

  function readClipboard() {
    try {
      return JSON.parse(sessionStorage.getItem(CLIP_KEY) || "null");
    } catch (e) {
      return null;
    }
  }

  function writeClipboard(mode, ids) {
    if (!mode || !ids || !ids.length) {
      sessionStorage.removeItem(CLIP_KEY);
    } else {
      sessionStorage.setItem(CLIP_KEY, JSON.stringify({ mode: mode, ids: ids }));
    }
    updateChrome();
  }

  function selectedIds() {
    return Array.from(selected).map(Number);
  }

  function updateChrome() {
    var n = selected.size;
    if (selectionEl) {
      selectionEl.textContent = n ? n + " selected" : "Nothing selected";
    }
    var clip = readClipboard();
    if (clipboardEl) {
      if (clip && clip.ids && clip.ids.length) {
        clipboardEl.hidden = false;
        clipboardEl.textContent =
          (clip.mode === "cut" ? "Cut" : "Copied") + ": " + clip.ids.length;
      } else {
        clipboardEl.hidden = true;
      }
    }
    root.querySelectorAll(".tpl-fm-tool").forEach(function (btn) {
      var action = btn.getAttribute("data-action");
      var enable = false;
      if (action === "paste") enable = !!(clip && clip.ids && clip.ids.length);
      else if (action === "cut" || action === "copy" || action === "delete")
        enable = n > 0;
      else if (action === "rename" || action === "open") enable = n === 1;
      btn.disabled = !enable;
    });
    rows().forEach(function (row) {
      var on = selected.has(row.getAttribute("data-id"));
      row.classList.toggle("is-selected", on);
      row.setAttribute("aria-selected", on ? "true" : "false");
      var cb = row.querySelector(".tpl-fm-check");
      if (cb) cb.checked = on;
    });
    if (selectAll) {
      var all = rows();
      selectAll.checked = all.length > 0 && selected.size === all.length;
      selectAll.indeterminate = selected.size > 0 && selected.size < all.length;
    }
  }

  function clearSelection() {
    selected.clear();
    updateChrome();
  }

  function selectOnly(id) {
    selected.clear();
    if (id != null) selected.add(String(id));
    updateChrome();
  }

  function toggleSelect(id) {
    id = String(id);
    if (selected.has(id)) selected.delete(id);
    else selected.add(id);
    updateChrome();
  }

  function selectRange(from, to) {
    var all = rows();
    var a = Math.min(from, to);
    var b = Math.max(from, to);
    selected.clear();
    for (var i = a; i <= b; i++) {
      if (all[i]) selected.add(all[i].getAttribute("data-id"));
    }
    updateChrome();
  }

  function folderDestId(key) {
    key = String(key || folderKey);
    if (key === "all" || key === "unfiled" || key === "") return 0;
    var n = parseInt(key, 10);
    return isNaN(n) ? 0 : n;
  }

  function reloadTo(key) {
    if (!key || key === "all") location.href = "/templates";
    else location.href = "/templates?folder=" + encodeURIComponent(key);
  }

  function postJSON(url, body) {
    return fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(body),
    }).then(function (r) {
      return r.json().then(function (data) {
        if (!r.ok || !data.ok) {
          throw new Error((data && data.error) || "Request failed");
        }
        return data;
      });
    });
  }

  function doCut() {
    var ids = selectedIds();
    if (!ids.length) return;
    writeClipboard("cut", ids);
    toast("Cut " + ids.length + " template" + (ids.length === 1 ? "" : "s"));
  }

  function doCopy() {
    var ids = selectedIds();
    if (!ids.length) return;
    writeClipboard("copy", ids);
    toast("Copied " + ids.length + " template" + (ids.length === 1 ? "" : "s"));
  }

  function doPaste(destKey) {
    var clip = readClipboard();
    if (!clip || !clip.ids || !clip.ids.length) return;
    var key = destKey != null ? destKey : folderKey;
    var folderId = folderDestId(key);
    var url = clip.mode === "cut" ? "/templates/ops/move" : "/templates/ops/copy";
    postJSON(url, { ids: clip.ids.map(Number), folder_id: folderId })
      .then(function (data) {
        if (clip.mode === "cut") writeClipboard(null, null);
        toast(
          (clip.mode === "cut" ? "Moved " : "Pasted ") +
            (data.count || clip.ids.length)
        );
        reloadTo(key === "all" ? "unfiled" : key);
      })
      .catch(function (err) {
        toast(err.message || "Paste failed", true);
      });
  }

  function doDelete() {
    var ids = selectedIds();
    if (!ids.length) return;
    var msg =
      ids.length === 1
        ? "Delete this template?"
        : "Delete " + ids.length + " templates?";
    if (!confirm(msg)) return;
    postJSON("/templates/ops/delete", { kind: "template", ids: ids })
      .then(function () {
        toast("Deleted");
        location.reload();
      })
      .catch(function (err) {
        toast(err.message || "Delete failed", true);
      });
  }

  function doOpen() {
    var ids = selectedIds();
    if (ids.length !== 1) return;
    var q =
      folderKey && folderKey !== "all"
        ? "?folder=" + encodeURIComponent(folderKey)
        : "";
    location.href = "/templates/" + ids[0] + "/edit" + q;
  }

  function startRename() {
    if (renaming) return;
    var ids = selectedIds();
    if (ids.length !== 1) return;
    var row = root.querySelector('.tpl-fm-row[data-id="' + ids[0] + '"]');
    if (!row) return;
    var label = row.querySelector("[data-rename-label]");
    if (!label) return;
    renaming = true;
    var old = label.textContent;
    var input = document.createElement("input");
    input.type = "text";
    input.className = "tpl-fm-rename-input";
    input.value = old;
    input.setAttribute("maxlength", "200");
    label.replaceWith(input);
    input.focus();
    input.select();

    function finish(commit) {
      if (!renaming) return;
      renaming = false;
      var name = input.value.trim();
      var span = document.createElement("span");
      span.className = "tpl-fm-name";
      span.setAttribute("data-rename-label", "");
      span.textContent = commit && name ? name : old;
      input.replaceWith(span);
      if (!commit || !name || name === old) return;
      postJSON("/templates/ops/rename", {
        kind: "template",
        id: ids[0],
        name: name,
      })
        .then(function () {
          row.setAttribute("data-name", name);
          toast("Renamed");
        })
        .catch(function (err) {
          span.textContent = old;
          toast(err.message || "Rename failed", true);
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

  function startFolderRename(navItem) {
    if (renaming || !navItem) return;
    var id = navItem.getAttribute("data-folder-id");
    if (!id) return;
    var label = navItem.querySelector("[data-rename-label]");
    if (!label) return;
    renaming = true;
    var old = label.textContent;
    var input = document.createElement("input");
    input.type = "text";
    input.className = "tpl-fm-rename-input tpl-fm-rename-input--nav";
    input.value = old;
    input.setAttribute("maxlength", "120");
    label.replaceWith(input);
    input.focus();
    input.select();

    function finish(commit) {
      if (!renaming) return;
      renaming = false;
      var name = input.value.trim();
      var span = document.createElement("span");
      span.className = "tpl-fm-nav-name";
      span.setAttribute("data-rename-label", "");
      span.textContent = commit && name ? name : old;
      input.replaceWith(span);
      if (!commit || !name || name === old) return;
      postJSON("/templates/ops/rename", {
        kind: "folder",
        id: Number(id),
        name: name,
      })
        .then(function () {
          navItem.setAttribute("data-folder-name", name);
          toast("Folder renamed");
          if (folderKey === String(id)) {
            var crumb = root.querySelector(".tpl-fm-crumb-current");
            if (crumb) crumb.textContent = name;
          }
        })
        .catch(function (err) {
          span.textContent = old;
          toast(err.message || "Rename failed", true);
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

  function newFolder() {
    var name = prompt("Folder name");
    if (name == null) return;
    name = name.trim();
    if (!name) return;
    postJSON("/templates/ops/folders", { name: name })
      .then(function (data) {
        reloadTo(String(data.id));
      })
      .catch(function (err) {
        toast(err.message || "Could not create folder", true);
      });
  }

  function deleteActiveFolder() {
    if (!/^\d+$/.test(folderKey)) return;
    if (!confirm("Delete this folder? Templates will move to Unfiled.")) return;
    postJSON("/templates/ops/delete", { kind: "folder", id: Number(folderKey) })
      .then(function () {
        reloadTo("unfiled");
      })
      .catch(function (err) {
        toast(err.message || "Could not delete folder", true);
      });
  }

  // Selection — checkbox column always toggles; Ctrl/⌘ click toggles; Shift extends.
  // Native checkbox click is prevented so it can't fight our selected Set.
  if (list) {
    list.addEventListener("mousedown", function (e) {
      if (e.button !== 0) return;
      var row = e.target.closest(".tpl-fm-row");
      if (!row || e.target.closest(".tpl-fm-rename-input")) return;
      var id = row.getAttribute("data-id");
      var idx = rows().indexOf(row);
      var checkCol = e.target.closest(".tpl-fm-col-check");

      if (checkCol) {
        e.preventDefault();
        if (e.shiftKey && lastIndex >= 0) {
          selectRange(lastIndex, idx);
          focusIndex = idx;
        } else {
          toggleSelect(id);
          lastIndex = idx;
          focusIndex = idx;
        }
        return;
      }

      if (e.shiftKey && lastIndex >= 0) {
        e.preventDefault();
        selectRange(lastIndex, idx);
        focusIndex = idx;
        return;
      }
      if (e.metaKey || e.ctrlKey) {
        e.preventDefault();
        toggleSelect(id);
        lastIndex = idx;
        focusIndex = idx;
        return;
      }
      // Keep multi-selection when grabbing an already-selected row (for drag).
      if (selected.has(id) && selected.size > 1) {
        focusIndex = idx;
        return;
      }
      selectOnly(id);
      lastIndex = idx;
      focusIndex = idx;
    });

    list.addEventListener("dblclick", function (e) {
      var row = e.target.closest(".tpl-fm-row");
      if (!row || e.target.closest(".tpl-fm-col-check") || e.target.closest("input")) return;
      selectOnly(row.getAttribute("data-id"));
      doOpen();
    });
  }

  if (selectAll) {
    selectAll.addEventListener("click", function (e) {
      e.preventDefault();
      var all = rows();
      if (selected.size === all.length && all.length > 0) {
        selected.clear();
      } else {
        selected.clear();
        all.forEach(function (r) {
          selected.add(r.getAttribute("data-id"));
        });
        if (all.length) {
          lastIndex = 0;
          focusIndex = all.length - 1;
        }
      }
      updateChrome();
    });
  }

  // Toolbar
  root.querySelectorAll(".tpl-fm-tool").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var action = btn.getAttribute("data-action");
      if (action === "cut") doCut();
      else if (action === "copy") doCopy();
      else if (action === "paste") doPaste();
      else if (action === "rename") startRename();
      else if (action === "delete") doDelete();
      else if (action === "open") doOpen();
    });
  });

  var newFolderBtn = document.getElementById("tpl-fm-new-folder");
  if (newFolderBtn) newFolderBtn.addEventListener("click", newFolder);

  // Drag & drop templates → folders
  root.addEventListener("dragstart", function (e) {
    var row = e.target.closest(".tpl-fm-row");
    if (!row) return;
    var id = row.getAttribute("data-id");
    if (!selected.has(id)) selectOnly(id);
    var ids = selectedIds();
    e.dataTransfer.setData(
      "application/x-jupsend-templates",
      JSON.stringify(ids)
    );
    e.dataTransfer.setData("text/plain", ids.join(","));
    e.dataTransfer.effectAllowed = "copyMove";
    row.classList.add("is-dragging");
    root.classList.add("is-dragging");
  });

  root.addEventListener("dragend", function () {
    root.classList.remove("is-dragging");
    root.querySelectorAll(".is-dragging, .is-drop-target").forEach(function (el) {
      el.classList.remove("is-dragging", "is-drop-target");
    });
  });

  root.querySelectorAll("[data-drop]").forEach(function (el) {
    el.addEventListener("dragover", function (e) {
      e.preventDefault();
      e.dataTransfer.dropEffect = e.altKey ? "copy" : "move";
      el.classList.add("is-drop-target");
    });
    el.addEventListener("dragleave", function () {
      el.classList.remove("is-drop-target");
    });
    el.addEventListener("drop", function (e) {
      e.preventDefault();
      el.classList.remove("is-drop-target");
      var raw =
        e.dataTransfer.getData("application/x-jupsend-templates") ||
        e.dataTransfer.getData("text/plain");
      var ids;
      try {
        ids = JSON.parse(raw);
      } catch (err) {
        ids = String(raw)
          .split(",")
          .map(Number)
          .filter(Boolean);
      }
      if (!ids || !ids.length) return;
      var key = el.getAttribute("data-drop");
      var folderId = folderDestId(key);
      var copy = e.altKey;
      var url = copy ? "/templates/ops/copy" : "/templates/ops/move";
      postJSON(url, { ids: ids, folder_id: folderId })
        .then(function (data) {
          toast((copy ? "Copied " : "Moved ") + (data.count || ids.length));
          reloadTo(key === "all" ? folderKey : key);
        })
        .catch(function (err) {
          toast(err.message || "Drop failed", true);
        });
    });
  });

  var menu = document.createElement("div");
  menu.className = "tpl-fm-menu";
  menu.hidden = true;
  document.body.appendChild(menu);

  function hideMenu() {
    menu.hidden = true;
    menu.innerHTML = "";
  }

  function showMenu(x, y, items) {
    menu.innerHTML = "";
    items.forEach(function (it) {
      if (it.sep) {
        var sep = document.createElement("div");
        sep.className = "tpl-fm-menu-sep";
        menu.appendChild(sep);
        return;
      }
      var btn = document.createElement("button");
      btn.type = "button";
      btn.textContent = it.label;
      if (it.danger) btn.className = "is-danger";
      btn.addEventListener("click", function () {
        hideMenu();
        it.run();
      });
      menu.appendChild(btn);
    });
    menu.hidden = false;
    var pad = 8;
    var rect = menu.getBoundingClientRect();
    var left = Math.min(x, window.innerWidth - rect.width - pad);
    var top = Math.min(y, window.innerHeight - rect.height - pad);
    menu.style.left = Math.max(pad, left) + "px";
    menu.style.top = Math.max(pad, top) + "px";
  }

  document.addEventListener("click", hideMenu);
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") hideMenu();
  });

  root.querySelectorAll(".tpl-fm-nav-item[data-folder-id]").forEach(function (item) {
    item.addEventListener("dblclick", function (e) {
      e.preventDefault();
      startFolderRename(item);
    });
    item.addEventListener("contextmenu", function (e) {
      e.preventDefault();
      var fid = Number(item.getAttribute("data-folder-id"));
      var key = item.getAttribute("data-folder-key");
      showMenu(e.clientX, e.clientY, [
        { label: "Rename", run: function () { startFolderRename(item); } },
        { label: "Paste into folder", run: function () { doPaste(key); } },
        { sep: true },
        {
          label: "Delete folder",
          danger: true,
          run: function () {
            if (!confirm("Delete this folder? Templates will move to Unfiled.")) return;
            postJSON("/templates/ops/delete", { kind: "folder", id: fid })
              .then(function () { reloadTo("unfiled"); })
              .catch(function (err) { toast(err.message || "Delete failed", true); });
          },
        },
      ]);
    });
  });

  if (list) {
    list.addEventListener("contextmenu", function (e) {
      var row = e.target.closest(".tpl-fm-row");
      if (!row) return;
      e.preventDefault();
      var id = row.getAttribute("data-id");
      if (!selected.has(id)) selectOnly(id);
      showMenu(e.clientX, e.clientY, [
        { label: "Open", run: doOpen },
        { label: "Rename", run: startRename },
        { sep: true },
        { label: "Cut", run: doCut },
        { label: "Copy", run: doCopy },
        { label: "Paste", run: function () { doPaste(); } },
        { sep: true },
        { label: "Delete", danger: true, run: doDelete },
      ]);
    });
  }

  // Keyboard
  root.addEventListener("keydown", function (e) {
    if (renaming) return;
    var tag = (e.target && e.target.tagName) || "";
    if (tag === "INPUT" || tag === "TEXTAREA" || e.target.isContentEditable) return;

    var meta = e.metaKey || e.ctrlKey;
    if (meta && e.key.toLowerCase() === "c") {
      e.preventDefault();
      doCopy();
    } else if (meta && e.key.toLowerCase() === "x") {
      e.preventDefault();
      doCut();
    } else if (meta && e.key.toLowerCase() === "v") {
      e.preventDefault();
      doPaste();
    } else if (meta && e.key.toLowerCase() === "a") {
      e.preventDefault();
      selected.clear();
      rows().forEach(function (r) {
        selected.add(r.getAttribute("data-id"));
      });
      if (rows().length) lastIndex = 0;
      updateChrome();
    } else if (e.key === " " || e.key === "Spacebar") {
      var spaceIdx = focusIndex >= 0 ? focusIndex : lastIndex;
      if (spaceIdx >= 0 && rows()[spaceIdx]) {
        e.preventDefault();
        toggleSelect(rows()[spaceIdx].getAttribute("data-id"));
        lastIndex = spaceIdx;
        focusIndex = spaceIdx;
      }
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      var allRows = rows();
      if (!allRows.length) return;
      e.preventDefault();
      var cur = focusIndex >= 0 ? focusIndex : lastIndex >= 0 ? lastIndex : -1;
      var next =
        e.key === "ArrowDown"
          ? Math.min(allRows.length - 1, cur + 1)
          : Math.max(0, cur < 0 ? 0 : cur - 1);
      if (e.shiftKey) {
        if (lastIndex < 0) lastIndex = next;
        selectRange(lastIndex, next);
        focusIndex = next;
        allRows[next].focus({ preventScroll: false });
      } else {
        selectOnly(allRows[next].getAttribute("data-id"));
        lastIndex = next;
        focusIndex = next;
        allRows[next].focus({ preventScroll: false });
      }
    } else if (e.key === "F2") {
      e.preventDefault();
      startRename();
    } else if (e.key === "Delete" || e.key === "Backspace") {
      if (selected.size) {
        e.preventDefault();
        doDelete();
      } else if (/^\d+$/.test(folderKey) && e.key === "Delete") {
        e.preventDefault();
        deleteActiveFolder();
      }
    } else if (e.key === "Enter") {
      if (selected.size === 1) {
        e.preventDefault();
        doOpen();
      }
    } else if (e.key === "Escape") {
      clearSelection();
    }
  });

  // Focus root for shortcuts when clicking inside
  root.addEventListener("mousedown", function () {
    if (!renaming) root.focus({ preventScroll: true });
  });

  updateChrome();
})();

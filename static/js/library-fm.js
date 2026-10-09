(function () {
  "use strict";

  var root = document.getElementById("lib-fm");
  if (!root) return;

  var CLIP_KEY = "jupsend.lib.clipboard";
  var folderKey = root.getAttribute("data-folder") || "all";
  var list = document.getElementById("lib-fm-list");
  var toastEl = document.getElementById("lib-fm-toast");
  var selectionEl = document.getElementById("lib-fm-selection");
  var clipboardEl = document.getElementById("lib-fm-clipboard");
  var selectAll = document.getElementById("lib-fm-select-all");
  var selected = new Set(); // "kind:id"
  var lastIndex = -1;
  var focusIndex = -1;
  var renaming = false;

  function rows() {
    return Array.prototype.slice.call(root.querySelectorAll(".tpl-fm-row"));
  }

  function rowKey(row) {
    return row.getAttribute("data-kind") + ":" + row.getAttribute("data-id");
  }

  function parseKey(key) {
    var i = key.indexOf(":");
    return { kind: key.slice(0, i), id: Number(key.slice(i + 1)) };
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

  function writeClipboard(mode, items) {
    if (!mode || !items || !items.length) sessionStorage.removeItem(CLIP_KEY);
    else sessionStorage.setItem(CLIP_KEY, JSON.stringify({ mode: mode, items: items }));
    updateChrome();
  }

  function selectedItems() {
    return Array.from(selected).map(parseKey);
  }

  function updateChrome() {
    var n = selected.size;
    if (selectionEl) selectionEl.textContent = n ? n + " selected" : "Nothing selected";
    var clip = readClipboard();
    if (clipboardEl) {
      if (clip && clip.items && clip.items.length) {
        clipboardEl.hidden = false;
        clipboardEl.textContent =
          (clip.mode === "cut" ? "Cut" : "Copied") + ": " + clip.items.length;
      } else clipboardEl.hidden = true;
    }
    root.querySelectorAll(".tpl-fm-tool").forEach(function (btn) {
      var action = btn.getAttribute("data-action");
      var enable = false;
      if (action === "paste") enable = !!(clip && clip.items && clip.items.length);
      else if (action === "cut" || action === "copy" || action === "delete") enable = n > 0;
      else if (action === "rename" || action === "open") enable = n === 1;
      btn.disabled = !enable;
    });
    rows().forEach(function (row) {
      var on = selected.has(rowKey(row));
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

  function selectOnlyKey(key) {
    selected.clear();
    if (key != null) selected.add(String(key));
    updateChrome();
  }

  function toggleKey(key) {
    key = String(key);
    if (selected.has(key)) selected.delete(key);
    else selected.add(key);
    updateChrome();
  }

  function selectRange(from, to) {
    var all = rows();
    var a = Math.min(from, to);
    var b = Math.max(from, to);
    selected.clear();
    for (var i = a; i <= b; i++) if (all[i]) selected.add(rowKey(all[i]));
    updateChrome();
  }

  function folderDestId(key) {
    key = String(key || folderKey);
    if (key === "all" || key === "unfiled" || key === "") return 0;
    var n = parseInt(key, 10);
    return isNaN(n) ? 0 : n;
  }

  function reloadTo(key) {
    if (!key || key === "all") location.href = "/library";
    else location.href = "/library?folder=" + encodeURIComponent(key);
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

  function openHref(kind, id) {
    if (kind === "workflow") return "/workflows/" + id + "/edit";
    if (kind === "contacts") return "/library/sheets/" + id;
    return "/templates/" + id + "/edit";
  }

  function doCut() {
    var items = selectedItems();
    if (!items.length) return;
    writeClipboard("cut", items);
    toast("Cut " + items.length);
  }

  function doCopy() {
    var items = selectedItems();
    if (!items.length) return;
    writeClipboard("copy", items);
    toast("Copied " + items.length);
  }

  function doPaste(destKey) {
    var clip = readClipboard();
    if (!clip || !clip.items || !clip.items.length) return;
    var key = destKey != null ? destKey : folderKey;
    var folderId = folderDestId(key);
    var url = clip.mode === "cut" ? "/library/ops/move" : "/library/ops/copy";
    postJSON(url, { items: clip.items, folder_id: folderId })
      .then(function (data) {
        if (clip.mode === "cut") writeClipboard(null, null);
        toast((clip.mode === "cut" ? "Moved " : "Pasted ") + (data.count || clip.items.length));
        reloadTo(key === "all" ? "unfiled" : key);
      })
      .catch(function (err) {
        toast(err.message || "Paste failed", true);
      });
  }

  function doDelete() {
    var items = selectedItems();
    if (!items.length) return;
    if (!confirm(items.length === 1 ? "Delete this item?" : "Delete " + items.length + " items?")) return;
    postJSON("/library/ops/delete", { items: items })
      .then(function () {
        toast("Deleted");
        location.reload();
      })
      .catch(function (err) {
        toast(err.message || "Delete failed", true);
      });
  }

  function doOpen() {
    var items = selectedItems();
    if (items.length !== 1) return;
    location.href = openHref(items[0].kind, items[0].id);
  }

  function startRename() {
    if (renaming) return;
    var items = selectedItems();
    if (items.length !== 1) return;
    var row = root.querySelector(
      '.tpl-fm-row[data-kind="' + items[0].kind + '"][data-id="' + items[0].id + '"]'
    );
    if (!row) return;
    var label = row.querySelector("[data-rename-label]");
    if (!label) return;
    renaming = true;
    var old = label.textContent;
    var input = document.createElement("input");
    input.type = "text";
    input.className = "tpl-fm-rename-input";
    input.value = old;
    input.maxLength = 200;
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
      postJSON("/library/ops/rename", { kind: items[0].kind, id: items[0].id, name: name })
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
    input.maxLength = 120;
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
      postJSON("/library/ops/rename", { kind: "folder", id: Number(id), name: name })
        .then(function () {
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
    var name = prompt("Campaign name");
    if (name == null) return;
    name = name.trim();
    if (!name) return;
    postJSON("/library/ops/folders", { name: name })
      .then(function (data) {
        reloadTo(String(data.id));
      })
      .catch(function (err) {
        toast(err.message || "Could not create campaign", true);
      });
  }

  function createNew(kind) {
    var folderId = folderDestId(folderKey === "all" ? "unfiled" : folderKey);
    if (kind === "template") {
      var q = folderKey && folderKey !== "all" ? "?folder=" + encodeURIComponent(folderKey) : "";
      location.href = "/templates/new" + q;
      return;
    }
    if (kind === "workflow") {
      var wname = prompt("Workflow name", "Workflow");
      if (wname == null) return;
      postJSON("/library/ops/workflows", { name: wname.trim() || "Workflow", folder_id: folderId })
        .then(function (data) {
          location.href = "/workflows/" + data.id + "/edit";
        })
        .catch(function (err) {
          toast(err.message || "Could not create workflow", true);
        });
      return;
    }
    if (kind === "contacts") {
      var cname = prompt("Contact sheet name", "Contacts");
      if (cname == null) return;
      postJSON("/library/ops/sheets", { name: cname.trim() || "Contacts", folder_id: folderId })
        .then(function (data) {
          location.href = "/library/sheets/" + data.id;
        })
        .catch(function (err) {
          toast(err.message || "Could not create sheet", true);
        });
    }
  }

  if (list) {
    list.addEventListener("mousedown", function (e) {
      if (e.button !== 0) return;
      var row = e.target.closest(".tpl-fm-row");
      if (!row || e.target.closest(".tpl-fm-rename-input")) return;
      var key = rowKey(row);
      var idx = rows().indexOf(row);
      var checkCol = e.target.closest(".tpl-fm-col-check");
      if (checkCol) {
        e.preventDefault();
        if (e.shiftKey && lastIndex >= 0) {
          selectRange(lastIndex, idx);
          focusIndex = idx;
        } else {
          toggleKey(key);
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
        toggleKey(key);
        lastIndex = idx;
        focusIndex = idx;
        return;
      }
      if (selected.has(key) && selected.size > 1) {
        focusIndex = idx;
        return;
      }
      selectOnlyKey(key);
      lastIndex = idx;
      focusIndex = idx;
    });

    list.addEventListener("dblclick", function (e) {
      var row = e.target.closest(".tpl-fm-row");
      if (!row || e.target.closest(".tpl-fm-col-check")) return;
      selectOnlyKey(rowKey(row));
      doOpen();
    });
  }

  if (selectAll) {
    selectAll.addEventListener("click", function (e) {
      e.preventDefault();
      var all = rows();
      if (selected.size === all.length && all.length > 0) selected.clear();
      else {
        selected.clear();
        all.forEach(function (r) {
          selected.add(rowKey(r));
        });
        if (all.length) {
          lastIndex = 0;
          focusIndex = all.length - 1;
        }
      }
      updateChrome();
    });
  }

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

  var newFolderBtn = document.getElementById("lib-fm-new-folder");
  if (newFolderBtn) newFolderBtn.addEventListener("click", newFolder);

  var newToggle = document.getElementById("lib-fm-new-toggle");
  var newMenu = document.getElementById("lib-fm-new-menu");
  if (newToggle && newMenu) {
    newToggle.addEventListener("click", function (e) {
      e.stopPropagation();
      newMenu.hidden = !newMenu.hidden;
    });
    document.addEventListener("click", function () {
      newMenu.hidden = true;
    });
    newMenu.addEventListener("click", function (e) {
      var btn = e.target.closest("[data-new]");
      if (!btn) return;
      newMenu.hidden = true;
      createNew(btn.getAttribute("data-new"));
    });
  }

  root.addEventListener("dragstart", function (e) {
    var row = e.target.closest(".tpl-fm-row");
    if (!row) return;
    var key = rowKey(row);
    if (!selected.has(key)) selectOnlyKey(key);
    e.dataTransfer.setData("application/x-jupsend-library", JSON.stringify(selectedItems()));
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
      var raw = e.dataTransfer.getData("application/x-jupsend-library");
      var items;
      try {
        items = JSON.parse(raw);
      } catch (err) {
        return;
      }
      if (!items || !items.length) return;
      var key = el.getAttribute("data-drop");
      var folderId = folderDestId(key);
      var copy = e.altKey;
      postJSON(copy ? "/library/ops/copy" : "/library/ops/move", {
        items: items,
        folder_id: folderId,
      })
        .then(function (data) {
          toast((copy ? "Copied " : "Moved ") + (data.count || items.length));
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
    menu.style.left = Math.max(pad, Math.min(x, window.innerWidth - rect.width - pad)) + "px";
    menu.style.top = Math.max(pad, Math.min(y, window.innerHeight - rect.height - pad)) + "px";
  }
  document.addEventListener("click", hideMenu);

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
            if (!confirm("Delete this folder? Items move to Unfiled.")) return;
            postJSON("/library/ops/delete", { kind: "folder", id: fid })
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
      var key = rowKey(row);
      if (!selected.has(key)) selectOnlyKey(key);
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
        selected.add(rowKey(r));
      });
      updateChrome();
    } else if (e.key === "F2") {
      e.preventDefault();
      startRename();
    } else if (e.key === "Delete" || e.key === "Backspace") {
      if (selected.size) {
        e.preventDefault();
        doDelete();
      }
    } else if (e.key === "Enter") {
      if (selected.size === 1) {
        e.preventDefault();
        doOpen();
      }
    } else if (e.key === "Escape") {
      selected.clear();
      updateChrome();
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
      } else {
        selectOnlyKey(rowKey(allRows[next]));
        lastIndex = next;
        focusIndex = next;
      }
      allRows[next].focus({ preventScroll: false });
    }
  });

  root.addEventListener("mousedown", function () {
    if (!renaming) root.focus({ preventScroll: true });
  });

  updateChrome();

  // Deep-link from /campaigns/new → /library?new_campaign=1
  try {
    var params = new URLSearchParams(location.search || "");
    if (params.get("new_campaign") === "1") {
      params.delete("new_campaign");
      var next = location.pathname + (params.toString() ? "?" + params.toString() : "");
      history.replaceState({}, "", next);
      newFolder();
    }
  } catch (_) { /* ignore */ }
})();

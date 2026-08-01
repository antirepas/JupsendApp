(function () {
  const modal = document.getElementById("contact-import-map-modal");
  if (!modal) return;

  const rowsEl = document.getElementById("contact-import-map-rows");
  const metaEl = document.getElementById("contact-import-map-meta");
  const multiEl = document.getElementById("contact-import-map-multi");
  const confirmBtn = document.getElementById("contact-import-map-confirm");

  let state = {
    mode: "file", // file | paste
    headers: [],
    sampleRows: [],
    suggestedMap: {},
    templateVars: [],
    file: null,
    paste: "",
    templateId: "",
    listId: "",
  };

  function escapeHtml(s) {
    return String(s ?? "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function looksMultiEmail(raw) {
    const s = String(raw || "");
    return /[@].*[,;].*[@]|[,;].*@/.test(s) || (s.split(/[,;]/).filter((p) => p.includes("@")).length > 1);
  }

  function openModal(peek, opts) {
    state = {
      mode: opts.mode || "file",
      headers: peek.headers || [],
      sampleRows: peek.sample_rows || [],
      suggestedMap: peek.suggested_map || {},
      templateVars: peek.template_vars || opts.templateVars || [],
      file: opts.file || null,
      paste: opts.paste || "",
      templateId: opts.templateId || "",
      listId: opts.listId || "",
    };
    renderRows();
    metaEl.textContent = state.headers.length
      ? `${state.headers.length} columns · ${peek.row_count || 0} data rows`
      : "";
    let multi = false;
    const emailHeader = Object.keys(state.suggestedMap).find((h) => state.suggestedMap[h] === "email");
    const emailIdx = emailHeader ? state.headers.indexOf(emailHeader) : -1;
    if (emailIdx >= 0) {
      for (const row of state.sampleRows) {
        if (looksMultiEmail(row[emailIdx])) {
          multi = true;
          break;
        }
      }
    }
    multiEl.classList.toggle("hidden", !multi);
    modal.classList.remove("hidden");
    document.body.classList.add("overflow-hidden");
    syncConfirm();
  }

  function closeModal() {
    modal.classList.add("hidden");
    document.body.classList.remove("overflow-hidden");
  }

  function targetOptions(header, selected) {
    const norm = String(header || "")
      .trim()
      .toLowerCase();
    const opts = [
      { value: "email", label: "Email" },
      { value: "skip", label: "Skip" },
    ];
    const seen = { email: true, skip: true };
    for (const v of state.templateVars) {
      const key = String(v).trim().toLowerCase();
      if (!key || seen[key]) continue;
      seen[key] = true;
      opts.push({ value: key, label: `Variable: ${key}` });
    }
    if (norm && !seen[norm] && norm !== "email") {
      opts.push({ value: norm, label: `Variable: ${norm}` });
    }
    // Custom selected variable not in list
    if (selected && !seen[selected] && selected !== "email" && selected !== "skip") {
      opts.push({ value: selected, label: `Variable: ${selected}` });
    }
    return opts
      .map((o) => `<option value="${escapeHtml(o.value)}"${o.value === selected ? " selected" : ""}>${escapeHtml(o.label)}</option>`)
      .join("");
  }

  function sampleFor(idx) {
    const vals = [];
    for (const row of state.sampleRows.slice(0, 3)) {
      const v = (row[idx] || "").trim();
      if (v) vals.push(v.slice(0, 48));
    }
    return vals.join(" · ");
  }

  function renderRows() {
    rowsEl.innerHTML = state.headers
      .map((header, idx) => {
        if (!String(header || "").trim()) return "";
        const suggested = state.suggestedMap[header] || "skip";
        const sample = sampleFor(idx);
        return `<div class="rounded-lg border border-slate-200 p-3">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <p class="text-sm font-medium text-slate-800 truncate">${escapeHtml(header)}</p>
              <p class="text-xs text-slate-500 mt-1 font-mono break-all">${escapeHtml(sample) || "—"}</p>
            </div>
            <select class="form-input text-sm w-full sm:w-52 import-map-select" data-header="${escapeHtml(header)}">
              ${targetOptions(header, suggested)}
            </select>
          </div>
        </div>`;
      })
      .join("");
    rowsEl.querySelectorAll(".import-map-select").forEach((el) => {
      el.addEventListener("change", () => {
        // Enforce single email mapping
        if (el.value === "email") {
          rowsEl.querySelectorAll(".import-map-select").forEach((other) => {
            if (other !== el && other.value === "email") other.value = "skip";
          });
          let multi = false;
          const idx = state.headers.indexOf(el.dataset.header);
          if (idx >= 0) {
            for (const row of state.sampleRows) {
              if (looksMultiEmail(row[idx])) {
                multi = true;
                break;
              }
            }
          }
          multiEl.classList.toggle("hidden", !multi);
        }
        syncConfirm();
      });
    });
  }

  function buildMap() {
    const map = {};
    rowsEl.querySelectorAll(".import-map-select").forEach((el) => {
      map[el.dataset.header] = el.value;
    });
    return map;
  }

  function syncConfirm() {
    const map = buildMap();
    const emailCount = Object.values(map).filter((v) => v === "email").length;
    confirmBtn.disabled = emailCount !== 1;
  }

  async function confirmImport() {
    const map = buildMap();
    if (Object.values(map).filter((v) => v === "email").length !== 1) return;
    confirmBtn.disabled = true;
    confirmBtn.textContent = "Importing…";

    if (state.mode === "file") {
      const fd = new FormData();
      fd.append("file", state.file);
      fd.append("column_map", JSON.stringify(map));
      if (state.templateId) fd.append("template_id", state.templateId);
      if (state.listId) fd.append("list_id", state.listId);
      const res = await fetch("/contacts/upload", { method: "POST", body: fd, credentials: "same-origin", redirect: "follow" });
      window.location.href = res.url || "/contacts?tab=import";
      return;
    }

    const fd = new FormData();
    fd.append("paste", state.paste);
    fd.append("column_map", JSON.stringify(map));
    if (state.templateId) fd.append("template_id", state.templateId);
    if (state.listId) fd.append("list_id", state.listId);
    const res = await fetch("/contacts/paste", { method: "POST", body: fd, credentials: "same-origin", redirect: "follow" });
    window.location.href = res.url || "/contacts?tab=import";
  }

  modal.querySelectorAll("[data-import-map-dismiss]").forEach((el) => {
    el.addEventListener("click", closeModal);
  });
  confirmBtn.addEventListener("click", confirmImport);

  function selectedTemplateVars(selectEl) {
    const option = selectEl?.options?.[selectEl.selectedIndex];
    if (!option?.value) return [];
    return option.dataset.vars ? option.dataset.vars.split(",").filter(Boolean) : [];
  }

  async function previewFile(file) {
    if (!file) return;
    const fd = new FormData();
    fd.append("file", file);
    const tpl = document.getElementById("import-template");
    if (tpl?.value) fd.append("template_id", tpl.value);
    const res = await fetch("/contacts/upload/preview", { method: "POST", body: fd, credentials: "same-origin" });
    const data = await res.json();
    if (!res.ok) {
      alert(data.error || "Could not read file");
      return;
    }
    const listSel = document.querySelector("#excel-import-form select[name=list_id]");
    openModal(data, {
      mode: "file",
      file,
      templateId: tpl?.value || "",
      listId: listSel?.value || "",
      templateVars: data.template_vars || selectedTemplateVars(tpl),
    });
  }

  const fileInput = document.getElementById("import-file");
  const excelForm = document.getElementById("excel-import-form");
  fileInput?.addEventListener("change", function () {
    if (this.files?.[0]) previewFile(this.files[0]);
  });
  excelForm?.addEventListener("submit", function (e) {
    e.preventDefault();
    if (fileInput?.files?.[0]) previewFile(fileInput.files[0]);
  });
  document.getElementById("import-template")?.addEventListener("change", function () {
    if (fileInput?.files?.[0]) previewFile(fileInput.files[0]);
  });

  const pasteForm = document.getElementById("paste-import-form");
  pasteForm?.addEventListener("submit", async function (e) {
    e.preventDefault();
    const pasteEl = pasteForm.querySelector("textarea[name=paste]");
    const paste = pasteEl?.value || "";
    const fd = new FormData();
    fd.append("paste", paste);
    const tpl = document.getElementById("paste-template");
    if (tpl?.value) fd.append("template_id", tpl.value);
    try {
      const res = await fetch("/contacts/paste/preview", { method: "POST", body: fd, credentials: "same-origin" });
      const data = await res.json();
      if (res.ok && data.headered) {
        const listSel = pasteForm.querySelector("select[name=list_id]");
        openModal(data, {
          mode: "paste",
          paste,
          templateId: tpl?.value || "",
          listId: listSel?.value || "",
          templateVars: data.template_vars || selectedTemplateVars(tpl),
        });
        return;
      }
    } catch (_) {
      /* fall through to plain import */
    }
    HTMLFormElement.prototype.submit.call(pasteForm);
  });
})();

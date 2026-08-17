(function () {
  "use strict";

  const Core = window.ResumeCore;
  const workspace = document.getElementById("workspace");
  const jsonInput = document.getElementById("json-file-input");
  const snapshotInput = document.getElementById("snapshot-file-input");
  const dialog = document.getElementById("message-dialog");
  const toast = document.getElementById("toast");
  const serverMode = Boolean(/^https?:$/.test(window.location.protocol) && /^(127\.0\.0\.1|localhost)$/.test(window.location.hostname));

  let state = Core.emptyResume();
  let currentView = "entries";
  let previewVariantId = "cv1";
  let fileHandle = null;
  let fileName = "resume.json";
  let dirty = false;
  let hasDocument = false;
  let draggedEntryIndex = null;
  let draggedSection = null;
  let toastTimer = null;
  let erpRunning = false;
  let erpEventSource = null;
  let erpStreamWarned = false;
  let autosaveTimer = null;
  let pdfSignature = "";
  let setupStatusTimer = null;
  let setupStatusRequest = null;
  let setupJobID = 0;
  let handledSetupJobID = 0;
  const savedSelections = new WeakMap();
  const blockEditorSync = new WeakMap();
  const inlineEditorSync = new WeakMap();

  function escapeHtml(value) {
    const element = document.createElement("div");
    element.textContent = value == null ? "" : String(value);
    return element.innerHTML;
  }

  function optionMarkup(options, selected, includeBlank) {
    const blank = includeBlank ? '<option value="">Select</option>' : "";
    return blank + options.map((option) => {
      const value = typeof option === "string" ? option : option.value;
      const label = typeof option === "string" ? option : option.label;
      return `<option value="${escapeHtml(value)}"${value === selected ? " selected" : ""}>${escapeHtml(label)}</option>`;
    }).join("");
  }

  function gapOptions(selected) {
    const current = Number(selected) || 0;
    return Array.from({ length: Core.MAX_GAP_PIXELS + 1 }, (_, value) => (
      `<option value="${value}"${value === current ? " selected" : ""}>${value}px</option>`
    )).join("");
  }

  function gapValue(value) {
    const number = Number(value);
    if (!Number.isInteger(number) || number <= 0) return 0;
    return Math.min(number, Core.MAX_GAP_PIXELS);
  }

  function setOptionalGap(target, key, value) {
    const gap = gapValue(value);
    if (gap) target[key] = gap;
    else delete target[key];
  }

  function showToast(message) {
    toast.textContent = message;
    toast.classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => toast.classList.remove("show"), 2600);
  }

  function showDialog(title, bodyHtml) {
    document.getElementById("dialog-title").textContent = title;
    document.getElementById("dialog-body").innerHTML = bodyHtml;
    dialog.showModal();
  }

  function apiURL(path) {
    return path;
  }

  async function apiFetch(path, options) {
    const request = Object.assign({}, options || {});
    const response = await fetch(apiURL(path), request);
    if (!response.ok) {
      let message = `${response.status} ${response.statusText}`;
      try {
        const payload = await response.json();
        if (payload && payload.error) message = payload.error;
      } catch (_) {
        try {
          message = await response.text();
        } catch (_) {}
      }
      throw new Error(message);
    }
    return response;
  }

  function updateChrome() {
    document.getElementById("entry-count").textContent = state.entries.length;
    document.getElementById("document-name").textContent = hasDocument
      ? (state.metadata.name || fileName)
      : "No document open";
    document.getElementById("save-status").textContent = hasDocument
      ? (dirty ? "Unsaved changes · recovery draft stored locally" : `Saved · ${fileName}${serverMode ? " on disk" : ""}`)
      : (serverMode ? "Loading data/resume.json from the local server." : "Open data/resume.json to begin.");
    const indicator = document.getElementById("dirty-indicator");
    indicator.className = `status-dot ${dirty ? "dirty" : hasDocument ? "saved" : ""}`;

    const result = Core.validateResume(state);
    const summary = document.getElementById("validation-summary");
    if (hasDocument && !result.valid) {
      summary.hidden = false;
      summary.innerHTML = `<strong>${result.errors.length} validation issue${result.errors.length === 1 ? "" : "s"}</strong><br>${result.errors.slice(0, 3).map(escapeHtml).join("<br>")}`;
    } else {
      summary.hidden = true;
      summary.innerHTML = "";
    }
  }

  function storeDraft() {
    try {
      localStorage.setItem(Core.DRAFT_KEY, JSON.stringify(state));
    } catch (_) {
      // JSON export remains the durable fallback when storage is unavailable.
    }
  }

  function markDirty(renderPreview) {
    hasDocument = true;
    dirty = true;
    state.metadata.updatedAt = new Date().toISOString().slice(0, 10);
    storeDraft();
    updateChrome();
    if (renderPreview && currentView === "preview") render();
    scheduleAutosave();
  }

  function scheduleAutosave() {
    if (!serverMode) return;
    clearTimeout(autosaveTimer);
    autosaveTimer = setTimeout(async () => {
      flushVisibleEditors(true);
      const result = Core.validateResume(state);
      if (!result.valid) { storeDraft(); updateChrome(); return; }
      try {
        await apiFetch("/api/resume", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(result.data, null, 2) + "\n" });
        markSaved();
        showToast("Saved locally.");
      } catch (_) { storeDraft(); }
    }, 750);
  }

  function markSaved() {
    dirty = false;
    try { localStorage.removeItem(Core.DRAFT_KEY); } catch (_) {}
    updateChrome();
  }

  function setDocument(data, name, handle) {
    const result = Core.validateResume(data);
    if (!result.valid) {
      showDialog("Cannot open resume", `<ul class="error-list">${result.errors.map((error) => `<li>${escapeHtml(error)}</li>`).join("")}</ul>`);
      return false;
    }
    state = result.data;
    fileName = name || "resume.json";
    fileHandle = handle || null;
    hasDocument = true;
    dirty = false;
    currentView = "entries";
    previewVariantId = "cv1";
    updateNavigation();
    updateChrome();
    render();
    return true;
  }

  function readFile(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result || ""));
      reader.onerror = () => reject(reader.error || new Error("Unable to read file."));
      reader.readAsText(file);
    });
  }

  async function openJson() {
    if (window.showOpenFilePicker) {
      try {
        const [handle] = await window.showOpenFilePicker({
          types: [{ description: "Resume JSON", accept: { "application/json": [".json"] } }],
          multiple: false
        });
        const file = await handle.getFile();
        const data = JSON.parse(await file.text());
        setDocument(data, file.name, handle);
        return;
      } catch (error) {
        if (error && error.name === "AbortError") return;
      }
    }
    jsonInput.value = "";
    jsonInput.click();
  }

  async function loadJsonFile(file) {
    try {
      const data = JSON.parse(await readFile(file));
      setDocument(data, file.name, null);
    } catch (error) {
      showDialog("Cannot open JSON", `<p>${escapeHtml(error.message || error)}</p>`);
    }
  }

  async function loadServerResume() {
    try {
      const response = await apiFetch("/api/resume");
      const data = await response.json();
      setDocument(data, "data/resume.json", null);
    } catch (error) {
      showDialog("Could not load data/resume.json", `<p>${escapeHtml(error.message || error)}</p><p>Check the terminal running <code>./cvpp editor</code>.</p>`);
      render();
    }
  }

  function serializedState() {
    flushVisibleEditors(true);
    const result = Core.validateResume(state);
    if (!result.valid) {
      showDialog("Fix validation issues before saving", `<ul class="error-list">${result.errors.map((error) => `<li>${escapeHtml(error)}</li>`).join("")}</ul>`);
      return null;
    }
    const updatedAt = new Date().toISOString().slice(0, 10);
    state.metadata.updatedAt = updatedAt;
    result.data.metadata.updatedAt = updatedAt;
    return JSON.stringify(result.data, null, 2) + "\n";
  }

  function downloadJson(name) {
    const content = serializedState();
    if (!content) return;
    const blob = new Blob([content], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = name || fileName || "resume.json";
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  async function saveJson() {
    const content = serializedState();
    if (!content) return false;
    if (serverMode) {
      try {
        await apiFetch("/api/resume", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: content
        });
        markSaved();
        showToast("Saved data/resume.json.");
        return true;
      } catch (error) {
        showDialog("Could not save data/resume.json", `<p>${escapeHtml(error.message || error)}</p>`);
        return false;
      }
    }
    if (fileHandle && fileHandle.createWritable) {
      try {
        const writable = await fileHandle.createWritable();
        await writable.write(content);
        await writable.close();
        markSaved();
        showToast("Resume JSON saved.");
        return true;
      } catch (error) {
        showDialog("Could not save in place", `<p>${escapeHtml(error.message || error)}</p><p>Use <strong>Download copy</strong> instead.</p>`);
        return false;
      }
    }
    downloadJson(fileName);
    markSaved();
    showToast("Downloaded a saved JSON copy.");
    return true;
  }

  async function importSnapshot(file) {
    try {
      const imported = Core.importPortalSnapshot(await readFile(file));
      const result = Core.validateResume(imported);
      if (!result.valid) throw new Error(result.errors.join("\n"));
      if (hasDocument && !window.confirm("Replace the current editor document with data from this portal snapshot?")) return;
      setDocument(result.data, "portal-import.json", null);
      markDirty(false);
      showToast("Portal snapshot imported. Save it as JSON when ready.");
    } catch (error) {
      showDialog("Snapshot import failed", `<p>${escapeHtml(error.message || error)}</p>`);
    }
  }

  function viewHeader(title, description, actions) {
    return `<div class="view-header"><div><h2>${escapeHtml(title)}</h2><p>${escapeHtml(description)}</p></div><div class="view-actions">${actions || ""}</div></div>`;
  }

  function render() {
    if (!hasDocument) {
      workspace.innerHTML = `${viewHeader("Open your resume", "The editor reads and writes a versioned JSON file; it never sends resume data anywhere.")}
        <div class="empty-state"><h3>No resume is open</h3><p>Choose <strong>Open JSON</strong> and select <code>data/resume.json</code>.</p><button id="empty-open" class="button primary">Open resume JSON</button></div>`;
      document.getElementById("empty-open").addEventListener("click", openJson);
      return;
    }
    if (currentView === "entries") renderEntries();
    if (currentView === "academics") renderAcademics();
    if (currentView === "variants") renderVariants();
    if (currentView === "shared") renderShared();
    if (currentView === "preview") renderPreview();
  }

  function detailMarkup(block, entryIndex, blockIndex, blockCount) {
    const first = blockIndex === 0;
    const last = blockIndex >= blockCount - 1;
    return `<div class="detail-row${block.hidden ? " is-hidden" : ""}">
      <select class="select detail-kind" data-entry="${entryIndex}" data-block="${blockIndex}" aria-label="Detail type">
        <option value="bullet"${block.kind === "bullet" ? " selected" : ""}>Bullet</option>
        <option value="paragraph"${block.kind === "paragraph" ? " selected" : ""}>Paragraph</option>
      </select>
      <div class="rich-wrap">
        ${richToolbar(false)}
        <div class="rich-input" contenteditable="true" data-entry="${entryIndex}" data-block="${blockIndex}" data-placeholder="Write one focused result or responsibility…">${Core.sanitizeInlineHtml(block.html, false)}</div>
      </div>
      <div class="detail-side">
        <div class="gap-controls" aria-label="Line spacing">
          <label>Before<select class="select detail-gap" data-entry="${entryIndex}" data-block="${blockIndex}" data-gap-key="gapBefore">${gapOptions(block.gapBefore)}</select></label>
          <label>After<select class="select detail-gap" data-entry="${entryIndex}" data-block="${blockIndex}" data-gap-key="gapAfter">${gapOptions(block.gapAfter)}</select></label>
        </div>
        <div class="detail-actions">
          <button type="button" class="button secondary small icon" data-action="move-block-up" data-entry="${entryIndex}" data-block="${blockIndex}" title="Move detail up"${first ? " disabled" : ""}>↑</button>
          <button type="button" class="button secondary small icon" data-action="move-block-down" data-entry="${entryIndex}" data-block="${blockIndex}" title="Move detail down"${last ? " disabled" : ""}>↓</button>
          <button type="button" class="button secondary small" data-action="toggle-block" data-entry="${entryIndex}" data-block="${blockIndex}" aria-pressed="${block.hidden ? "true" : "false"}" title="${block.hidden ? "Include this detail in resume output" : "Keep this detail in JSON but omit it from resume output"}">${block.hidden ? "Unhide" : "Hide"}</button>
          <button type="button" class="button danger small icon" data-action="delete-block" data-entry="${entryIndex}" data-block="${blockIndex}" title="Delete detail">×</button>
        </div>
      </div>
    </div>`;
  }

  function renderEntries() {
    const actions = `<button id="add-entry" class="button primary">Add entry</button>`;
    const cards = state.entries.map((entry, index) => `<article class="card entry-card${entry.hidden ? " is-hidden" : ""}" draggable="true" data-entry-card="${index}">
      <div class="card-header">
        <div class="card-tools"><span class="drag-handle" title="Drag to reorder">⋮⋮</span><span class="pill">${index + 1} / 50</span></div>
        <div class="card-tools">
          <button class="button secondary small" data-action="toggle-entry" data-entry="${index}" aria-pressed="${entry.hidden ? "true" : "false"}" title="${entry.hidden ? "Include this entry in configured CVs" : "Keep this entry in JSON but omit it from every CV"}">${entry.hidden ? "Unhide entry" : "Hide entry"}</button>
          <button class="button secondary small" data-action="duplicate-entry" data-entry="${index}">Duplicate</button>
          <button class="button danger small" data-action="delete-entry" data-entry="${index}">Delete</button>
        </div>
      </div>
      <div class="card-body">
        <div class="form-grid">
          <div class="field third"><label>Portal category</label><select class="select entry-type" data-entry="${index}">${optionMarkup(Core.ENTRY_TYPES, entry.type, true)}</select></div>
          <div class="field half"><label>Overview / heading</label><input class="input entry-overview" data-entry="${index}" value="${escapeHtml(entry.overview)}"></div>
          <div class="field third"><span class="field-label">Include in</span><div class="variant-checks">${Core.VARIANT_IDS.map((id, variantIndex) => `<label class="check"><input type="checkbox" class="entry-include" data-entry="${index}" data-variant="${id}"${entry.includeIn.includes(id) ? " checked" : ""}> CV${variantIndex + 1}</label>`).join("")}</div></div>
        </div>
        <div class="details">${entry.details.map((block, blockIndex) => detailMarkup(block, index, blockIndex, entry.details.length)).join("")}</div>
        <div class="details-footer"><button class="button secondary small" data-action="add-block" data-entry="${index}">Add bullet</button></div>
      </div>
    </article>`).join("");

    workspace.innerHTML = `${viewHeader("Resume entries", "Maintain up to 50 portal entries. ERP export uses 9 px text, inline bold/italic markup, and optional per-line spacing.", actions)}
      <div class="stack">${cards || '<div class="empty-state"><h3>No entries yet</h3><p>Add internships, projects, achievements, and responsibilities as separate cards.</p></div>'}</div>`;
    bindEntryEvents();
  }

  function createEntry() {
    return {
      id: `entry-${Date.now()}`,
      type: "Experience",
      overview: "",
      details: [{ kind: "bullet", html: "" }],
      includeIn: ["cv1"]
    };
  }

  function richToolbar(allowLists) {
    const sizes = Array.from({ length: 17 }, (_, index) => index + 8);
    const lineSpacing = allowLists ? `
      <select class="line-gap-select" data-gap-position="before" title="Gap before current line" aria-label="Gap before current line">
        <option value="">Before</option>${gapOptions(0)}
      </select>
      <select class="line-gap-select" data-gap-position="after" title="Gap after current line" aria-label="Gap after current line">
        <option value="">After</option>${gapOptions(0)}
      </select>` : "";
    return `<div class="rich-toolbar">
      <button type="button" data-command="bold" title="Bold" aria-label="Bold">B</button>
      <button type="button" data-command="italic" title="Italic" aria-label="Italic"><em>I</em></button>
      ${allowLists ? '<button type="button" data-command="insertUnorderedList" title="Bulleted list" aria-label="Bulleted list">• List</button>' : ""}
      <select class="font-size-select" data-command="fontSize" title="Font size" aria-label="Font size">
        <option value="">Size</option><option value="default">Default</option>${sizes.map((size) => `<option value="${size}">${size}</option>`).join("")}
      </select>
      ${lineSpacing}
    </div>`;
  }

  function rememberSelection(editor) {
    const selection = window.getSelection();
    if (!selection || !selection.rangeCount) return;
    const range = selection.getRangeAt(0);
    const container = range.commonAncestorContainer.nodeType === Node.TEXT_NODE
      ? range.commonAncestorContainer.parentNode
      : range.commonAncestorContainer;
    if (container && editor.contains(container)) savedSelections.set(editor, range.cloneRange());
  }

  function restoreSelection(editor) {
    const range = savedSelections.get(editor);
    if (!range) return false;
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    return true;
  }

  function applyFontSize(editor, value) {
    const size = Number(value);
    if (value !== "default" && (!Number.isInteger(size) || size < 8 || size > 24)) return;
    restoreSelection(editor);
    const selection = window.getSelection();
    if (!selection || !selection.rangeCount) return;
    if (selection.getRangeAt(0).collapsed) {
      const range = document.createRange();
      range.selectNodeContents(editor);
      selection.removeAllRanges();
      selection.addRange(range);
    }
    if (value === "default") {
      const range = selection.getRangeAt(0);
      const fragment = range.extractContents();
      fragment.querySelectorAll("span").forEach((span) => {
        span.style.removeProperty("font-size");
        if (!span.getAttribute("style")) span.removeAttribute("style");
        if (!span.attributes.length) span.replaceWith(...span.childNodes);
      });
      range.insertNode(fragment);
      editor.normalize();
      return;
    }
    document.execCommand("fontSize", false, "7");
    editor.querySelectorAll('font[size="7"]').forEach((font) => {
      const span = document.createElement("span");
      span.style.fontSize = `${size}px`;
      while (font.firstChild) span.appendChild(font.firstChild);
      font.replaceWith(span);
    });
  }

  function currentEditableLine(editor) {
    restoreSelection(editor);
    const selection = window.getSelection();
    if (!selection || !selection.rangeCount) return null;
    let node = selection.anchorNode;
    if (node && node.nodeType === Node.TEXT_NODE) node = node.parentNode;
    while (node && node !== editor) {
      if (node.nodeType === Node.ELEMENT_NODE && (node.tagName === "P" || node.tagName === "LI")) return node;
      node = node.parentNode;
    }
    const firstLine = editor.querySelector("p, li");
    return firstLine || null;
  }

  function applyLineGapStyle(line) {
    if (!line || !line.getAttribute) return;
    const before = gapValue(line.getAttribute("data-gap-before"));
    const after = gapValue(line.getAttribute("data-gap-after"));
    if (before) line.style.marginTop = `${before}px`;
    else line.style.removeProperty("margin-top");
    if (after) line.style.marginBottom = `${after}px`;
    else line.style.removeProperty("margin-bottom");
    if (!line.getAttribute("style")) line.removeAttribute("style");
  }

  function applyEditorGapStyles(editor) {
    editor.querySelectorAll("p, li").forEach(applyLineGapStyle);
  }

  function applyLineGap(editor, position, value) {
    const gap = gapValue(value);
    editor.focus();
    const line = currentEditableLine(editor);
    if (!line) {
      showToast("Put the cursor on a paragraph/list item before setting spacing.");
      return;
    }
    const attribute = `data-gap-${position}`;
    if (gap) line.setAttribute(attribute, String(gap));
    else line.removeAttribute(attribute);
    applyLineGapStyle(line);
    rememberSelection(editor);
    editor.dispatchEvent(new Event("input", { bubbles: true }));
  }

  function applyRichCommand(editor, command, value) {
    editor.focus();
    restoreSelection(editor);
    if (command === "fontSize") {
      applyFontSize(editor, value);
    } else {
      document.execCommand(command, false, null);
    }
    rememberSelection(editor);
    editor.dispatchEvent(new Event("input", { bubbles: true }));
  }

  function bindRichToolbar(editor) {
    ["focus", "keyup", "mouseup"].forEach((eventName) => {
      editor.addEventListener(eventName, () => rememberSelection(editor));
    });
    const toolbar = editor.closest(".rich-wrap").querySelector(".rich-toolbar");
    toolbar.querySelectorAll("button[data-command]").forEach((button) => {
      button.addEventListener("mousedown", (event) => event.preventDefault());
      button.addEventListener("click", () => applyRichCommand(editor, button.dataset.command));
    });
    toolbar.querySelectorAll("select[data-command]").forEach((select) => {
      select.addEventListener("mousedown", () => rememberSelection(editor));
      select.addEventListener("change", () => {
        applyRichCommand(editor, select.dataset.command, select.value);
        select.value = "";
      });
    });
    toolbar.querySelectorAll("select[data-gap-position]").forEach((select) => {
      select.addEventListener("mousedown", () => rememberSelection(editor));
      select.addEventListener("change", () => {
        applyLineGap(editor, select.dataset.gapPosition, select.value);
        select.value = "";
      });
    });
  }

  function bindEntryEvents() {
    document.getElementById("add-entry").addEventListener("click", () => {
      if (state.entries.length >= 50) return showDialog("Portal limit reached", "<p>The portal accepts at most 50 entries.</p>");
      state.entries.push(createEntry());
      markDirty(false);
      renderEntries();
      workspace.querySelector(".entry-card:last-child").scrollIntoView({ behavior: "smooth", block: "center" });
    });

    workspace.querySelectorAll(".entry-type").forEach((select) => select.addEventListener("change", () => {
      state.entries[Number(select.dataset.entry)].type = select.value;
      markDirty(false);
    }));
    workspace.querySelectorAll(".entry-overview").forEach((input) => input.addEventListener("input", () => {
      state.entries[Number(input.dataset.entry)].overview = input.value;
      markDirty(false);
    }));
    workspace.querySelectorAll(".entry-include").forEach((input) => input.addEventListener("change", () => {
      const entry = state.entries[Number(input.dataset.entry)];
      entry.includeIn = input.checked
        ? Array.from(new Set(entry.includeIn.concat(input.dataset.variant)))
        : entry.includeIn.filter((id) => id !== input.dataset.variant);
      markDirty(false);
    }));
    workspace.querySelectorAll(".detail-kind").forEach((select) => select.addEventListener("change", () => {
      state.entries[Number(select.dataset.entry)].details[Number(select.dataset.block)].kind = select.value;
      markDirty(false);
    }));
    workspace.querySelectorAll(".detail-gap").forEach((select) => select.addEventListener("change", () => {
      const block = state.entries[Number(select.dataset.entry)].details[Number(select.dataset.block)];
      setOptionalGap(block, select.dataset.gapKey, select.value);
      markDirty(false);
    }));
    workspace.querySelectorAll(".rich-input").forEach(bindInlineEditor);
    workspace.querySelectorAll("[data-action]").forEach((button) => button.addEventListener("click", () => {
      flushVisibleInlineEditors(false);
      const entryIndex = Number(button.dataset.entry);
      if (button.dataset.action === "delete-entry") {
        if (!window.confirm(`Delete "${state.entries[entryIndex].overview || "this entry"}"?`)) return;
        state.entries.splice(entryIndex, 1);
      }
      if (button.dataset.action === "duplicate-entry") {
        if (state.entries.length >= 50) return showDialog("Portal limit reached", "<p>The portal accepts at most 50 entries.</p>");
        const copy = Core.clone(state.entries[entryIndex]);
        copy.id = `${copy.id}-copy-${Date.now()}`;
        state.entries.splice(entryIndex + 1, 0, copy);
      }
      if (button.dataset.action === "toggle-entry") {
        const entry = state.entries[entryIndex];
        if (entry.hidden) delete entry.hidden;
        else entry.hidden = true;
      }
      if (button.dataset.action === "add-block") {
        state.entries[entryIndex].details.push({ kind: "bullet", html: "" });
      }
      if (button.dataset.action === "delete-block") {
        state.entries[entryIndex].details.splice(Number(button.dataset.block), 1);
      }
      if (button.dataset.action === "move-block-up" || button.dataset.action === "move-block-down") {
        const details = state.entries[entryIndex].details;
        const from = Number(button.dataset.block);
        const to = button.dataset.action === "move-block-up" ? from - 1 : from + 1;
        if (to >= 0 && to < details.length) {
          const [moved] = details.splice(from, 1);
          details.splice(to, 0, moved);
        }
      }
      if (button.dataset.action === "toggle-block") {
        const block = state.entries[entryIndex].details[Number(button.dataset.block)];
        if (block.hidden) delete block.hidden;
        else block.hidden = true;
      }
      markDirty(false);
      renderEntries();
    }));

    workspace.querySelectorAll(".entry-card").forEach((card) => {
      card.addEventListener("dragstart", () => {
        draggedEntryIndex = Number(card.dataset.entryCard);
        card.classList.add("dragging");
      });
      card.addEventListener("dragend", () => {
        draggedEntryIndex = null;
        card.classList.remove("dragging");
        workspace.querySelectorAll(".drag-over").forEach((node) => node.classList.remove("drag-over"));
      });
      card.addEventListener("dragover", (event) => {
        event.preventDefault();
        card.classList.add("drag-over");
      });
      card.addEventListener("dragleave", () => card.classList.remove("drag-over"));
      card.addEventListener("drop", (event) => {
        event.preventDefault();
        const targetIndex = Number(card.dataset.entryCard);
        if (draggedEntryIndex == null || targetIndex === draggedEntryIndex) return;
        const [moved] = state.entries.splice(draggedEntryIndex, 1);
        state.entries.splice(targetIndex, 0, moved);
        markDirty(false);
        renderEntries();
      });
    });
  }

  function academicCard(academic, current, index) {
    const key = current ? "current" : `previous-${index}`;
    return `<article class="card academic-card" data-academic="${key}">
      <div class="card-header"><h3>${current ? "Current qualification" : `Previous qualification · portal slot ${academic.slot}`}</h3>${current ? '<span class="pill">Server-managed identity fields</span>' : `<button class="button danger small" data-delete-academic="${index}">Remove</button>`}</div>
      <div class="card-body"><div class="form-grid">
        <div class="field quarter"><label>Standard</label><input class="input academic-input" data-field="standard" value="${escapeHtml(academic.standard)}"${current ? " disabled" : ""}></div>
        <div class="field quarter"><label>Degree / exam</label><input class="input academic-input" data-field="qualification" value="${escapeHtml(academic.qualification)}"${current ? " disabled" : ""}></div>
        <div class="field quarter"><label>Institution</label><input class="input academic-input" data-field="institution" value="${escapeHtml(academic.institution)}"${current ? " disabled" : ""}></div>
        <div class="field quarter"><label>Completion year</label><input class="input academic-input" data-field="completionYear" value="${escapeHtml(academic.completionYear)}"></div>
        <div class="field quarter"><label>Score type</label><select class="select academic-score-kind"><option value="percentage"${academic.score.kind === "percentage" ? " selected" : ""}>Percentage</option><option value="cgpa"${academic.score.kind === "cgpa" ? " selected" : ""}>CGPA</option></select></div>
        <div class="field quarter"><label>${academic.score.kind === "percentage" ? "Percentage" : "CGPA"}</label><input class="input academic-score-value" value="${escapeHtml(academic.score.value)}"></div>
        <div class="field quarter"><label>Maximum CGPA</label><input class="input academic-score-outof" value="${escapeHtml(academic.score.outOf)}"${academic.score.kind === "percentage" ? " disabled" : ""}></div>
        ${current ? `<div class="field half"><label>Specialization (preview only)</label><input class="input academic-input" data-field="specialization" value="${escapeHtml(academic.specialization)}" disabled></div>` : ""}
      </div></div>
    </article>`;
  }

  function renderAcademics() {
    workspace.innerHTML = `${viewHeader("Academics", "Manage previous qualifications and the editable score/year fields for your current degree.", '<button id="add-academic" class="button primary">Add qualification</button>')}
      <div class="stack">${academicCard(state.academics.current, true, 0)}${state.academics.previous.sort((a, b) => a.slot - b.slot).map((academic, index) => academicCard(academic, false, index)).join("")}</div>`;
    bindAcademicEvents();
  }

  function bindAcademicEvents() {
    document.getElementById("add-academic").addEventListener("click", () => {
      const used = new Set(state.academics.previous.map((item) => item.slot));
      const slot = [1, 2, 3, 4, 5].find((candidate) => !used.has(candidate));
      if (!slot) return showDialog("No academic slot available", "<p>The portal provides five previous-qualification slots.</p>");
      state.academics.previous.push({ slot, standard: "", qualification: "", institution: "", completionYear: "", score: { kind: "percentage", value: "", outOf: "" } });
      markDirty(false);
      renderAcademics();
    });
    workspace.querySelectorAll(".academic-card").forEach((card) => {
      const current = card.dataset.academic === "current";
      const index = current ? -1 : Number(card.dataset.academic.split("-")[1]);
      const academic = current ? state.academics.current : state.academics.previous[index];
      card.querySelectorAll(".academic-input:not([disabled])").forEach((input) => input.addEventListener("input", () => {
        academic[input.dataset.field] = input.value;
        markDirty(false);
      }));
      card.querySelector(".academic-score-kind").addEventListener("change", (event) => {
        academic.score.kind = event.target.value;
        academic.score.outOf = event.target.value === "percentage" ? "" : academic.score.outOf;
        markDirty(false);
        renderAcademics();
      });
      card.querySelector(".academic-score-value").addEventListener("input", (event) => {
        academic.score.value = event.target.value;
        markDirty(false);
      });
      const outOf = card.querySelector(".academic-score-outof:not([disabled])");
      if (outOf) outOf.addEventListener("input", () => { academic.score.outOf = outOf.value; markDirty(false); });
    });
    workspace.querySelectorAll("[data-delete-academic]").forEach((button) => button.addEventListener("click", () => {
      state.academics.previous.splice(Number(button.dataset.deleteAcademic), 1);
      markDirty(false);
      renderAcademics();
    }));
  }

  function richSection(title, editorId, html, description) {
    const preview = Core.formatPortalBlockHtml(html);
    return `<div class="field"><label>${escapeHtml(title)}</label>${description ? `<span class="muted">${escapeHtml(description)}</span>` : ""}
      <div class="editor-panel">
        <div class="rich-wrap">${richToolbar(true)}<div id="${editorId}" class="html-editor" contenteditable="true">${Core.sanitizeBlockHtml(html, false)}</div></div>
        <div id="${editorId}-preview" class="html-preview">${preview || '<span class="muted">Preview will appear here.</span>'}</div>
      </div>
    </div>`;
  }

  function renderVariants() {
    const variants = state.variants.map((variant, variantIndex) => {
      const available = Core.SECTION_OPTIONS.filter((option) => !variant.sectionOrder.includes(option.value));
      return `<article class="card variant-card" data-variant-index="${variantIndex}">
        <div class="card-header"><h3>${escapeHtml(variant.label)}</h3><span class="pill">${variant.sectionOrder.length} / 14 sections</span></div>
        <div class="card-body stack">
          <div class="form-grid">
            <div class="field half"><span class="field-label">Academic visibility</span><div class="variant-checks"><label class="check"><input type="checkbox" class="variant-toggle" data-field="showMinor"${variant.showMinor ? " checked" : ""}> Show minor</label><label class="check"><input type="checkbox" class="variant-toggle" data-field="showMicro"${variant.showMicro ? " checked" : ""}> Show micro-specialization</label></div></div>
          </div>
          <div class="field"><label>Section order</label>
            <ol class="order-list">${variant.sectionOrder.map((section, sectionIndex) => `<li class="order-item" draggable="true" data-section-index="${sectionIndex}"><span class="order-number">${sectionIndex + 1}</span><span>${escapeHtml((Core.SECTION_OPTIONS.find((item) => item.value === section) || { label: section }).label)}</span><button class="button danger small icon" data-remove-section="${sectionIndex}">×</button></li>`).join("")}</ol>
            <div class="add-section-row"><select class="select add-section-select">${optionMarkup(available, "", true)}</select><button class="button secondary add-section"${available.length ? "" : " disabled"}>Add section</button></div>
          </div>
          ${richSection("Skills and expertise", `skills-${variantIndex}`, variant.skillsHtml, "This portal field is specific to this CV variant.")}
          ${richSection("Extra-curricular activities", `extras-${variantIndex}`, variant.extracurricularHtml, "This portal field is specific to this CV variant.")}
        </div>
      </article>`;
    }).join("");
    workspace.innerHTML = `${viewHeader("CV variants", "Choose what appears in CV1, CV2, and CV3 and control the portal’s section order.")}<div class="stack">${variants}</div>`;
    bindVariantEvents();
  }

  function bindBlockEditor(editor, preview, onChange) {
    function sync(clean) {
      const previous = editor.dataset.syncedHtml || "";
      const next = clean ? Core.sanitizeBlockHtml(editor.innerHTML, false) : editor.innerHTML;
      if (clean && editor.innerHTML !== next) editor.innerHTML = next;
      applyEditorGapStyles(editor);
      preview.innerHTML = Core.formatPortalBlockHtml(next) || '<span class="muted">Preview will appear here.</span>';
      onChange(next);
      editor.dataset.syncedHtml = next;
      return next !== previous;
    }

    editor.dataset.syncedHtml = editor.innerHTML;
    applyEditorGapStyles(editor);
    blockEditorSync.set(editor, sync);

    editor.addEventListener("input", () => {
      sync(false);
      markDirty(false);
    });
    editor.addEventListener("blur", () => {
      if (sync(true)) markDirty(false);
      storeDraft();
    });
    bindRichToolbar(editor);
  }

  function flushVisibleBlockEditors(clean) {
    document.querySelectorAll(".html-editor").forEach((editor) => {
      const sync = blockEditorSync.get(editor);
      if (sync && sync(clean)) markDirty(false);
    });
  }

  function bindInlineEditor(editor) {
    function sync(clean) {
      const previous = editor.dataset.syncedHtml || "";
      const next = clean ? Core.sanitizeInlineHtml(editor.innerHTML, false) : editor.innerHTML;
      if (clean && editor.innerHTML !== next) editor.innerHTML = next;
      state.entries[Number(editor.dataset.entry)].details[Number(editor.dataset.block)].html = next;
      editor.dataset.syncedHtml = next;
      return next !== previous;
    }

    editor.dataset.syncedHtml = editor.innerHTML;
    inlineEditorSync.set(editor, sync);

    editor.addEventListener("input", () => {
      sync(false);
      markDirty(false);
    });
    editor.addEventListener("blur", () => {
      if (sync(true)) markDirty(false);
      storeDraft();
    });
    bindRichToolbar(editor);
  }

  function flushVisibleInlineEditors(clean) {
    document.querySelectorAll(".rich-input").forEach((editor) => {
      const sync = inlineEditorSync.get(editor);
      if (sync && sync(clean)) markDirty(false);
    });
  }

  function flushVisibleEditors(clean) {
    flushVisibleBlockEditors(clean);
    flushVisibleInlineEditors(clean);
  }

  function bindVariantEvents() {
    workspace.querySelectorAll(".variant-card").forEach((card) => {
      const variantIndex = Number(card.dataset.variantIndex);
      const variant = state.variants[variantIndex];
      card.querySelectorAll(".variant-toggle").forEach((toggle) => toggle.addEventListener("change", () => {
        variant[toggle.dataset.field] = toggle.checked;
        markDirty(false);
      }));
      card.querySelector(".add-section").addEventListener("click", () => {
        const value = card.querySelector(".add-section-select").value;
        if (!value || variant.sectionOrder.includes(value)) return;
        variant.sectionOrder.push(value);
        markDirty(false);
        renderVariants();
      });
      card.querySelectorAll("[data-remove-section]").forEach((button) => button.addEventListener("click", () => {
        variant.sectionOrder.splice(Number(button.dataset.removeSection), 1);
        markDirty(false);
        renderVariants();
      }));
      card.querySelectorAll(".order-item").forEach((item) => {
        item.addEventListener("dragstart", () => {
          draggedSection = { variantIndex, index: Number(item.dataset.sectionIndex) };
          item.classList.add("dragging");
        });
        item.addEventListener("dragend", () => { draggedSection = null; item.classList.remove("dragging"); });
        item.addEventListener("dragover", (event) => event.preventDefault());
        item.addEventListener("drop", (event) => {
          event.preventDefault();
          if (!draggedSection || draggedSection.variantIndex !== variantIndex) return;
          const target = Number(item.dataset.sectionIndex);
          const [moved] = variant.sectionOrder.splice(draggedSection.index, 1);
          variant.sectionOrder.splice(target, 0, moved);
          markDirty(false);
          renderVariants();
        });
      });
      bindBlockEditor(card.querySelector(`#skills-${variantIndex}`), card.querySelector(`#skills-${variantIndex}-preview`), (html) => { variant.skillsHtml = html; });
      bindBlockEditor(card.querySelector(`#extras-${variantIndex}`), card.querySelector(`#extras-${variantIndex}-preview`), (html) => { variant.extracurricularHtml = html; });
    });
  }

  function renderShared() {
    workspace.innerHTML = `${viewHeader("Shared sections", "Coursework is shared by all portal CV variants; its visibility is controlled in each section order.")}
      <article class="card"><div class="card-header"><h3>Coursework information</h3><span class="pill">Shared</span></div><div class="card-body">${richSection("Relevant coursework", "coursework-editor", state.shared.courseworkHtml, "Use short, relevant course names rather than full descriptions.")}</div></article>`;
    bindBlockEditor(document.getElementById("coursework-editor"), document.getElementById("coursework-editor-preview"), (html) => { state.shared.courseworkHtml = html; });
  }

  function previewEntry(entry) {
    return `<div class="preview-entry">${Core.sanitizeBlockHtml(Core.entrySubjectHtml(entry), false)}</div>`;
  }

  function sectionEntries(section, variantId) {
    const accepted = section === "Internship/Project" ? ["Internship", "Project", "Internship/Project"] : [section];
    return state.entries.filter((entry) => !entry.hidden && accepted.includes(entry.type) && entry.includeIn.includes(variantId));
  }

  function renderPreview() {
    const variant = state.variants.find((item) => item.id === previewVariantId) || state.variants[0];
    const previewSections = [];
    previewSections.push(`<h2>Education</h2><h3>${escapeHtml(state.academics.current.institution)}</h3><p>${escapeHtml(state.academics.current.specialization || state.academics.current.qualification)} · ${escapeHtml(state.academics.current.completionYear)}</p><p>CGPA: ${escapeHtml(state.academics.current.score.value)}${state.academics.current.score.outOf ? ` / ${escapeHtml(state.academics.current.score.outOf)}` : ""}</p>`);

    variant.sectionOrder.forEach((section) => {
      const label = (Core.SECTION_OPTIONS.find((item) => item.value === section) || { label: section }).label;
      if (section === "skill" && variant.skillsHtml) previewSections.push(`<h2>${escapeHtml(label)}</h2>${Core.formatPortalBlockHtml(variant.skillsHtml)}`);
      else if (section === "coursework" && state.shared.courseworkHtml) previewSections.push(`<h2>${escapeHtml(label)}</h2>${Core.formatPortalBlockHtml(state.shared.courseworkHtml)}`);
      else if (section === "eaa" && variant.extracurricularHtml) previewSections.push(`<h2>${escapeHtml(label)}</h2>${Core.formatPortalBlockHtml(variant.extracurricularHtml)}`);
      else if (!["skill", "coursework", "eaa"].includes(section)) {
        const entries = sectionEntries(section, variant.id);
        if (entries.length) previewSections.push(`<h2>${escapeHtml(label)}</h2>${entries.map(previewEntry).join("")}`);
      }
    });

    const tabs = state.variants.map((item) => `<button data-preview-variant="${item.id}" class="${item.id === variant.id ? "active" : ""}">${escapeHtml(item.label)}</button>`).join("");
    const warning = variant.sectionOrder.length ? "" : '<div class="warning">This variant has no section order. Add sections under CV variants before transferring it to the portal.</div>';
    workspace.innerHTML = `${viewHeader("Content preview", "This verifies filtering, formatting, and section order. Final pagination remains the portal’s responsibility.", `<div class="preview-toolbar">${tabs}</div>`)}${warning}<div class="resume-preview">${previewSections.join("")}</div>`;
    workspace.querySelectorAll("[data-preview-variant]").forEach((button) => button.addEventListener("click", () => {
      previewVariantId = button.dataset.previewVariant;
      renderPreview();
    }));
  }

  function setupServerMode() {
    const panel = document.getElementById("server-panel");
    panel.hidden = false;
    document.getElementById("pdf-column").hidden = false;
    document.getElementById("open-pdf-viewer").addEventListener("click", openPDFViewer);
    document.getElementById("open-erp").addEventListener("click", openERPFromEditor);
    document.getElementById("run-erp").addEventListener("click", runERPFromEditor);
    document.getElementById("forget-login").addEventListener("click", forgetLogin);
    document.getElementById("quit-app").addEventListener("click", quitApp);
    connectERPEvents();
    refreshPDF();
    setInterval(refreshPDF, 1200);
    refreshSetupStatus();
    setupStatusTimer = setInterval(refreshSetupStatus, 1000);
  }

  async function forgetLogin() {
    if (!window.confirm("Forget the ERP credentials and session? Your local resume will remain.")) return;
    try { await apiFetch("/api/setup/credentials", { method: "DELETE" }); showSetup(true); showToast("ERP login forgotten. Resume content is preserved."); }
    catch (error) { showDialog("Could not forget ERP login", `<p>${escapeHtml(error.message || error)}</p>`); }
  }

  async function quitApp() {
    try { await apiFetch("/api/app/shutdown", { method: "POST" }); showToast("CV++ is shutting down."); }
    catch (error) { showDialog("Could not quit CV++", `<p>${escapeHtml(error.message || error)}</p>`); }
  }

  async function refreshPDF() {
    if (!serverMode) return;
    try {
      const response = await apiFetch("/api/pdf/status?cv=1");
      const payload = await response.json();
      const state = document.getElementById("pdf-state");
      if (!payload.exists) { state.textContent = "No PDF downloaded yet"; return; }
      state.textContent = `Updated ${new Date(payload.modTime).toLocaleTimeString()}`;
      if (payload.signature && payload.signature !== pdfSignature) {
        pdfSignature = payload.signature;
        document.getElementById("pdf-frame").src = `/pdf/file/cv1?v=${encodeURIComponent(pdfSignature)}`;
      }
    } catch (_) { document.getElementById("pdf-state").textContent = "PDF unavailable"; }
  }

  function showSetup(show) {
    const overlay = document.getElementById("setup-overlay");
    overlay.hidden = !show;
    document.querySelector(".app-shell").classList.toggle("setup-blocked", show);
  }

  function setSetupError(message) {
    const target = document.getElementById("setup-error");
    target.textContent = message || "";
    target.hidden = !message;
  }

  async function fetchSetupQuestion() {
    setSetupError("");
    const roll = document.getElementById("setup-roll").value.trim();
    if (!roll) { setSetupError("Enter your roll number first."); return; }
    const button = document.getElementById("fetch-question");
    button.disabled = true;
    try {
      const response = await apiFetch("/api/setup/security-question", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ rollNumber: roll }) });
      const payload = await response.json();
      document.getElementById("setup-question").value = payload.question || "";
    } catch (error) {
      document.getElementById("setup-question").readOnly = false;
      document.getElementById("setup-manual-question").checked = true;
      setSetupError(error.message || "ERP could not provide the question. Enter it manually.");
    }
    button.disabled = false;
  }

  async function submitSetup(event) {
    event.preventDefault();
    setSetupError("");
    const roll = document.getElementById("setup-roll").value.trim();
    const password = document.getElementById("setup-password").value;
    const question = document.getElementById("setup-question").value.trim();
    const answer = document.getElementById("setup-answer").value;
    if (!roll || !password || !question || !answer) { setSetupError("Complete all ERP login fields."); return; }
    setSetupWaiting(true);
    try {
      await apiFetch("/api/setup/credentials", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ roll_number: roll, password, answers: { [question]: answer } }) });
      const response = await apiFetch("/api/setup/import", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ freshLogin: true }) });
      const payload = await response.json();
      setupJobID = Number(payload.jobID) || 0;
      showToast("Connecting to ERP…");
    } catch (error) {
      setSetupWaiting(false);
      setSetupError(error.message || "Could not start ERP import.");
    }
  }

  async function submitOTP(event) {
    event.preventDefault();
    setSetupError("");
    const input = document.getElementById("setup-otp");
    const button = document.querySelector("#otp-form button[type=\"submit\"]");
    input.disabled = true;
    button.disabled = true;
    try {
      await apiFetch("/api/erp/otp", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ otp: input.value.trim() }) });
      document.getElementById("otp-panel").hidden = true;
      showToast("OTP accepted. Finishing import…");
    } catch (error) {
      input.disabled = false;
      button.disabled = false;
      setSetupError(error.message || "That OTP was not accepted.");
    }
  }

  function setSetupWaiting(waiting) {
    const form = document.getElementById("setup-form");
    form.classList.toggle("is-waiting", waiting);
    form.querySelectorAll("input, button").forEach((element) => {
      if (element.id !== "restore-backup") element.disabled = waiting;
    });
    if (!waiting) {
      document.getElementById("setup-otp").disabled = false;
      document.querySelector("#otp-form button[type=\"submit\"]").disabled = false;
    }
  }

  async function finishSetupJob(job) {
    const jobID = Number(job.id || job.jobID) || 0;
    if (jobID && handledSetupJobID === jobID) return;
    if (jobID) handledSetupJobID = jobID;
    setupJobID = 0;
    setSetupWaiting(false);
    document.getElementById("otp-panel").hidden = true;
    if (!job.ok) {
      const message = job.error || "ERP import failed. Your local resume was not changed.";
      showSetup(true);
      setSetupError(message);
      if (message.toLowerCase().includes("security question")) {
        document.getElementById("setup-question").readOnly = false;
        document.getElementById("setup-manual-question").checked = true;
      }
      return;
    }
    setSetupError("");
    showSetup(false);
    document.getElementById("setup-password").value = "";
    document.getElementById("setup-answer").value = "";
    document.getElementById("setup-otp").value = "";
    await loadServerResume();
    refreshPDF();
    showToast(job.message || "ERP resume imported without changing the portal.");
  }

  async function refreshSetupStatus() {
    if (!serverMode) return;
    if (setupStatusRequest) return setupStatusRequest;
    setupStatusRequest = (async () => {
      try {
        const response = await apiFetch("/api/app/status");
        const status = await response.json();
        const job = status.job || {};
        const jobID = Number(job.id) || 0;
        if (!setupJobID && job.kind === "import" && jobID !== handledSetupJobID && (job.running || (job.completed && status.onboarding))) {
          setupJobID = jobID;
        }
        const isSetupJob = setupJobID && jobID === setupJobID && job.kind === "import";
        if (isSetupJob && job.completed) {
          await finishSetupJob(job);
          return;
        }
        const waitingForStart = document.getElementById("setup-form").classList.contains("is-waiting") && !setupJobID;
        const shouldShow = Boolean(status.onboarding || status.otpRequired || waitingForStart || (isSetupJob && job.running));
        showSetup(shouldShow);
        document.getElementById("otp-panel").hidden = !status.otpRequired;
        if (isSetupJob && job.running) setSetupWaiting(true);
      } catch (_) {
        // The event stream and the next poll can recover from a transient request failure.
      } finally {
        setupStatusRequest = null;
      }
    })();
    return setupStatusRequest;
  }

  function connectERPEvents() {
    if (!serverMode || erpEventSource) return;
    erpEventSource = new EventSource(apiURL("/api/erp/events"));
    erpEventSource.addEventListener("open", () => {
      erpStreamWarned = false;
    });
    erpEventSource.addEventListener("status", (event) => {
      const payload = JSON.parse(event.data || "{}");
      setERPRunning(Boolean(payload.running));
    });
    erpEventSource.addEventListener("log", (event) => {
      const payload = JSON.parse(event.data || "{}");
      if (payload.message) appendERPLog(payload.message);
    });
    erpEventSource.addEventListener("done", (event) => {
      const payload = JSON.parse(event.data || "{}");
      setERPRunning(false);
      const eventJobID = Number(payload.jobID) || 0;
      if (eventJobID && eventJobID === handledSetupJobID) return;
      if (setupJobID && eventJobID === setupJobID) {
        finishSetupJob(payload);
        return;
      }
      if (payload.ok) {
        if (payload.message) appendERPLog(payload.message);
        showToast(payload.message || "ERP PDF saved.");
        showSetup(false);
        document.getElementById("otp-panel").hidden = true;
        loadServerResume();
      } else {
        if (payload.error) appendERPLog(`error: ${payload.error}`);
        if (payload.error && payload.error.toLowerCase().includes("security question")) {
          showSetup(true);
          document.getElementById("setup-question").readOnly = false;
          document.getElementById("setup-manual-question").checked = true;
        }
        showDialog("ERP run failed", `<p>${escapeHtml(payload.error || "Unknown ERP error")}</p>`);
      }
    });
    erpEventSource.addEventListener("phase", (event) => {
      const payload = JSON.parse(event.data || "{}");
      if (payload.phase === "otp-required") document.getElementById("otp-panel").hidden = false;
      appendERPLog(payload.phase || "");
    });
    erpEventSource.addEventListener("error", () => {
      if (erpStreamWarned) return;
      erpStreamWarned = true;
      appendERPLog("Editor event stream disconnected. Reload the editor if ERP logs stop updating.");
    });
  }

  function appendERPLog(message) {
    const log = document.getElementById("erp-log");
    log.hidden = false;
    log.textContent += `${log.textContent ? "\n" : ""}${message}`;
    log.scrollTop = log.scrollHeight;
  }

  function clearERPLog() {
    const log = document.getElementById("erp-log");
    log.textContent = "";
    log.hidden = true;
  }

  function setERPRunning(running) {
    erpRunning = running;
    const runButton = document.getElementById("run-erp");
    const openButton = document.getElementById("open-erp");
    runButton.disabled = running;
    openButton.disabled = running;
    runButton.textContent = running ? "Updating ERP…" : "Update ERP Resume";
    openButton.textContent = running ? "Opening ERP…" : "Open ERP";
  }

  function openPDFViewer() {
    const cv = Number(document.getElementById("erp-cv").value) || 1;
    const viewer = window.open(apiURL(`/pdf/cv${cv}`), "_blank");
    if (viewer) viewer.opener = null;
    else showDialog("Could not open PDF viewer", "<p>The browser blocked the new tab. Allow pop-ups for this local editor and try again.</p>");
  }

  async function openERPFromEditor() {
    if (erpRunning) return;
    clearERPLog();
    setERPRunning(true);
    try {
      await apiFetch("/api/erp/open", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          freshLogin: document.getElementById("erp-fresh-login").checked
        })
      });
    } catch (error) {
      setERPRunning(false);
      showDialog("Could not open ERP", `<p>${escapeHtml(error.message || error)}</p>`);
    }
  }

  async function runERPFromEditor() {
    if (erpRunning) return;
    if (!hasDocument) {
      showDialog("No resume loaded", "<p>Load data/resume.json before running ERP.</p>");
      return;
    }
    const saved = await saveJson();
    if (!saved) return;
    clearERPLog();
    setERPRunning(true);
    try {
      await apiFetch("/api/erp/run", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          cv: Number(document.getElementById("erp-cv").value),
          freshLogin: document.getElementById("erp-fresh-login").checked
        })
      });
    } catch (error) {
      setERPRunning(false);
      showDialog("Could not start ERP", `<p>${escapeHtml(error.message || error)}</p>`);
    }
  }

  function updateNavigation() {
    document.querySelectorAll(".nav-item").forEach((button) => button.classList.toggle("active", button.dataset.view === currentView));
  }

  document.querySelectorAll(".nav-item").forEach((button) => button.addEventListener("click", () => {
    flushVisibleEditors(true);
    currentView = button.dataset.view;
    updateNavigation();
    render();
    workspace.focus();
  }));
  document.getElementById("open-json").addEventListener("click", openJson);
  document.getElementById("save-json").addEventListener("click", () => { saveJson(); });
  document.getElementById("download-json").addEventListener("click", () => downloadJson(fileName));
  document.getElementById("import-snapshot").addEventListener("click", () => { snapshotInput.value = ""; snapshotInput.click(); });
  jsonInput.addEventListener("change", () => { if (jsonInput.files[0]) loadJsonFile(jsonInput.files[0]); });
  snapshotInput.addEventListener("change", () => { if (snapshotInput.files[0]) importSnapshot(snapshotInput.files[0]); });
  document.getElementById("fetch-question").addEventListener("click", fetchSetupQuestion);
  document.getElementById("setup-manual-question").addEventListener("change", (event) => { document.getElementById("setup-question").readOnly = !event.target.checked; });
  document.getElementById("setup-form").addEventListener("submit", submitSetup);
  document.getElementById("otp-form").addEventListener("submit", submitOTP);
  document.getElementById("restore-backup").addEventListener("click", () => { showSetup(false); openJson(); });

  window.addEventListener("beforeunload", (event) => {
    if (!dirty) return;
    event.preventDefault();
    event.returnValue = "";
  });

  async function initialize() {
    if (serverMode) {
      setupServerMode();
      updateChrome();
      render();
      await loadServerResume();
      await refreshSetupStatus();
      return;
    }
    try {
      const draft = localStorage.getItem(Core.DRAFT_KEY);
      if (draft) {
        const recovered = JSON.parse(draft);
        if (setDocument(recovered, "recovered-draft.json", null)) {
          dirty = true;
          updateChrome();
          showToast("Recovered an unsaved local draft.");
        }
      }
    } catch (_) {
      render();
    }
    updateChrome();
    render();
  }

  initialize();
})();

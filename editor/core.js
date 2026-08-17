(function (global) {
  "use strict";

  const ENTRY_TYPES = [
    "Internship",
    "Project",
    "Internship/Project",
    "Academic Achievement",
    "Certification",
    "Training",
    "Experience",
    "Entrepreneurial",
    "Competition/Conference",
    "Publication",
    "Position of Responsibilities"
  ];

  const SECTION_OPTIONS = [
    ["Internship", "Internships"],
    ["Project", "Projects"],
    ["Internship/Project", "Internships and Projects"],
    ["Academic Achievement", "Academic Achievement"],
    ["Certification", "Certification"],
    ["Training", "Training"],
    ["Experience", "Experience"],
    ["Entrepreneurial", "Entrepreneurial Experience"],
    ["Competition/Conference", "Competition/Conference"],
    ["Publication", "Publication"],
    ["Position of Responsibilities", "Position of Responsibilities"],
    ["eaa", "Extra-Curricular Activities"],
    ["skill", "Skills and Expertise"],
    ["coursework", "Coursework Information"]
  ].map(([value, label]) => ({ value, label }));

  const SECTION_VALUES = SECTION_OPTIONS.map((option) => option.value);
  const VARIANT_IDS = ["cv1", "cv2", "cv3"];
  const DRAFT_KEY = "erp-cv-portal-draft-v1";
  const PORTAL_FONT_SIZE = 9;
  const MAX_GAP_PIXELS = 24;

  function clone(value) {
    return JSON.parse(JSON.stringify(value));
  }

  function emptyResume() {
    return {
      schemaVersion: 1,
      metadata: {
        name: "",
        documentId: "resume",
        updatedAt: new Date().toISOString().slice(0, 10)
      },
      academics: {
        previous: [],
        current: {
          slot: 6,
          standard: "",
          qualification: "",
          institution: "",
          completionYear: "",
          score: { kind: "cgpa", value: "", outOf: "" },
          specialization: ""
        }
      },
      shared: { courseworkHtml: "" },
      variants: VARIANT_IDS.map((id, index) => ({
        id,
        label: `CV${index + 1}`,
        sectionOrder: [],
        skillsHtml: "",
        extracurricularHtml: "",
        showMinor: false,
        showMicro: false
      })),
      entries: []
    };
  }

  function text(value) {
    return value == null ? "" : String(value);
  }

  function gapPixels(value) {
    const number = Number(value);
    if (!Number.isInteger(number) || number <= 0) return 0;
    return Math.min(number, MAX_GAP_PIXELS);
  }

  function normalizeGap(value) {
    const number = Number(value);
    if (!Number.isInteger(number) || number <= 0) return 0;
    return number;
  }

  function validGap(value) {
    return Number.isInteger(value) && value >= 0 && value <= MAX_GAP_PIXELS;
  }

  function escapeHtml(value) {
    return text(value)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  function normalizeScore(score) {
    const source = score && typeof score === "object" ? score : {};
    return {
      kind: source.kind === "percentage" ? "percentage" : "cgpa",
      value: text(source.value),
      outOf: text(source.outOf)
    };
  }

  function normalizeResume(input) {
    const blank = emptyResume();
    const source = input && typeof input === "object" ? clone(input) : {};
    const variantsById = new Map(
      (Array.isArray(source.variants) ? source.variants : []).map((variant) => [variant.id, variant])
    );

    const previous = Array.isArray(source.academics && source.academics.previous)
      ? source.academics.previous
      : [];
    const current = source.academics && source.academics.current
      ? source.academics.current
      : blank.academics.current;

    return {
      schemaVersion: Number(source.schemaVersion || 1),
      metadata: {
        name: text(source.metadata && source.metadata.name),
        documentId: text(source.metadata && source.metadata.documentId) || "resume",
        updatedAt: text(source.metadata && source.metadata.updatedAt) || new Date().toISOString().slice(0, 10)
      },
      academics: {
        previous: previous.map((academic, index) => ({
          slot: Number(academic.slot || index + 1),
          standard: text(academic.standard),
          qualification: text(academic.qualification),
          institution: text(academic.institution),
          completionYear: text(academic.completionYear),
          score: normalizeScore(academic.score)
        })),
        current: {
          slot: 6,
          standard: text(current.standard),
          qualification: text(current.qualification),
          institution: text(current.institution),
          completionYear: text(current.completionYear),
          score: normalizeScore(current.score),
          specialization: text(current.specialization)
        }
      },
      shared: {
        courseworkHtml: text(source.shared && source.shared.courseworkHtml)
      },
      variants: VARIANT_IDS.map((id, index) => {
        const variant = variantsById.get(id) || blank.variants[index];
        return {
          id,
          label: text(variant.label) || `CV${index + 1}`,
          sectionOrder: Array.isArray(variant.sectionOrder)
            ? variant.sectionOrder.map(text).filter(Boolean)
            : [],
          skillsHtml: text(variant.skillsHtml),
          extracurricularHtml: text(variant.extracurricularHtml),
          showMinor: Boolean(variant.showMinor),
          showMicro: Boolean(variant.showMicro)
        };
      }),
      entries: (Array.isArray(source.entries) ? source.entries : []).map((entry, index) => {
        const normalized = {
          id: text(entry.id) || `entry-${index + 1}`,
          type: text(entry.type),
          overview: text(entry.overview),
          details: (Array.isArray(entry.details) ? entry.details : []).map((block) => {
            const detail = {
              kind: block && block.kind === "paragraph" ? "paragraph" : "bullet",
              html: text(block && block.html)
            };
            if (block && block.hidden === true) detail.hidden = true;
            const gapBefore = normalizeGap(block && block.gapBefore);
            const gapAfter = normalizeGap(block && block.gapAfter);
            if (gapBefore) detail.gapBefore = gapBefore;
            if (gapAfter) detail.gapAfter = gapAfter;
            return detail;
          }),
          includeIn: (Array.isArray(entry.includeIn) ? entry.includeIn : [])
            .map(text)
            .filter((id) => VARIANT_IDS.includes(id))
        };
        if (entry && entry.hidden === true) normalized.hidden = true;
        return normalized;
      })
    };
  }

  function validateResume(input) {
    const data = normalizeResume(input);
    const errors = [];

    if (data.schemaVersion !== 1) {
      errors.push(`Unsupported schemaVersion ${data.schemaVersion}; expected 1.`);
    }
    if (data.entries.length > 50) {
      errors.push(`The portal accepts at most 50 entries; found ${data.entries.length}.`);
    }

    const ids = new Set();
    data.entries.forEach((entry, index) => {
      const label = `Entry ${index + 1}`;
      if (!entry.id.trim()) errors.push(`${label} needs a stable ID.`);
      if (ids.has(entry.id)) errors.push(`${label} duplicates ID "${entry.id}".`);
      ids.add(entry.id);
      if (!ENTRY_TYPES.includes(entry.type)) {
        errors.push(`${label} has unsupported type "${entry.type}".`);
      }
      if (!entry.details.length) errors.push(`${label} needs at least one detail block.`);
      if (entry.details.some((block) => !block.html.trim())) {
        errors.push(`${label} contains an empty detail block.`);
      }
      if (entry.details.some((block) => !validGap(block.gapBefore || 0) || !validGap(block.gapAfter || 0))) {
        errors.push(`${label} has detail spacing outside 0-${MAX_GAP_PIXELS}px.`);
      }
    });

    data.variants.forEach((variant) => {
      if (variant.sectionOrder.length > 14) {
        errors.push(`${variant.label} has more than 14 section-order items.`);
      }
      const seen = new Set();
      variant.sectionOrder.forEach((section) => {
        if (!SECTION_VALUES.includes(section)) {
          errors.push(`${variant.label} has unsupported section "${section}".`);
        }
        if (seen.has(section)) {
          errors.push(`${variant.label} repeats section "${section}".`);
        }
        seen.add(section);
      });
    });

    data.academics.previous.forEach((academic) => {
      if (academic.slot < 1 || academic.slot > 5) {
        errors.push(`Previous academic slot ${academic.slot} must be between 1 and 5.`);
      }
      if (academic.score.kind === "cgpa" && academic.score.value && !academic.score.outOf) {
        errors.push(`Academic slot ${academic.slot} needs a maximum CGPA.`);
      }
    });

    return { valid: errors.length === 0, errors, data };
  }

  function safeUrl(url) {
    try {
      const parsed = new URL(url, global.location && global.location.href ? global.location.href : "https://example.invalid");
      return parsed.protocol === "http:" || parsed.protocol === "https:" ? parsed.href : "";
    } catch (_) {
      return "";
    }
  }

  function sanitizeHtml(html, options) {
    const settings = Object.assign({ block: false, forPortal: false }, options || {});
    const template = document.createElement("template");
    template.innerHTML = text(html);
    const output = document.createDocumentFragment();
    const inlineTags = new Set(["STRONG", "B", "EM", "I", "A", "BR", "SPAN"]);
    const blockTags = new Set(["P", "UL", "LI"]);

    function clean(node, parent) {
      if (node.nodeType === Node.TEXT_NODE) {
        const value = settings.forPortal
          ? portalCompatibleText(node.nodeValue || "")
          : node.nodeValue || "";
        parent.appendChild(document.createTextNode(value));
        return;
      }
      if (node.nodeType !== Node.ELEMENT_NODE) return;

      const tag = node.tagName;
      const isInline = inlineTags.has(tag);
      const isBlock = settings.block && blockTags.has(tag);
      if (!isInline && !isBlock) {
        Array.from(node.childNodes).forEach((child) => clean(child, parent));
        return;
      }

      if (tag === "A" && settings.forPortal) {
        Array.from(node.childNodes).forEach((child) => clean(child, parent));
        return;
      }

      let normalizedTag = tag.toLowerCase();
      if (normalizedTag === "b") normalizedTag = "strong";
      if (normalizedTag === "i") normalizedTag = "em";
      const element = document.createElement(normalizedTag);

      if (tag === "A") {
        const href = safeUrl(node.getAttribute("href") || "");
        if (!href) {
          Array.from(node.childNodes).forEach((child) => clean(child, parent));
          return;
        }
        element.setAttribute("href", href);
        element.setAttribute("target", "_blank");
        element.setAttribute("rel", "noopener noreferrer");
      }

      if (tag === "SPAN") {
        const match = (node.style.fontSize || "").match(/^(\d{1,2})px$/);
        const size = match ? Number(match[1]) : 0;
        if (size >= 8 && size <= 24) element.style.fontSize = `${size}px`;
      }

      if (isBlock && (tag === "P" || tag === "LI")) {
        ["before", "after"].forEach((position) => {
          const gap = gapPixels(node.getAttribute(`data-gap-${position}`));
          if (gap) element.setAttribute(`data-gap-${position}`, String(gap));
        });
      }

      Array.from(node.childNodes).forEach((child) => clean(child, element));
      parent.appendChild(element);
    }

    Array.from(template.content.childNodes).forEach((node) => clean(node, output));
    const container = document.createElement("div");
    container.appendChild(output);
    return container.innerHTML;
  }

  function sanitizeInlineHtml(html, forPortal) {
    return sanitizeHtml(html, { block: false, forPortal: Boolean(forPortal) });
  }

  function sanitizeBlockHtml(html, forPortal) {
    return sanitizeHtml(html, { block: true, forPortal: Boolean(forPortal) });
  }

  function portalCompatibleText(value) {
    return text(value)
      .replace(/[\u2010-\u2015\u2212]/g, "-")
      .replace(/\s*[\u2192\u279c\u279d\u27f6]\s*/g, " to ")
      .replace(/\u00d7/g, "x")
      .replace(/[\u2022\u25e6\u25aa]/g, "-");
  }

  function preservePortalTextSpacing(value) {
    let output = "";
    let previousSpace = false;
    let previousAmpersand = false;
    Array.from(text(value)).forEach((character) => {
      if (character === "\u00a0") {
        output += "\u00a0";
        previousSpace = true;
        previousAmpersand = false;
        return;
      }
      if (/[\t\n\f\r ]/.test(character)) {
        output += previousAmpersand || previousSpace ? "\u00a0" : " ";
        previousSpace = true;
        previousAmpersand = false;
        return;
      }
      output += character;
      previousSpace = false;
      previousAmpersand = character === "&";
    });
    return output;
  }

  function preservePortalSpacing(root) {
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    const nodes = [];
    while (walker.nextNode()) nodes.push(walker.currentNode);
    nodes.forEach((node) => {
      node.nodeValue = preservePortalTextSpacing(node.nodeValue || "");
    });
  }

  function wrapContents(element) {
    const span = document.createElement("span");
    span.style.fontSize = `${PORTAL_FONT_SIZE}px`;
    while (element.firstChild) span.appendChild(element.firstChild);
    element.appendChild(span);
  }

  function addPortalBulletSpacing(element) {
    if (!element) return;
    const spacer = document.createTextNode("\u00a0");
    element.insertBefore(spacer, element.firstChild);
  }

  function removeLeadingBulletSpacing(element) {
    let firstText = null;
    const findFirstText = (current) => {
      Array.from(current.childNodes || []).some((child) => {
        if (child.nodeType === Node.TEXT_NODE) {
          const value = child.nodeValue || "";
          if (value.includes("\u00a0") || value.trim()) {
            firstText = child;
            return true;
          }
          return false;
        }
        if (child.nodeType === Node.ELEMENT_NODE) {
          findFirstText(child);
          return Boolean(firstText);
        }
        return false;
      });
    };
    findFirstText(element);
    if (firstText) {
      firstText.nodeValue = (firstText.nodeValue || "").replace(/^[\u00a0 ]/, "");
    }
  }

  function applyPortalTypography(html, block) {
    const container = document.createElement("div");
    container.innerHTML = block
      ? sanitizeBlockHtml(html, true)
      : sanitizeInlineHtml(html, true);

    container.querySelectorAll("span, a").forEach((element) => {
      element.replaceWith(...Array.from(element.childNodes));
    });
    container.normalize();
    preservePortalSpacing(container);

    if (block) {
      container.querySelectorAll("p, li").forEach((element) => {
        if (element.tagName === "LI") removeLeadingBulletSpacing(element);
        wrapContents(element);
        if (element.tagName === "LI") addPortalBulletSpacing(element.firstChild);
      });
      Array.from(container.childNodes).forEach((node) => {
        const isBlock = node.nodeType === Node.ELEMENT_NODE && ["P", "UL"].includes(node.tagName);
        if (isBlock) return;
        const span = document.createElement("span");
        span.style.fontSize = `${PORTAL_FONT_SIZE}px`;
        node.replaceWith(span);
        span.appendChild(node);
      });
    } else {
      wrapContents(container);
    }
    return container.innerHTML;
  }

  function formatPortalInlineHtml(html) {
    return applyPortalTypography(html, false);
  }

  function formatPortalBulletHtml(html) {
    const container = document.createElement("div");
    container.innerHTML = formatPortalInlineHtml(html);
    addPortalBulletSpacing(container.firstElementChild);
    return container.innerHTML;
  }

  function portalSpacerHtml(pixels) {
    const gap = gapPixels(pixels);
    return gap ? `<p><span style="font-size: ${gap}px; line-height: ${gap}px;">&nbsp;</span></p>` : "";
  }

  function elementGap(element, position) {
    return element && element.getAttribute ? gapPixels(element.getAttribute(`data-gap-${position}`)) : 0;
  }

  function formatPortalParagraphFromNode(node) {
    const copy = node.cloneNode(true);
    copy.removeAttribute("data-gap-before");
    copy.removeAttribute("data-gap-after");
    return `<p>${formatPortalInlineHtml(copy.innerHTML)}</p>`;
  }

  function formatPortalBulletFromNode(node) {
    const copy = node.cloneNode(true);
    copy.removeAttribute("data-gap-before");
    copy.removeAttribute("data-gap-after");
    removeLeadingBulletSpacing(copy);
    return `<li>${formatPortalBulletHtml(copy.innerHTML)}</li>`;
  }

  function formatPortalBlockHtml(html) {
    const container = document.createElement("div");
    container.innerHTML = sanitizeBlockHtml(html, true);
    let output = "";
    let inList = false;

    const closeList = () => {
      if (inList) {
        output += "</ul>";
        inList = false;
      }
    };
    const writeGap = (pixels) => {
      const spacer = portalSpacerHtml(pixels);
      if (!spacer) return;
      closeList();
      output += spacer;
    };
    const writeParagraph = (node) => {
      writeGap(elementGap(node, "before"));
      closeList();
      output += formatPortalParagraphFromNode(node);
      writeGap(elementGap(node, "after"));
    };
    const writeBullet = (node) => {
      writeGap(elementGap(node, "before"));
      if (!inList) {
        output += "<ul>";
        inList = true;
      }
      output += formatPortalBulletFromNode(node);
      writeGap(elementGap(node, "after"));
    };

    Array.from(container.childNodes).forEach((node) => {
      if (node.nodeType === Node.TEXT_NODE) {
        if (!node.nodeValue.trim()) return;
        const paragraph = document.createElement("p");
        paragraph.textContent = node.nodeValue;
        writeParagraph(paragraph);
        return;
      }
      if (node.nodeType !== Node.ELEMENT_NODE) return;
      if (node.tagName === "UL") {
        Array.from(node.children).forEach((item) => {
          if (item.tagName === "LI") writeBullet(item);
        });
        return;
      }
      if (node.tagName === "P") {
        writeParagraph(node);
        return;
      }
      const paragraph = document.createElement("p");
      paragraph.innerHTML = node.innerHTML;
      writeParagraph(paragraph);
    });
    closeList();
    return output;
  }

  function blocksToPortalHtml(blocks) {
    let html = "";
    let inList = false;
    const closeList = () => {
      if (inList) {
        html += "</ul>";
        inList = false;
      }
    };
    (blocks || []).forEach((block) => {
      if (block && block.hidden === true) return;
      if (block.gapBefore) {
        closeList();
        html += portalSpacerHtml(block.gapBefore);
      }
      if (block.kind === "paragraph") {
        closeList();
        html += `<p>${formatPortalInlineHtml(block.html)}</p>`;
      } else {
        if (!inList) {
          html += "<ul>";
          inList = true;
        }
        html += `<li>${formatPortalBulletHtml(block.html)}</li>`;
      }
      if (block.gapAfter) {
        closeList();
        html += portalSpacerHtml(block.gapAfter);
      }
    });
    closeList();
    return html;
  }

  function entrySubjectBlocks(entry) {
    const overview = text(entry && entry.overview).trim();
    const details = Array.isArray(entry && entry.details) ? entry.details : [];
    if (!overview) return details;
    return [
      { kind: "paragraph", html: `<strong>${escapeHtml(overview)}</strong>` },
      ...details
    ];
  }

  function entrySubjectHtml(entry) {
    return blocksToPortalHtml(entrySubjectBlocks(entry));
  }

  function portalHtmlToBlocks(html) {
    const template = document.createElement("template");
    template.innerHTML = sanitizeBlockHtml(html, false);
    const blocks = [];

    const withGaps = (element, block) => {
      const gapBefore = elementGap(element, "before");
      const gapAfter = elementGap(element, "after");
      if (gapBefore) block.gapBefore = gapBefore;
      if (gapAfter) block.gapAfter = gapAfter;
      return block;
    };

    Array.from(template.content.childNodes).forEach((node) => {
      if (node.nodeType === Node.TEXT_NODE && node.nodeValue.trim()) {
        blocks.push({ kind: "paragraph", html: sanitizeInlineHtml(node.nodeValue, false) });
      } else if (node.nodeType === Node.ELEMENT_NODE && node.tagName === "UL") {
        Array.from(node.children).forEach((item) => {
          if (item.tagName === "LI") {
            const copy = item.cloneNode(true);
            removeLeadingBulletSpacing(copy);
            blocks.push(withGaps(item, { kind: "bullet", html: sanitizeInlineHtml(copy.innerHTML, false) }));
          }
        });
      } else if (node.nodeType === Node.ELEMENT_NODE && node.tagName === "P") {
        const copy = node.cloneNode(true);
        let firstText = null;
        const findFirstText = (current) => {
          Array.from(current.childNodes || []).some((child) => {
            if (child.nodeType === Node.TEXT_NODE && child.nodeValue.trim()) {
              firstText = child;
              return true;
            }
            if (child.nodeType === Node.ELEMENT_NODE) {
              findFirstText(child);
              return Boolean(firstText);
            }
            return false;
          });
        };
        findFirstText(copy);
        const isCompatBullet = firstText && /^(?:\s*[-*]\s+|\s*\u2022\s+)/.test(firstText.nodeValue || "");
        if (isCompatBullet) firstText.nodeValue = (firstText.nodeValue || "").replace(/^(?:\s*[-*]\s+|\s*\u2022\s+)/, "");
        blocks.push(withGaps(node, {
          kind: isCompatBullet ? "bullet" : "paragraph",
          html: sanitizeInlineHtml(copy.innerHTML, false)
        }));
      } else if (node.nodeType === Node.ELEMENT_NODE) {
        blocks.push({ kind: "paragraph", html: sanitizeInlineHtml(node.innerHTML, false) });
      }
    });
    return blocks;
  }

  function createPortalFieldMap(input) {
    const result = validateResume(input);
    if (!result.valid) throw new Error(result.errors.join("\n"));
    const data = result.data;
    const fields = {};

    for (let slot = 1; slot <= 5; slot += 1) {
      const academic = data.academics.previous.find((item) => item.slot === slot);
      fields[`standard${slot}`] = academic ? academic.standard : "";
      fields[`qualification${slot}`] = academic ? academic.qualification : "";
      fields[`university${slot}`] = academic ? academic.institution : "";
      fields[`year${slot}`] = academic ? academic.completionYear : "";
      fields[`percgpa${slot}`] = academic && academic.score.kind === "cgpa" ? "cgparadio" : "perradio";
      fields[`percentage${slot}`] = academic && academic.score.kind === "percentage" ? academic.score.value : "";
      fields[`cgpa${slot}`] = academic && academic.score.kind === "cgpa" ? academic.score.value : "";
      fields[`outof${slot}`] = academic && academic.score.kind === "cgpa" ? academic.score.outOf : "";
    }

    const current = data.academics.current;
    fields.year6 = current.completionYear;
    fields.percgpa6 = current.score.kind === "percentage" ? "perradio" : "cgparadio";
    fields.percentage6 = current.score.kind === "percentage" ? current.score.value : "";
    fields.cgpa6 = current.score.kind === "cgpa" ? current.score.value : "";
    fields.outof6 = current.score.kind === "cgpa" ? current.score.outOf : "";

    for (let slot = 7; slot <= 56; slot += 1) {
      const entry = data.entries[slot - 7];
      fields[`standard${slot}`] = entry ? entry.type : "";
      fields[`university${slot}`] = "";
      fields[`subject${slot}`] = entry ? entrySubjectHtml(entry) : "";
      VARIANT_IDS.forEach((variantId, index) => {
        fields[`${slot}resume${index + 1}`] = entry && entry.hidden !== true && entry.includeIn.includes(variantId) ? "Y" : "N";
      });
    }

    fields.research_area = formatPortalBlockHtml(data.shared.courseworkHtml);
    data.variants.forEach((variant, index) => {
      const number = index + 1;
      fields[`showminor${number}`] = variant.showMinor ? "Y" : "N";
      fields[`showmicro${number}`] = variant.showMicro ? "Y" : "N";
      fields[["skill", "skill2", "skill3"][index]] = formatPortalBlockHtml(variant.skillsHtml);
      fields[["eaa", "objective", "gymkhana"][index]] = formatPortalBlockHtml(variant.extracurricularHtml);
      for (let position = 1; position <= 14; position += 1) {
        fields[`cv${number}_pref${position}`] = variant.sectionOrder[position - 1] || "";
      }
    });

    return fields;
  }

  function importPortalSnapshot(html) {
    const documentNode = new DOMParser().parseFromString(text(html), "text/html");
    const get = (name) => {
      const element = documentNode.querySelector(`[name="${CSS.escape(name)}"]`);
      if (!element) return "";
      if (element.type === "radio") {
        const checked = documentNode.querySelector(`[name="${CSS.escape(name)}"]:checked`);
        return checked ? checked.value : "";
      }
      return element.value || "";
    };

    const data = emptyResume();
    data.metadata.name = get("full_name");
    data.metadata.documentId = get("rollno") ? `resume-${get("rollno")}` : "resume";

    for (let slot = 1; slot <= 5; slot += 1) {
      if (![get(`standard${slot}`), get(`qualification${slot}`), get(`university${slot}`)].some(Boolean)) continue;
      const kind = get(`percgpa${slot}`) === "cgparadio" ? "cgpa" : "percentage";
      data.academics.previous.push({
        slot,
        standard: get(`standard${slot}`),
        qualification: get(`qualification${slot}`),
        institution: get(`university${slot}`),
        completionYear: get(`year${slot}`),
        score: {
          kind,
          value: kind === "cgpa" ? get(`cgpa${slot}`) : get(`percentage${slot}`),
          outOf: kind === "cgpa" ? get(`outof${slot}`) : ""
        }
      });
    }

    const currentKind = get("percgpa6") === "perradio" ? "percentage" : "cgpa";
    data.academics.current = {
      slot: 6,
      standard: get("standard6"),
      qualification: get("qualification6"),
      institution: get("university6"),
      completionYear: get("year6"),
      score: {
        kind: currentKind,
        value: currentKind === "cgpa" ? get("cgpa6") : get("percentage6"),
        outOf: currentKind === "cgpa" ? get("outof6") : ""
      },
      specialization: get("subject6")
    };

    data.shared.courseworkHtml = sanitizeBlockHtml(get("research_area"), false);
    data.variants.forEach((variant, index) => {
      const number = index + 1;
      variant.sectionOrder = [];
      for (let position = 1; position <= 14; position += 1) {
        const section = get(`cv${number}_pref${position}`);
        if (section) variant.sectionOrder.push(section);
      }
      variant.skillsHtml = sanitizeBlockHtml(get(["skill", "skill2", "skill3"][index]), false);
      variant.extracurricularHtml = sanitizeBlockHtml(get(["eaa", "objective", "gymkhana"][index]), false);
      variant.showMinor = get(`showminor${number}`) === "Y";
      variant.showMicro = get(`showmicro${number}`) === "Y";
    });

    for (let slot = 7; slot <= 56; slot += 1) {
      const type = get(`standard${slot}`);
      const overview = get(`university${slot}`);
      const subject = get(`subject${slot}`);
      if (!type && !overview && !subject) continue;
      data.entries.push({
        id: `portal-entry-${slot}`,
        type,
        overview,
        details: portalHtmlToBlocks(subject),
        includeIn: VARIANT_IDS.filter((_, index) => get(`${slot}resume${index + 1}`) === "Y")
      });
    }

    return normalizeResume(data);
  }

  global.ResumeCore = {
    ENTRY_TYPES,
    SECTION_OPTIONS,
    SECTION_VALUES,
    VARIANT_IDS,
    DRAFT_KEY,
    MAX_GAP_PIXELS,
    clone,
    emptyResume,
    normalizeResume,
    validateResume,
    sanitizeInlineHtml,
    sanitizeBlockHtml,
    portalCompatibleText,
    formatPortalInlineHtml,
    formatPortalBlockHtml,
    blocksToPortalHtml,
    entrySubjectBlocks,
    entrySubjectHtml,
    portalHtmlToBlocks,
    createPortalFieldMap,
    importPortalSnapshot
  };
})(window);

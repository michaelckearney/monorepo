// The page as an agent reads it: its accessibility tree, one element a line,
// as Playwright MCP's browser_snapshot gives it. Each element has a ref, such
// as e12, that the agent's next action names it by. An element keeps its ref
// for as long as it is on the page, so a later snapshot names it the same.
(limit) => {
  const refs = (window.__desktopRefs ??= { next: 1, of: new WeakMap(), to: new Map() });
  const refOf = (el) => {
    let ref = refs.of.get(el);
    if (!ref) {
      ref = "e" + refs.next++;
      refs.of.set(el, ref);
    }
    refs.to.set(ref, new WeakRef(el));
    return ref;
  };
  refs.to.clear();

  const skip = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "HEAD", "META", "LINK", "SVG"]);
  const inputRoles = {
    button: "button", submit: "button", reset: "button", image: "button",
    checkbox: "checkbox", radio: "radio", range: "slider", number: "spinbutton",
    search: "searchbox", email: "textbox", tel: "textbox", text: "textbox", url: "textbox", password: "textbox",
  };
  const tagRoles = {
    BUTTON: "button", TEXTAREA: "textbox", OPTION: "option", NAV: "navigation", MAIN: "main",
    ASIDE: "complementary", UL: "list", OL: "list", MENU: "list", LI: "listitem", TABLE: "table", TR: "row",
    TD: "cell", TH: "columnheader", DIALOG: "dialog", P: "paragraph", FIELDSET: "group", PROGRESS: "progressbar",
    HR: "separator", ARTICLE: "article", SUMMARY: "button", IFRAME: "iframe", DL: "list", DT: "term", DD: "definition",
    BLOCKQUOTE: "blockquote", CODE: "code", PRE: "code",
  };
  const role = (el) => {
    const explicit = el.getAttribute("role");
    if (explicit) return explicit.trim().split(/\s+/)[0];
    const tag = el.tagName;
    if (tag === "A") return el.hasAttribute("href") ? "link" : "";
    if (tag === "INPUT") return el.type === "hidden" ? "" : inputRoles[el.type] ?? "textbox";
    if (tag === "SELECT") return el.multiple || el.size > 1 ? "listbox" : "combobox";
    if (tag === "IMG") return el.getAttribute("alt") ? "img" : "";
    if (/^H[1-6]$/.test(tag)) return "heading";
    if (tag === "HEADER" && !el.closest("main, article, section, aside, nav")) return "banner";
    if (tag === "FOOTER" && !el.closest("main, article, section, aside, nav")) return "contentinfo";
    if ((tag === "FORM" || tag === "SECTION") && (el.getAttribute("aria-label") || el.getAttribute("aria-labelledby"))) {
      return tag === "FORM" ? "form" : "region";
    }
    if (el.isContentEditable && !el.parentElement?.isContentEditable) return "textbox";
    return tagRoles[tag] ?? "";
  };
  // Roles whose name is their text, when nothing else names them.
  const namedByText = new Set([
    "button", "link", "heading", "option", "cell", "columnheader", "rowheader", "tab", "menuitem", "menuitemcheckbox",
    "menuitemradio", "checkbox", "radio", "switch", "treeitem", "term", "tooltip",
  ]);
  const clean = (s) => (s ?? "").replace(/\s+/g, " ").trim();
  const cut = (s, n = 120) => (s.length > n ? s.slice(0, n - 1) + "…" : s);
  // A label's text, without that of the fields in it, such as a select's
  // options.
  const labelText = (label) => {
    const copy = label.cloneNode(true);
    copy.querySelectorAll("input, select, textarea, button").forEach((c) => c.remove());
    return copy.textContent;
  };
  const name = (el, r) => {
    const by = el.getAttribute("aria-labelledby");
    if (by) {
      const text = by.split(/\s+/).map((id) => document.getElementById(id)?.innerText ?? "").join(" ");
      if (clean(text)) return clean(text);
    }
    const label = el.getAttribute("aria-label");
    if (clean(label)) return clean(label);
    if (el.labels?.length) return clean([...el.labels].map(labelText).join(" "));
    if (el.tagName === "IMG") return clean(el.getAttribute("alt"));
    if (el.tagName === "INPUT" && ["button", "submit", "reset"].includes(el.type)) return clean(el.value);
    if (namedByText.has(r)) return clean(el.innerText ?? el.textContent);
    return clean(el.getAttribute("title") || el.getAttribute("placeholder"));
  };
  const visible = (el) => {
    if (el.getAttribute("aria-hidden") === "true") return false;
    if (el.checkVisibility) return el.checkVisibility({ visibilityProperty: true });
    return el.getClientRects().length > 0;
  };
  // A generic element that takes clicks, as a div with a click handler does:
  // the page cannot see handlers, but the pointer such an element shows.
  const clickable = (el) => {
    if (getComputedStyle(el).cursor !== "pointer") return false;
    const parent = el.parentElement;
    return !parent || getComputedStyle(parent).cursor !== "pointer";
  };

  const lines = [];
  let length = 0;
  let truncated = false;
  const emit = (depth, text) => {
    if (truncated) return;
    const line = "  ".repeat(depth) + "- " + text;
    if (limit > 0 && length + line.length > limit) {
      truncated = true;
      return;
    }
    lines.push(line);
    length += line.length + 1;
  };

  // node returns what a node gives its parent: a line, with its children, or
  // its children's, for an element with no role of its own.
  const walk = (node) => {
    if (node.nodeType === Node.TEXT_NODE) {
      const text = clean(node.textContent);
      return text ? [{ text }] : [];
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return [];
    const el = node;
    if (skip.has(el.tagName.toUpperCase()) || !visible(el)) return [];
    const childNodes = [...(el.shadowRoot ? el.shadowRoot.childNodes : []), ...el.childNodes];
    if (el.tagName === "SLOT") childNodes.push(...el.assignedNodes());
    let r = role(el);
    if (r === "presentation" || r === "none") r = "";
    if (!r && clickable(el)) r = "generic";
    if (!r) return childNodes.flatMap(walk);
    const n = name(el, r);
    const attrs = [];
    if (r === "heading") attrs.push(`[level=${el.getAttribute("aria-level") ?? el.tagName.slice(1)}]`);
    if (el.checked || el.getAttribute("aria-checked") === "true") attrs.push("[checked]");
    if (el.disabled || el.getAttribute("aria-disabled") === "true") attrs.push("[disabled]");
    const expanded = el.getAttribute("aria-expanded");
    if (expanded) attrs.push(expanded === "true" ? "[expanded]" : "[collapsed]");
    if (el.getAttribute("aria-selected") === "true" || (el.tagName === "OPTION" && el.selected)) attrs.push("[selected]");
    if (r === "generic") attrs.push("[cursor=pointer]");
    attrs.push(`[ref=${refOf(el)}]`);
    const item = { line: r + (n ? ` "${cut(n).replace(/"/g, '\\"')}"` : "") + " " + attrs.join(" "), children: [] };
    if (el.tagName === "INPUT" || el.tagName === "TEXTAREA") {
      if (!["checkbox", "radio", "button", "submit", "reset", "image"].includes(el.type)) {
        item.value = el.type === "password" ? (el.value ? "••••" : "") : el.value;
      }
    } else if (el.tagName === "SELECT") {
      item.value = [...el.selectedOptions].map((o) => clean(o.text)).join(", ");
      item.children = [...el.options].map((o) => ({ line: `option "${cut(clean(o.text))}"` + (o.selected ? " [selected]" : "") }));
    } else if (el.tagName === "IFRAME") {
      item.children = [{ text: "/url: " + (el.getAttribute("src") ?? "") }];
    } else if (!(namedByText.has(r) && n)) {
      item.children = childNodes.flatMap(walk);
    } else {
      // Its text is its name; what else is in it, such as an image's name,
      // still shows.
      item.children = childNodes.flatMap(walk).filter((c) => c.line);
    }
    if (r === "link") item.children.unshift({ text: "/url: " + el.getAttribute("href") });
    return [item];
  };
  const print = (items, depth) => {
    for (const item of items) {
      if (item.text !== undefined) {
        emit(depth, item.text.startsWith("/url: ") ? item.text : "text: " + cut(item.text, 400));
        continue;
      }
      item.children ??= [];
      // Text alone in an element goes on its line.
      const texts = item.children.filter((c) => c.text !== undefined && !c.text.startsWith("/url: "));
      if (item.value !== undefined && item.value !== "") {
        emit(depth, item.line + ": " + cut(item.value, 400));
        print(item.children, depth + 1);
      } else if (texts.length === 1 && item.children.length === 1) {
        emit(depth, item.line + ": " + cut(texts[0].text, 400));
      } else {
        emit(depth, item.line);
        print(item.children, depth + 1);
      }
    }
  };
  print(walk(document.body ?? document.documentElement), 0);
  return { url: location.href, title: document.title, snapshot: lines.join("\n"), truncated };
}

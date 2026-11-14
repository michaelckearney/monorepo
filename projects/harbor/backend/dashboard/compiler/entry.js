// What Harbor runs in goja to compile a dashboard. TestCompiler bundles this
// file with Vue's compiler into compiler.js.
import { babelParse, compileScript, compileStyle, compileTemplate, MagicString, parse, walkIdentifiers } from "@vue/compiler-sfc";

// The only modules a dashboard can import. Harbor provides both.
const allowed = ["vue", "harbor"];

const filename = "dashboard.vue";

// compile returns the problems in a dashboard, and when there are none, its
// code: one TypeScript module that exports the component, and its CSS; and
// the tools it uses, as [service, tool]. id names the dashboard's scoped
// styles. has is what "harbor" has, and like gives the names closest to one
// it lacks (kit.Exports and kit.Like), so a model that makes up a piece,
// such as Tile, is told what to use. widgets are the kit's widgets, by name.
globalThis.compileDashboard = (source, id, has, like, widgets) => {
  const { descriptor, errors } = parse(source, { filename });
  if (errors.length > 0) {
    return { problems: errors.map((e) => describe(e, 0)) };
  }
  const problems = [];
  if (!descriptor.template) {
    problems.push("a dashboard needs a <template>");
  }

  const scoped = descriptor.styles.some((s) => s.scoped);
  const block = descriptor.scriptSetup ?? descriptor.script;
  const isTS = block?.lang === "ts";
  let code = "const _sfc_main = {}\n";
  let bindings;
  let graph = { services: new Map(), names: new Map(), tools: new Set() };
  if (block) {
    try {
      const script = compileScript(descriptor, { id, genDefaultAs: "_sfc_main" });
      bindings = script.bindings;
      code = script.content + "\n";
      for (const { source, imported, isType } of Object.values(script.imports ?? {})) {
        if (!allowed.includes(source)) {
          problems.push(`a dashboard can only import from "vue" and "harbor", not "${source}"`);
        } else if (source === "harbor" && !isType && !has.includes(imported)) {
          const close = like(imported);
          problems.push(
            `"harbor" has no ${imported}.` +
              (close.length ? ` Did you mean ${close.join(" or ")}?` : "") +
              ` search_kit finds what it has.`,
          );
        }
      }
      // Babel's offsets are from the start of each block's content.
      const lines = (b) => (i) => b.loc.start.line - 1 + lineAt(b.content, i);
      const statements = [
        ...(script.scriptAst ?? []).map((node) => ({ node, line: lines(descriptor.script) })),
        ...(script.scriptSetupAst ?? []).map((node) => ({ node, line: lines(descriptor.scriptSetup) })),
      ];
      graph = analyze(statements, problems);
    } catch (e) {
      problems.push(describe(e, 0));
    }
  }

  if (descriptor.template) {
    const offset = descriptor.template.loc.start.line - 1;
    const template = bindWidgets(descriptor.template, source, graph, widgets, isTS);
    for (const code of expressions(descriptor.template.ast)) {
      expressionUses(code, graph, isTS).tools.forEach((t) => graph.tools.add(t));
    }
    const result = compileTemplate({
      source: template,
      filename,
      id,
      scoped,
      compilerOptions: { bindingMetadata: bindings, expressionPlugins: isTS ? ["typescript"] : [] },
    });
    problems.push(...result.errors.map((e) => (typeof e === "string" ? e : describe(e, offset))));
    code += result.code + "\n_sfc_main.render = render\n";
  }

  const css = [];
  for (const style of descriptor.styles) {
    const offset = style.loc.start.line - 1;
    const result = compileStyle({ source: style.content, filename, id: `data-v-${id}`, scoped: style.scoped });
    problems.push(...result.errors.map((e) => describe(e, offset)));
    css.push(result.code);
  }

  if (problems.length > 0) {
    return { problems };
  }
  if (scoped) {
    code += `_sfc_main.__scopeId = "data-v-${id}"\n`;
  }
  // The Bound each widget is in, as the template asks for it by name.
  code += `import { Bound as __HarborBound } from "harbor"\n`;
  code += `_sfc_main.components = Object.assign({ HarborBound: __HarborBound }, _sfc_main.components)\n`;
  code += "export default _sfc_main\n";
  const uses = [...graph.tools].sort().map((t) => t.split("."));
  return { problems: [], code, css: css.join("\n"), uses };
};

const lineAt = (source, index) => source.slice(0, index).split("\n").length;

// analyze finds the services a script gets with useService("name"), and
// the tools it uses as service.tool. It returns, for each name the script
// declares at its top level, the names it reads and the tools it uses
// (service.tool), so a widget's props can be followed to the tools behind
// them; and every tool used anywhere.
function analyze(statements, problems) {
  const services = new Map(); // the variable's name → the service's name
  for (const { node: s, line } of statements) {
    if (s.type !== "VariableDeclaration") {
      continue;
    }
    for (const d of s.declarations) {
      const init = d.init;
      if (init?.type !== "CallExpression" || init.callee.type !== "Identifier" || init.callee.name !== "useService") {
        continue;
      }
      const arg = init.arguments[0];
      const at = `line ${line(init.start)}: `;
      if (init.arguments.length !== 1 || arg.type !== "StringLiteral") {
        problems.push(`${at}useService takes the service's name in quotes, such as useService("tasklane")`);
      } else if (d.id.type !== "Identifier") {
        problems.push(`${at}name the service, as const ${arg.value} = useService("${arg.value}"), and call its tools as ${arg.value}.tool_name(...)`);
      } else {
        services.set(d.id.name, arg.value);
      }
    }
  }

  const tools = new Set();
  const names = new Map(); // a top-level name → { reads, tools }
  for (const { node: s, line } of statements) {
    const declared = declaredBy(s);
    const found = { reads: new Set(), tools: new Set() };
    uses(s, services, line, problems, found);
    found.tools.forEach((t) => tools.add(t));
    for (const n of declared) {
      names.set(n, found);
    }
  }
  return { services, names, tools };
}

// The names a top-level statement declares.
function declaredBy(s) {
  const out = [];
  const add = (p) => {
    if (!p) return;
    if (p.type === "Identifier") out.push(p.name);
    else if (p.type === "ObjectPattern") p.properties.forEach((q) => add(q.type === "RestElement" ? q.argument : q.value));
    else if (p.type === "ArrayPattern") p.elements.forEach(add);
    else if (p.type === "AssignmentPattern") add(p.left);
    else if (p.type === "RestElement") add(p.argument);
  };
  if (s.type === "VariableDeclaration") s.declarations.forEach((d) => add(d.id));
  else if ((s.type === "FunctionDeclaration" || s.type === "ClassDeclaration") && s.id) out.push(s.id.name);
  else if (s.type === "ExportNamedDeclaration" && s.declaration) out.push(...declaredBy(s.declaration));
  return out;
}

// uses adds to found the names node reads and the tools it uses. A service
// is only ever used as service.tool: anything else hides which tool, so it
// is a problem.
function uses(node, services, line, problems, found) {
  walkIdentifiers(
    node,
    (id, parent, _, isReference, isLocal) => {
      if (!isReference || isLocal) {
        return;
      }
      const service = services.get(id.name);
      if (service === undefined) {
        found.reads.add(id.name);
        return;
      }
      if (parent?.type === "VariableDeclarator" && parent.id === id) {
        return;
      }
      const member = (parent?.type === "MemberExpression" || parent?.type === "OptionalMemberExpression") && parent.object === id;
      if (member && !parent.computed && parent.property.type === "Identifier") {
        found.tools.add(`${service}.${parent.property.name}`);
      } else {
        const at = line && typeof id.start === "number" ? `line ${line(id.start)}: ` : "";
        problems.push(`${at}call ${id.name}'s tools by name, as ${id.name}.tool_name(...), so Harbor knows which tools the dashboard uses`);
      }
    },
    true,
  );
}

// The tools a set of names reaches, following what each reads.
function reach(graph, reads) {
  const tools = new Set();
  const seen = new Set();
  const visit = (n) => {
    if (seen.has(n)) return;
    seen.add(n);
    const found = graph.names.get(n);
    if (!found) return;
    found.tools.forEach((t) => tools.add(t));
    found.reads.forEach(visit);
  };
  reads.forEach(visit);
  return tools;
}

// What a template expression reads and the tools it uses. Handlers can be
// statements, so one that is not an expression is read as a script.
function expressionUses(code, graph, isTS) {
  const found = { reads: new Set(), tools: new Set() };
  const plugins = isTS ? ["typescript"] : [];
  let ast;
  try {
    ast = babelParse(`(${code})`, { plugins });
  } catch {
    try {
      ast = babelParse(code, { plugins });
    } catch {
      return found;
    }
  }
  uses(ast.program, graph.services, null, [], found);
  return found;
}

// The code in a template node: its directives' expressions, and the
// interpolations in it.
function expressions(node, out = []) {
  if (node.type === 5 && node.content?.content) {
    out.push(node.content.content);
  }
  for (const p of node.props ?? []) {
    if (p.type === 7) {
      if (p.name === "for") {
        out.push(p.forParseResult?.source?.content ?? p.exp?.content.split(/\s+(?:in|of)\s+/).pop() ?? "");
      } else if (p.exp?.content && p.name !== "slot") {
        out.push(p.exp.content);
      }
      if (p.arg && !p.arg.isStatic && p.arg.content) {
        out.push(p.arg.content);
      }
    }
  }
  for (const c of node.children ?? []) {
    expressions(c, out);
  }
  if (node.branches) {
    node.branches.forEach((b) => expressions(b, out));
  }
  return out;
}

// bindWidgets puts each widget in the template (the outermost, where one
// is in another) inside a HarborBound: <HarborBound :uses="[...]"
// :frame-title="() => ..."><BoardWidget .../></HarborBound>.
// The widget is then the Bound's slot, so its props are worked out as the
// Bound renders it, not as the dashboard does, and what they throw stays in
// the widget. uses is the tools its props and events reach. The props that
// say whether and how often it is there (v-if, v-else-if, v-else, v-for,
// and key) move to the Bound. Nothing is added on a new line, so the
// template's lines stay where they were.
function bindWidgets(block, source, graph, widgets, isTS) {
  const s = new MagicString(source);
  const visit = (node) => {
    if (node.type === 1 && node.tagType === 1 && widgets.includes(node.tag)) {
      wrap(s, node, graph, isTS);
      return;
    }
    (node.children ?? []).forEach(visit);
    (node.branches ?? []).forEach(visit);
  };
  block.ast.children.forEach(visit);
  return s.slice(block.loc.start.offset, block.loc.end.offset);
}

const moves = (p) =>
  (p.type === 7 && ["if", "else-if", "else", "for"].includes(p.name)) ||
  (p.type === 6 && p.name === "key") ||
  (p.type === 7 && p.name === "bind" && p.arg?.isStatic && p.arg.content === "key");

function wrap(s, node, graph, isTS) {
  const found = { reads: new Set(), tools: new Set() };
  for (const code of expressions(node)) {
    const f = expressionUses(code, graph, isTS);
    f.reads.forEach((r) => found.reads.add(r));
    f.tools.forEach((t) => found.tools.add(t));
  }
  const tools = [...new Set([...found.tools, ...reach(graph, found.reads)])].sort();

  // The widget's title and size, each a function the Bound calls, where
  // what it throws is caught.
  const frame = [];
  for (const name of ["title", "span", "rows"]) {
    const p = node.props.find(
      (p) => (p.type === 6 && p.name === name) || (p.type === 7 && p.name === "bind" && p.arg?.isStatic && p.arg.content === name),
    );
    if (p?.type === 6) {
      // No ' in it, so the attribute can be in '…'.
      frame.push(attr(`:frame-${name}`, `() => ${JSON.stringify(p.value?.content ?? "").replace(/'/g, "\\u0027")}`));
    } else if (p?.exp) {
      // Its own attribute's quotes are not in it.
      frame.push(attr(`:frame-${name}`, `() => (${p.exp.content})`));
    }
  }

  const moved = node.props.filter(moves);
  for (const p of moved) {
    s.remove(p.loc.start.offset, p.loc.end.offset);
  }
  const attrs = [
    ...moved.map((p) => p.loc.source),
    attr(":uses", JSON.stringify(tools.map((t) => t.split(".")))),
    ...frame,
  ];
  s.appendLeft(node.loc.start.offset, `<HarborBound ${attrs.join(" ")}>`);
  s.appendRight(node.loc.end.offset, "</HarborBound>");
}

// An attribute, in the quotes its value does not have.
function attr(name, value) {
  return value.includes('"') ? `${name}='${value}'` : `${name}="${value}"`;
}

// Template and style errors put the line in loc.start, Babel's script errors
// in loc itself.
function describe(e, offset) {
  const line = e.loc?.start?.line ?? e.loc?.line ?? e.line;
  return line ? `line ${line + offset}: ${firstLine(e.message)}` : firstLine(e.message);
}

// The compilers add a code frame under the message, and Babel adds the
// position after it; the editor shows both already.
function firstLine(message) {
  return message
    .split("\n")[0]
    .replace(/^\[@?vue\/compiler-sfc\] /, "")
    .replace(/ \(\d+:\d+\)$/, "");
}

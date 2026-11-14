import { highlightCode, tagHighlighter, tags as t } from "@lezer/highlight";
import { javascriptLanguage, typescriptLanguage } from "@codemirror/lang-javascript";
import { vueLanguage } from "@codemirror/lang-vue";
import { parser as jsonParser } from "@lezer/json";

// Code in the chat, coloured as the code editor would, without an editor: a
// run of tokens, each with the class Code.vue colours it by.

export type Language = "json" | "javascript" | "typescript" | "vue";

export interface Token {
  text: string;
  class: string;
}

const parsers = {
  json: jsonParser,
  javascript: javascriptLanguage.parser,
  typescript: typescriptLanguage.parser,
  vue: vueLanguage.parser,
};

const highlighter = tagHighlighter([
  { tag: t.keyword, class: "tok-keyword" },
  { tag: [t.string, t.regexp, t.attributeValue], class: "tok-string" },
  { tag: [t.number, t.bool, t.null, t.atom], class: "tok-literal" },
  { tag: [t.propertyName, t.attributeName], class: "tok-property" },
  { tag: [t.typeName, t.className, t.tagName], class: "tok-type" },
  { tag: t.comment, class: "tok-comment" },
]);

/** text, as tokens: a line break is a token of its own, with no class. */
export function highlight(text: string, language: Language): Token[] {
  const tokens: Token[] = [];
  highlightCode(
    text,
    parsers[language].parse(text),
    highlighter,
    (text, cls) => tokens.push({ text, class: cls }),
    () => tokens.push({ text: "\n", class: "" }),
  );
  return tokens;
}

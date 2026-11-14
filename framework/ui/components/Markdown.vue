<script setup lang="ts">
import { computed } from "vue";
import MarkdownIt from "markdown-it";

// Text someone wrote, shown as Markdown. Raw HTML in it is shown as text,
// and links that would run code (javascript: and the like) stay text, so it
// can't put anything in the window but formatting. Its links open in your
// browser, as every link in the apps does (openLinksInBrowser).
//
// image, if given, says where to show each image from: the src to use, or
// null while it is still on its way, when the image waits without one. An
// image it returns undefined for is shown from where the text says.
const props = defineProps<{ text: string; image?: (src: string) => string | null | undefined }>();

const md = new MarkdownIt({ linkify: true, breaks: true });
const image = md.renderer.rules.image!;
md.renderer.rules.image = (tokens, i, options, env, self) => {
  const token = tokens[i];
  const shown = props.image?.(String(token.attrGet("src") ?? ""));
  if (shown === null) {
    token.attrs = (token.attrs ?? []).filter(([name]) => name !== "src");
  } else if (shown !== undefined) {
    token.attrSet("src", shown);
  }
  return image(tokens, i, options, env, self);
};
const html = computed(() => md.render(props.text));
</script>

<template>
  <div class="markdown" v-html="html"></div>
</template>

<style scoped>
.markdown {
  min-width: 0;
  overflow-wrap: anywhere;
}
.markdown > :deep(:first-child) {
  margin-top: 0;
}
.markdown > :deep(:last-child) {
  margin-bottom: 0;
}
.markdown :deep(:is(p, ul, ol, pre, blockquote, table, hr)) {
  margin: calc(var(--spacing) * 2) 0;
}
.markdown :deep(:is(h1, h2, h3, h4, h5, h6)) {
  margin: calc(var(--spacing) * 3) 0 calc(var(--spacing) * 1);
  font-weight: 600;
}
.markdown :deep(:is(h1, h2)) {
  font-size: var(--text-title);
  line-height: var(--text-title--line-height);
}
.markdown :deep(:is(h3, h4, h5, h6)) {
  font-size: var(--text-body);
  line-height: var(--text-body--line-height);
}
.markdown :deep(:is(ul, ol)) {
  padding-left: calc(var(--spacing) * 6);
}
.markdown :deep(ul) {
  list-style: disc;
}
.markdown :deep(ol) {
  list-style: decimal;
}
.markdown :deep(li > :is(ul, ol)) {
  margin: 0;
}
.markdown :deep(li + li) {
  margin-top: calc(var(--spacing) * 0.5);
}
.markdown :deep(a) {
  color: var(--primary);
  text-decoration: underline;
  text-underline-offset: 2px;
}
.markdown :deep(code) {
  padding: 0 calc(var(--spacing) * 1);
  border-radius: var(--radius-sm);
  background: var(--muted);
  font-family: var(--font-mono);
  font-size: var(--text-small);
}
.markdown :deep(pre) {
  overflow-x: auto;
  padding: calc(var(--spacing) * 2) calc(var(--spacing) * 3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--muted);
}
.markdown :deep(pre code) {
  padding: 0;
  background: none;
}
.markdown :deep(blockquote) {
  padding-left: calc(var(--spacing) * 3);
  border-left: 2px solid var(--border);
  color: var(--muted-foreground);
}
.markdown :deep(hr) {
  border: none;
  border-top: 1px solid var(--border);
}
.markdown :deep(table) {
  display: block;
  overflow-x: auto;
  border-collapse: collapse;
}
.markdown :deep(:is(th, td)) {
  padding: calc(var(--spacing) * 1) calc(var(--spacing) * 2);
  border: 1px solid var(--border);
  text-align: left;
}
.markdown :deep(th) {
  font-weight: 600;
}
.markdown :deep(img) {
  max-width: 100%;
  border-radius: var(--radius-md);
}
</style>

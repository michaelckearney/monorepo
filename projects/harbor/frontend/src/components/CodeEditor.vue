<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { basicSetup } from "codemirror";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { indentWithTab } from "@codemirror/commands";
import { javascript } from "@codemirror/lang-javascript";
import { vue } from "@codemirror/lang-vue";
import { oneDark } from "@codemirror/theme-one-dark";

const props = defineProps<{ language: "typescript" | "vue"; label: string }>();
const model = defineModel<string>({ required: true });

const host = ref<HTMLElement>();
let view: EditorView | undefined;

// The editor is dark when the window is: when <html> has .dark (themes.ts).
const theme = new Compartment();
const themeFor = () => (document.documentElement.classList.contains("dark") ? oneDark : []);
const scheme = new MutationObserver(() => view?.dispatch({ effects: theme.reconfigure(themeFor()) }));

onMounted(() => {
  view = new EditorView({
    parent: host.value,
    state: EditorState.create({
      doc: model.value,
      extensions: [
        basicSetup,
        keymap.of([indentWithTab]),
        props.language === "vue" ? vue() : javascript({ typescript: true }),
        theme.of(themeFor()),
        EditorView.contentAttributes.of({ "aria-label": props.label }),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) {
            model.value = u.state.doc.toString();
          }
        }),
      ],
    }),
  });
  scheme.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
});

// A change from outside, such as the starter file arriving, replaces the text.
watch(model, (text) => {
  if (view && text !== view.state.doc.toString()) {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } });
  }
});

onBeforeUnmount(() => {
  scheme.disconnect();
  view?.destroy();
});
</script>

<template>
  <div ref="host" class="code-editor"></div>
</template>

<style scoped>
.code-editor {
  height: 100%;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
.code-editor :deep(.cm-editor) {
  height: 100%;
}
.code-editor :deep(.cm-scroller) {
  font-family: var(--font-mono);
  font-size: var(--text-body);
}
</style>

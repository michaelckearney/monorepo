<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch, type Component } from "vue";
import { ChevronLeft, ChevronRight } from "lucide-vue-next";
import type { Panel } from "../panels";
import Tip from "./Tip.vue";

// A side panel of the window, on the left or the right. You resize it by
// dragging its inner edge, and collapse it by dragging it narrower than it
// can be, to a strip with its icon and a button that opens it again at the
// width it had. Its header is the same on both sides: the icon, a title,
// and what you can do there. Where the panel's width is kept is the app's:
// it passes the saved panel, and saves what save gives it.
const props = defineProps<{
  side: "left" | "right";
  /** The panel as saved. */
  panel: Panel;
  /** The other side's panel, which the middle of the window leaves room for. */
  other: Panel;
  /** What a double click on the edge puts it back to. */
  initial: Panel;
  /** The panel is this landmark, named this. */
  element: "nav" | "aside";
  landmark: string;
  /** What the strip's button opens, as in "Show the agent". */
  label: string;
  icon: Component;
  /** The narrowest it can be open. Dragged to half of this, it collapses. */
  minimum: number;
  /** The key that, with ⌘ or Ctrl, collapses and opens it. */
  shortcut: string;
  /** Something in the panel needs you: a dot on the strip's icon, and this as its title. */
  alert?: string;
}>();
const emit = defineEmits<{ save: [panel: Panel] }>();

const strip = 48;
// The middle of the window is never narrower than this.
const middle = 480;
const inward = props.side === "left" ? 1 : -1;

const windowWidth = ref(window.innerWidth);
// The panel as a drag has it, until the saved one catches up.
const dragged = ref<Panel | null>(null);
const dragging = ref(false);
const panel = computed(() => dragged.value ?? props.panel);
const shown = computed(() => (panel.value.collapsed ? strip : panel.value.width));
const maximum = computed(() => {
  const other = props.other;
  return Math.max(props.minimum, windowWidth.value - middle - (other.collapsed ? strip : other.width));
});

watch(
  () => props.panel,
  () => {
    if (!dragging.value) {
      dragged.value = null;
    }
  },
);

/** A panel this wide, kept between its minimum and maximum. */
const open = (width: number): Panel => ({ width: Math.min(Math.max(width, props.minimum), maximum.value), collapsed: false });

function save(next: Panel) {
  const saved = props.panel;
  if (next.width !== saved.width || next.collapsed !== saved.collapsed) {
    emit("save", { width: Math.round(next.width), collapsed: next.collapsed });
  }
}

function toggle() {
  save(panel.value.collapsed ? open(panel.value.width) : { ...panel.value, collapsed: true });
}

function drag(down: PointerEvent) {
  down.preventDefault();
  const handle = down.currentTarget as HTMLElement;
  handle.setPointerCapture(down.pointerId);
  const from = shown.value;
  const kept = props.panel.width;
  dragging.value = true;
  const move = (e: PointerEvent) => {
    const width = from + (e.clientX - down.clientX) * inward;
    dragged.value = width < props.minimum / 2 ? { width: kept, collapsed: true } : open(width);
  };
  const up = () => {
    handle.removeEventListener("pointermove", move);
    dragging.value = false;
    if (dragged.value) {
      save(dragged.value);
    }
    const saved = props.panel;
    if (dragged.value?.width === saved.width && dragged.value.collapsed === saved.collapsed) {
      dragged.value = null;
    }
  };
  handle.addEventListener("pointermove", move);
  handle.addEventListener("pointerup", up, { once: true });
  handle.addEventListener("pointercancel", up, { once: true });
}

// The arrow keys move the edge; past the minimum, the panel collapses, and
// from the strip, it opens.
function key(e: KeyboardEvent) {
  const step = ({ ArrowRight: 16, ArrowLeft: -16 } as Record<string, number>)[e.key];
  if (e.key === "Enter") {
    e.preventDefault();
    toggle();
  } else if (step) {
    e.preventDefault();
    const width = shown.value + step * inward;
    if (panel.value.collapsed) {
      if (width > strip) {
        toggle();
      }
    } else {
      save(width < props.minimum ? { ...panel.value, collapsed: true } : open(width));
    }
  }
}

function shortcut(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === props.shortcut) {
    e.preventDefault();
    toggle();
  }
}

function resized() {
  windowWidth.value = window.innerWidth;
}

onMounted(() => {
  window.addEventListener("keydown", shortcut);
  window.addEventListener("resize", resized);
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", shortcut);
  window.removeEventListener("resize", resized);
});
</script>

<template>
  <component
    :is="element"
    :aria-label="landmark"
    class="relative flex min-h-0 shrink-0 flex-col bg-sidebar text-sidebar-foreground"
    :class="[side === 'left' ? 'border-r' : 'border-l', !dragging && 'transition-[width] duration-200 ease-out']"
    :style="{ width: `${shown}px` }"
  >
    <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <header class="flex h-14 shrink-0 items-center gap-2 border-b px-3" :class="panel.collapsed && 'justify-center'">
        <span class="relative grid size-6 shrink-0 place-items-center rounded-md bg-primary text-primary-foreground">
          <component :is="icon" class="size-3.5" />
          <template v-if="panel.collapsed && alert">
            <span class="absolute -right-1 -top-1 size-2 rounded-full bg-warning ring-2 ring-sidebar" :title="alert"></span>
            <span class="sr-only">({{ alert }})</span>
          </template>
        </span>
        <template v-if="!panel.collapsed">
          <h2 class="min-w-0 flex-1 truncate font-semibold tracking-tight"><slot name="title" /></h2>
          <div class="flex shrink-0 items-center gap-1"><slot name="actions" /></div>
        </template>
      </header>
      <div v-if="panel.collapsed" class="flex flex-1 flex-col items-center justify-end p-2">
        <Tip :label="`Show ${label}`" :side="side === 'left' ? 'right' : 'left'">
          <button
            type="button"
            class="grid size-8 place-items-center rounded-md text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
            :aria-label="`Show ${label}`"
            @click="toggle"
          >
            <ChevronRight v-if="side === 'left'" class="size-4" /><ChevronLeft v-else class="size-4" />
          </button>
        </Tip>
      </div>
      <!-- Hidden rather than removed when collapsed, so a chat's unsent message is still there. -->
      <div v-show="!panel.collapsed" class="flex min-h-0 flex-1 flex-col" :style="{ minWidth: `${panel.width}px` }">
        <slot />
      </div>
    </div>
    <div
      role="separator"
      aria-orientation="vertical"
      :aria-label="`Resize ${label}`"
      :aria-valuenow="shown"
      :aria-valuemin="strip"
      :aria-valuemax="maximum"
      tabindex="0"
      class="group absolute inset-y-0 z-10 flex w-2 cursor-col-resize justify-center outline-none"
      :class="side === 'left' ? 'right-0 translate-x-1/2' : 'left-0 -translate-x-1/2'"
      @pointerdown="drag"
      @dblclick="save(initial)"
      @keydown="key"
    >
      <span
        class="w-0.5 transition-colors group-hover:bg-ring/60 group-focus-visible:bg-ring"
        :class="dragging && 'bg-ring'"
      ></span>
    </div>
  </component>
</template>

<!--
description: The frame every widget has, for a widget of your own when none in the catalog fits: a title, the services its data is from, a row of controls under it, the content, and a footer. It shows loading, failing, and signing in by itself. Put building blocks inside, such as ItemList, Item, Value, and Chart.
keywords: widget, frame, card, custom widget, build your own, container, search box, filter, switch, button, footer, header, anything else
example:
  <Widget title="Before the appointment" :rows="2">
    <Text>Bring the insurance card and the list of medicines.</Text>
  </Widget>
example:
  <Widget title="Accounts" search="Search accounts" filter-label="Kind" :filters="['All', 'Checking', 'Savings']" filter="All" toggle="Hide closed" :toggled="true" action="Open an account">
    <ItemList :items="[{ name: 'Checking', number: '••4410' }, { name: 'Savings', number: '••9921' }]" label="name" detail="number" />
  </Widget>
-->
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { LoaderCircle } from "lucide-vue-next";
import { Button as UiButton } from "@ui/components/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@ui/components/tooltip";
import ServiceIcon from "../../components/ServiceIcon.vue";
import { signingIn, useServices } from "../../services";
import { useStatus } from "../status";
import type { Option, Total } from "../frame";
import type { Span } from "../grid";
import Search from "../controls/Search.vue";
import Segmented from "../controls/Segmented.vue";
import Select from "../controls/Select.vue";
import Toggle from "../controls/Toggle.vue";
import Button from "../primitives/Button.vue";
import Card from "../primitives/Card.vue";
import Skeleton from "../primitives/Skeleton.vue";
import Value from "../primitives/Value.vue";

// Every widget in the catalog is this frame with its content inside, and
// takes these props too: they fall through to it.
//
// The row under the header holds what the props ask for, each reporting
// what you do and leaving what it means to the dashboard: a search box
// (search, its placeholder; v-model:query, what is typed, once you stop
// typing), a select (filters; v-model:filter; filterLabel names it), a
// segmented control between views of the same thing (views; v-model:view),
// a switch (toggle, its label; v-model:toggled), and a button (action, its
// label; @action). count is a number beside the title, more how many a
// limit left out, and total a sum, both in the footer.
//
// A dashboard's widgets are each inside a Bound (kit/runtime.ts), which
// tells this frame the state of the reads behind it: until they load, a
// skeleton in place of the content; if one failed, why, or what to sign in
// to; the services they are from as icons; a spinner while one loads again
// or a write runs; and a line for each that could not.
const props = withDefaults(
  defineProps<{
    title?: string;
    span?: Span;
    rows?: number;
    count?: number;
    more?: number;
    total?: Total;
    search?: string | boolean;
    query?: string;
    filters?: Option[];
    filter?: string;
    filterLabel?: string;
    views?: Option[];
    view?: string;
    toggle?: string;
    toggled?: boolean;
    action?: string;
  }>(),
  { query: "", filterLabel: "Filter" },
);
const emit = defineEmits<{
  "update:query": [string];
  "update:filter": [string];
  "update:view": [string];
  "update:toggled": [boolean];
  action: [];
}>();

const status = useStatus();
const state = computed(() => status?.state ?? "ready");
const { services } = useServices();
const icons = computed(() =>
  (status?.services ?? []).map((n) => services.value.find((s) => s.name === n)).filter((s) => s !== undefined),
);
const signInTitle = computed(() => {
  const n = status?.signIn;
  return n ? (services.value.find((s) => s.name === n.service)?.title ?? n.service) : "";
});
// What a read found you must sign in to, and the widget's other services
// that Harbor has found you signed out of: the widget names them all at
// once, by title, rather than one after another.
const signInItems = computed(() => {
  const n = status?.signIn;
  if (!n) {
    return [];
  }
  if (n.tool) {
    return [{ title: signInTitle.value, signIn: () => status!.signInNow() }];
  }
  const others = services.value.filter(
    (s) => s.name !== n.service && status!.services.includes(s.name) && s.needed.some((x) => x.tool === ""),
  );
  return [
    { title: signInTitle.value, signIn: () => status!.signInNow() },
    ...others.map((s) => ({ title: s.title, signIn: () => status!.signInNow(s.name) })),
  ].sort((a, b) => a.title.localeCompare(b.title));
});

// What is typed reaches the dashboard once you stop typing, so a read of it
// runs once, not for every letter.
const typed = ref(props.query);
watch(
  () => props.query,
  (q) => (typed.value = q),
);
let timer: ReturnType<typeof setTimeout> | undefined;
function type(text: string) {
  typed.value = text;
  clearTimeout(timer);
  timer = setTimeout(() => emit("update:query", text), 300);
}
onUnmounted(() => clearTimeout(timer));

const hasControls = computed(
  () => !!props.search || !!props.filters?.length || !!props.views?.length || !!props.toggle || !!props.action,
);
</script>

<template>
  <Card :span="span" :rows="rows" :aria-label="title" :aria-busy="state === 'loading' || status?.busy">
    <template v-if="title || icons.length" #header>
      <div class="flex h-control-xs min-w-0 flex-1 items-center gap-2">
        <h3 v-if="title" class="truncate font-medium">{{ title }}</h3>
        <span
          v-if="count !== undefined && state === 'ready'"
          class="shrink-0 rounded-full bg-muted px-1.5 text-caption font-medium text-muted-foreground"
          >{{ count }}</span
        >
      </div>
      <div class="flex shrink-0 items-center gap-1.5">
        <LoaderCircle v-if="status?.busy && state === 'ready'" class="size-3 shrink-0 animate-spin text-muted-foreground" aria-label="Refreshing" />
        <TooltipProvider v-if="icons.length" :delay-duration="300">
          <ul class="flex items-center gap-1.5" aria-label="Where this is from">
            <li v-for="s in icons" :key="s.name" class="flex">
              <Tooltip>
                <TooltipTrigger as-child>
                  <span role="img" class="grid size-control-xs place-items-center" :aria-label="s.title">
                    <ServiceIcon :url="s.url" />
                  </span>
                </TooltipTrigger>
                <TooltipContent>{{ s.title }}</TooltipContent>
              </Tooltip>
            </li>
          </ul>
        </TooltipProvider>
      </div>
    </template>

    <template v-if="hasControls || $slots.controls || status?.asking" #controls>
      <template v-if="status?.asking">
        <div class="flex w-full items-center gap-2" role="alertdialog" :aria-label="status.asking">
          <span class="min-w-0 flex-1 truncate font-medium">{{ status.asking }}</span>
          <Button size="sm" variant="ghost" @click="status.answer(false)">Cancel</Button>
          <Button size="sm" @click="status.answer(true)">Confirm</Button>
        </div>
      </template>
      <template v-else>
        <Search v-if="search" class="min-w-0 flex-1" :placeholder="search === true ? 'Search' : search" :model-value="typed" @update:model-value="(v) => type(v ?? '')" />
        <Select v-if="filters?.length" :label="filterLabel" :options="filters" :model-value="filter" @update:model-value="(v) => emit('update:filter', v ?? '')" />
        <Segmented v-if="views?.length" :label="title ?? 'View'" :options="views" :model-value="view" @update:model-value="(v) => emit('update:view', v ?? '')" />
        <Toggle v-if="toggle" :label="toggle" :model-value="toggled" @update:model-value="(v) => emit('update:toggled', !!v)" />
        <slot name="controls" />
        <Button v-if="action" size="sm" variant="secondary" class="ml-auto" @click="emit('action')">{{ action }}</Button>
      </template>
    </template>

    <div v-if="state === 'loading'" class="grid gap-2" aria-label="Loading">
      <Skeleton class="h-5 w-1/2" />
      <Skeleton class="h-3" />
      <Skeleton class="h-3 w-3/4" />
    </div>
    <div v-else-if="state === 'signIn'" class="grid justify-items-start gap-2">
      <p v-if="signingIn === `${status!.signIn!.service}/${status!.signIn!.tool ?? ''}`" role="status">Signing in to {{ signInTitle }}…</p>
      <template v-else>
        <p class="text-small text-muted-foreground">{{ status!.signIn!.tool ? "Confirm it's you to see this:" : "Sign in to see this:" }}</p>
        <ul class="grid gap-1.5">
          <li v-for="item in signInItems" :key="item.title" class="flex min-h-row items-center gap-2">
            <span class="size-1.5 shrink-0 rounded-full bg-warning" aria-hidden="true"></span>
            <span class="min-w-0 flex-1">{{ item.title }}</span>
            <UiButton size="xs" class="shrink-0" :disabled="signingIn !== ''" @click="item.signIn()">{{ status!.signIn!.tool ? "Confirm" : "Sign in" }}</UiButton>
          </li>
        </ul>
      </template>
    </div>
    <p v-else-if="state === 'failed'" class="whitespace-pre-wrap text-destructive" role="alert">{{ status!.problem }}</p>
    <template v-else>
      <slot />
      <div v-if="status?.notices.length" class="mt-3 grid gap-1.5 border-t pt-3 text-small">
        <p v-for="(n, i) in status.notices" :key="i" class="whitespace-pre-wrap text-destructive" role="alert">{{ n }}</p>
      </div>
    </template>

    <template v-if="state === 'ready' && ($slots.footer || more || total)" #footer>
      <slot name="footer" />
      <span v-if="more">{{ more }} more</span>
      <span v-if="total" class="ml-auto flex items-center gap-2"
        >Total
        <Value :value="total.value" :format="total.format" :currency="total.currency" :signed="total.signed" class="font-medium text-foreground"
      /></span>
    </template>
  </Card>
</template>

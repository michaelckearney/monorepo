<!--
description: A date or time as people say it: "Today", "Thu, Nov 5", "in 2 days", "3h ago", or a range from one to another.
keywords: time, date, day, when, due, relative, ago, today, tomorrow, range, deadline, timestamp
example:
  <Time value="2026-11-05" as="day" />
example:
  <Time value="2026-11-05" end="2026-11-07" />
example:
  <Time :value="Date.now() - 3 * 3600000" as="relative" />
-->
<script setup lang="ts">
import { computed } from "vue";
import { dateText, dayText, relativeText, timeText, when } from "../time";

// A date ("2026-11-05"), a time ("2026-11-05T09:30:00"), or milliseconds,
// shown as `as` says: date ("Nov 5", with the time if it has one), day
// ("Today", "Thu, Nov 5"), time ("9:30 AM"), or relative ("in 2 days",
// "3h ago"). Given end, it is a range: "Nov 5 – 7". A day or time is shown
// as the tool wrote it, not moved to your time zone.
const props = withDefaults(
  defineProps<{
    value: unknown;
    end?: unknown;
    as?: "date" | "day" | "time" | "relative";
  }>(),
  { as: "date" },
);

const text = computed(() => {
  const w = when(props.value);
  if (!w) {
    return props.value === null || props.value === undefined || props.value === "" ? "—" : String(props.value);
  }
  const e = when(props.end);
  if (e && e.day !== w.day) {
    const sameMonth = e.day.slice(0, 7) === w.day.slice(0, 7);
    return `${dateText(w.day)} – ${sameMonth ? Number(e.day.slice(8)) : dateText(e.day)}`;
  }
  if (e && e.time && w.time) {
    return `${timeText(w.time)} – ${timeText(e.time)}`;
  }
  switch (props.as) {
    case "day":
      return dayText(w.day) + (w.time ? `, ${timeText(w.time)}` : "");
    case "time":
      return w.time ? timeText(w.time) : "All day";
    case "relative":
      return relativeText(w);
  }
  return dateText(w.day) + (w.time ? `, ${timeText(w.time)}` : "");
});
const iso = computed(() => {
  const w = when(props.value);
  return w ? (w.time ? `${w.day}T${w.time}` : w.day) : undefined;
});
</script>

<template>
  <time class="whitespace-nowrap tabular-nums" :datetime="iso">{{ text }}</time>
</template>

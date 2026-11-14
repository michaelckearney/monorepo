<!--
description: A picture, cropped to a shape and rounded, that loads as you scroll to it; it can link somewhere.
keywords: image, picture, photo, thumbnail, preview, img, cover
example:
  <Image src="data:image/svg+xml,%3Csvg xmlns=%27http://www.w3.org/2000/svg%27 viewBox=%270 0 16 9%27%3E%3Crect width=%2716%27 height=%279%27 fill=%27%237aa7c7%27/%3E%3C/svg%3E" alt="A harbor" aspect="wide" />
-->
<script setup lang="ts">
// A picture. aspect is square, wide (16:9), or auto (its own shape). link
// opens in a new tab. For a file a tool returned, src is fileURL(file).
withDefaults(defineProps<{ src: string; alt: string; aspect?: "square" | "wide" | "auto"; link?: string }>(), {
  aspect: "square",
});
</script>

<template>
  <component :is="link ? 'a' : 'span'" :href="link" class="block min-w-0">
    <img
      :src="src"
      :alt="alt"
      loading="lazy"
      class="block w-full rounded-md bg-muted object-cover"
      :class="{ square: 'aspect-square', wide: 'aspect-video', auto: '' }[aspect]"
    />
  </component>
</template>

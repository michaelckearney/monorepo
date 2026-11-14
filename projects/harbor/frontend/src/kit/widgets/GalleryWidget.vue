<!--
description: Pictures in a grid of squares: photos from an album, recent photos, images, document previews, with a caption under each if you like.
keywords: gallery, photos, recent photos, pictures, images, album, albums, thumbnails, media, camera roll, previews, attachments
example:
  <GalleryWidget title="Summer" :pictures="[{ alt: 'Beach', src: 'data:image/svg+xml,%3Csvg xmlns=%27http://www.w3.org/2000/svg%27 viewBox=%270 0 1 1%27%3E%3Crect width=%271%27 height=%271%27 fill=%27%237aa7c7%27/%3E%3C/svg%3E' }, { alt: 'Garden', src: 'data:image/svg+xml,%3Csvg xmlns=%27http://www.w3.org/2000/svg%27 viewBox=%270 0 1 1%27%3E%3Crect width=%271%27 height=%271%27 fill=%27%2398b57a%27/%3E%3C/svg%3E' }]" />
-->
<script setup lang="ts">
import type { Picture } from "../shapes";
import Image from "../primitives/Image.vue";
import Widget from "./Widget.vue";

// Pictures, as Picture says (for a file a tool returned, src is
// fileURL(file)). size is how wide each square is at least: sm, md, or lg.
withDefaults(defineProps<{ pictures: Picture[]; size?: "sm" | "md" | "lg"; empty?: string }>(), { size: "md", empty: "No pictures." });
</script>

<template>
  <Widget>
    <p v-if="pictures.length === 0" class="py-2 text-muted-foreground">{{ empty }}</p>
    <ul v-else class="grid gap-2" :style="{ gridTemplateColumns: `repeat(auto-fill, minmax(${{ sm: 5, md: 8, lg: 12 }[size]}rem, 1fr))` }">
      <li v-for="(p, i) in pictures" :key="i" class="grid min-w-0 content-start gap-1">
        <Image :src="p.src" :alt="p.alt" :link="p.link" />
        <p v-if="p.caption" class="truncate text-small text-muted-foreground">{{ p.caption }}</p>
      </li>
    </ul>
  </Widget>
</template>

import { computed, effectScope, ref, watch, type ComputedRef } from "vue";
import { sql, useQuery } from "@desktop";
import { dark } from "./themes";

/**
 * The key a service's icon is kept under: its URL's origin. backend/favicon's
 * Origin gives the same, and icons.test.ts checks both against one table. If
 * they differed, nothing would fail: every icon would just never show.
 */
export function serviceOrigin(url: string): string {
  try {
    const u = new URL(url.trim());
    if (u.protocol !== "http:" && u.protocol !== "https:") {
      return "";
    }
    return u.origin === "null" ? "" : u.origin;
  } catch {
    return "";
  }
}

// Icons whose bytes an <img> could not draw. The service shows the
// placeholder rather than a broken image, and a dark icon that will not draw
// falls back to the light one. Cleared when the icons change.
const broken = ref(new Set<string>());

let icons: ComputedRef<(url: string) => string> | undefined;

/**
 * The icon for a service's URL, as a data: URL, or "" for none. It is the
 * site's dark icon when the window is dark and the site has one. The backend
 * finds them (icons.go); this reads what it found.
 */
export function useServiceIcons(): ComputedRef<(url: string) => string> {
  // One query for the whole window, whichever component asks first.
  icons ??= effectScope(true).run(() => {
    const rows = useQuery<{ origin: string; mime_type: string; bytes: string; dark_mime_type: string | null; dark_bytes: string | null }>(
      () => sql`
        SELECT origin, mime_type, bytes, dark_mime_type, dark_bytes FROM service_icons
        WHERE status = 'ok' AND bytes IS NOT NULL`,
    );
    // New bytes may draw where the old would not.
    watch(rows, () => (broken.value = new Set()));
    const byOrigin = computed(
      () =>
        new Map(
        rows.value.map((r) => [
          r.origin,
          {
            light: `data:${r.mime_type};base64,${r.bytes}`,
            dark: r.dark_bytes ? `data:${r.dark_mime_type};base64,${r.dark_bytes}` : "",
          },
        ]),
        ),
    );
    return computed(() => {
      const map = byOrigin.value;
      const isDark = dark.value;
      return (url: string) => {
        const icon = map.get(serviceOrigin(url));
        if (!icon) {
          return "";
        }
        const preferred = isDark && icon.dark && !broken.value.has(icon.dark) ? icon.dark : icon.light;
        return broken.value.has(preferred) ? "" : preferred;
      };
    });
  })!;
  return icons;
}

/** Notes that an icon would not draw. */
export function markBroken(src: string) {
  if (!broken.value.has(src)) {
    broken.value = new Set(broken.value).add(src);
  }
}

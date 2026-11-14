import { onMounted, onUnmounted, ref, watch, type Ref, type WatchSource } from "vue";

/**
 * Loads a value now, again every `every` milliseconds, and again whenever
 * a source changes. The server, GitHub, and harness run say nothing when they
 * change, so the app asks.
 */
export function usePoll<T>(load: () => Promise<T>, initial: T, every: number, sources: WatchSource[] = []) {
  const value = ref(initial) as Ref<T>;
  const error = ref("");
  const loading = ref(false);
  let timer: ReturnType<typeof setInterval> | undefined;
  let seq = 0;
  const reload = async () => {
    const mine = ++seq;
    loading.value = true;
    try {
      const v = await load();
      if (mine === seq) {
        value.value = v;
        error.value = "";
      }
    } catch (e) {
      if (mine === seq) {
        error.value = String(e instanceof Error ? e.message : e);
      }
    } finally {
      if (mine === seq) {
        loading.value = false;
      }
    }
  };
  onMounted(() => {
    void reload();
    timer = setInterval(reload, every);
  });
  onUnmounted(() => clearInterval(timer));
  if (sources.length) {
    watch(sources, () => void reload());
  }
  return { value, error, loading, reload };
}

export function message(e: unknown): string {
  return String(e instanceof Error ? e.message : e);
}

export function ago(when: string | Date): string {
  const t = typeof when === "string" ? Date.parse(when) : when.getTime();
  if (!t || t < 0) {
    return "";
  }
  const s = Math.round((Date.now() - t) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)} h ago`;
  return `${Math.round(s / 86400)} d ago`;
}

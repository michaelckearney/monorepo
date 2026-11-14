// How a dashboard reaches its services, and how each widget shows the state
// of the calls behind it, without the dashboard saying so.
//
// A dashboard gets a service with useService("tracker") and calls its
// tools as methods. A tool that only reads (as its service's manifest says)
// returns its output at once: the output if Harbor has it, else a stand-in
// that, when anything in it is used, throws to say it is still loading or
// why it failed. Reads go in a computed or the template, as Vue's own state
// does: they run again when what their input reads changes, when the
// dashboard refreshes, and every `every` seconds if a read asks to. The
// same read with the same input is one request, however many places ask.
// A tool that changes something returns a promise: it asks you first if
// its service says it is destructive, and once it has run, every read runs
// again.
//
// The compiler puts each widget in the template inside a Bound, and passes
// it the tools the widget's props and events reach (backend/dashboard). A
// widget's props are worked out inside its Bound, so a read still loading,
// or one that failed, throws there: the Bound shows the widget's frame
// loading, or failing, or asking you to sign in, and the rest of the
// dashboard shows as it is. Once the read settles, it tries again.
import {
  computed as vueComputed,
  defineComponent,
  getCurrentInstance,
  h,
  onErrorCaptured,
  onMounted,
  onUnmounted,
  provide,
  reactive,
  ref,
  shallowReactive,
  watch,
  type ComputedGetter,
  type PropType,
  type WritableComputedOptions,
} from "vue";
import { signIn } from "../services";
import type { SignInNeeded } from "./data";
import type { Span } from "./grid";
import { statusKey, type Status } from "./status";
import Widget from "./widgets/Widget.vue";

/** A tool a dashboard uses, as the backend found it in the file and its service's manifest. */
export interface Use {
  service: string;
  tool: string;
  /** The tool's title, for asking before it runs. */
  title: string;
  /** It only reads: its output is data to show. */
  read: boolean;
  /** It changes something that cannot be put back: ask first. */
  destructive: boolean;
}

/** What a read throws while its output is still loading. */
export class Loading extends Error {
  constructor(readonly read: Read) {
    super(`${read.use.service}.${read.use.tool} is still loading`);
  }
}

/** What a read throws when it failed. */
export class ReadFailed extends Error {
  constructor(
    readonly read: Read,
    message: string,
  ) {
    super(message);
  }
}

/** What a read throws when you must sign in first. */
export class SignInRequired extends Error {
  constructor(
    readonly read: Read,
    readonly needed: SignInNeeded,
  ) {
    super(needed.heading);
  }
}

/** What a write rejects with once its widget shows why it failed. */
export class WriteFailed extends Error {}

/** One read: a tool and its input, and what it last gave. */
export interface Read {
  use: Use;
  input: Record<string, unknown>;
  state: {
    data: unknown;
    error: string;
    needsSignIn: SignInNeeded | null;
    loading: boolean;
    /** Counts each request that settled, to try again after. */
    settled: number;
    /** When data last loaded, in milliseconds. */
    updated: number;
    /** Counts each `every` that passed, so the reads that ask for it run again. */
    tick: number;
  };
  /** The refresh it last ran for. */
  round: number;
  /** Its `every` has passed. */
  stale: boolean;
  every?: number;
  timer?: ReturnType<typeof setTimeout>;
  /** The request under way, to ignore one an earlier request answers late. */
  request: number;
  /** A request is under way; loading says so too, a moment later. */
  pending: boolean;
}

/** What a page gives the runtime. */
export interface Host {
  /** Runs a tool and returns its output; rejects with an error whose cause is a SignInNeeded when you must sign in. */
  call(service: string, tool: string, input: Record<string, unknown>): Promise<unknown>;
  /** A write found you must sign in: the page offers it. */
  needsSignIn(n: SignInNeeded): void;
  /** A read found you must sign in, and its widget offers it. */
  shown(n: SignInNeeded): void;
}

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

function signInNeeded(e: unknown): SignInNeeded | null {
  const cause = (e as { cause?: Partial<SignInNeeded> })?.cause;
  return cause?.service && cause.heading ? (cause as SignInNeeded) : null;
}

/**
 * A stand-in for output that is not there: it throws what make makes when
 * anything in it is used. Vue's checks of what a value is (its __v_ keys)
 * pass it by, so it can sit in a ref or a prop until it is used.
 */
function standIn(make: () => Error): unknown {
  const fail = () => {
    throw make();
  };
  return new Proxy(function () {}, {
    get: (_, key) => (typeof key === "string" && key.startsWith("__v_") ? key === "__v_skip" : fail()),
    has: fail,
    ownKeys: fail,
    apply: fail,
    construct: fail,
    getOwnPropertyDescriptor: fail,
  });
}

/**
 * Vue's computed, with two differences. One whose getter threw throws again
 * each time it is read, until what it read changes: Vue's own keeps the
 * value it had and throws only once, so a second widget reading it would
 * get that. And while a read it makes is loading, once it has had a value,
 * it keeps that value: a widget whose filter changed shows what it had,
 * and its controls, until the new data is there.
 */
export function computed<T>(getter: ComputedGetter<T> | WritableComputedOptions<T>) {
  const get = typeof getter === "function" ? getter : getter.get;
  let last: { value: T } | null = null;
  const inner = vueComputed(() => {
    try {
      const value = get(undefined);
      last = { value };
      return { ok: true as const, value };
    } catch (error) {
      if (error instanceof Loading && last) {
        return { ok: true as const, value: last.value };
      }
      return { ok: false as const, error };
    }
  });
  return {
    __v_isRef: true,
    get value(): T {
      const r = inner.value;
      if (!r.ok) {
        throw r.error;
      }
      return r.value;
    },
    set value(v: T) {
      if (typeof getter === "function") {
        throw new Error("this computed has no set");
      }
      getter.set(v);
    },
  };
}

/** A widget's Bound as the runtime keeps it, to say where a write ran from. */
interface Mounted {
  uses: Set<string>;
  /** When you last pressed, typed, or dropped something in it. */
  touched: number;
  status: Status;
  /** Asks what a destructive write asks, in the widget. */
  ask(question: string): Promise<boolean>;
}

const keyOf = (u: { service: string; tool: string }) => `${u.service}.${u.tool}`;

/** A dashboard's runtime: its reads, its writes, and its widgets' Bounds. */
export function runtime(host: Host) {
  let uses: Use[] = [];
  const reads = new Map<string, Read>();
  const readsByTool = new Map<string, Set<Read>>();
  const round = ref(0);
  const bounds = new Set<Mounted>();
  // Writes under way and failed, by tool, for each widget's spinner and notices.
  const writes = reactive({ running: new Map<string, number>(), failed: new Map<string, string>() });

  function fetch(r: Read) {
    r.round = round.value;
    r.stale = false;
    clearTimeout(r.timer);
    const id = ++r.request;
    r.pending = true;
    // Not now: a read runs inside a computed, which must not change state.
    queueMicrotask(() => (r.state.loading = true));
    host.call(r.use.service, r.use.tool, r.input).then(
      (data) => {
        if (id === r.request) {
          Object.assign(r.state, { data, error: "", needsSignIn: null, updated: Date.now() });
        }
      },
      (e) => {
        if (id !== r.request) {
          return;
        }
        const n = signInNeeded(e);
        if (n) {
          host.shown(n);
        }
        Object.assign(r.state, { error: n ? "" : message(e), needsSignIn: n });
      },
    ).finally(() => {
      if (id !== r.request) {
        return;
      }
      r.pending = false;
      r.state.loading = false;
      r.state.settled++;
      if (r.every) {
        r.timer = setTimeout(() => {
          r.stale = true;
          r.state.tick++;
        }, r.every * 1000);
      }
    });
  }

  function read(use: Use, input: Record<string, unknown>, every?: number): unknown {
    const at = round.value;
    const key = JSON.stringify([use.service, use.tool, input]);
    let r = reads.get(key);
    if (!r) {
      r = {
        use,
        input,
        state: shallowReactive({ data: undefined, error: "", needsSignIn: null, loading: false, settled: 0, updated: 0, tick: 0 }),
        round: -1,
        stale: false,
        request: 0,
        pending: false,
      };
      reads.set(key, r);
      const set = readsByTool.get(keyOf(use)) ?? new Set();
      readsByTool.set(keyOf(use), set.add(r));
    }
    if (every) {
      r.every = every;
    }
    if (r.round < at || r.stale) {
      fetch(r);
    }
    const s = r.state;
    void s.tick;
    void s.settled;
    const found = r;
    if (s.needsSignIn) {
      const n = s.needsSignIn;
      return standIn(() => new SignInRequired(found, n));
    }
    if (s.data === undefined) {
      if (s.error && !found.pending) {
        const e = s.error;
        return standIn(() => new ReadFailed(found, e));
      }
      return standIn(() => new Loading(found));
    }
    return s.data;
  }

  async function write(use: Use, input: Record<string, unknown>) {
    const k = keyOf(use);
    const from = [...bounds].filter((b) => b.uses.has(k)).sort((a, b) => b.touched - a.touched)[0];
    if (!from) {
      throw new Error(`${k} changes something, so it runs only from something you do in a widget, such as @act or @move`);
    }
    if (use.destructive && !(await from.ask(`${use.title}?`))) {
      return undefined;
    }
    writes.running.set(k, (writes.running.get(k) ?? 0) + 1);
    writes.failed.delete(k);
    try {
      const out = await host.call(use.service, use.tool, input);
      refresh();
      return out;
    } catch (e) {
      const n = signInNeeded(e);
      if (n) {
        host.needsSignIn(n);
      } else {
        writes.failed.set(k, message(e));
      }
      throw new WriteFailed(message(e));
    } finally {
      writes.running.set(k, writes.running.get(k)! - 1);
    }
  }

  /** Every read runs again, the next time something reads it, and keeps what it had until then. */
  function refresh() {
    writes.failed.clear();
    round.value++;
  }

  /**
   * A service's tools, as methods: tracker.list_issues({ project: "WEB" }).
   * A read takes { every: seconds } after its input, to run again that often.
   */
  function useService(name: string) {
    return new Proxy({} as Record<string, (input?: Record<string, unknown>, options?: { every?: number }) => any>, {
      get(_, tool) {
        const use = typeof tool === "string" ? uses.find((u) => u.service === name && u.tool === tool) : undefined;
        if (!use) {
          return undefined;
        }
        return use.read
          ? (input: Record<string, unknown> = {}, options: { every?: number } = {}) => read(use, input, options.every)
          : (input: Record<string, unknown> = {}) => write(use, input);
      },
    });
  }

  /** The frame a Bound shows while its widget cannot: its title and size, as the widget has them. */
  interface Frame {
    title?: string;
    span?: Span;
    rows?: number;
  }

  // Renders the widget: what its props read throws here, where Vue's
  // handling of a render's error catches it, and the Bound hears of it.
  const Inner = defineComponent({
    name: "HarborWidget",
    props: { onTouch: { type: Function as PropType<() => void>, required: true } },
    setup(props, { slots }) {
      const instance = getCurrentInstance()!;
      const kinds = ["pointerdown", "keydown", "drop", "change"];
      let el: Element | null = null;
      onMounted(() => {
        el = instance.subTree.el instanceof Element ? instance.subTree.el : null;
        kinds.forEach((k) => el?.addEventListener(k, props.onTouch, true));
      });
      onUnmounted(() => kinds.forEach((k) => el?.removeEventListener(k, props.onTouch, true)));
      return () => slots.default?.();
    },
  });

  const Bound = defineComponent({
    name: "HarborBound",
    props: {
      /** The tools the widget reaches, as [service, tool]. */
      uses: { type: Array as PropType<[string, string][]>, default: () => [] },
      /** The widget's title and size, worked out here, where they may throw. */
      frameTitle: Function as PropType<() => string>,
      frameSpan: Function as PropType<() => Span>,
      frameRows: Function as PropType<() => number>,
    },
    setup(props, { slots }) {
      const tools = vueComputed(() => new Set(props.uses.map(([s, t]) => `${s}.${t}`)));
      // What stopped the widget, and what to wait for before trying again.
      const stopped = shallowReactive({ state: "ready" as Status["state"], problem: "", signIn: null as SignInNeeded | null });
      const attempt = ref(0);
      const handlerErrors = ref<string[]>([]);
      watch(round, () => (handlerErrors.value = []));
      let waiting: (() => void) | null = null;
      function retryWhen(source: () => unknown) {
        waiting?.();
        const stop = watch(source, () => {
          stop();
          waiting = null;
          Object.assign(stopped, { state: "ready", problem: "", signIn: null });
          attempt.value++;
        });
        waiting = stop;
      }
      onUnmounted(() => waiting?.());

      onErrorCaptured((e, _, info) => {
        if (e instanceof WriteFailed) {
          return false;
        }
        // An event handler's error is the handler's, not the widget's: it
        // says so under it. (A production build gives Vue's error code.)
        if (/handler|#runtime-[56]$/.test(info)) {
          handlerErrors.value = [...handlerErrors.value, message(e)];
          return false;
        }
        const at = round.value;
        if (e instanceof Loading) {
          const r = e.read;
          const settled = r.state.settled;
          Object.assign(stopped, { state: "loading", problem: "", signIn: null });
          retryWhen(() => r.state.settled !== settled || round.value !== at);
        } else if (e instanceof SignInRequired) {
          Object.assign(stopped, { state: "signIn", problem: "", signIn: e.needed });
          retryWhen(() => round.value !== at || e.read.state.needsSignIn === null);
        } else {
          Object.assign(stopped, { state: "failed", problem: message(e), signIn: null });
          retryWhen(() => round.value !== at);
        }
        return false;
      });

      const running = (k: string) => (writes.running.get(k) ?? 0) > 0;
      // Shown only once the widget is: a read loading then is loading again,
      // or for an input that changed.
      const busy = vueComputed(() => [...tools.value].some((k) => running(k) || [...(readsByTool.get(k) ?? [])].some((r) => r.state.loading)));
      const notices = vueComputed(() => [
        ...[...tools.value].flatMap((k) =>
          [...(readsByTool.get(k) ?? [])].filter((r) => r.state.error && r.state.data !== undefined).map((r) => `Couldn't refresh: ${r.state.error}`),
        ),
        ...[...tools.value].map((k) => writes.failed.get(k)).filter((m): m is string => !!m),
        ...handlerErrors.value,
      ]);

      let answer: ((yes: boolean) => void) | null = null;
      const status: Status = reactive({
        state: vueComputed(() => stopped.state),
        problem: vueComputed(() => stopped.problem),
        signIn: vueComputed(() => stopped.signIn),
        services: vueComputed(() => [...new Set(props.uses.map(([s]) => s))]),
        busy,
        notices,
        async signInNow(service?: string) {
          const n = stopped.signIn;
          if (n && (await signIn(service ?? n.service, service ? "" : (n.tool ?? "")))) {
            refresh();
          }
        },
        asking: "",
        answer(yes: boolean) {
          answer?.(yes);
          answer = null;
          status.asking = "";
        },
      }) as unknown as Status;
      provide(statusKey, status);

      const mounted: Mounted = {
        get uses() {
          return tools.value;
        },
        touched: 0,
        status,
        ask(question) {
          answer?.(false);
          status.asking = question;
          return new Promise((resolve) => (answer = resolve));
        },
      };
      bounds.add(mounted);
      onUnmounted(() => bounds.delete(mounted));
      const touch = () => (mounted.touched = performance.now());

      // The frame's title and size, those that working out does not throw.
      const frame = () => {
        const out: Frame = {};
        for (const [key, get] of [
          ["title", props.frameTitle],
          ["span", props.frameSpan],
          ["rows", props.frameRows],
        ] as const) {
          try {
            if (get) {
              Object.assign(out, { [key]: get() });
            }
          } catch {
            // Without it, then.
          }
        }
        return out;
      };
      return () =>
        stopped.state === "ready"
          ? h(Inner, { key: attempt.value, onTouch: touch }, slots)
          : h(Widget, frame());
    },
  });

  return {
    useService,
    Bound,
    refresh,
    /** Stops the reads that run again every so often, once the dashboard is gone. */
    dispose() {
      reads.forEach((r) => clearTimeout(r.timer));
      reads.clear();
    },
    /** The tools the dashboard uses, before it runs. */
    setUses(u: Use[]) {
      uses = u;
    },
  };
}

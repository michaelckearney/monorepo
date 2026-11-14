import { computed, inject, ref, type InjectionKey, type Ref } from "vue";

/** Whether the Grid around a card is stacked: one column, as on a narrow window. */
export const stackedKey: InjectionKey<Ref<boolean>> = Symbol("stacked");

/** The element a dashboard scrolls in, whose height its Grid fills. */
export const viewKey: InjectionKey<Ref<HTMLElement | null>> = Symbol("view");

/** Every dashboard's grid: so many columns by so many rows, filling the view. */
export const columns = 12;
export const rows = 8;

/** How wide a Grid is at least, in rem, to lay out as written: narrower, and it stacks. */
export const stackBelow = 48;

/** How tall a row is, in rem, where the Grid does not fill a view: stacked, or outside a dashboard. */
export const rowHeight = 4.5;

/** How wide a card is: a number of the grid's columns, or "full" for all of them. */
export type Span = number | "full";

/** The CSS grid-column for a card's span: the only column, when the Grid is stacked. */
export function useSpan(span: () => Span | undefined) {
  const stacked = inject(stackedKey, ref(false));
  return computed(() => {
    const s = span();
    if (s === undefined || stacked.value) {
      return undefined;
    }
    // span="2" in a template is a string.
    return s === "full" ? "1 / -1" : `span ${Number(s)} / span ${Number(s)}`;
  });
}

/** The CSS grid-row for a card that is rows rows tall. */
export function useRows(rows: () => number | undefined) {
  return computed(() => {
    const r = rows();
    // rows="2" in a template is a string.
    return r === undefined ? undefined : `span ${Number(r)} / span ${Number(r)}`;
  });
}

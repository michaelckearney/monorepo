// What the Widget frame takes besides its content.
import type { Format } from "./data";

/** A choice in a select or segmented control: a string, or { value, label }. */
export type Option = string | { value: string; label: string };

/** A sum the footer shows, as Value shows it. */
export interface Total {
  value: number;
  format?: Format;
  currency?: string;
  signed?: boolean;
}

import type { ClassValue } from "clsx";
import { clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// The sizes from style.css, so that text-caption is a size (and
// text-muted-foreground beside it a colour) and h-control a height.
const twMerge = extendTailwindMerge({
  extend: {
    theme: {
      text: ["caption", "small", "body", "title", "heading", "figure"],
      spacing: ["control-xs", "control-sm", "control", "control-lg", "row", "page", "card"],
    },
  },
});

/** Joins class names, letting a later Tailwind class win over an earlier one. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

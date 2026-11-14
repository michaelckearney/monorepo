import { reactive } from "vue";
import { GitHub } from "@backend";

// The images in issues and comments. One attached on GitHub needs GitHub's
// credentials in a private repository, so the backend fetches it, and the
// window shows it from what the backend gives back. Any other image shows
// from where its Markdown says.
const attached = /^https:\/\/github\.com\/user-attachments\/assets\/[0-9a-f-]{36}$/i;

// What each attachment shows from: a data: URL, or null while it is on its
// way or if it could not be fetched, when Markdown shows its words instead.
const shown = reactive(new Map<string, string | null>());

/** Where Markdown shows each image of issue or pull request number of repo from. */
export function images(repo: string, number: number) {
  return (src: string): string | null | undefined => {
    if (!attached.test(src)) {
      return undefined;
    }
    if (!shown.has(src)) {
      shown.set(src, null);
      GitHub.Image(repo, number, src).then(
        (data) => shown.set(src, data),
        () => {},
      );
    }
    return shown.get(src) ?? null;
  };
}

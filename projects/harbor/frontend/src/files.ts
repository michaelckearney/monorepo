import { Tools } from "@backend";

// Files tools take and return. A tool's output names each file it returned
// as { file, name, type, size }; an input property with format: "file"
// takes a file's id.

export interface FileRef {
  file: string;
  name: string;
  type: string;
  size: number;
}

function isFileRef(v: unknown): v is FileRef {
  const r = v as FileRef;
  return !!r && typeof r === "object" && typeof r.file === "string" && typeof r.size === "number" && "type" in r;
}

/** Every file a tool's output names, wherever it is. */
export function filesIn(value: unknown): FileRef[] {
  if (isFileRef(value)) {
    return [value];
  }
  if (value && typeof value === "object") {
    return Object.values(value).flatMap(filesIn);
  }
  return [];
}

function bytes(base64: string) {
  return Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
}

/**
 * A URL for a file a tool returned, to show it, such as in an <img>. This
 * is `fileURL` in the "harbor" module that dashboards import.
 */
export async function fileURL(file: FileRef | string): Promise<string> {
  const f = await Tools.File(typeof file === "string" ? file : file.file);
  return URL.createObjectURL(new Blob([bytes(f.data)], { type: f.type }));
}

/** Saves a file a tool returned to your computer, as a download. */
export async function download(file: FileRef) {
  const a = document.createElement("a");
  a.href = await fileURL(file);
  a.download = file.name || "file";
  a.click();
  URL.revokeObjectURL(a.href);
}

/** Keeps a file you chose, for a tool's input, and returns its id. */
export async function addFile(file: File): Promise<string> {
  let binary = "";
  const all = new Uint8Array(await file.arrayBuffer());
  for (let i = 0; i < all.length; i += 0x8000) {
    binary += String.fromCharCode(...all.subarray(i, i + 0x8000));
  }
  const ref = await Tools.AddFile({ name: file.name, type: file.type, data: btoa(binary) });
  return ref.file;
}

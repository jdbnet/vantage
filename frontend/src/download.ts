import { apiFetch, isDesktopShell } from "@/api";
import { desktopSaveDownload, desktopSaveFile } from "@/desktopBridge";

function saveBlobInBrowser(blob: Blob, filename: string) {
  const href = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = href;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(href);
}

export async function saveBlobAsFile(blob: Blob, filename: string): Promise<void> {
  if (isDesktopShell()) {
    const data = Array.from(new Uint8Array(await blob.arrayBuffer()));
    await desktopSaveFile(filename, data);
    return;
  }
  saveBlobInBrowser(blob, filename);
}

// saveRemoteFile saves an /api download. The desktop app streams it to the
// chosen path. The browser downloads it through the page.
export async function saveRemoteFile(apiPath: string, filename: string): Promise<void> {
  if (isDesktopShell()) {
    await desktopSaveDownload(filename, apiPath);
    return;
  }
  const res = await apiFetch(apiPath);
  if (!res.ok) throw new Error(await res.text());
  saveBlobInBrowser(await res.blob(), filename);
}

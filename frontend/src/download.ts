import { isDesktopShell } from "@/api";
import { desktopSaveFile } from "@/desktopBridge";

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

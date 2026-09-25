import { isDesktopShell } from "@/api";

type DesktopSave = {
  SaveFile: (defaultFilename: string, data: number[]) => Promise<string>;
};

function desktopSave(): DesktopSave | undefined {
  const w = window as Window & {
    go?: { main?: { Desktop?: DesktopSave } };
  };
  return w.go?.main?.Desktop;
}

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
    const bridge = desktopSave();
    if (!bridge?.SaveFile) {
      throw new Error("Desktop save dialog is unavailable");
    }
    const data = Array.from(new Uint8Array(await blob.arrayBuffer()));
    await bridge.SaveFile(filename, data);
    return;
  }
  saveBlobInBrowser(blob, filename);
}

export type DesktopBridge = {
  SaveFile: (defaultFilename: string, data: number[]) => Promise<string>;
  ReadClipboard: () => Promise<string>;
  WriteClipboard: (text: string) => Promise<void>;
};

export function getDesktopBridge(): DesktopBridge | undefined {
  const w = window as Window & {
    go?: { main?: { Desktop?: DesktopBridge } };
  };
  return w.go?.main?.Desktop;
}

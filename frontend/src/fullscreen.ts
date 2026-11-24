export function wailsRuntime(): WailsRuntime | undefined {
  return window.runtime;
}

export function useMacNativeFullscreen(): boolean {
  const host = navigator.userAgent || "";
  if (!/Mac/i.test(host)) return false;
  const protocol = location.protocol;
  const name = location.hostname || location.host;
  return (
    protocol.startsWith("wails") ||
    name === "wails" ||
    name === "wails.localhost" ||
    name.endsWith(".wails.localhost")
  );
}

export type FullscreenDocument = Document & {
  webkitFullscreenElement?: Element | null;
  webkitExitFullscreen?: () => Promise<void> | void;
};

export type FullscreenHost = HTMLElement & {
  webkitRequestFullscreen?: () => Promise<void> | void;
};

export function fullscreenElementOf(doc: Document): Element | null {
  const d = doc as FullscreenDocument;
  return d.fullscreenElement || d.webkitFullscreenElement || null;
}

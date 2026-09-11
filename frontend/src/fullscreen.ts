export function wailsRuntime(): WailsRuntime | undefined {
  return window.runtime;
}

export function useMacNativeFullscreen(): boolean {
  return typeof wailsRuntime()?.WindowFullscreen === "function" && /Mac/i.test(navigator.userAgent);
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

import { isDesktopShell } from "@/api";
import { getDesktopBridge } from "@/desktopBridge";

const CLIPBOARD_MAX = 1024 * 1024;

let sharedText = "";

export function rememberClipboard(text: string) {
  if (text && text.length <= CLIPBOARD_MAX) {
    sharedText = text;
  }
}

export function sharedClipboardText(): string {
  return sharedText;
}

export function trimClipboardText(text: string): string {
  return text.length <= CLIPBOARD_MAX ? text : text.slice(0, CLIPBOARD_MAX);
}

async function readNativeClipboard(win: Window): Promise<string | null> {
  if (isDesktopShell()) {
    const bridge = getDesktopBridge();
    if (bridge?.ReadClipboard) {
      try {
        const text = await bridge.ReadClipboard();
        if (text) return text;
      } catch {
        /* try web clipboard below */
      }
    }
  }
  try {
    const text = await win.navigator.clipboard?.readText();
    if (text) return text;
  } catch {
    /* ignore */
  }
  return null;
}

async function writeNativeClipboard(win: Window, text: string): Promise<void> {
  if (isDesktopShell()) {
    const bridge = getDesktopBridge();
    if (bridge?.WriteClipboard) {
      try {
        await bridge.WriteClipboard(text);
        return;
      } catch {
        /* try web clipboard below */
      }
    }
  }
  try {
    await win.navigator.clipboard?.writeText(text);
  } catch {
    /* shared clipboard still holds the text */
  }
}

export async function readClipboard(win: Window): Promise<string> {
  const text = await readNativeClipboard(win);
  if (text) {
    rememberClipboard(text);
    return text;
  }
  return sharedText;
}

export async function writeClipboard(win: Window, text: string): Promise<void> {
  if (!text) return;
  const trimmed = trimClipboardText(text);
  rememberClipboard(trimmed);
  await writeNativeClipboard(win, trimmed);
}

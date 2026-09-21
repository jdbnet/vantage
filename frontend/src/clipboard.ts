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

export async function readClipboard(win: Window): Promise<string> {
  try {
    const text = await win.navigator.clipboard?.readText();
    if (text) {
      rememberClipboard(text);
      return text;
    }
  } catch {
    /* system clipboard may be unavailable in the desktop shell */
  }
  return sharedText;
}

export async function writeClipboard(win: Window, text: string): Promise<void> {
  if (!text) return;
  rememberClipboard(text);
  try {
    await win.navigator.clipboard?.writeText(trimClipboardText(text));
  } catch {
    /* shared clipboard still holds the text */
  }
}

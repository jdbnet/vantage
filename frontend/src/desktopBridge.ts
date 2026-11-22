import { isDesktopShell } from "@/api";

type WailsCall = {
  ByName: (method: string, ...args: unknown[]) => Promise<unknown>;
};

async function callDesktop(method: string, ...args: unknown[]): Promise<unknown> {
  if (!isDesktopShell()) {
    throw new Error("desktop shell unavailable");
  }
  const specifier = "/wails/runtime.js";
  const runtime = (await import(/* @vite-ignore */ specifier)) as { Call?: WailsCall };
  if (!runtime.Call) {
    throw new Error("desktop shell unavailable");
  }
  return runtime.Call.ByName(`main.Desktop.${method}`, ...args);
}

export async function desktopSaveFile(defaultFilename: string, data: number[]): Promise<string> {
  const path = await callDesktop("SaveFile", defaultFilename, data);
  return typeof path === "string" ? path : "";
}

export async function desktopReadClipboard(): Promise<string> {
  const text = await callDesktop("ReadClipboard");
  return typeof text === "string" ? text : "";
}

export async function desktopWriteClipboard(text: string): Promise<void> {
  await callDesktop("WriteClipboard", text);
}

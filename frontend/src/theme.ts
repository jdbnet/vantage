export const DEFAULT_ACCENT = "#1ebe8a";

export function normalizeAccent(hex: string | undefined | null): string {
  const raw = (hex || "").trim();
  const m = raw.match(/^#?([0-9a-fA-F]{6})$/);
  if (!m) return DEFAULT_ACCENT;
  return `#${m[1].toLowerCase()}`;
}

export function applyAccent(hex: string | undefined | null) {
  const n = normalizeAccent(hex);
  const r = parseInt(n.slice(1, 3), 16);
  const g = parseInt(n.slice(3, 5), 16);
  const b = parseInt(n.slice(5, 7), 16);
  const root = document.documentElement;
  root.style.setProperty("--accent", `${r} ${g} ${b}`);
  root.style.setProperty(
    "--accent-muted",
    `${Math.round(r * 0.78)} ${Math.round(g * 0.78)} ${Math.round(b * 0.78)}`,
  );
}

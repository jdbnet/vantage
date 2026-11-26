<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import {
  ClipboardPaste,
  Copy,
  Download,
  Eye,
  File,
  Folder,
  FolderPlus,
  HardDrive,
  LayoutGrid,
  List,
  Pencil,
  RefreshCw,
  Trash2,
  X,
} from "@lucide/vue";
import {
  api,
  apiFetch,
  isDesktopShell,
  type FileTransfer,
  type SftpEntry,
} from "@/api";
import { saveRemoteFile } from "@/download";

const props = defineProps<{
  sessions: { tabId: string; connId: string; label: string }[];
  open: boolean;
}>();

type Sys = { id: string; kind: "local" | "sftp"; connId: string; label: string };
type Clip = { kind: "local" | "sftp"; connId: string; paths: string[] };

const activeId = ref("local");
const paths = ref<Record<string, string>>({ local: "" });
const entries = ref<SftpEntry[]>([]);
const err = ref("");
const busy = ref(false);
const showHidden = ref(false);
const grid = ref(false);
const sortKey = ref<"name" | "size" | "mtime">("name");
const sortDir = ref<1 | -1>(1);
const renameTarget = ref("");
const newName = ref("");
const preview = ref<SftpEntry | null>(null);
const previewText = ref("");
const previewSrc = ref("");
let previewURL = "";
const transfers = ref<FileTransfer[]>([]);
const clip = ref<Clip | null>(null);
const applyAll = ref(false);
let poll: ReturnType<typeof setInterval> | null = null;

const localLabel = computed(() => (isDesktopShell() ? "Local" : "Vantage server"));

const systems = computed<Sys[]>(() => [
  { id: "local", kind: "local", connId: "", label: localLabel.value },
  ...props.sessions.map((s) => ({
    id: s.tabId,
    kind: "sftp" as const,
    connId: s.connId,
    label: s.label,
  })),
]);

const active = computed(() => systems.value.find((s) => s.id === activeId.value) || systems.value[0]);
const cwd = computed(() => paths.value[active.value.id] ?? "");

const shown = computed(() => {
  const list = entries.value.filter((e) => showHidden.value || !e.filename.startsWith("."));
  const dir = (e: SftpEntry) => (e.st_mode & 0o170000) === 0o040000;
  return [...list].sort((a, b) => {
    const ad = dir(a);
    const bd = dir(b);
    if (ad !== bd) return ad ? -1 : 1;
    let cmp = 0;
    if (sortKey.value === "size") cmp = a.st_size - b.st_size;
    else if (sortKey.value === "mtime") cmp = a.st_mtime - b.st_mtime;
    else cmp = a.filename.localeCompare(b.filename, undefined, { sensitivity: "base", numeric: true });
    return cmp * sortDir.value;
  });
});

const crumbs = computed(() => {
  const p = cwd.value;
  if (!p) return [{ label: "/", path: "/" }];
  const win = active.value.kind === "local" && /^[A-Za-z]:[\\/]/.test(p);
  const sep = win ? "\\" : "/";
  const parts = p.split(/[/\\]/).filter(Boolean);
  const out: { label: string; path: string }[] = [];
  if (!win) out.push({ label: "/", path: "/" });
  let acc = "";
  for (const part of parts) {
    if (win && acc === "") acc = `${part}\\`;
    else if (!acc || acc === "/") acc = `${sep}${part}`;
    else acc = `${acc.replace(/[/\\]$/, "")}${sep}${part}`;
    out.push({ label: win && out.length === 0 ? acc : part, path: acc });
  }
  return out.length ? out : [{ label: p, path: p }];
});

const waiting = computed(() => transfers.value.find((t) => t.state === "waiting") || null);

function isDir(m: number): boolean {
  return (m & 0o170000) === 0o040000;
}

function join(base: string, name: string): string {
  if (!base || base === "/" || base === "\\") {
    return active.value.kind === "local" && base.includes("\\") ? `${base}${name}` : `/${name}`;
  }
  const sep = base.includes("\\") ? "\\" : "/";
  return `${base.replace(/[/\\]$/, "")}${sep}${name}`;
}

async function load() {
  const sys = active.value;
  err.value = "";
  busy.value = true;
  try {
    const r =
      sys.kind === "local"
        ? await api.localList(paths.value[sys.id] ?? "")
        : await api.sftpList(sys.connId, paths.value[sys.id] ?? "");
    paths.value = { ...paths.value, [sys.id]: r.path };
    entries.value = r.entries || [];
  } catch (e) {
    err.value = e instanceof Error ? e.message : "List failed";
  } finally {
    busy.value = false;
  }
}

function openSys(id: string) {
  activeId.value = id;
  clearPreview();
  if (!(id in paths.value)) paths.value = { ...paths.value, [id]: "" };
  void load();
}

function enter(e: SftpEntry) {
  if (!isDir(e.st_mode)) {
    void openPreview(e);
    return;
  }
  paths.value = { ...paths.value, [active.value.id]: join(cwd.value, e.filename) };
  void load();
}

function go(path: string) {
  paths.value = { ...paths.value, [active.value.id]: path };
  clearPreview();
  void load();
}

async function mkdir() {
  const name = window.prompt("Folder name");
  if (!name) return;
  const p = join(cwd.value, name);
  try {
    if (active.value.kind === "local") await api.localMkdir(p);
    else await api.sftpMkdir(active.value.connId, p);
    await load();
  } catch (e) {
    err.value = e instanceof Error ? e.message : "Could not create folder";
  }
}

async function removeEntry(e: SftpEntry) {
  const p = join(cwd.value, e.filename);
  const msg = isDir(e.st_mode)
    ? `Delete folder ${e.filename} and everything inside it?`
    : `Delete ${e.filename}?`;
  if (!confirm(msg)) return;
  try {
    if (active.value.kind === "local") await api.localRemove(p);
    else await api.sftpRemove(active.value.connId, p, true);
    if (preview.value?.filename === e.filename) preview.value = null;
    await load();
  } catch (e2) {
    err.value = e2 instanceof Error ? e2.message : "Delete failed";
  }
}

function startRename(e: SftpEntry) {
  renameTarget.value = e.filename;
  newName.value = e.filename;
}

async function commitRename() {
  const oldName = renameTarget.value;
  const name = newName.value.trim();
  renameTarget.value = "";
  if (!oldName || !name || name === oldName) return;
  try {
    if (active.value.kind === "local") await api.localRename(join(cwd.value, oldName), join(cwd.value, name));
    else await api.sftpRename(active.value.connId, join(cwd.value, oldName), join(cwd.value, name));
    await load();
  } catch (e) {
    err.value = e instanceof Error ? e.message : "Rename failed";
  }
}

function endOf(sys: Sys, filePath: string) {
  return { system: sys.kind, conn_id: sys.connId, path: filePath };
}

async function transfer(from: Sys, filePaths: string[], to: Sys) {
  if (!filePaths.length) return;
  const destPath = paths.value[to.id] ?? "";
  try {
    await api.createTransfer(
      filePaths.map((p) => endOf(from, p)),
      { system: to.kind, conn_id: to.connId, path: destPath },
    );
    await refreshTransfers();
  } catch (e) {
    err.value = e instanceof Error ? e.message : "Transfer failed";
  }
}

function onDragStart(ev: DragEvent, e: SftpEntry) {
  const payload = {
    id: active.value.id,
    paths: [join(cwd.value, e.filename)],
  };
  ev.dataTransfer?.setData("application/x-vantage-files", JSON.stringify(payload));
  if (ev.dataTransfer) ev.dataTransfer.effectAllowed = "copy";
}

function onNavDrop(ev: DragEvent, sys: Sys) {
  const raw = ev.dataTransfer?.getData("application/x-vantage-files");
  if (!raw) return;
  const data = JSON.parse(raw) as { id: string; paths: string[] };
  const from = systems.value.find((s) => s.id === data.id);
  if (!from || from.id === sys.id) return;
  void transfer(from, data.paths, sys);
}

function copyEntry(e: SftpEntry) {
  clip.value = { kind: active.value.kind, connId: active.value.connId, paths: [join(cwd.value, e.filename)] };
}

function paste() {
  if (!clip.value) return;
  const from = systems.value.find((s) => s.kind === clip.value?.kind && s.connId === clip.value?.connId);
  if (!from) {
    err.value = "The copied session is no longer open";
    return;
  }
  void transfer(from, clip.value.paths, active.value);
}

async function downloadEntry(e: SftpEntry) {
  const p = join(cwd.value, e.filename);
  const url =
    active.value.kind === "local"
      ? api.localDownloadUrl(p)
      : api.sftpDownloadUrl(active.value.connId, p);
  try {
    await saveRemoteFile(url, e.filename);
  } catch (e2) {
    err.value = e2 instanceof Error ? e2.message : "Download failed";
  }
}

function streamUrl(e: SftpEntry): string {
  const p = join(cwd.value, e.filename);
  return active.value.kind === "local"
    ? api.localStreamUrl(p)
    : api.sftpStreamUrl(active.value.connId, p);
}

function previewKind(e: SftpEntry): "text" | "image" | "pdf" | "audio" | "none" {
  const n = e.filename.toLowerCase();
  if (/\.(png|jpe?g|gif|webp|svg)$/.test(n)) return "image";
  if (n.endsWith(".pdf")) return "pdf";
  if (/\.(mp3|wav|ogg|m4a)$/.test(n)) return "audio";
  if (/\.(txt|md|json|csv|log|js|ts|vue|go|py|css|html|xml|ya?ml|sh|conf|ini)$/.test(n)) return "text";
  return "none";
}

function clearPreview() {
  preview.value = null;
  previewText.value = "";
  previewSrc.value = "";
  if (previewURL) {
    URL.revokeObjectURL(previewURL);
    previewURL = "";
  }
}

async function openPreview(e: SftpEntry) {
  if (isDir(e.st_mode)) return;
  clearPreview();
  const kind = previewKind(e);
  preview.value = e;
  if (kind === "none") {
    previewText.value = "No preview for this file.";
    return;
  }
  if (kind === "text" && e.st_size > 1024 * 1024) {
    previewText.value = "This file is larger than 1 MB.";
    return;
  }
  try {
    const res = await apiFetch(streamUrl(e));
    if (!res.ok) {
      const text = await res.text();
      try {
        const data = JSON.parse(text) as { error?: string };
        previewText.value = data.error || text.trim() || "Could not load preview";
      } catch {
        previewText.value = text.trim() || "Could not load preview";
      }
      return;
    }
    if (kind === "text") {
      previewText.value = await res.text();
      return;
    }
    const blob = await res.blob();
    previewURL = URL.createObjectURL(blob);
    previewSrc.value = previewURL;
  } catch (e2) {
    previewText.value = e2 instanceof Error ? e2.message : "Could not load preview";
  }
}

const refreshedJobs = new Set<string>();

function sameFolder(dest: FileTransfer["dest"], sys: Sys, folder: string): boolean {
  if (!dest || dest.system !== sys.kind) return false;
  if (dest.system === "sftp" && dest.conn_id !== sys.connId) return false;
  const a = (dest.path || "").replace(/[/\\]+$/, "");
  const b = (folder || "").replace(/[/\\]+$/, "");
  return a === b;
}

function fmtDuration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

function transferLabel(t: FileTransfer): string {
  const amount = t.total > 0 ? `${fmtSize(t.bytes)} / ${fmtSize(t.total)}` : fmtSize(t.bytes);
  if (t.state !== "running" || t.bytes <= 0 || !t.total || !t.started) return `${t.state} · ${amount}`;
  const elapsed = (Date.now() - Date.parse(t.started)) / 1000;
  if (!Number.isFinite(elapsed) || elapsed < 0.4) return `${t.state} · ${amount}`;
  const left = (t.total - t.bytes) / (t.bytes / elapsed);
  if (!Number.isFinite(left) || left < 0) return `${t.state} · ${amount}`;
  return `${amount} · ${fmtDuration(left)} left`;
}

async function refreshTransfers() {
  try {
    const next = await api.listTransfers();
    transfers.value = next;
    const sys = active.value;
    const folder = paths.value[sys.id] ?? "";
    let reload = false;
    for (const t of next) {
      if (t.state === "running" || t.state === "waiting") continue;
      if (refreshedJobs.has(t.id)) continue;
      refreshedJobs.add(t.id);
      if (t.state === "done" && sameFolder(t.dest, sys, folder)) reload = true;
    }
    if (reload) void load();
  } catch {
    /* ignore polling errors */
  }
}

async function resolve(action: "overwrite" | "skip" | "rename") {
  if (!waiting.value) return;
  try {
    await api.resolveTransfer(waiting.value.id, action, applyAll.value);
    applyAll.value = false;
    await refreshTransfers();
  } catch (e) {
    err.value = e instanceof Error ? e.message : "Could not answer the transfer";
  }
}

function fileType(e: SftpEntry): string {
  if (isDir(e.st_mode)) return "Folder";
  const dot = e.filename.lastIndexOf(".");
  if (dot <= 0 || dot === e.filename.length - 1) return "File";
  return e.filename.slice(dot + 1).toUpperCase();
}

function fmtTime(unix: number): string {
  if (!unix) return "";
  return new Date(unix * 1000).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

watch(
  () => props.sessions.map((s) => s.tabId).join(","),
  () => {
    if (!systems.value.some((s) => s.id === activeId.value)) openSys("local");
  },
);

watch(
  () => props.open,
  (on) => {
    if (!on) return;
    err.value = "";
    void load();
  },
);

onMounted(() => {
  void load();
  void refreshTransfers();
  poll = setInterval(() => void refreshTransfers(), 400);
});

onUnmounted(() => {
  if (poll) clearInterval(poll);
  if (previewURL) URL.revokeObjectURL(previewURL);
});
</script>

<template>
  <div class="flex h-full min-h-0 bg-surface font-sans text-sm">
    <aside class="flex w-52 shrink-0 flex-col border-r border-slate-800 bg-surface-raised">
      <div class="px-3 py-2 text-xs font-medium uppercase tracking-wide text-slate-500">Systems</div>
      <button
        v-for="sys in systems"
        :key="sys.id"
        type="button"
        class="mx-2 mb-1 flex items-center gap-2 truncate rounded px-2 py-1.5 text-left text-xs"
        :class="sys.id === active.id ? 'bg-slate-800 text-white' : 'text-slate-300 hover:bg-slate-800/60'"
        @click="openSys(sys.id)"
        @dragover.prevent
        @drop.prevent="onNavDrop($event, sys)"
      >
        <HardDrive v-if="sys.kind === 'local'" class="h-3.5 w-3.5 shrink-0" />
        <Folder v-else class="h-3.5 w-3.5 shrink-0 text-accent" />
        <span class="truncate">{{ sys.label }}</span>
      </button>
    </aside>
    <section class="flex min-w-0 flex-1 flex-col">
      <div class="flex flex-wrap items-center gap-2 border-b border-slate-800 px-3 py-2">
        <div class="flex min-w-0 flex-1 flex-wrap items-center gap-1 font-mono text-xs text-slate-300">
          <template v-for="(c, i) in crumbs" :key="c.path + i">
            <span v-if="i > 0 && crumbs[i - 1].label !== '/' && crumbs[i - 1].label !== '\\'" class="text-slate-500">{{ c.path.includes("\\") ? "\\" : "/" }}</span>
            <button
              type="button"
              class="truncate rounded px-1 hover:bg-slate-800 hover:text-white"
              @click="go(c.path)"
            >
              {{ c.label }}
            </button>
          </template>
        </div>
        <button type="button" class="rounded bg-slate-800 p-1.5 text-slate-300 hover:text-white" title="Refresh" @click="load">
          <RefreshCw class="h-3.5 w-3.5" />
        </button>
        <button type="button" class="rounded bg-slate-800 p-1.5 text-slate-300 hover:text-white" title="New folder" @click="mkdir">
          <FolderPlus class="h-3.5 w-3.5" />
        </button>
        <button type="button" class="rounded bg-slate-800 p-1.5 text-slate-300 hover:text-white" title="Paste" @click="paste">
          <ClipboardPaste class="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          class="rounded px-2 py-1 text-xs"
          :class="showHidden ? 'bg-accent/20 text-accent' : 'bg-slate-800 text-slate-300'"
          @click="showHidden = !showHidden"
        >
          <Eye v-if="showHidden" class="h-3.5 w-3.5" />
          <span v-else class="inline-flex items-center gap-1"><Eye class="h-3.5 w-3.5 opacity-40" /> Hidden</span>
        </button>
        <button type="button" class="rounded bg-slate-800 p-1.5 text-slate-300" @click="grid = !grid">
          <LayoutGrid v-if="!grid" class="h-3.5 w-3.5" />
          <List v-else class="h-3.5 w-3.5" />
        </button>
        <select v-model="sortKey" class="rounded border border-slate-700 bg-surface px-2 py-1 text-xs">
          <option value="name">Name</option>
          <option value="size">Size</option>
          <option value="mtime">Modified</option>
        </select>
      </div>
      <p v-if="err" class="px-3 py-1 text-xs text-red-400">{{ err }}</p>
      <div class="min-h-0 flex-1 overflow-auto p-3" :class="grid ? 'grid auto-rows-max grid-cols-[repeat(auto-fill,6.75rem)] content-start justify-start gap-2' : 'flex flex-col gap-0.5'">
        <p v-if="busy" class="col-span-full text-xs text-slate-500">Loading…</p>
        <button
          v-for="e in shown"
          :key="e.filename"
          type="button"
          draggable="true"
          :title="e.filename"
          class="group relative flex min-w-0 text-left hover:bg-surface-overlay"
          :class="grid ? 'h-[4.75rem] w-full shrink-0 flex-col items-center justify-center gap-1 self-start rounded-xl border border-slate-800/80 bg-surface-raised px-1.5 py-2' : 'w-full items-center gap-2 rounded-lg px-2 py-1'"
          @click="enter(e)"
          @dragstart="onDragStart($event, e)"
        >
          <Folder v-if="isDir(e.st_mode)" class="shrink-0 text-accent" :class="grid ? 'h-5 w-5' : 'h-4 w-4'" />
          <File v-else class="shrink-0 text-slate-400" :class="grid ? 'h-5 w-5' : 'h-4 w-4'" />
          <input
            v-if="renameTarget === e.filename"
            v-model="newName"
            class="w-full min-w-0 rounded-lg border border-slate-700 bg-surface px-1 py-0.5 text-center font-mono text-xs"
            @click.stop
            @keydown.enter.prevent="commitRename"
            @blur="commitRename"
          />
          <span v-else class="w-full min-w-0 truncate text-xs" :class="grid ? 'text-center' : 'flex-1 font-mono'">{{ e.filename }}</span>
          <span v-if="!grid" class="w-16 shrink-0 truncate text-[10px] text-slate-400">{{ fileType(e) }}</span>
          <span v-if="!grid" class="w-16 shrink-0 text-right text-[10px] tabular-nums text-slate-500">{{ isDir(e.st_mode) ? "" : fmtSize(e.st_size) }}</span>
          <span v-if="!grid" class="w-40 shrink-0 text-right text-[10px] tabular-nums text-slate-400">{{ fmtTime(e.st_mtime) }}</span>
          <span class="flex gap-0.5 opacity-0 group-hover:opacity-100" :class="grid ? 'absolute right-1 top-1 rounded-lg bg-surface-raised/90' : ''" @click.stop>
            <button type="button" class="rounded p-1 text-slate-400 hover:text-white" title="Copy" @click="copyEntry(e)"><Copy class="h-3.5 w-3.5" /></button>
            <button v-if="!isDir(e.st_mode)" type="button" class="rounded p-1 text-slate-400 hover:text-white" title="Download" @click="downloadEntry(e)"><Download class="h-3.5 w-3.5" /></button>
            <button type="button" class="rounded p-1 text-slate-400 hover:text-white" title="Rename" @click="startRename(e)"><Pencil class="h-3.5 w-3.5" /></button>
            <button type="button" class="rounded p-1 text-slate-400 hover:text-red-400" title="Delete" @click="removeEntry(e)"><Trash2 class="h-3.5 w-3.5" /></button>
          </span>
        </button>
      </div>
      <div v-if="preview" class="max-h-64 shrink-0 overflow-auto border-t border-slate-800 p-3">
        <div class="mb-2 flex items-center justify-between text-xs text-slate-400">
          <span>{{ preview.filename }}</span>
          <button type="button" @click="clearPreview"><X class="h-3.5 w-3.5" /></button>
        </div>
        <img v-if="previewKind(preview) === 'image' && previewSrc" :src="previewSrc" class="max-h-48" alt="" />
        <iframe v-else-if="previewKind(preview) === 'pdf' && previewSrc" :src="previewSrc" class="h-48 w-full bg-white"></iframe>
        <audio v-else-if="previewKind(preview) === 'audio' && previewSrc" :src="previewSrc" controls class="w-full"></audio>
        <pre v-else class="whitespace-pre-wrap font-mono text-xs text-slate-200">{{ previewText }}</pre>
      </div>
      <div class="max-h-36 shrink-0 overflow-auto border-t border-slate-800 px-3 py-2">
        <div class="mb-1 text-[10px] uppercase tracking-wide text-slate-500">Transfers</div>
        <p v-if="!transfers.length" class="text-xs text-slate-600">No transfers</p>
        <div v-for="t in transfers" :key="t.id" class="mb-1 flex items-center gap-2 text-xs text-slate-300">
          <div class="h-1.5 min-w-0 flex-1 overflow-hidden rounded bg-slate-800">
            <div
              class="h-full bg-accent"
              :style="{ width: t.total ? Math.min(100, Math.round((t.bytes / t.total) * 100)) + '%' : '0%' }"
            ></div>
          </div>
          <span class="shrink-0 tabular-nums">{{ transferLabel(t) }}</span>
          <button v-if="t.state === 'running' || t.state === 'waiting'" type="button" class="text-slate-500 hover:text-red-400" @click="api.cancelTransfer(t.id)">
            <X class="h-3 w-3" />
          </button>
        </div>
      </div>
    </section>
    <div v-if="waiting" class="vantage-modal fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div class="w-full max-w-md rounded-xl border border-slate-800 bg-surface-raised p-4">
        <h2 class="text-sm font-medium text-white">File already exists</h2>
        <p class="mt-2 break-all font-mono text-xs text-slate-400">{{ waiting.conflict }}</p>
        <label class="mt-3 flex items-center gap-2 text-xs text-slate-300">
          <input v-model="applyAll" type="checkbox" />
          Apply to all remaining files in this transfer
        </label>
        <div class="mt-4 flex justify-end gap-2">
          <button type="button" class="rounded bg-slate-800 px-3 py-1.5 text-xs" @click="resolve('skip')">Skip</button>
          <button type="button" class="rounded bg-slate-800 px-3 py-1.5 text-xs" @click="resolve('rename')">Keep both</button>
          <button type="button" class="rounded bg-accent px-3 py-1.5 text-xs font-medium text-slate-950" @click="resolve('overwrite')">Overwrite</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from "vue";
import { File, Folder } from "@lucide/vue";
import { api, apiFetch, type SftpEntry } from "@/api";

const props = defineProps<{
  kind?: "sftp" | "shared";
  connId?: string;
  hostId?: string;
}>();

const kind = () => props.kind || "sftp";

const path = ref("/");
const entries = ref<SftpEntry[]>([]);
const err = ref("");
const busy = ref(false);
const renameTarget = ref<SftpEntry | null>(null);
const newName = ref("");
const uploadProgress = ref<number | null>(null);

function isDir(m: number): boolean {
  return (m & 0o170000) === 0o040000;
}

function joinRemote(base: string, name: string): string {
  if (base === "/") return `/${name}`.replace("//", "/");
  return `${base.replace(/\/$/, "")}/${name}`;
}

async function load() {
  err.value = "";
  busy.value = true;
  try {
    const r =
      kind() === "shared"
        ? await api.sharedList(props.hostId || "", path.value)
        : await api.sftpList(props.connId || "", path.value);
    path.value = r.path;
    entries.value = r.entries;
  } catch (e) {
    err.value = e instanceof Error ? e.message : "List failed";
  } finally {
    busy.value = false;
  }
}

watch(
  () => [props.connId, props.hostId, props.kind],
  () => {
    path.value = "/";
    load();
  },
  { immediate: true },
);

function enter(e: SftpEntry) {
  const p = joinRemote(path.value, e.filename);
  if (isDir(e.st_mode)) {
    path.value = p;
    load();
  }
}

function parent() {
  if (path.value === "/") return;
  const parts = path.value.replace(/\/$/, "").split("/");
  parts.pop();
  path.value = parts.length ? parts.join("/") || "/" : "/";
  load();
}

async function onUpload(ev: Event) {
  const input = ev.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  err.value = "";
  uploadProgress.value = 0;
  try {
    if (kind() === "shared") {
      await api.sharedUpload(props.hostId || "", path.value, file, (loaded, total) => {
        uploadProgress.value = Math.round((loaded / total) * 100);
      });
    } else {
      await api.sftpUpload(props.connId || "", path.value, file, (loaded, total) => {
        uploadProgress.value = Math.round((loaded / total) * 100);
      });
    }
    await load();
  } catch (e) {
    err.value = e instanceof Error ? e.message : "Upload failed";
  } finally {
    uploadProgress.value = null;
  }
}

async function downloadFile(e: SftpEntry) {
  const p = joinRemote(path.value, e.filename);
  err.value = "";
  try {
    const url =
      kind() === "shared"
        ? api.sharedDownloadUrl(props.hostId || "", p)
        : api.sftpDownloadUrl(props.connId || "", p);
    const res = await apiFetch(url);
    if (!res.ok) throw new Error(await res.text());
    const blob = await res.blob();
    const href = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = href;
    a.download = e.filename;
    a.click();
    URL.revokeObjectURL(href);
  } catch (e) {
    err.value = e instanceof Error ? e.message : "Download failed";
  }
}

async function removeEntry(e: SftpEntry) {
  if (!confirm(`Delete ${e.filename}?`)) return;
  const p = joinRemote(path.value, e.filename);
  err.value = "";
  try {
    if (kind() === "shared") await api.sharedRemove(props.hostId || "", p);
    else await api.sftpRemove(props.connId || "", p);
    await load();
  } catch (err2) {
    err.value = err2 instanceof Error ? err2.message : "Remove failed";
  }
}

function startRename(e: SftpEntry) {
  renameTarget.value = e;
  newName.value = e.filename;
}

async function confirmRename() {
  if (!renameTarget.value) return;
  const oldP = joinRemote(path.value, renameTarget.value.filename);
  const newP = joinRemote(path.value, newName.value.trim());
  renameTarget.value = null;
  err.value = "";
  try {
    if (kind() === "shared") await api.sharedRename(props.hostId || "", oldP, newP);
    else await api.sftpRename(props.connId || "", oldP, newP);
    await load();
  } catch (e) {
    err.value = e instanceof Error ? e.message : "Rename failed";
  }
}

async function mkdir() {
  const name = prompt("Folder name");
  if (!name?.trim()) return;
  const p = joinRemote(path.value, name.trim());
  err.value = "";
  try {
    if (kind() === "shared") await api.sharedMkdir(props.hostId || "", p);
    else await api.sftpMkdir(props.connId || "", p);
    await load();
  } catch (e) {
    err.value = e instanceof Error ? e.message : "mkdir failed";
  }
}

function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col bg-surface-raised font-sans text-sm">
    <div class="border-b border-slate-800 px-3 py-2">
      <div class="text-xs font-medium uppercase tracking-wide text-slate-500">
        {{ kind() === 'shared' ? 'Shared files' : 'SFTP' }}
      </div>
      <div class="mt-1 truncate font-mono text-xs text-slate-300" :title="path">
        {{ path }}
      </div>
      <div class="mt-2 flex flex-wrap gap-1">
        <button
          type="button"
          class="rounded bg-slate-800 px-2 py-1 text-xs hover:bg-slate-700"
          @click="parent"
        >
          Up
        </button>
        <button
          type="button"
          class="rounded bg-slate-800 px-2 py-1 text-xs hover:bg-slate-700"
          @click="load"
        >
          Refresh
        </button>
        <button
          type="button"
          class="rounded bg-slate-800 px-2 py-1 text-xs hover:bg-slate-700"
          @click="mkdir"
        >
          New folder
        </button>
        <label
          class="cursor-pointer rounded bg-accent/20 px-2 py-1 text-xs text-accent hover:bg-accent/30"
        >
          Upload
          <input type="file" class="hidden" @change="onUpload" />
        </label>
      </div>
    </div>
    <div class="min-h-0 flex-1 overflow-auto p-2">
      <p v-if="err" class="mb-2 text-xs text-red-400">{{ err }}</p>
      <div v-if="uploadProgress !== null" class="mb-2 space-y-1">
        <div class="flex justify-between text-[10px] text-slate-400">
          <span>Uploading...</span>
          <span>{{ uploadProgress }}%</span>
        </div>
        <div class="h-1.5 w-full overflow-hidden rounded-full bg-slate-800">
          <div
            class="h-full bg-accent transition-all duration-200"
            :style="{ width: uploadProgress + '%' }"
          ></div>
        </div>
      </div>
      <p v-else-if="busy" class="text-xs text-slate-500">Loading…</p>
      <ul v-else class="space-y-0.5">
        <li
          v-for="e in entries"
          :key="e.filename"
          class="group flex items-center gap-2 rounded px-2 py-1 hover:bg-surface-overlay"
        >
          <button
            type="button"
            class="min-w-0 flex-1 truncate text-left font-mono text-xs"
            :class="isDir(e.st_mode) ? 'text-accent' : 'text-slate-200'"
            @click="enter(e)"
          >
            <span class="inline-flex items-center gap-1.5">
              <Folder
                v-if="isDir(e.st_mode)"
                class="h-3.5 w-3.5 shrink-0"
                aria-hidden="true"
              />
              <File
                v-else
                class="h-3.5 w-3.5 shrink-0"
                aria-hidden="true"
              />
              <span>{{ e.filename }}</span>
            </span>
          </button>
          <span class="shrink-0 text-[10px] text-slate-500">{{
            isDir(e.st_mode) ? "" : fmtSize(e.st_size)
          }}</span>
          <button
            v-if="!isDir(e.st_mode)"
            type="button"
            class="shrink-0 text-[10px] text-slate-500 opacity-0 group-hover:opacity-100 hover:text-accent"
            @click="downloadFile(e)"
          >
            Get
          </button>
          <button
            type="button"
            class="shrink-0 text-[10px] text-slate-500 opacity-0 group-hover:opacity-100 hover:text-accent"
            @click="startRename(e)"
          >
            Ren
          </button>
          <button
            type="button"
            class="shrink-0 text-[10px] text-red-400/80 opacity-0 group-hover:opacity-100"
            @click="removeEntry(e)"
          >
            Del
          </button>
        </li>
      </ul>
    </div>
    <div
      v-if="renameTarget"
      class="border-t border-slate-800 p-2"
    >
      <input
        v-model="newName"
        class="mb-2 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1 font-mono text-xs"
        @keyup.enter="confirmRename"
      />
      <div class="flex gap-2">
        <button
          type="button"
          class="rounded bg-accent px-2 py-1 text-xs text-slate-950"
          @click="confirmRename"
        >
          OK
        </button>
        <button
          type="button"
          class="rounded bg-slate-800 px-2 py-1 text-xs"
          @click="renameTarget = null"
        >
          Cancel
        </button>
      </div>
    </div>
  </div>
</template>

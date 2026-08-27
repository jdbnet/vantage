<script setup lang="ts">
import {
  onMounted,
  onUnmounted,
  ref,
  watch,
  nextTick,
} from "vue";
import { api, sessionToken, wsOrigin, wsURL, type HostProtocol, type Settings } from "@/api";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import SftpPanel from "./SftpPanel.vue";
import Guacamole from "guacamole-common-js";
import { Maximize2, Minimize2, Search, X } from "@lucide/vue";
import { addGuacKeySink, removeGuacKeySink, type GuacKeySink } from "@/guacKeyboard";

const CLIPBOARD_MAX = 1024 * 1024;

const props = defineProps<{
  hostId: string;
  protocol: HostProtocol;
  visible: boolean;
  showSftp: boolean;
  settings: Settings | null;
}>();

const emit = defineEmits<{
  (e: "broadcast-data", data: string): void;
}>();

defineExpose({
  sendData: (data: string) => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(new TextEncoder().encode(data));
    }
  },
  reconnect,
});

type HostKeyPrompt = {
  hostname: string;
  port: number;
  fingerprint: string;
  key_type: string;
  status: string;
  previous?: string;
};

const termEl = ref<HTMLElement | null>(null);
const guacEl = ref<HTMLElement | null>(null);
const sessionPane = ref<HTMLElement | null>(null);
const searchInput = ref<HTMLInputElement | null>(null);
const status = ref("Connecting…");
const connId = ref<string | null>(null);
const serverBackOnline = ref(false);
const hostKeyPrompt = ref<HostKeyPrompt | null>(null);
const showSearch = ref(false);
const searchQuery = ref("");
const isFullscreen = ref(false);

let ws: WebSocket | null = null;
let term: Terminal | null = null;
let fit: FitAddon | null = null;
let searchAddon: SearchAddon | null = null;
let ro: ResizeObserver | null = null;
let visibilityHandler: (() => void) | null = null;
let pingInterval: number | null = null;
let guacClient: InstanceType<typeof Guacamole.Client> | null = null;
let guacKeySink: GuacKeySink | null = null;
let guacPasteHandler: ((ev: ClipboardEvent) => void) | null = null;
let guacFocusHandler: ((ev: Event) => void) | null = null;
let guacOutsideClick: ((ev: MouseEvent) => void) | null = null;
let guacInputActive = false;
let guacResizeTimer: number | null = null;
let fullscreenHandler: (() => void) | null = null;
let sessionAlive = true;

function clearPingInterval() {
  if (pingInterval) {
    clearInterval(pingInterval);
    pingInterval = null;
  }
}

function closeSocket() {
  if (!ws) return;
  const socket = ws;
  ws = null;
  socket.onopen = null;
  socket.onmessage = null;
  socket.onerror = null;
  socket.onclose = null;
  try {
    socket.close();
  } catch {
    /* ignore */
  }
}

const isSSH = () => props.protocol === "ssh";

function terminalTheme() {
  const name = props.settings?.terminal_theme || "default";
  if (name === "light") {
    return {
      background: "#f6f8fa",
      foreground: "#1f2328",
      cursor: "#1ebe8a",
      selectionBackground: "rgba(30, 190, 138, 0.3)",
    };
  }
  if (name === "solarized") {
    return {
      background: "#002b36",
      foreground: "#839496",
      cursor: "#268bd2",
      selectionBackground: "rgba(38, 139, 210, 0.3)",
    };
  }
  return {
    background: "#0d1117",
    foreground: "#e6edf3",
    cursor: "#1ebe8a",
    selectionBackground: "rgba(30, 190, 138, 0.3)",
  };
}

function wsUrl(hostId: string): string {
  const path = isSSH() ? "/ws/terminal" : "/ws/guac";
  return wsURL(path, { host_id: hostId });
}

function sendResize() {
  if (!ws || ws.readyState !== WebSocket.OPEN || !term) return;
  const dims = { cols: term.cols, rows: term.rows };
  ws.send(JSON.stringify({ type: "resize", ...dims }));
}

function sendPing() {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: "ping" }));
  }
}

function fitAndResize() {
  if (!fit || !term || !props.visible) return;
  try {
    fit.fit();
    sendResize();
  } catch {
    /* ignore */
  }
}

function isControlMessage(raw: string): boolean {
  try {
    const o = JSON.parse(raw) as {
      type?: string;
      conn_id?: string;
      hostname?: string;
      port?: number;
      fingerprint?: string;
      key_type?: string;
      status?: string;
      previous?: string;
      error?: string;
    };
    if (o.type === "ready" && o.conn_id) {
      connId.value = o.conn_id;
      status.value = "";
      fitAndResize();
      term?.focus();
      return true;
    }
    if (o.type === "hostkey") {
      hostKeyPrompt.value = {
        hostname: o.hostname || "",
        port: o.port || 22,
        fingerprint: o.fingerprint || "",
        key_type: o.key_type || "",
        status: o.status || "new",
        previous: o.previous,
      };
      return true;
    }
    if (o.type === "error") {
      status.value = o.error || "Connection failed";
      return true;
    }
    if (o.type === "keepalive" || o.type === "pong") {
      return true;
    }
  } catch {
    /* not JSON control traffic */
  }
  return false;
}

function replyHostKey(accept: boolean, replace: boolean) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: "hostkey-reply", accept, replace }));
  }
  hostKeyPrompt.value = null;
}

function connectSsh() {
  serverBackOnline.value = false;
  hostKeyPrompt.value = null;
  clearPingInterval();
  closeSocket();
  if (!sessionAlive) return;

  ws = new WebSocket(wsUrl(props.hostId));
  ws.binaryType = "arraybuffer";

  ws.onopen = () => {
    if (!sessionAlive) return;
    status.value = "Handshaking…";
    sendResize();
  };

  ws.onmessage = (ev) => {
    if (!term) return;
    if (typeof ev.data === "string") {
      if (isControlMessage(ev.data)) return;
      term.write(ev.data);
      return;
    }
    const u8 = new Uint8Array(ev.data as ArrayBuffer);
    const text = new TextDecoder().decode(u8);
    if (text.startsWith("{") && isControlMessage(text)) return;
    term.write(text);
    if (!connId.value) {
      status.value = "";
      term.focus();
    }
  };

  ws.onerror = () => {
    if (!sessionAlive) return;
    status.value = "WebSocket error";
  };

  ws.onclose = () => {
    if (!sessionAlive) return;
    hostKeyPrompt.value = null;
    if (connId.value) {
      status.value = "Session ended";
    } else if (
      status.value === "Handshaking…" ||
      status.value === "Connecting…" ||
      status.value === ""
    ) {
      status.value = "Disconnected";
    }

    clearPingInterval();
    pingInterval = window.setInterval(async () => {
      if (!sessionAlive) {
        clearPingInterval();
        return;
      }
      try {
        const { up } = await api.pingHost(props.hostId);
        if (up) {
          clearPingInterval();
          serverBackOnline.value = true;
        }
      } catch {
        /* ignore */
      }
    }, 5000);
  };
}

function guacStatusText(e: { message?: string; code?: number } | undefined): string {
  const msg = e?.message?.trim();
  if (msg) return msg;
  switch (e?.code) {
    case 0x0203:
      return "guacd or remote desktop error";
    case 0x0207:
      return "Could not open the display websocket";
    case 0x0208:
      return "Display server unavailable";
    case 0x0200:
      return "Display connection failed";
    default:
      return "Display error";
  }
}

function guacViewport(): { width: number; height: number } {
  const el = guacEl.value;
  const fallbackW = props.settings?.display_width || 1920;
  const fallbackH = props.settings?.display_height || 1080;
  const width = Math.max(el?.clientWidth || 0, 0);
  const height = Math.max(el?.clientHeight || 0, 0);
  return {
    width: width >= 320 ? width : fallbackW,
    height: height >= 240 ? height : fallbackH,
  };
}

function fitGuacDisplay() {
  const el = guacEl.value;
  const display = guacClient?.getDisplay();
  if (!el || !display) return;
  const dw = display.getWidth();
  const dh = display.getHeight();
  if (!dw || !dh) return;
  const scale = Math.min(el.clientWidth / dw, el.clientHeight / dh);
  if (scale > 0 && Number.isFinite(scale)) {
    display.scale(scale);
  }
}

function scheduleGuacRemoteResize() {
  if (guacResizeTimer) {
    window.clearTimeout(guacResizeTimer);
  }
  guacResizeTimer = window.setTimeout(() => {
    if (!guacClient) return;
    const { width, height } = guacViewport();
    guacClient.sendSize(width, height);
    fitGuacDisplay();
  }, 200);
}

function guacDisplayFocused(): boolean {
  return guacInputActive && props.visible;
}

function isEditableTarget(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
}

function armGuacInput() {
  const wrap = guacEl.value;
  guacInputActive = true;
  wrap?.focus({ preventScroll: true });
}

function sendGuacClipboard(text: string) {
  if (!guacClient || text.length > CLIPBOARD_MAX) return;
  const stream = guacClient.createClipboardStream("text/plain");
  const writer = new Guacamole.StringWriter(stream);
  writer.sendText(text);
  writer.sendEnd();
}

function disposeGuacInput() {
  const wrap = guacEl.value;
  if (guacKeySink) {
    removeGuacKeySink(guacKeySink);
    guacKeySink = null;
  }
  if (guacPasteHandler && wrap) {
    wrap.removeEventListener("paste", guacPasteHandler);
  }
  guacPasteHandler = null;
  if (guacFocusHandler && wrap) {
    wrap.removeEventListener("mousedown", guacFocusHandler);
  }
  guacFocusHandler = null;
  if (guacOutsideClick) {
    document.removeEventListener("mousedown", guacOutsideClick, true);
    guacOutsideClick = null;
  }
  guacInputActive = false;
}

function attachGuacInput(displayEl: HTMLElement) {
  const wrap = guacEl.value;
  if (!wrap) return;
  wrap.tabIndex = 0;
  if (!guacFocusHandler) {
    guacFocusHandler = () => armGuacInput();
    wrap.addEventListener("mousedown", guacFocusHandler);
  }
  if (!guacOutsideClick) {
    guacOutsideClick = (ev: MouseEvent) => {
      if (!wrap.contains(ev.target as Node)) {
        guacInputActive = false;
      }
    };
    document.addEventListener("mousedown", guacOutsideClick, true);
  }
  if (!guacKeySink) {
    guacKeySink = {
      isActive: () => guacDisplayFocused() && !isEditableTarget(document.activeElement),
      keydown: (keysym: number) => {
        guacClient?.sendKeyEvent(1, keysym);
      },
      keyup: (keysym: number) => {
        guacClient?.sendKeyEvent(0, keysym);
      },
    };
    addGuacKeySink(guacKeySink);
  }
  if (!guacPasteHandler) {
    guacPasteHandler = (ev: ClipboardEvent) => {
      if (!guacDisplayFocused()) return;
      const text = ev.clipboardData?.getData("text/plain") || "";
      if (!text || text.length > CLIPBOARD_MAX) return;
      ev.preventDefault();
      sendGuacClipboard(text);
    };
    wrap.addEventListener("paste", guacPasteHandler);
  }
  if (guacClient) {
    guacClient.onclipboard = (stream: unknown, mimetype: string) => {
      if (!mimetype || !mimetype.startsWith("text/")) return;
      const reader = new Guacamole.StringReader(stream);
      let text = "";
      reader.ontext = (chunk: string) => {
        text += chunk;
      };
      reader.onend = () => {
        if (!text || text.length > CLIPBOARD_MAX) return;
        void navigator.clipboard.writeText(text).catch(() => {
          /* ignore */
        });
      };
    };
  }
  const mouse = new Guacamole.Mouse(displayEl);
  mouse.onmousedown = mouse.onmouseup = mouse.onmousemove = (mouseState: unknown) => {
    armGuacInput();
    guacClient?.sendMouseState(mouseState as never, true);
  };
}

function connectGuac() {
  serverBackOnline.value = false;
  status.value = "Connecting to display…";
  if (!guacEl.value || !sessionAlive) return;
  try {
    guacClient?.disconnect();
  } catch {
    /* ignore */
  }
  guacClient = null;
  guacEl.value.replaceChildren();
  const tunnel = new Guacamole.WebSocketTunnel(`${wsOrigin()}/ws/guac`);
  const onGuacError = (e: { message?: string; code?: number }) => {
    if (!sessionAlive) return;
    status.value = guacStatusText(e);
  };
  tunnel.onerror = onGuacError;
  guacClient = new Guacamole.Client(tunnel);
  const guacDisplay = guacClient.getDisplay();
  const displayEl = guacDisplay.getElement();
  guacEl.value.appendChild(displayEl);
  guacDisplay.onresize = () => fitGuacDisplay();
  guacClient.onerror = onGuacError;
  guacClient.onstatechange = (state: number) => {
    if (state === 3) {
      status.value = "";
      fitGuacDisplay();
      armGuacInput();
    } else if (state === 5 && !status.value) {
      status.value = "Display disconnected";
    }
  };
  attachGuacInput(displayEl);
  const { width, height } = guacViewport();
  try {
    const q = new URLSearchParams({
      host_id: props.hostId,
      width: String(width),
      height: String(height),
    });
    const tok = sessionToken();
    if (tok) q.set("token", tok);
    guacClient.connect(q.toString());
  } catch (e) {
    status.value = e instanceof Error ? e.message : "Failed to connect";
  }
}

function reconnect() {
  term?.clear();
  term?.focus();
  connId.value = null;
  hostKeyPrompt.value = null;
  status.value = "Connecting…";
  if (isSSH()) connectSsh();
  else connectGuac();
}

function openSearch() {
  showSearch.value = true;
  nextTick(() => searchInput.value?.focus());
}

function runSearch(dir: "next" | "prev") {
  const q = searchQuery.value;
  if (!searchAddon || !q) return;
  if (dir === "next") searchAddon.findNext(q);
  else searchAddon.findPrevious(q);
}

async function toggleFullscreen() {
  const el = sessionPane.value;
  if (!el) return;
  try {
    if (document.fullscreenElement) {
      await document.exitFullscreen();
    } else {
      await el.requestFullscreen();
    }
  } catch {
    /* ignore */
  }
}

function onFullscreenChange() {
  isFullscreen.value = document.fullscreenElement === sessionPane.value;
  nextTick(() => {
    fitAndResize();
    fitGuacDisplay();
    scheduleGuacRemoteResize();
  });
}

onMounted(async () => {
  await nextTick();
  if (isSSH()) {
    if (!termEl.value) return;
    term = new Terminal({
      cursorBlink: true,
      scrollback: 1000,
      fontFamily: props.settings?.terminal_font_family || "DM Mono, ui-monospace, monospace",
      fontSize: props.settings?.terminal_font_size || 14,
      theme: terminalTheme(),
    });
    fit = new FitAddon();
    searchAddon = new SearchAddon();
    term.loadAddon(fit);
    term.loadAddon(searchAddon);
    term.loadAddon(
      new WebLinksAddon((_ev, uri) => {
        window.open(uri, "_blank", "noopener,noreferrer");
      }),
    );
    term.open(termEl.value);
    fit.fit();

    term.attachCustomKeyEventHandler((ev) => {
      if (ev.ctrlKey && ev.shiftKey && ev.key.toLowerCase() === "f") {
        if (ev.type === "keydown") {
          ev.preventDefault();
          openSearch();
        }
        return false;
      }
      return true;
    });

    term.onData((data) => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data));
      }
      emit("broadcast-data", data);
    });

    term.onResize(({ cols, rows }) => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols, rows }));
      }
    });

    ro = new ResizeObserver(() => fitAndResize());
    ro.observe(termEl.value);
    connectSsh();
  } else {
    await nextTick();
    connectGuac();
    if (guacEl.value) {
      ro = new ResizeObserver(() => {
        fitGuacDisplay();
        scheduleGuacRemoteResize();
      });
      ro.observe(guacEl.value);
    }
  }

  visibilityHandler = () => {
    if (document.visibilityState === "visible") {
      sendPing();
      fitAndResize();
      fitGuacDisplay();
    }
  };
  document.addEventListener("visibilitychange", visibilityHandler);
  fullscreenHandler = onFullscreenChange;
  document.addEventListener("fullscreenchange", fullscreenHandler);
});

onUnmounted(() => {
  sessionAlive = false;
  clearPingInterval();
  if (guacResizeTimer) {
    window.clearTimeout(guacResizeTimer);
    guacResizeTimer = null;
  }
  if (visibilityHandler) {
    document.removeEventListener("visibilitychange", visibilityHandler);
    visibilityHandler = null;
  }
  if (fullscreenHandler) {
    document.removeEventListener("fullscreenchange", fullscreenHandler);
    fullscreenHandler = null;
  }
  disposeGuacInput();
  ro?.disconnect();
  ro = null;
  closeSocket();
  try {
    term?.dispose();
  } catch {
    /* ignore */
  }
  term = null;
  fit = null;
  searchAddon = null;
  try {
    guacClient?.disconnect();
  } catch {
    /* ignore */
  }
  guacClient = null;
  guacEl.value?.replaceChildren();
  termEl.value?.replaceChildren();
});

watch(
  () => props.visible,
  async (v) => {
    if (v) {
      await nextTick();
      fitAndResize();
      fitGuacDisplay();
      term?.focus();
      sendPing();
    }
  },
);
</script>

<template>
  <div class="flex h-full min-h-0 gap-0">
    <div ref="sessionPane" class="relative flex h-full min-h-0 min-w-0 flex-1 flex-col bg-surface">
      <div
        v-if="status"
        class="absolute left-3 top-2 z-10 rounded bg-black/70 px-2 py-1 font-mono text-xs text-amber-200"
      >
        {{ status }}
      </div>
      <button
        type="button"
        class="absolute right-3 top-2 z-10 rounded bg-black/70 p-1.5 text-slate-300 hover:bg-black/80 hover:text-white"
        :title="isFullscreen ? 'Exit fullscreen' : 'Fullscreen'"
        @click="toggleFullscreen"
      >
        <Minimize2 v-if="isFullscreen" class="h-4 w-4" />
        <Maximize2 v-else class="h-4 w-4" />
      </button>
      <div
        v-if="showSearch && protocol === 'ssh'"
        class="absolute left-3 right-12 top-2 z-20 flex items-center gap-1 rounded border border-slate-700 bg-slate-900/95 p-1"
      >
        <Search class="h-3.5 w-3.5 shrink-0 text-slate-500" />
        <input
          ref="searchInput"
          v-model="searchQuery"
          class="min-w-0 flex-1 bg-transparent px-1 py-0.5 font-mono text-xs text-white outline-none"
          placeholder="Find"
          @keydown.enter.prevent="runSearch($event.shiftKey ? 'prev' : 'next')"
          @keydown.escape.prevent="showSearch = false"
        />
        <button type="button" class="rounded px-1.5 py-0.5 text-[10px] text-slate-400 hover:bg-slate-800" @click="runSearch('prev')">
          Prev
        </button>
        <button type="button" class="rounded px-1.5 py-0.5 text-[10px] text-slate-400 hover:bg-slate-800" @click="runSearch('next')">
          Next
        </button>
        <button type="button" class="rounded p-0.5 text-slate-400 hover:bg-slate-800" @click="showSearch = false">
          <X class="h-3.5 w-3.5" />
        </button>
      </div>
      <div
        v-if="hostKeyPrompt"
        class="absolute inset-0 z-30 flex items-center justify-center bg-black/70 p-4"
      >
        <div class="w-full max-w-md rounded-xl border border-slate-700 bg-surface-raised p-5 shadow-xl">
          <h3 class="text-sm font-semibold text-white">SSH host key</h3>
          <p class="mt-2 text-sm text-slate-300">
            <template v-if="hostKeyPrompt.status === 'mismatch'">
              The host key for {{ hostKeyPrompt.hostname }}:{{ hostKeyPrompt.port }} does not match the stored key.
            </template>
            <template v-else>
              The host key for {{ hostKeyPrompt.hostname }}:{{ hostKeyPrompt.port }} is not in your inventory.
            </template>
          </p>
          <p class="mt-3 font-mono text-xs text-slate-400">
            {{ hostKeyPrompt.key_type }} {{ hostKeyPrompt.fingerprint }}
          </p>
          <p v-if="hostKeyPrompt.previous" class="mt-2 font-mono text-[11px] text-amber-300/90">
            Stored: {{ hostKeyPrompt.previous }}
          </p>
          <div class="mt-5 flex flex-wrap justify-end gap-2">
            <button
              type="button"
              class="rounded-lg px-3 py-1.5 text-sm text-slate-400 hover:bg-slate-800"
              @click="replyHostKey(false, false)"
            >
              Reject
            </button>
            <button
              v-if="hostKeyPrompt.status === 'mismatch'"
              type="button"
              class="rounded-lg bg-amber-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-amber-500"
              @click="replyHostKey(true, true)"
            >
              Replace
            </button>
            <button
              v-if="hostKeyPrompt.status !== 'mismatch'"
              type="button"
              class="rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-slate-950"
              @click="replyHostKey(true, false)"
            >
              Accept
            </button>
          </div>
        </div>
      </div>
      <div
        v-if="serverBackOnline"
        class="absolute bottom-4 right-4 z-20 flex items-center gap-3 rounded bg-slate-800 px-4 py-3 text-sm shadow-lg border border-slate-700"
      >
        <span class="text-slate-200">Server is back online</span>
        <button
          @click="reconnect"
          class="rounded bg-emerald-600 px-3 py-1.5 font-medium text-white hover:bg-emerald-500 transition-colors"
        >
          Reconnect
        </button>
      </div>
      <div
        v-if="protocol === 'ssh'"
        ref="termEl"
        class="h-full min-h-[320px] rounded-lg border border-slate-800 bg-[#0d1117] p-1"
      />
      <div
        v-else
        ref="guacEl"
        tabindex="0"
        class="flex h-full min-h-[320px] items-center justify-center overflow-hidden rounded-lg border border-slate-800 bg-black outline-none"
      />
    </div>
    <template v-if="showSftp && protocol === 'ssh'">
      <div
        v-if="connId"
        class="hidden h-full w-80 shrink-0 flex-col border-l border-slate-800 md:flex"
      >
        <SftpPanel kind="sftp" :conn-id="connId" />
      </div>
      <div
        v-else
        class="hidden w-72 shrink-0 items-center justify-center border-l border-slate-800 bg-surface-raised text-xs text-slate-500 md:flex"
      >
        SFTP unlocks when the shell session is ready.
      </div>
    </template>
    <template v-else-if="showSftp && protocol === 'rdp'">
      <div class="hidden h-full w-80 shrink-0 flex-col border-l border-slate-800 md:flex">
        <SftpPanel kind="shared" :host-id="hostId" />
      </div>
    </template>
  </div>
</template>

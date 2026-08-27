<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { Folder, Pencil, Trash2, Radio, ChevronDown, Settings } from "@lucide/vue";
import {
  api,
  type HostRow,
  type IdentityRow,
  type FolderRow,
  type ConnectionAuditRow,
  type ApiKeyRow,
  type ApiKeyScopeDef,
  type SnippetRow,
  type HostProtocol,
  type Settings as AppSettings,
} from "@/api";
import LoginForm from "@/components/LoginForm.vue";
import TabContent from "@/components/TabContent.vue";
import TagInput from "@/components/TagInput.vue";
import SnippetForm from "@/components/SnippetForm.vue";

interface TabItem {
  id: string;
  hostId: string;
  protocol: HostProtocol;
  label: string;
}

const loggedIn = ref(false);
const checking = ref(true);
const appVersion = ref("unknown");
const auditLogEnabled = ref(true);
const identities = ref<IdentityRow[]>([]);
const allHosts = ref<HostRow[]>([]);
const allFolders = ref<FolderRow[]>([]);
const allTags = ref<string[]>([]);
const browseFolders = ref<FolderRow[]>([]);
const browseHosts = ref<HostRow[]>([]);
const breadcrumb = ref<{ id: string; label: string }[]>([]);
const searchActive = ref(false);
const currentFolderId = ref<string | null>(null);
const searchQuery = ref("");
const searchInput = ref<HTMLInputElement | null>(null);
const tabs = ref<{ id: string; hostId: string; protocol: HostProtocol; label: string }[]>([]);
const activePanes = ref<string[]>([]);
const draggedTabId = ref<string | null>(null);

const broadcastMode = ref(false);
const tabRefs = ref<Record<string, any>>({});
function setTabRef(el: any, id: string) {
  if (el) tabRefs.value[id] = el;
  else delete tabRefs.value[id];
}

function handleBroadcast(data: string, sourceId: string) {
  if (!broadcastMode.value) return;
  for (const paneId of activePanes.value) {
    if (paneId !== sourceId && tabRefs.value[paneId]) {
      tabRefs.value[paneId].sendData(data);
    }
  }
}

const snippets = ref<SnippetRow[]>([]);
const showSnippetsMenu = ref(false);
const showSnippetForm = ref(false);
const editingSnippet = ref<SnippetRow | null>(null);
const snippetsButtonRef = ref<HTMLElement | null>(null);
const snippetsMenuRef = ref<HTMLElement | null>(null);

function closeSnippetsMenu(e: MouseEvent) {
  if (
    showSnippetsMenu.value &&
    !snippetsButtonRef.value?.contains(e.target as Node) &&
    !snippetsMenuRef.value?.contains(e.target as Node)
  ) {
    showSnippetsMenu.value = false;
  }
}

onMounted(() => {
  document.addEventListener("click", closeSnippetsMenu);
  document.addEventListener("keydown", onAppKeydown);
});

onUnmounted(() => {
  document.removeEventListener("click", closeSnippetsMenu);
  document.removeEventListener("keydown", onAppKeydown);
  stopHostPingLoop();
});

async function loadSnippets() {
  try {
    snippets.value = await api.listSnippets();
  } catch (err: any) {
    loadErr.value = err.message;
  }
}

function runSnippet(command: string) {
  showSnippetsMenu.value = false;
  const targetPanes = broadcastMode.value ? activePanes.value : [activePanes.value[0]];
  for (const paneId of targetPanes) {
    if (paneId && tabRefs.value[paneId]) {
      tabRefs.value[paneId].sendData(command + "\r");
    }
  }
}

async function saveSnippet(data: { label: string; command: string }) {
  try {
    if (editingSnippet.value) {
      await api.updateSnippet(editingSnippet.value.id, data);
    } else {
      await api.createSnippet(data);
    }
    showSnippetForm.value = false;
    await loadSnippets();
  } catch (err: any) {
    alert(err.message);
  }
}

async function deleteSnippet(id: string) {
  if (!confirm("Are you sure you want to delete this snippet?")) return;
  try {
    await api.deleteSnippet(id);
    await loadSnippets();
  } catch (err: any) {
    alert(err.message);
  }
}

function openEditSnippet(s?: SnippetRow) {
  showSnippetsMenu.value = false;
  editingSnippet.value = s || null;
  showSnippetForm.value = true;
}

function onTabDragStart(id: string) {
  draggedTabId.value = id;
}

function onTabDrop(targetId: string) {
  if (!draggedTabId.value || draggedTabId.value === targetId) return;
  const from = tabs.value.findIndex((t) => t.id === draggedTabId.value);
  const to = tabs.value.findIndex((t) => t.id === targetId);
  if (from === -1 || to === -1) return;
  const [t] = tabs.value.splice(from, 1);
  tabs.value.splice(to, 0, t);
  draggedTabId.value = null;
}

function onTabDragEnd() {
  draggedTabId.value = null;
}

function onSplitDrop() {
  if (!draggedTabId.value || activePanes.value.includes(draggedTabId.value)) {
    draggedTabId.value = null;
    return;
  }
  if (activePanes.value.length >= 2) {
    activePanes.value = [activePanes.value[0], draggedTabId.value];
  } else if (activePanes.value.length === 1) {
    activePanes.value.push(draggedTabId.value);
  } else {
    activePanes.value = [draggedTabId.value];
  }
  draggedTabId.value = null;
}
const loadErr = ref("");
const hostSortOrder = ref<"name" | "last_connected">("name");
/** Narrow viewports: slide-over hosts panel; md+ sidebar stays visible */
const sidebarOpen = ref(true);
const hostUp = ref<Record<string, boolean | undefined>>({});
const hostViaJump = ref<Record<string, boolean>>({});
let hostPingTimer = 0;
let hostPingGen = 0;

let searchDebounceTimer = 0;

const showIdentityForm = ref(false);
const showIdentities = ref(false);
const showEditIdentity = ref(false);
const showHostForm = ref(false);
const showFolderForm = ref(false);
const showEditHost = ref(false);
const showAuditLog = ref(false);
const showApiKeys = ref(false);
const showSftpPanel = ref(false);
const showSettings = ref(false);
const appSettings = ref<AppSettings | null>(null);
const settingsBusy = ref(false);
const settingsErr = ref("");
const settingsForm = ref({
  listen_addr: "",
  guacd_addr: "",
  shared_files_dir: "",
  terminal_theme: "default",
  terminal_font_family: "DM Mono, ui-monospace, monospace",
  terminal_font_size: 14,
  display_color_depth: 24,
  display_width: 1920,
  display_height: 1080,
  sync_url: "",
  sync_api_key: "",
  audit_log_enabled: true,
});
const passwordForm = ref({
  current: "",
  next: "",
  confirm: "",
});
const passwordBusy = ref(false);
const passwordErr = ref("");
const backupErr = ref("");
const importFile = ref<HTMLInputElement | null>(null);
function toggleSftp() {
  showSftpPanel.value = !showSftpPanel.value;
}

function filesPanelLabel(): string | null {
  const t = tabs.value.find((tab) => activePanes.value.includes(tab.id));
  if (!t) return null;
  if (t.protocol === "ssh") return "SFTP";
  if (t.protocol === "rdp") return "Shared files";
  return null;
}
const auditLoading = ref(false);
const auditErr = ref("");
const auditRows = ref<ConnectionAuditRow[]>([]);
const auditShowAll = ref(false);
const apiKeysLoading = ref(false);
const apiKeysErr = ref("");
const apiKeyRows = ref<ApiKeyRow[]>([]);
const apiKeyScopes = ref<ApiKeyScopeDef[]>([]);
const apiKeyForm = ref({
  label: "",
  scopes: [] as string[],
  expires_at: "",
});
const apiKeyCreating = ref(false);
const apiKeyCreateErr = ref("");
const createdApiKey = ref("");
const deleteIdentityErr = ref("");
const deleteIdentityErrId = ref<string | null>(null);
const newFolderLabel = ref("");
const showEditFolder = ref(false);
const editFolderForm = ref({
  id: "",
  label: "",
  parent_id: null as string | null,
});
const identityForm = ref({
  label: "",
  auth_type: "password" as "password" | "publickey",
  ssh_username: "",
  password: "",
  private_key: "",
  key_passphrase: "",
});
const editIdentityForm = ref({
  id: "",
  label: "",
  auth_type: "password" as "password" | "publickey",
  ssh_username: "",
  password: "",
  private_key: "",
  key_passphrase: "",
});
const hostForm = ref({
  label: "",
  hostname: "",
  port: 22,
  protocol: "ssh" as HostProtocol,
  tags: "",
  use_inline_identity: false,
  identity_id: "" as string,
  auth_type: "password" as "password" | "publickey",
  ssh_username: "",
  password: "",
  private_key: "",
  key_passphrase: "",
  jump_host_id: null as string | null,
});
const editHostForm = ref({
  id: "",
  label: "",
  hostname: "",
  port: 22,
  protocol: "ssh" as HostProtocol,
  tags: "",
  use_inline_identity: false,
  identity_id: "",
  auth_type: "password" as "password" | "publickey",
  ssh_username: "",
  password: "",
  private_key: "",
  key_passphrase: "",
  folder_id: null as string | null,
  jump_host_id: null as string | null,
});

function folderOptionLabel(id: string | null): string {
  if (id == null) return "(root)";
  const byId = new Map<string, { id: string; label: string; parent_id: string | null }>();
  for (const f of allFolders.value) byId.set(f.id, f);
  for (const f of browseFolders.value) byId.set(f.id, f);
  for (const b of breadcrumb.value) {
    if (!byId.has(b.id)) {
      byId.set(b.id, { id: b.id, label: b.label, parent_id: null });
    }
  }
  const parts: string[] = [];
  let cur: string | null | undefined = id;
  const guard = new Set<string>();
  while (cur != null && !guard.has(cur)) {
    guard.add(cur);
    const f = byId.get(cur);
    if (!f) break;
    parts.unshift(f.label);
    cur = f.parent_id;
  }
  return parts.join(" / ") || `#${id}`;
}

function parseTagsInput(raw: string): string[] {
  const seen = new Set<string>();
  const tags: string[] = [];
  for (const part of raw.split(",")) {
    const name = part.trim().toLowerCase().replace(/\s+/g, "");
    if (!name || seen.has(name)) continue;
    seen.add(name);
    tags.push(name);
  }
  return tags.sort();
}

function getSortedBrowseHosts(): HostRow[] {
  const hosts = [...browseHosts.value];
  if (hostSortOrder.value === "last_connected") {
    // Sort by last_connected_at descending (most recent first), with never-connected at end
    return hosts.sort((a, b) => {
      const aTime = a.last_connected_at ? new Date(a.last_connected_at).getTime() : 0;
      const bTime = b.last_connected_at ? new Date(b.last_connected_at).getTime() : 0;
      return bTime - aTime;
    });
  }
  // Sort alphabetically by label
  return hosts.sort((a, b) => a.label.localeCompare(b.label));
}

async function refreshBrowse() {
  try {
    const d = await api.browse(currentFolderId.value, searchQuery.value);
    browseFolders.value = d.folders;
    browseHosts.value = d.hosts;
    breadcrumb.value = d.breadcrumb;
    searchActive.value = d.search_active;
    void scanSidebarHosts();
  } catch (e) {
    loadErr.value = e instanceof Error ? e.message : "Browse failed";
  }
}

async function scanSidebarHosts() {
  if (!loggedIn.value) return;
  const ids = browseHosts.value.map((h) => h.id);
  if (!ids.length) return;
  const gen = ++hostPingGen;
  try {
    const { up, via_jump } = await api.pingHosts(ids);
    if (gen !== hostPingGen) return;
    const next: Record<string, boolean | undefined> = { ...hostUp.value };
    const nextVia: Record<string, boolean> = { ...hostViaJump.value };
    for (const id of ids) {
      next[id] = up[id] === true;
      nextVia[id] = via_jump?.[id] === true;
    }
    hostUp.value = next;
    hostViaJump.value = nextVia;
  } catch {
    /* ignore */
  }
}

function startHostPingLoop() {
  stopHostPingLoop();
  hostPingTimer = window.setInterval(() => {
    void scanSidebarHosts();
  }, 30000);
}

function stopHostPingLoop() {
  if (hostPingTimer) {
    window.clearInterval(hostPingTimer);
    hostPingTimer = 0;
  }
}

function hostUpClass(id: string): string {
  const v = hostUp.value[id];
  if (v === true) return "bg-emerald-500";
  if (v === false) return "bg-red-500";
  return "bg-slate-500";
}

function hostUpTitle(id: string): string {
  const v = hostUp.value[id];
  const via = hostViaJump.value[id] === true;
  if (v === true) return via ? "Up (via jump)" : "Up";
  if (v === false) return via ? "Jump host down" : "Down";
  return "Checking host";
}

function onSearchInput() {
  window.clearTimeout(searchDebounceTimer);
  searchDebounceTimer = window.setTimeout(() => {
    void refreshBrowse();
  }, 300);
}

function goToFolder(id: string | null) {
  currentFolderId.value = id;
  searchQuery.value = "";
  void refreshBrowse();
}

function searchByTag(tag: string) {
  searchQuery.value = `tag:${tag}`;
  void refreshBrowse();
}

async function refreshData() {
  loadErr.value = "";
  try {
    identities.value = await api.listIdentities();
    allHosts.value = await api.listHosts();
    allFolders.value = await api.listFoldersFlat();
    allTags.value = await api.listTags();
    appSettings.value = await api.getSettings();
    await loadSnippets();
    if (!hostForm.value.identity_id && identities.value.length) {
      hostForm.value.identity_id = identities.value[0].id;
    }
    await refreshBrowse();
  } catch (e) {
    loadErr.value = e instanceof Error ? e.message : "Failed to load data";
  }
}

onMounted(async () => {
  try {
    const m = await api.me();
    loggedIn.value = m.logged_in;
    if (m.app_version) {
      appVersion.value = m.app_version;
    }
    if (m.audit_log_enabled !== undefined) {
      auditLogEnabled.value = m.audit_log_enabled;
    }
    if (loggedIn.value) {
      await refreshData();
      startHostPingLoop();
    }
  } catch {
    loggedIn.value = false;
  } finally {
    checking.value = false;
  }
});

async function onLoggedIn() {
  loggedIn.value = true;
  await refreshData();
  startHostPingLoop();
}

async function logout() {
  await api.logout();
    tabs.value = [];
    activePanes.value = [];
    allHosts.value = [];
    loggedIn.value = false;
    hostUp.value = {};
    stopHostPingLoop();
}

function fmtDate(ts: string | null): string {
  if (!ts) return "In progress";
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? ts : d.toLocaleString();
}

function fmtDuration(totalSeconds: number | null): string {
  if (totalSeconds == null) return "In progress";
  const s = Math.max(0, Math.floor(totalSeconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0) return `${h}h ${m}m ${sec}s`;
  if (m > 0) return `${m}m ${sec}s`;
  return `${sec}s`;
}

async function openAuditLog() {
  showAuditLog.value = true;
  auditShowAll.value = false;
  auditLoading.value = true;
  auditErr.value = "";
  try {
    auditRows.value = await api.listConnectionAudit(250, 7);
  } catch (e) {
    auditErr.value = e instanceof Error ? e.message : "Failed to load audit log";
  } finally {
    auditLoading.value = false;
  }
}

async function loadAllAuditLog() {
  auditShowAll.value = true;
  auditLoading.value = true;
  auditErr.value = "";
  try {
    auditRows.value = await api.listConnectionAudit(500);
  } catch (e) {
    auditErr.value = e instanceof Error ? e.message : "Failed to load audit log";
  } finally {
    auditLoading.value = false;
  }
}

function resetApiKeyForm() {
  apiKeyForm.value = {
    label: "",
    scopes: apiKeyScopes.value.some((s) => s.id === "read:hosts")
      ? ["read:hosts"]
      : [],
    expires_at: "",
  };
  apiKeyCreateErr.value = "";
  createdApiKey.value = "";
}

function toggleApiKeyScope(scopeId: string) {
  const scopes = apiKeyForm.value.scopes;
  const idx = scopes.indexOf(scopeId);
  if (idx >= 0) {
    apiKeyForm.value.scopes = scopes.filter((s) => s !== scopeId);
  } else {
    apiKeyForm.value.scopes = [...scopes, scopeId];
  }
}

async function openApiKeys() {
  showApiKeys.value = true;
  apiKeysLoading.value = true;
  apiKeysErr.value = "";
  resetApiKeyForm();
  try {
    if (!apiKeyScopes.value.length) {
      apiKeyScopes.value = await api.listApiKeyScopes();
      resetApiKeyForm();
    }
    apiKeyRows.value = await api.listApiKeys();
  } catch (e) {
    apiKeysErr.value = e instanceof Error ? e.message : "Failed to load API keys";
  } finally {
    apiKeysLoading.value = false;
  }
}

async function refreshApiKeys() {
  apiKeysLoading.value = true;
  apiKeysErr.value = "";
  try {
    apiKeyRows.value = await api.listApiKeys();
  } catch (e) {
    apiKeysErr.value = e instanceof Error ? e.message : "Failed to load API keys";
  } finally {
    apiKeysLoading.value = false;
  }
}

async function submitApiKey() {
  apiKeyCreating.value = true;
  apiKeyCreateErr.value = "";
  createdApiKey.value = "";
  try {
    const body: {
      label: string;
      scopes: string[];
      expires_at?: string | null;
    } = {
      label: apiKeyForm.value.label.trim(),
      scopes: apiKeyForm.value.scopes,
    };
    if (apiKeyForm.value.expires_at) {
      body.expires_at = new Date(apiKeyForm.value.expires_at).toISOString();
    }
    const created = await api.createApiKey(body);
    createdApiKey.value = created.key;
    apiKeyForm.value = {
      label: "",
      scopes: apiKeyScopes.value.some((s) => s.id === "read:hosts")
        ? ["read:hosts"]
        : [],
      expires_at: "",
    };
    await refreshApiKeys();
  } catch (e) {
    apiKeyCreateErr.value = e instanceof Error ? e.message : "Failed to create API key";
  } finally {
    apiKeyCreating.value = false;
  }
}

async function deleteApiKey(id: string, label: string) {
  if (!confirm(`Delete API key "${label}"? This cannot be undone.`)) return;
  apiKeysErr.value = "";
  try {
    await api.deleteApiKey(id);
    await refreshApiKeys();
  } catch (e) {
    apiKeysErr.value = e instanceof Error ? e.message : "Failed to delete API key";
  }
}

async function copyCreatedApiKey() {
  if (!createdApiKey.value) return;
  try {
    await navigator.clipboard.writeText(createdApiKey.value);
  } catch {
    /* clipboard may be unavailable */
  }
}

function apiKeyStatus(row: ApiKeyRow): string {
  if (row.revoked_at) return "Revoked";
  if (row.expired) return "Expired";
  return "Active";
}

function fmtScopes(scopes: string[]): string {
  return scopes.join(", ");
}

function defaultPort(p: HostProtocol): number {
  if (p === "vnc") return 5900;
  if (p === "rdp") return 3389;
  return 22;
}

function onProtocolChange(form: { protocol: HostProtocol; port: number; auth_type: "password" | "publickey" }) {
  form.port = defaultPort(form.protocol);
  if (form.protocol !== "ssh") {
    form.auth_type = "password";
  }
}

function credUsernameLabel(protocol: HostProtocol): string {
  if (protocol === "rdp") return "Username";
  if (protocol === "vnc") return "Username (optional)";
  return "SSH username";
}

async function openSettings() {
  showSettings.value = true;
  settingsErr.value = "";
  try {
    const st = await api.getSettings();
    appSettings.value = st;
    passwordForm.value = { current: "", next: "", confirm: "" };
    passwordErr.value = "";
    backupErr.value = "";
    settingsForm.value = {
      listen_addr: st.listen_addr,
      guacd_addr: st.guacd_addr,
      shared_files_dir: st.shared_files_dir,
      terminal_theme: st.terminal_theme,
      terminal_font_family: st.terminal_font_family,
      terminal_font_size: st.terminal_font_size,
      display_color_depth: st.display_color_depth,
      display_width: st.display_width,
      display_height: st.display_height,
      sync_url: st.sync_url,
      sync_api_key: "",
      audit_log_enabled: st.audit_log_enabled,
    };
  } catch (e) {
    settingsErr.value = e instanceof Error ? e.message : "Failed to load settings";
  }
}

async function submitSettings() {
  settingsBusy.value = true;
  settingsErr.value = "";
  try {
    const body: Record<string, unknown> = { ...settingsForm.value };
    if (!settingsForm.value.sync_api_key) delete body.sync_api_key;
    appSettings.value = await api.patchSettings(body);
    settingsForm.value.sync_api_key = "";
    showSettings.value = false;
  } catch (e) {
    settingsErr.value = e instanceof Error ? e.message : "Failed to save settings";
  } finally {
    settingsBusy.value = false;
  }
}

async function submitPassword() {
  passwordErr.value = "";
  if (passwordForm.value.next !== passwordForm.value.confirm) {
    passwordErr.value = "New passwords do not match";
    return;
  }
  passwordBusy.value = true;
  try {
    await api.changePassword(passwordForm.value.current, passwordForm.value.next);
    passwordForm.value = { current: "", next: "", confirm: "" };
  } catch (e) {
    passwordErr.value = e instanceof Error ? e.message : "Failed to change password";
  } finally {
    passwordBusy.value = false;
  }
}

async function downloadBackup() {
  backupErr.value = "";
  if (
    !confirm(
      "The backup file includes identity secrets in plaintext. Download anyway?",
    )
  ) {
    return;
  }
  try {
    const blob = await api.exportInventory();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "vantage-backup.json";
    a.click();
    URL.revokeObjectURL(url);
  } catch (e) {
    backupErr.value = e instanceof Error ? e.message : "Export failed";
  }
}

async function onImportBackup(ev: Event) {
  backupErr.value = "";
  const input = ev.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  if (
    !confirm(
      "Import merges by id. The file includes identity secrets. Continue?",
    )
  ) {
    return;
  }
  try {
    const text = await file.text();
    const body = JSON.parse(text) as unknown;
    await api.importInventory(body);
    await refreshBrowse();
    identities.value = await api.listIdentities();
  } catch (e) {
    backupErr.value = e instanceof Error ? e.message : "Import failed";
  }
}

function isShortcutTypingTarget(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false;
  const tag = el.tagName;
  if (tag === "TEXTAREA" && el.classList.contains("xterm-helper-textarea")) {
    return false;
  }
  if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA" || el.isContentEditable) {
    return true;
  }
  return false;
}

function onAppKeydown(e: KeyboardEvent) {
  if (!loggedIn.value) return;
  if (!e.ctrlKey || !e.shiftKey || e.altKey || e.metaKey) return;
  if (isShortcutTypingTarget(e.target)) return;
  const k = e.key.toLowerCase();
  if (k === "w") {
    e.preventDefault();
    const id = activePanes.value[0];
    if (id) closeTab(id);
  } else if (k === "k") {
    e.preventDefault();
    sidebarOpen.value = true;
    searchInput.value?.focus();
    searchInput.value?.select();
  } else if (k === "r") {
    e.preventDefault();
    const id = activePanes.value[0];
    if (id) tabRefs.value[id]?.reconnect?.();
  }
}

function openTab(h: HostRow) {
  const id = crypto.randomUUID();
  tabs.value.push({ id, hostId: h.id, protocol: h.protocol || "ssh", label: h.label });
  activePanes.value = [id];
  if (window.matchMedia("(max-width: 767px)").matches) {
    sidebarOpen.value = false;
  }
}

function toggleSidebar() {
  sidebarOpen.value = !sidebarOpen.value;
}

function closeTab(id: string) {
  tabs.value = tabs.value.filter((t) => t.id !== id);
  activePanes.value = activePanes.value.filter((p) => p !== id);
  if (activePanes.value.length === 0 && tabs.value.length) {
    activePanes.value = [tabs.value[tabs.value.length - 1].id];
  }
}

function openIdentities() {
  showIdentities.value = true;
  deleteIdentityErr.value = "";
}

function openNewIdentity() {
  identityForm.value = {
    label: "",
    auth_type: "password",
    ssh_username: "",
    password: "",
    private_key: "",
    key_passphrase: "",
  };
  showIdentityForm.value = true;
}

async function submitIdentity() {
  const f = identityForm.value;
  const body: Record<string, unknown> = {
    label: f.label.trim(),
    auth_type: f.auth_type,
    ssh_username: f.ssh_username.trim(),
  };
  if (f.auth_type === "password") body.password = f.password;
  else {
    body.private_key = f.private_key;
    if (f.key_passphrase) body.key_passphrase = f.key_passphrase;
  }
  await api.createIdentity(body);
  showIdentityForm.value = false;
  identityForm.value = {
    label: "",
    auth_type: "password",
    ssh_username: "",
    password: "",
    private_key: "",
    key_passphrase: "",
  };
  await refreshData();
}

async function openEditIdentity(i: IdentityRow) {
  editIdentityForm.value = {
    id: i.id,
    label: i.label,
    auth_type: i.auth_type,
    ssh_username: "",
    password: "",
    private_key: "",
    key_passphrase: "",
  };
  showEditIdentity.value = true;
}

async function submitEditIdentity() {
  const f = editIdentityForm.value;
  const body: Partial<{
    label: string;
    ssh_username: string;
    password: string;
    private_key: string;
    key_passphrase: string;
  }> = {
    label: f.label.trim(),
  };
  if (f.ssh_username.trim()) body.ssh_username = f.ssh_username.trim();
  if (f.auth_type === "password" && f.password) body.password = f.password;
  else if (f.auth_type === "publickey") {
    if (f.private_key) body.private_key = f.private_key;
    if (f.key_passphrase !== undefined) body.key_passphrase = f.key_passphrase;
  }
  await api.updateIdentity(f.id, body);
  showEditIdentity.value = false;
  editIdentityForm.value = {
    id: "",
    label: "",
    auth_type: "password",
    ssh_username: "",
    password: "",
    private_key: "",
    key_passphrase: "",
  };
  await refreshData();
}

async function submitHost() {
  const f = hostForm.value;
  const body: Record<string, unknown> = {
    label: f.label.trim(),
    hostname: f.hostname.trim(),
    port: Number(f.port) || defaultPort(f.protocol),
    protocol: f.protocol || "ssh",
    folder_id: currentFolderId.value,
    jump_host_id: f.jump_host_id,
  };
  
  if (f.use_inline_identity) {
    body.use_inline_identity = true;
    body.auth_type = f.auth_type;
    body.ssh_username = f.ssh_username.trim();
    if (f.auth_type === "password") {
      body.password = f.password;
    } else {
      body.private_key = f.private_key;
      if (f.key_passphrase) {
        body.key_passphrase = f.key_passphrase;
      }
    }
  } else {
    body.identity_id = f.identity_id;
  }

  const tags = parseTagsInput(f.tags);
  if (tags.length) {
    body.tags = tags;
  }
  
  await api.createHost(body);
  showHostForm.value = false;
  hostForm.value = {
    label: "",
    hostname: "",
    port: 22,
    protocol: "ssh",
    tags: "",
    use_inline_identity: false,
    identity_id: hostForm.value.identity_id,
    auth_type: "password",
    ssh_username: "",
    password: "",
    private_key: "",
    key_passphrase: "",
    jump_host_id: null,
  };
  await refreshData();
}

async function submitFolder() {
  const l = newFolderLabel.value.trim();
  if (!l) return;
  await api.createFolder({ label: l, parent_id: currentFolderId.value });
  newFolderLabel.value = "";
  showFolderForm.value = false;
  await refreshData();
}

function openEditFolder(f: FolderRow) {
  editFolderForm.value = {
    id: f.id,
    label: f.label,
    parent_id: f.parent_id,
  };
  showEditFolder.value = true;
}

async function submitEditFolder() {
  const f = editFolderForm.value;
  await api.updateFolder(f.id, {
    label: f.label.trim(),
    parent_id: f.parent_id,
  });
  showEditFolder.value = false;
  await refreshData();
}

function openEditHost(h: HostRow) {
  const hasInlineIdentity = h.identity_id === null;
  editHostForm.value = {
    id: h.id,
    label: h.label,
    hostname: h.hostname,
    port: h.port,
    protocol: h.protocol || "ssh",
    tags: (h.tags || []).join(", "),
    use_inline_identity: hasInlineIdentity,
    identity_id: h.identity_id || "",
    auth_type: (h.identity_auth_type as "password" | "publickey") || "password",
    ssh_username: "",
    password: "",
    private_key: "",
    key_passphrase: "",
    folder_id: h.folder_id,
    jump_host_id: h.jump_host_id,
  };
  showEditHost.value = true;
}

async function submitEditHost() {
  const f = editHostForm.value;
  const body: Record<string, unknown> = {
    label: f.label.trim(),
    hostname: f.hostname.trim(),
    port: Number(f.port) || defaultPort(f.protocol),
    protocol: f.protocol || "ssh",
    folder_id: f.folder_id,
    jump_host_id: f.jump_host_id,
  };
  
  if (f.use_inline_identity) {
    body.use_inline_identity = true;
    body.auth_type = f.auth_type;
    body.ssh_username = f.ssh_username.trim();
    if (f.auth_type === "password") {
      body.password = f.password;
    } else {
      body.private_key = f.private_key;
      if (f.key_passphrase) {
        body.key_passphrase = f.key_passphrase;
      }
    }
  } else {
    body.identity_id = f.identity_id;
  }

  body.tags = parseTagsInput(f.tags);
  
  await api.patchHost(f.id, body);
  showEditHost.value = false;
  allHosts.value = await api.listHosts();
  await refreshBrowse();
}

async function deleteFolderRow(id: string) {
  if (
    !confirm(
      "Delete this folder and all subfolders? Hosts in those folders become unfiled (root).",
    )
  )
    return;
  await api.deleteFolder(id);
  if (currentFolderId.value === id) {
    currentFolderId.value = null;
  }
  await refreshData();
}

async function deleteHostRow(id: string) {
  if (!confirm("Remove this host entry?")) return;
  await api.deleteHost(id);
  allHosts.value = allHosts.value.filter((h) => h.id !== id);
  tabs.value = tabs.value.filter((t) => t.hostId !== id);
  activePanes.value = activePanes.value.filter(p => tabs.value.some(t => t.id === p));
  if (activePanes.value.length === 0 && tabs.value.length) {
    activePanes.value = [tabs.value[tabs.value.length - 1].id];
  }
  await refreshBrowse();
}

async function deleteIdentityRow(id: string) {
  if (!confirm("Remove this identity?")) return;
  deleteIdentityErr.value = "";
  try {
    await api.deleteIdentity(id);
    await refreshData();
  } catch (e) {
    deleteIdentityErr.value = e instanceof Error ? e.message : "Failed to delete identity";
    deleteIdentityErrId.value = id;
  }
}
</script>

<template>
  <div v-if="checking" class="flex min-h-screen items-center justify-center text-slate-500">
    Loading…
  </div>
  <LoginForm v-else-if="!loggedIn" @logged-in="onLoggedIn" />
  <div v-else class="flex h-screen min-h-0 flex-col bg-surface font-sans">
    <header
      class="flex shrink-0 items-center justify-between border-b border-slate-800 bg-surface-raised px-3 py-2 md:px-4"
    >
      <div class="flex min-w-0 items-center gap-2">
        <button
          type="button"
          class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-slate-300 hover:bg-slate-800 hover:text-white"
          aria-controls="hosts-sidebar"
          :aria-expanded="sidebarOpen"
          aria-label="Toggle hosts menu"
          @click="toggleSidebar"
        >
          <span class="sr-only">Menu</span>
          <svg
            class="h-5 w-5"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M4 6h16M4 12h16M4 18h16"
            />
          </svg>
        </button>
        <div class="flex items-center gap-2 truncate">
<span class="truncate text-sm font-semibold text-white">Vantage</span>
          <span class="truncate text-xs text-slate-400">{{ appVersion }}</span>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <button
          v-if="activePanes.length > 1"
          type="button"
          class="hidden rounded-lg px-3 py-1.5 text-xs md:inline-flex border transition-colors"
          :class="broadcastMode ? 'border-accent bg-accent/10 text-accent' : 'border-slate-800 text-slate-400 hover:border-slate-700 hover:text-white'"
          @click="broadcastMode = !broadcastMode"
          title="Broadcast input to all visible terminals"
        >
          <span class="flex items-center gap-2">
            <Radio class="h-3.5 w-3.5" />
            Broadcast
          </span>
        </button>
        <div class="relative hidden md:block">
          <button
            ref="snippetsButtonRef"
            type="button"
            class="rounded-lg px-3 py-1.5 text-xs text-slate-400 hover:bg-slate-800 hover:text-white inline-flex items-center gap-1"
            @click="showSnippetsMenu = !showSnippetsMenu"
          >
            Snippets
            <ChevronDown class="h-3 w-3" />
          </button>
          <div
            v-if="showSnippetsMenu"
            ref="snippetsMenuRef"
            class="absolute right-0 top-full mt-2 w-64 rounded-xl border border-slate-700 bg-surface shadow-xl z-50 overflow-hidden"
          >
            <div class="max-h-64 overflow-y-auto p-1">
              <div v-if="snippets.length === 0" class="p-3 text-center text-xs text-slate-500">
                No snippets saved.
              </div>
              <div
                v-for="s in snippets"
                :key="s.id"
                class="flex items-center justify-between group rounded-lg px-2 py-1.5 hover:bg-slate-800"
              >
                <button
                  type="button"
                  class="flex-1 text-left text-sm text-white truncate"
                  @click="runSnippet(s.command)"
                  :title="s.command"
                >
                  {{ s.label }}
                </button>
                <div class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                  <button type="button" class="text-slate-400 hover:text-white p-1" @click="openEditSnippet(s)">
                    <Pencil class="h-3 w-3" />
                  </button>
                  <button type="button" class="text-red-400 hover:text-red-300 p-1" @click="deleteSnippet(s.id)">
                    <Trash2 class="h-3 w-3" />
                  </button>
                </div>
              </div>
            </div>
            <div class="border-t border-slate-700 p-1">
              <button
                type="button"
                class="w-full rounded-lg px-2 py-1.5 text-left text-xs text-slate-400 hover:bg-slate-800 hover:text-white"
                @click="openEditSnippet()"
              >
                + Add new snippet...
              </button>
            </div>
          </div>
        </div>
        <button
          v-if="filesPanelLabel()"
          type="button"
          class="hidden rounded-lg px-3 py-1.5 text-xs md:inline-flex"
          :class="showSftpPanel ? 'bg-slate-800 text-white' : 'text-slate-400 hover:bg-slate-800 hover:text-white'"
          @click="toggleSftp"
        >
          {{ filesPanelLabel() }}
        </button>
        <button
          type="button"
          class="hidden rounded-lg px-3 py-1.5 text-xs text-slate-400 hover:bg-slate-800 hover:text-white md:inline-flex"
          @click="openSettings"
        >
          <span class="inline-flex items-center gap-1"><Settings class="h-3.5 w-3.5" /> Settings</span>
        </button>
        <button
          type="button"
          class="hidden rounded-lg px-3 py-1.5 text-xs text-slate-400 hover:bg-slate-800 hover:text-white md:inline-flex"
          @click="openApiKeys"
        >
          API keys
        </button>
        <button
          v-if="auditLogEnabled"
          type="button"
          class="hidden rounded-lg px-3 py-1.5 text-xs text-slate-400 hover:bg-slate-800 hover:text-white md:inline-flex"
          @click="openAuditLog"
        >
          Connection audit
        </button>
        <button
          type="button"
          class="rounded-lg px-3 py-1.5 text-xs text-slate-400 hover:bg-slate-800 hover:text-white"
          @click="logout"
        >
          Sign out
        </button>
      </div>
    </header>
    <div class="relative flex min-h-0 flex-1">
      <div
        v-show="sidebarOpen"
        class="fixed inset-0 top-14 z-30 bg-black/50 md:hidden"
        aria-hidden="true"
        @click="sidebarOpen = false"
      />
      <aside
        id="hosts-sidebar"
        class="flex w-72 shrink-0 flex-col border-r border-slate-800 bg-surface-raised transition-all duration-200 ease-out max-md:fixed max-md:bottom-0 max-md:left-0 max-md:top-14 max-md:z-40 max-md:max-h-[calc(100dvh-3.5rem)] max-md:shadow-2xl md:relative"
        :class="
          sidebarOpen
            ? 'max-md:translate-x-0 md:translate-x-0 md:w-72 md:opacity-100'
            : 'max-md:-translate-x-full md:-translate-x-full md:w-0 md:opacity-0 md:border-r-0 md:overflow-hidden'
        "
      >
        <div class="border-b border-slate-800 p-3">
          <div class="text-xs font-medium uppercase tracking-wide text-slate-500">
            Hosts
          </div>
          <input
            ref="searchInput"
            v-model="searchQuery"
            type="search"
            placeholder="Search…"
            class="mt-2 w-full rounded-lg border border-slate-700 bg-surface-overlay px-2 py-1.5 text-xs text-white placeholder:text-slate-500 focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent"
            @input="onSearchInput"
          >
          <p
            v-if="searchActive && searchQuery.trim()"
            class="mt-1 text-[10px] text-slate-500"
          >
            Matches in
            {{
              searchQuery.trim().toLowerCase().startsWith("tag:")
                ? "all folders (tag search)"
                : currentFolderId == null
                  ? "all folders"
                  : "this folder and below"
            }}
          </p>
          <nav class="mt-2 flex flex-wrap items-center gap-1 text-[11px] text-slate-400">
            <button
              type="button"
              class="rounded px-1.5 py-0.5 hover:bg-slate-800 hover:text-white"
              @click="goToFolder(null)"
            >
              Home
            </button>
            <template v-for="c in breadcrumb" :key="c.id">
              <span class="text-slate-600">/</span>
              <button
                type="button"
                class="rounded px-1.5 py-0.5 hover:bg-slate-800 hover:text-white"
                @click="goToFolder(c.id)"
              >
                {{ c.label }}
              </button>
            </template>
          </nav>
          <div class="mt-2 flex flex-wrap gap-2">
            <button
              type="button"
              class="rounded bg-accent/20 px-2 py-1 text-xs text-accent hover:bg-accent/30"
              @click="showHostForm = true"
            >
              Add host
            </button>
            <button
              type="button"
              class="rounded bg-slate-800 px-2 py-1 text-xs hover:bg-slate-700"
              @click="showFolderForm = true"
            >
              New folder
            </button>
            <button
              type="button"
              class="rounded bg-slate-800 px-2 py-1 text-xs hover:bg-slate-700"
              @click="openIdentities"
            >
              Identities
            </button>
          </div>
        </div>
        <div class="min-h-0 flex-1 overflow-auto p-2">
          <p v-if="loadErr" class="mb-2 text-xs text-red-400">{{ loadErr }}</p>
          <ul v-if="!searchActive" class="mb-3 space-y-1">
            <li
              v-for="f in browseFolders"
              :key="'f' + f.id"
              class="flex items-center justify-between gap-1 rounded-lg border border-slate-800 bg-surface-overlay px-2 py-1.5"
            >
              <button
                type="button"
                class="min-w-0 flex-1 truncate text-left text-sm text-accent hover:underline"
                @click="goToFolder(f.id)"
              >
                <span class="inline-flex items-center gap-1.5">
                  <Folder class="h-4 w-4 shrink-0" aria-hidden="true" />
                  <span>{{ f.label }}</span>
                </span>
              </button>
              <div class="flex gap-1 shrink-0">
                <button
                  type="button"
                  class="text-slate-400/70 hover:text-slate-300"
                  title="Rename folder"
                  @click="openEditFolder(f)"
                >
                  <Pencil class="h-3 w-3" aria-hidden="true" />
                </button>
                <button
                  type="button"
                  class="text-red-400/70 hover:text-red-400"
                  title="Delete folder"
                  @click="deleteFolderRow(f.id)"
                >
                  <Trash2 class="h-3 w-3" aria-hidden="true" />
                </button>
              </div>
            </li>
          </ul>
          <div v-if="browseHosts.length" class="mb-2 flex items-center justify-between">
            <label class="text-xs text-slate-500 uppercase">Sort by:</label>
            <select
              v-model="hostSortOrder"
              class="rounded border border-slate-700 bg-surface-overlay px-2 py-1 text-xs"
            >
              <option value="name">Name</option>
              <option value="last_connected">Last connected</option>
            </select>
          </div>
          <ul class="space-y-1">
            <li
              v-for="h in getSortedBrowseHosts()"
              :key="'h' + h.id"
              class="rounded-lg border border-slate-800 bg-surface-overlay p-2"
            >
              <button
                type="button"
                class="flex w-full items-center gap-2 text-left text-sm font-medium text-white hover:text-accent"
                @click="openTab(h)"
              >
                <span
                  class="inline-block h-2 w-2 shrink-0 rounded-full"
                  :class="hostUpClass(h.id)"
                  :title="hostUpTitle(h.id)"
                  :aria-label="hostUpTitle(h.id)"
                />
                <span class="min-w-0 truncate">{{ h.label }}</span>
              </button>
              <div class="mt-0.5 truncate font-mono text-[10px] text-slate-500">
                {{ h.hostname }}:{{ h.port }} · {{ (h.protocol || "ssh").toUpperCase() }} · {{ h.identity_label }}
              </div>
              <p
                v-if="h.jump_host_id"
                class="mt-0.5 truncate text-[10px] text-slate-500"
              >
                via {{ h.jump_host_label || `#${h.jump_host_id}` }}
              </p>
              <div
                v-if="h.tags?.length"
                class="mt-1 flex flex-wrap gap-1"
              >
                <button
                  v-for="tag in h.tags"
                  :key="tag"
                  type="button"
                  class="rounded bg-slate-800 px-1.5 py-0.5 text-[10px] text-slate-400 hover:bg-slate-700 hover:text-accent"
                  :title="`Search tag:${tag}`"
                  @click.stop="searchByTag(tag)"
                >
                  {{ tag }}
                </button>
              </div>
              <p
                v-if="searchActive"
                class="mt-0.5 truncate text-[10px] text-slate-600"
              >
                {{ folderOptionLabel(h.folder_id) }}
              </p>
              <div class="mt-1 flex gap-1">
                <button
                  type="button"
                  class="text-slate-400/70 hover:text-slate-300"
                  title="Edit host"
                  @click="openEditHost(h)"
                >
                  <Pencil class="h-3 w-3" aria-hidden="true" />
                </button>
                <button
                  type="button"
                  class="text-red-400/70 hover:text-red-400"
                  title="Delete host"
                  @click="deleteHostRow(h.id)"
                >
                  <Trash2 class="h-3 w-3" aria-hidden="true" />
                </button>
              </div>
            </li>
          </ul>
          <p
            v-if="!browseFolders.length && !browseHosts.length && !loadErr"
            class="py-4 text-center text-xs text-slate-500"
          >
            {{
              searchActive
                ? "No hosts match."
                : "Empty folder - add a host or subfolder."
            }}
          </p>
        </div>
      </aside>
      <main class="flex min-h-0 min-w-0 flex-1 flex-col">
        <div
          v-if="!tabs.length"
          class="flex flex-1 items-center justify-center text-sm text-slate-500"
        >
          Select a host to open a tab, or add one from the sidebar.
        </div>
        <template v-else>
          <div
            class="flex shrink-0 gap-1 overflow-x-auto border-b border-slate-800 bg-surface-raised px-2 pt-2"
          >
            <button
              v-for="t in tabs"
              :key="t.id"
              type="button"
              draggable="true"
              @dragstart="onTabDragStart(t.id)"
              @dragover.prevent
              @drop="onTabDrop(t.id)"
              @dragend="onTabDragEnd"
              class="flex items-center gap-2 rounded-t-lg border border-b-0 px-3 py-2 text-sm transition-colors"
              :class="[
                activePanes.includes(t.id)
                  ? 'border-slate-700 bg-surface text-white'
                  : 'border-transparent bg-transparent text-slate-400 hover:text-white',
                draggedTabId && draggedTabId !== t.id ? 'hover:bg-slate-800/50' : ''
              ]"
              @click="activePanes = [t.id]"
            >
              {{ t.label }}
              <span
                class="rounded px-1 text-slate-500 hover:bg-slate-800 hover:text-white"
                @click.stop="closeTab(t.id)"
              >×</span>
            </button>
          </div>
          <div class="relative min-h-0 flex-1 flex flex-row gap-2 p-2 md:p-3">
            <div
              v-for="t in tabs"
              v-show="activePanes.includes(t.id)"
              :key="t.id"
              class="flex-1 min-w-0 h-full"
            >
              <TabContent
                :ref="(el) => setTabRef(el, t.id)"
                :host-id="t.hostId"
                :protocol="t.protocol || 'ssh'"
                :visible="activePanes.includes(t.id)"
                :show-sftp="showSftpPanel"
                :settings="appSettings"
                @broadcast-data="(data: string) => handleBroadcast(data, t.id)"
              />
            </div>
            
            <div 
              v-if="draggedTabId && activePanes.length === 1 && !activePanes.includes(draggedTabId)"
              class="absolute inset-y-2 right-2 md:inset-y-3 md:right-3 w-[calc(50%-0.25rem)] z-50 flex items-center justify-center rounded-lg border-2 border-dashed border-accent bg-accent/10 backdrop-blur-[2px] transition-all"
              @dragover.prevent
              @drop="onSplitDrop"
            >
              <span class="font-medium text-accent">Drop to split side-by-side</span>
            </div>
          </div>
        </template>
      </main>
    </div>

    <SnippetForm
      v-if="showSnippetForm"
      :snippet="editingSnippet"
      @save="saveSnippet"
      @cancel="showSnippetForm = false"
    />

    <div
      v-if="showApiKeys"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
    >
      <div
        class="max-h-[90vh] w-full max-w-4xl overflow-auto rounded-xl border border-slate-800 bg-surface-raised p-6 shadow-xl"
      >
        <div class="flex items-center justify-between gap-3">
          <h2 class="text-lg font-semibold text-white">API keys</h2>
          <button
            type="button"
            class="rounded-lg px-3 py-1.5 text-xs text-slate-400 hover:bg-slate-800 hover:text-white"
            @click="showApiKeys = false"
          >
            Close
          </button>
        </div>
        <p class="mt-1 text-xs text-slate-500">
          Create keys for external systems. Send
          <code class="text-slate-400">Authorization: Bearer &lt;key&gt;</code>
          on API requests. For WebSocket terminals, append
          <code class="text-slate-400">?token=&lt;key&gt;</code>.
        </p>

        <form class="mt-5 rounded-lg border border-slate-800 bg-surface-overlay/40 p-4" @submit.prevent="submitApiKey">
          <h3 class="text-sm font-medium text-white">Create key</h3>
          <label class="mt-3 block text-xs uppercase text-slate-500">Label</label>
          <input
            v-model="apiKeyForm.label"
            required
            maxlength="255"
            placeholder="CI deploy, monitoring, …"
            class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          />
          <p class="mt-3 text-xs uppercase text-slate-500">Scopes</p>
          <div class="mt-2 space-y-2">
            <label
              v-for="scope in apiKeyScopes"
              :key="scope.id"
              class="flex cursor-pointer items-start gap-2 rounded border border-slate-800 px-3 py-2 hover:border-slate-700"
            >
              <input
                type="checkbox"
                class="mt-0.5"
                :checked="apiKeyForm.scopes.includes(scope.id)"
                @change="toggleApiKeyScope(scope.id)"
              />
              <span>
                <span class="block text-sm text-slate-200">{{ scope.label }}</span>
                <span class="block text-xs text-slate-500">{{ scope.description }}</span>
              </span>
            </label>
          </div>
          <label class="mt-3 block text-xs uppercase text-slate-500">Expiry (optional)</label>
          <input
            v-model="apiKeyForm.expires_at"
            type="datetime-local"
            class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          />
          <p v-if="apiKeyCreateErr" class="mt-3 text-xs text-red-400">{{ apiKeyCreateErr }}</p>
          <div
            v-if="createdApiKey"
            class="mt-3 rounded border border-amber-900/60 bg-amber-950/30 p-3"
          >
            <p class="text-xs text-amber-200">
              Copy this key now - it will not be shown again.
            </p>
            <div class="mt-2 flex items-center gap-2">
              <code class="min-w-0 flex-1 break-all rounded bg-surface-overlay px-2 py-1 text-[11px] text-slate-200">
                {{ createdApiKey }}
              </code>
              <button
                type="button"
                class="shrink-0 rounded bg-slate-800 px-2 py-1 text-xs hover:bg-slate-700"
                @click="copyCreatedApiKey"
              >
                Copy
              </button>
            </div>
          </div>
          <button
            type="submit"
            class="mt-4 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-slate-950 hover:bg-sky-400 disabled:opacity-50"
            :disabled="apiKeyCreating || !apiKeyForm.scopes.length"
          >
            {{ apiKeyCreating ? "Creating…" : "Create key" }}
          </button>
        </form>

        <p v-if="apiKeysErr" class="mt-4 text-xs text-red-400">{{ apiKeysErr }}</p>
        <p v-else-if="apiKeysLoading" class="mt-4 text-xs text-slate-400">Loading…</p>
        <div v-else class="mt-4 overflow-x-auto">
          <table class="min-w-full text-left text-xs">
            <thead class="text-slate-500">
              <tr class="border-b border-slate-800">
                <th class="px-2 py-2 font-medium">Label</th>
                <th class="px-2 py-2 font-medium">Prefix</th>
                <th class="px-2 py-2 font-medium">Scopes</th>
                <th class="px-2 py-2 font-medium">Expires</th>
                <th class="px-2 py-2 font-medium">Last used</th>
                <th class="px-2 py-2 font-medium">Status</th>
                <th class="px-2 py-2 font-medium"></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="row in apiKeyRows"
                :key="row.id"
                class="border-b border-slate-900/80 text-slate-300"
              >
                <td class="px-2 py-2">{{ row.label }}</td>
                <td class="px-2 py-2 font-mono text-[11px]">{{ row.key_prefix }}…</td>
                <td class="px-2 py-2">{{ fmtScopes(row.scopes) }}</td>
                <td class="px-2 py-2">{{ row.expires_at ? fmtDate(row.expires_at) : "Never" }}</td>
                <td class="px-2 py-2">{{ row.last_used_at ? fmtDate(row.last_used_at) : "Never" }}</td>
                <td class="px-2 py-2">{{ apiKeyStatus(row) }}</td>
                <td class="px-2 py-2 text-right">
                  <button
                    type="button"
                    class="rounded px-2 py-1 text-red-400 hover:bg-slate-800"
                    @click="deleteApiKey(row.id, row.label)"
                  >
                    Delete
                  </button>
                </td>
              </tr>
              <tr v-if="!apiKeyRows.length">
                <td class="px-2 py-4 text-center text-slate-500" colspan="7">
                  No API keys yet.
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <div
      v-if="showAuditLog"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
    >
      <div
        class="max-h-[90vh] w-full max-w-4xl overflow-auto rounded-xl border border-slate-800 bg-surface-raised p-6 shadow-xl"
      >
        <div class="flex items-center justify-between gap-3">
          <h2 class="text-lg font-semibold text-white">Connection audit</h2>
          <div class="flex gap-2">
            <button
              v-if="!auditShowAll"
              type="button"
              class="rounded-lg bg-slate-800 px-3 py-1.5 text-xs hover:bg-slate-700"
              @click="loadAllAuditLog"
            >
              Show all
            </button>
            <button
              type="button"
              class="rounded-lg px-3 py-1.5 text-xs text-slate-400 hover:bg-slate-800 hover:text-white"
              @click="showAuditLog = false"
            >
              Close
            </button>
          </div>
        </div>
        <p class="mt-1 text-xs text-slate-500">
          Recent SSH sessions from the last 7 days and how long they lasted.
        </p>
        <p v-if="auditErr" class="mt-3 text-xs text-red-400">{{ auditErr }}</p>
        <p v-else-if="auditLoading" class="mt-3 text-xs text-slate-400">
          Loading…
        </p>
        <div v-else class="mt-4 overflow-x-auto">
          <table class="min-w-full text-left text-xs">
            <thead class="text-slate-500">
              <tr class="border-b border-slate-800">
                <th class="px-2 py-2 font-medium">Host</th>
                <th class="px-2 py-2 font-medium">Address</th>
                <th class="px-2 py-2 font-medium">Started</th>
                <th class="px-2 py-2 font-medium">Ended</th>
                <th class="px-2 py-2 font-medium">Duration</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="r in auditRows"
                :key="r.id"
                class="border-b border-slate-900/80 text-slate-300"
              >
                <td class="px-2 py-2">
                  <div class="truncate">{{ r.host_label }}</div>
                  <div
                    v-if="r.jump_host_id"
                    class="truncate text-[10px] text-slate-500"
                  >
                    via #{{ r.jump_host_id }}
                  </div>
                </td>
                <td class="px-2 py-2 font-mono text-[11px]">
                  {{ r.hostname }}:{{ r.port }}
                </td>
                <td class="px-2 py-2">{{ fmtDate(r.started_at) }}</td>
                <td class="px-2 py-2">{{ fmtDate(r.ended_at) }}</td>
                <td class="px-2 py-2">{{ fmtDuration(r.duration_seconds) }}</td>
              </tr>
              <tr v-if="!auditRows.length">
                <td class="px-2 py-4 text-center text-slate-500" colspan="5">
                  No connection sessions yet.
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <div
      v-if="showIdentities"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      @click.self="showIdentities = false"
    >
      <div class="flex max-h-[90vh] w-full max-w-lg flex-col overflow-hidden rounded-xl border border-slate-800 bg-surface-raised shadow-xl">
        <div class="flex items-center justify-between border-b border-slate-800 px-6 py-4">
          <h2 class="text-lg font-semibold text-white">Identities</h2>
          <button
            type="button"
            class="rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-slate-950 hover:bg-sky-400"
            @click="openNewIdentity"
          >
            Add identity
          </button>
        </div>
        <div class="min-h-0 flex-1 overflow-auto p-4">
          <p v-if="deleteIdentityErr" class="mb-3 text-sm text-red-400">
            {{ deleteIdentityErr }}
            <button
              type="button"
              class="ml-2 underline hover:text-red-300"
              @click="deleteIdentityErr = ''"
            >
              Dismiss
            </button>
          </p>
          <p v-if="!identities.length" class="py-8 text-center text-sm text-slate-500">
            No saved identities yet.
          </p>
          <ul v-else class="space-y-1">
            <li
              v-for="i in identities"
              :key="i.id"
              class="flex items-center justify-between gap-2 rounded-lg border border-slate-800 bg-surface-overlay px-3 py-2"
            >
              <div class="min-w-0">
                <div class="truncate text-sm text-white">{{ i.label }}</div>
                <div class="text-[11px] text-slate-500">
                  {{ i.auth_type === "publickey" ? "SSH private key" : "Password" }}
                </div>
              </div>
              <div class="flex shrink-0 gap-1">
                <button
                  type="button"
                  class="rounded p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white"
                  title="Edit identity"
                  @click="openEditIdentity(i)"
                >
                  <Pencil class="h-4 w-4" aria-hidden="true" />
                </button>
                <button
                  type="button"
                  class="rounded p-1.5 text-red-400/70 hover:bg-slate-800 hover:text-red-400"
                  title="Delete identity"
                  @click="deleteIdentityRow(i.id)"
                >
                  <Trash2 class="h-4 w-4" aria-hidden="true" />
                </button>
              </div>
            </li>
          </ul>
        </div>
        <div class="border-t border-slate-800 px-6 py-3 text-right">
          <button
            type="button"
            class="rounded-lg px-4 py-2 text-sm text-slate-400 hover:bg-slate-800"
            @click="showIdentities = false"
          >
            Close
          </button>
        </div>
      </div>
    </div>

    <div
      v-if="showIdentityForm"
      class="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-4"
    >
      <form
        class="max-h-[90vh] w-full max-w-lg overflow-auto rounded-xl border border-slate-800 bg-surface-raised p-6 shadow-xl"
        @submit.prevent="submitIdentity"
      >
        <h2 class="text-lg font-semibold text-white">New identity</h2>
        <label class="mt-4 block text-xs uppercase text-slate-500">Label</label>
        <input
          v-model="identityForm.label"
          required
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        />
        <label class="mt-3 block text-xs uppercase text-slate-500">Auth</label>
        <select
          v-model="identityForm.auth_type"
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        >
          <option value="password">Password</option>
          <option value="publickey">SSH private key</option>
        </select>
        <label class="mt-3 block text-xs uppercase text-slate-500">Username</label>
        <input
          v-model="identityForm.ssh_username"
          required
          autocomplete="off"
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        />
        <template v-if="identityForm.auth_type === 'password'">
          <label class="mt-3 block text-xs uppercase text-slate-500">Password</label>
          <input
            v-model="identityForm.password"
            type="password"
            required
            class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          />
        </template>
        <template v-else>
          <label class="mt-3 block text-xs uppercase text-slate-500">Private key (PEM)</label>
          <textarea
            v-model="identityForm.private_key"
            required
            rows="6"
            class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 font-mono text-xs"
          />
          <label class="mt-3 block text-xs uppercase text-slate-500">Key passphrase (optional)</label>
          <input
            v-model="identityForm.key_passphrase"
            type="password"
            class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          />
        </template>
        <div class="mt-6 flex justify-end gap-2">
          <button
            type="button"
            class="rounded-lg px-4 py-2 text-sm text-slate-400 hover:bg-slate-800"
            @click="showIdentityForm = false"
          >
            Cancel
          </button>
          <button
            type="submit"
            class="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-slate-950"
          >
            Save
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="showEditIdentity"
      class="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-4"
    >
      <form
        class="max-h-[90vh] w-full max-w-lg overflow-auto rounded-xl border border-slate-800 bg-surface-raised p-6 shadow-xl"
        @submit.prevent="submitEditIdentity"
      >
        <h2 class="text-lg font-semibold text-white">Edit identity</h2>
        <label class="mt-4 block text-xs uppercase text-slate-500">Label</label>
        <input
          v-model="editIdentityForm.label"
          required
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        />
        <label class="mt-3 block text-xs uppercase text-slate-500">Auth type</label>
        <div class="mt-1 rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm text-slate-400">
          {{ editIdentityForm.auth_type }}
        </div>
        <p class="mt-1 text-[10px] text-slate-500">Auth type cannot be changed</p>
        <label class="mt-3 block text-xs uppercase text-slate-500">Username</label>
        <input
          v-model="editIdentityForm.ssh_username"
          placeholder="Leave empty to keep unchanged"
          autocomplete="off"
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        />
        <template v-if="editIdentityForm.auth_type === 'password'">
          <label class="mt-3 block text-xs uppercase text-slate-500">Password (optional)</label>
          <input
            v-model="editIdentityForm.password"
            type="password"
            placeholder="Leave empty to keep current password"
            class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          />
        </template>
        <template v-else>
          <label class="mt-3 block text-xs uppercase text-slate-500">Private key (PEM, optional)</label>
          <textarea
            v-model="editIdentityForm.private_key"
            placeholder="Leave empty to keep current key"
            rows="6"
            class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 font-mono text-xs"
          />
          <label class="mt-3 block text-xs uppercase text-slate-500">Key passphrase (optional)</label>
          <input
            v-model="editIdentityForm.key_passphrase"
            type="password"
            placeholder="Leave empty to remove passphrase"
            class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          />
        </template>
        <div class="mt-6 flex justify-end gap-2">
          <button
            type="button"
            class="rounded-lg px-4 py-2 text-sm text-slate-400 hover:bg-slate-800"
            @click="showEditIdentity = false"
          >
            Cancel
          </button>
          <button
            type="submit"
            class="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-slate-950"
          >
            Save
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="showHostForm"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
    >
      <form
        class="w-full max-w-md rounded-xl border border-slate-800 bg-surface-raised p-6 shadow-xl"
        @submit.prevent="submitHost"
      >
        <h2 class="text-lg font-semibold text-white">New host</h2>
        <p class="mt-1 text-xs text-slate-500">
          Folder: {{ folderOptionLabel(currentFolderId) }}
        </p>
        <label class="mt-4 block text-xs uppercase text-slate-500">Label</label>
        <input
          v-model="hostForm.label"
          required
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        />
        <label class="mt-3 block text-xs uppercase text-slate-500">Protocol</label>
        <select
          v-model="hostForm.protocol"
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          @change="onProtocolChange(hostForm)"
        >
          <option value="ssh">SSH</option>
          <option value="vnc">VNC</option>
          <option value="rdp">RDP</option>
        </select>
        <label class="mt-3 block text-xs uppercase text-slate-500">Hostname or IP</label>
        <input
          v-model="hostForm.hostname"
          required
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        />
        <label class="mt-3 block text-xs uppercase text-slate-500">Port</label>
        <input
          v-model.number="hostForm.port"
          type="number"
          min="1"
          max="65535"
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        />
        <label class="mt-3 block text-xs uppercase text-slate-500">Tags</label>
        <TagInput v-model="hostForm.tags" :suggestions="allTags" />
        <p class="mt-1 text-[10px] text-slate-500">
          Comma-separated. Search with
          <code class="text-slate-400">tag:name</code>.
        </p>
        <label class="mt-3 block text-xs uppercase text-slate-500">Credentials</label>
        <div class="mt-1 flex gap-2">
          <label class="flex items-center gap-2 cursor-pointer">
            <input
              type="radio"
              :checked="!hostForm.use_inline_identity"
              @change="hostForm.use_inline_identity = false"
              class="w-4 h-4"
            />
            <span class="text-sm">Saved identity</span>
          </label>
          <label class="flex items-center gap-2 cursor-pointer">
            <input
              type="radio"
              :checked="hostForm.use_inline_identity"
              @change="hostForm.use_inline_identity = true"
              class="w-4 h-4"
            />
            <span class="text-sm">One-time</span>
          </label>
        </div>
        <div v-if="!hostForm.use_inline_identity" class="mt-3">
          <label class="block text-xs uppercase text-slate-500">Identity</label>
          <select
            v-model="hostForm.identity_id"
            required
            class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          >
            <option v-for="i in identities" :key="i.id" :value="i.id">
              {{ i.label }} ({{ i.auth_type }})
            </option>
          </select>
        </div>
        <div v-else class="mt-3 space-y-3">
          <div v-if="hostForm.protocol === 'ssh'">
            <label class="block text-xs uppercase text-slate-500">Auth type</label>
            <select
              v-model="hostForm.auth_type"
              class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
            >
              <option value="password">Password</option>
              <option value="publickey">Public key</option>
            </select>
          </div>
          <div>
            <label class="block text-xs uppercase text-slate-500">{{ credUsernameLabel(hostForm.protocol) }}</label>
            <input
              v-model="hostForm.ssh_username"
              :required="hostForm.protocol !== 'vnc'"
              class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
            />
          </div>
          <div v-if="hostForm.auth_type === 'password' || hostForm.protocol !== 'ssh'">
            <label class="block text-xs uppercase text-slate-500">Password</label>
            <input
              v-model="hostForm.password"
              type="password"
              required
              class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
            />
          </div>
          <div v-else-if="hostForm.protocol === 'ssh'">
            <label class="block text-xs uppercase text-slate-500">Private key</label>
            <textarea
              v-model="hostForm.private_key"
              required
              rows="4"
              class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm font-mono text-xs"
              placeholder="-----BEGIN PRIVATE KEY-----"
            />
            <label class="mt-2 block text-xs uppercase text-slate-500">Key passphrase (optional)</label>
            <input
              v-model="hostForm.key_passphrase"
              type="password"
              class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
            />
          </div>
        </div>
        <label v-if="hostForm.protocol === 'ssh'" class="mt-3 block text-xs uppercase text-slate-500">Jump host (optional)</label>
        <select
          v-if="hostForm.protocol === 'ssh'"
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          :value="hostForm.jump_host_id === null ? '' : String(hostForm.jump_host_id)"
          @change="
            hostForm.jump_host_id =
              ($event.target as HTMLSelectElement).value === ''
                ? null
                : ($event.target as HTMLSelectElement).value
          "
        >
          <option value="">None (direct)</option>
          <option v-for="h in allHosts" :key="h.id" :value="String(h.id)">
            {{ h.label }} ({{ h.hostname }}:{{ h.port }})
          </option>
        </select>
        <div class="mt-6 flex justify-end gap-2">
          <button
            type="button"
            class="rounded-lg px-4 py-2 text-sm text-slate-400 hover:bg-slate-800"
            @click="showHostForm = false"
          >
            Cancel
          </button>
          <button
            type="submit"
            class="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-slate-950"
            :disabled="!hostForm.use_inline_identity && !identities.length"
          >
            Save
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="showFolderForm"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
    >
      <form
        class="w-full max-w-sm rounded-xl border border-slate-800 bg-surface-raised p-6 shadow-xl"
        @submit.prevent="submitFolder"
      >
        <h2 class="text-lg font-semibold text-white">New folder</h2>
        <p class="mt-1 text-xs text-slate-500">
          Inside: {{ folderOptionLabel(currentFolderId) }}
        </p>
        <label class="mt-4 block text-xs uppercase text-slate-500">Name</label>
        <input
          v-model="newFolderLabel"
          required
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        >
        <div class="mt-6 flex justify-end gap-2">
          <button
            type="button"
            class="rounded-lg px-4 py-2 text-sm text-slate-400 hover:bg-slate-800"
            @click="showFolderForm = false"
          >
            Cancel
          </button>
          <button
            type="submit"
            class="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-slate-950"
          >
            Create
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="showEditFolder"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
    >
      <form
        class="w-full max-w-sm rounded-xl border border-slate-800 bg-surface-raised p-6 shadow-xl"
        @submit.prevent="submitEditFolder"
      >
        <h2 class="text-lg font-semibold text-white">Rename folder</h2>
        <label class="mt-4 block text-xs uppercase text-slate-500">Name</label>
        <input
          v-model="editFolderForm.label"
          required
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        >
        <div class="mt-6 flex justify-end gap-2">
          <button
            type="button"
            class="rounded-lg px-4 py-2 text-sm text-slate-400 hover:bg-slate-800"
            @click="showEditFolder = false"
          >
            Cancel
          </button>
          <button
            type="submit"
            class="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-slate-950"
          >
            Save
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="showEditHost"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
    >
      <form
        class="max-h-[90vh] w-full max-w-md overflow-auto rounded-xl border border-slate-800 bg-surface-raised p-6 shadow-xl"
        @submit.prevent="submitEditHost"
      >
        <h2 class="text-lg font-semibold text-white">Edit host</h2>
        <label class="mt-4 block text-xs uppercase text-slate-500">Label</label>
        <input
          v-model="editHostForm.label"
          required
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        >
        <label class="mt-3 block text-xs uppercase text-slate-500">Protocol</label>
        <select
          v-model="editHostForm.protocol"
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          @change="onProtocolChange(editHostForm)"
        >
          <option value="ssh">SSH</option>
          <option value="vnc">VNC</option>
          <option value="rdp">RDP</option>
        </select>
        <label class="mt-3 block text-xs uppercase text-slate-500">Hostname or IP</label>
        <input
          v-model="editHostForm.hostname"
          required
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        >
        <label class="mt-3 block text-xs uppercase text-slate-500">Port</label>
        <input
          v-model.number="editHostForm.port"
          type="number"
          min="1"
          max="65535"
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
        >
        <label class="mt-3 block text-xs uppercase text-slate-500">Tags</label>
        <TagInput v-model="editHostForm.tags" :suggestions="allTags" />
        <p class="mt-1 text-[10px] text-slate-500">
          Comma-separated. Search with
          <code class="text-slate-400">tag:name</code>.
        </p>
        <label class="mt-3 block text-xs uppercase text-slate-500">Folder</label>
        <select
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          :value="editHostForm.folder_id === null ? '' : String(editHostForm.folder_id)"
          @change="
            editHostForm.folder_id =
              ($event.target as HTMLSelectElement).value === ''
                ? null
                : ($event.target as HTMLSelectElement).value
          "
        >
          <option value="">(root)</option>
          <option
            v-for="f in allFolders"
            :key="f.id"
            :value="String(f.id)"
          >
            {{ folderOptionLabel(f.id) }}
          </option>
        </select>
        <label class="mt-3 block text-xs uppercase text-slate-500">Credentials</label>
        <div class="mt-1 flex gap-2">
          <label class="flex items-center gap-2 cursor-pointer">
            <input
              type="radio"
              :checked="!editHostForm.use_inline_identity"
              @change="editHostForm.use_inline_identity = false"
              class="w-4 h-4"
            />
            <span class="text-sm">Saved identity</span>
          </label>
          <label class="flex items-center gap-2 cursor-pointer">
            <input
              type="radio"
              :checked="editHostForm.use_inline_identity"
              @change="editHostForm.use_inline_identity = true"
              class="w-4 h-4"
            />
            <span class="text-sm">One-time</span>
          </label>
        </div>
        <div v-if="!editHostForm.use_inline_identity" class="mt-3">
          <label class="block text-xs uppercase text-slate-500">Identity</label>
          <select
            v-model="editHostForm.identity_id"
            required
            class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          >
            <option v-for="i in identities" :key="i.id" :value="i.id">
              {{ i.label }} ({{ i.auth_type }})
            </option>
          </select>
        </div>
        <div v-else class="mt-3 space-y-3">
          <div v-if="editHostForm.protocol === 'ssh'">
            <label class="block text-xs uppercase text-slate-500">Auth type</label>
            <select
              v-model="editHostForm.auth_type"
              class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
            >
              <option value="password">Password</option>
              <option value="publickey">Public key</option>
            </select>
          </div>
          <div>
            <label class="block text-xs uppercase text-slate-500">{{ credUsernameLabel(editHostForm.protocol) }}</label>
            <input
              v-model="editHostForm.ssh_username"
              :required="editHostForm.protocol !== 'vnc'"
              class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
            />
          </div>
          <div v-if="editHostForm.auth_type === 'password' || editHostForm.protocol !== 'ssh'">
            <label class="block text-xs uppercase text-slate-500">Password</label>
            <input
              v-model="editHostForm.password"
              type="password"
              class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
            />
          </div>
          <div v-else-if="editHostForm.protocol === 'ssh'">
            <label class="block text-xs uppercase text-slate-500">Private key</label>
            <textarea
              v-model="editHostForm.private_key"
              rows="4"
              class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm font-mono text-xs"
              placeholder="-----BEGIN PRIVATE KEY-----"
            />
            <label class="mt-2 block text-xs uppercase text-slate-500">Key passphrase (optional)</label>
            <input
              v-model="editHostForm.key_passphrase"
              type="password"
              class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
            />
          </div>
        </div>
        <label v-if="editHostForm.protocol === 'ssh'" class="mt-3 block text-xs uppercase text-slate-500">Jump host (optional)</label>
        <select
          v-if="editHostForm.protocol === 'ssh'"
          class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm"
          :value="editHostForm.jump_host_id === null ? '' : String(editHostForm.jump_host_id)"
          @change="
            editHostForm.jump_host_id =
              ($event.target as HTMLSelectElement).value === ''
                ? null
                : ($event.target as HTMLSelectElement).value
          "
        >
          <option value="">None (direct)</option>
          <option
            v-for="h in allHosts.filter((x) => x.id !== editHostForm.id)"
            :key="h.id"
            :value="String(h.id)"
          >
            {{ h.label }} ({{ h.hostname }}:{{ h.port }})
          </option>
        </select>
        <div class="mt-6 flex justify-end gap-2">
          <button
            type="button"
            class="rounded-lg px-4 py-2 text-sm text-slate-400 hover:bg-slate-800"
            @click="showEditHost = false"
          >
            Cancel
          </button>
          <button
            type="submit"
            class="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-slate-950"
          >
            Save
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="showSettings"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
    >
      <form
        class="max-h-[90vh] w-full max-w-lg overflow-auto rounded-xl border border-slate-800 bg-surface-raised p-6 shadow-xl"
        @submit.prevent="submitSettings"
      >
        <h2 class="text-lg font-semibold text-white">Settings</h2>
        <p class="mt-1 text-xs text-slate-500">All configuration is stored on this node.</p>
        <label class="mt-4 block text-xs uppercase text-slate-500">Listen address</label>
        <input v-model="settingsForm.listen_addr" class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" />
        <label class="mt-3 block text-xs uppercase text-slate-500">guacd address</label>
        <input v-model="settingsForm.guacd_addr" placeholder="127.0.0.1:4822" class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" />
        <label class="mt-3 block text-xs uppercase text-slate-500">Shared files directory</label>
        <input v-model="settingsForm.shared_files_dir" class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" />
        <p class="mt-1 text-[11px] text-slate-500">
          Per-host folders are created here for RDP drives. If guacd runs in Docker, this path must be mounted into the container.
        </p>
        <label class="mt-3 block text-xs uppercase text-slate-500">Terminal theme</label>
        <select v-model="settingsForm.terminal_theme" class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm">
          <option value="default">Default dark</option>
          <option value="light">Light</option>
          <option value="solarized">Solarized</option>
        </select>
        <label class="mt-3 block text-xs uppercase text-slate-500">Terminal font</label>
        <input v-model="settingsForm.terminal_font_family" class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" />
        <label class="mt-3 block text-xs uppercase text-slate-500">Font size</label>
        <input v-model.number="settingsForm.terminal_font_size" type="number" min="8" max="32" class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" />
        <label class="mt-3 block text-xs uppercase text-slate-500">Display width / height / color depth</label>
        <div class="mt-1 flex gap-2">
          <input v-model.number="settingsForm.display_width" type="number" class="w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" />
          <input v-model.number="settingsForm.display_height" type="number" class="w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" />
          <input v-model.number="settingsForm.display_color_depth" type="number" class="w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" />
        </div>
        <template v-if="appSettings?.mode === 'desktop'">
          <label class="mt-3 block text-xs uppercase text-slate-500">Sync server URL</label>
          <input v-model="settingsForm.sync_url" placeholder="https://vantage.example" class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" />
          <label class="mt-3 block text-xs uppercase text-slate-500">Sync API key</label>
          <input v-model="settingsForm.sync_api_key" type="password" :placeholder="appSettings?.sync_api_key_set ? 'Set (leave blank to keep)' : 'Paste API key from vantaged'" class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" />
        </template>
        <label class="mt-3 flex items-center gap-2 text-sm">
          <input v-model="settingsForm.audit_log_enabled" type="checkbox" />
          Connection audit log
        </label>
        <div class="mt-6 border-t border-slate-800 pt-4">
          <h3 class="text-sm font-medium text-white">Inventory backup</h3>
          <p class="mt-1 text-[11px] text-slate-500">
            Export includes folders, hosts, identities, snippets, tags, and known hosts. Secrets are written in plaintext.
          </p>
          <div class="mt-3 flex flex-wrap gap-2">
            <button type="button" class="rounded-lg bg-slate-800 px-3 py-1.5 text-sm hover:bg-slate-700" @click="downloadBackup">
              Download backup
            </button>
            <label class="cursor-pointer rounded-lg bg-slate-800 px-3 py-1.5 text-sm hover:bg-slate-700">
              Import file
              <input ref="importFile" type="file" accept="application/json" class="hidden" @change="onImportBackup" />
            </label>
          </div>
          <p v-if="backupErr" class="mt-2 text-sm text-red-400">{{ backupErr }}</p>
        </div>
        <div class="mt-6 border-t border-slate-800 pt-4">
          <h3 class="text-sm font-medium text-white">Change password</h3>
          <label class="mt-3 block text-xs uppercase text-slate-500">Current password</label>
          <input v-model="passwordForm.current" type="password" class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" autocomplete="current-password" />
          <label class="mt-3 block text-xs uppercase text-slate-500">New password</label>
          <input v-model="passwordForm.next" type="password" class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" autocomplete="new-password" />
          <label class="mt-3 block text-xs uppercase text-slate-500">Confirm new password</label>
          <input v-model="passwordForm.confirm" type="password" class="mt-1 w-full rounded border border-slate-700 bg-surface-overlay px-2 py-1.5 text-sm" autocomplete="new-password" />
          <p v-if="passwordErr" class="mt-2 text-sm text-red-400">{{ passwordErr }}</p>
          <button
            type="button"
            class="mt-3 rounded-lg bg-slate-800 px-3 py-1.5 text-sm hover:bg-slate-700"
            :disabled="passwordBusy"
            @click="submitPassword"
          >
            {{ passwordBusy ? "Updating…" : "Update password" }}
          </button>
        </div>
        <p v-if="settingsErr" class="mt-3 text-sm text-red-400">{{ settingsErr }}</p>
        <div class="mt-6 flex justify-end gap-2">
          <button type="button" class="rounded-lg px-4 py-2 text-sm text-slate-400 hover:bg-slate-800" @click="showSettings = false">Cancel</button>
          <button type="submit" :disabled="settingsBusy" class="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-slate-950">
            {{ settingsBusy ? "Saving…" : "Save" }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

const jsonHeaders = { "Content-Type": "application/json" };
const SESSION_KEY = "vantage_session";

export function sessionToken(): string {
  try {
    return localStorage.getItem(SESSION_KEY) || "";
  } catch {
    return "";
  }
}

export function setSessionToken(token: string | null): void {
  try {
    if (token) localStorage.setItem(SESSION_KEY, token);
    else localStorage.removeItem(SESSION_KEY);
  } catch {
    /* ignore */
  }
}

export function authQuery(): string {
  const t = sessionToken();
  return t ? `token=${encodeURIComponent(t)}` : "";
}

function withAuthQuery(url: string): string {
  const q = authQuery();
  if (!q) return url;
  return url + (url.includes("?") ? "&" : "?") + q;
}

export async function apiFetch(input: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  const t = sessionToken();
  if (t && !headers.has("Authorization")) {
    headers.set("Authorization", "Bearer " + t);
    headers.set("X-Vantage-Session", t);
  }
  return fetch(input, { ...init, credentials: "include", headers });
}

function authXhr(xhr: XMLHttpRequest): void {
  const t = sessionToken();
  if (t) {
    xhr.setRequestHeader("Authorization", "Bearer " + t);
    xhr.setRequestHeader("X-Vantage-Session", t);
  }
}

async function handle<T>(res: Response): Promise<T> {
  const data = (await res.json().catch(() => ({}))) as { error?: string; session?: string };
  if (typeof data.session === "string" && data.session) {
    setSessionToken(data.session);
  }
  if (!res.ok) {
    throw new Error(data.error || res.statusText || "request failed");
  }
  return data as T;
}

function browseParams(folderId: string | null, q: string): string {
  const p = new URLSearchParams();
  if (folderId != null) p.set("folder_id", folderId);
  else p.set("folder_id", "root");
  const t = q.trim();
  if (t) p.set("q", t);
  return p.toString();
}

export const api = {
  async me(): Promise<{
    logged_in: boolean;
    needs_setup?: boolean;
    app_version?: string;
    audit_log_enabled?: boolean;
    mode?: string;
  }> {
    const res = await apiFetch("/api/me", { credentials: "include" });
    return handle(res);
  },

  async setup(username: string, password: string): Promise<void> {
    const res = await apiFetch("/api/setup", {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ username, password }),
    });
    await handle(res);
  },

  async login(username: string, password: string): Promise<void> {
    const res = await apiFetch("/api/login", {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ username, password }),
    });
    await handle(res);
  },

  async logout(): Promise<void> {
    try {
      const res = await apiFetch("/api/logout", {
        method: "POST",
        credentials: "include",
      });
      await handle(res);
    } finally {
      setSessionToken(null);
    }
  },

  async browse(
    folderId: string | null,
    q: string,
  ): Promise<{
    breadcrumb: { id: string; label: string }[];
    folders: FolderRow[];
    hosts: HostRow[];
    search_active: boolean;
  }> {
    const res = await apiFetch(`/api/browse?${browseParams(folderId, q)}`, {
      credentials: "include",
    });
    return handle(res);
  },

  async listHosts(): Promise<HostRow[]> {
    const res = await apiFetch("/api/hosts", { credentials: "include" });
    const d = await handle<{ items: HostRow[] }>(res);
    return d.items;
  },

  async pingHost(id: string): Promise<{ up: boolean; via_jump?: boolean }> {
    const res = await apiFetch(`/api/hosts/${id}/ping`, { credentials: "include" });
    return handle<{ up: boolean; via_jump?: boolean }>(res);
  },

  async pingHosts(ids: string[]): Promise<{
    up: Record<string, boolean>;
    via_jump?: Record<string, boolean>;
  }> {
    const res = await apiFetch("/api/hosts/ping", {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ ids }),
    });
    return handle<{ up: Record<string, boolean>; via_jump?: Record<string, boolean> }>(res);
  },

  async listTags(): Promise<string[]> {
    const res = await apiFetch("/api/tags", { credentials: "include" });
    const d = await handle<{ items: string[] }>(res);
    return d.items;
  },

  async listFoldersFlat(): Promise<FolderRow[]> {
    const res = await apiFetch("/api/folders", { credentials: "include" });
    const d = await handle<{ items: FolderRow[] }>(res);
    return d.items;
  },

  async createFolder(body: {
    label: string;
    parent_id?: string | null;
  }): Promise<{ id: string }> {
    const res = await apiFetch("/api/folders", {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    });
    return handle(res);
  },

  async deleteFolder(id: string): Promise<void> {
    const res = await apiFetch(`/api/folders/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
    await handle(res);
  },

  async updateFolder(
    id: string,
    body: {
      label?: string;
      parent_id?: string | null;
    },
  ): Promise<void> {
    const res = await apiFetch(`/api/folders/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    });
    await handle(res);
  },

  async listIdentities(): Promise<IdentityRow[]> {
    const res = await apiFetch("/api/identities", { credentials: "include" });
    const d = await handle<{ items: IdentityRow[] }>(res);
    return d.items;
  },

  async createHost(body: Record<string, unknown>): Promise<{ id: string }> {
    const res = await apiFetch("/api/hosts", {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    });
    return handle(res);
  },

  async patchHost(id: string, body: Record<string, unknown>): Promise<void> {
    const res = await apiFetch(`/api/hosts/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    });
    await handle(res);
  },

  async deleteHost(id: string): Promise<void> {
    const res = await apiFetch(`/api/hosts/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
    await handle(res);
  },

  async createIdentity(body: Record<string, unknown>): Promise<{ id: string }> {
    const res = await apiFetch("/api/identities", {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    });
    return handle(res);
  },

  async updateIdentity(
    id: string,
    body: Partial<{
      label: string;
      ssh_username: string;
      password: string;
      private_key: string;
      key_passphrase: string;
      domain: string;
    }>,
  ): Promise<void> {
    const res = await apiFetch(`/api/identities/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    });
    await handle(res);
  },

  async deleteIdentity(id: string): Promise<void> {
    const res = await apiFetch(`/api/identities/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
    await handle(res);
  },

  async listSnippets(): Promise<SnippetRow[]> {
    const res = await apiFetch("/api/snippets", { credentials: "include" });
    const d = await handle<{ items: SnippetRow[] }>(res);
    return d.items;
  },

  async createSnippet(body: Record<string, unknown>): Promise<{ id: string }> {
    const res = await apiFetch("/api/snippets", {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    });
    return handle(res);
  },

  async updateSnippet(
    id: string,
    body: Partial<{ label: string; command: string }>,
  ): Promise<void> {
    const res = await apiFetch(`/api/snippets/${id}`, {
      method: "PATCH",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    });
    await handle(res);
  },

  async deleteSnippet(id: string): Promise<void> {
    const res = await apiFetch(`/api/snippets/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
    await handle(res);
  },

  async listConnectionAudit(limit = 200, daysBack?: number): Promise<ConnectionAuditRow[]> {
    const q = new URLSearchParams({ limit: String(limit) });
    if (daysBack !== undefined) {
      q.set("days_back", String(daysBack));
    }
    const res = await apiFetch(`/api/audit/connections?${q.toString()}`, {
      credentials: "include",
    });
    const d = await handle<{ items: ConnectionAuditRow[] }>(res);
    return d.items;
  },

  async listApiKeyScopes(): Promise<ApiKeyScopeDef[]> {
    const res = await apiFetch("/api/api-keys/scopes", { credentials: "include" });
    const d = await handle<{ items: ApiKeyScopeDef[] }>(res);
    return d.items;
  },

  async listApiKeys(): Promise<ApiKeyRow[]> {
    const res = await apiFetch("/api/api-keys", { credentials: "include" });
    const d = await handle<{ items: ApiKeyRow[] }>(res);
    return d.items;
  },

  async createApiKey(body: {
    label: string;
    scopes: string[];
    expires_at?: string | null;
  }): Promise<CreateApiKeyResponse> {
    const res = await apiFetch("/api/api-keys", {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    });
    return handle(res);
  },

  async deleteApiKey(id: string): Promise<void> {
    const res = await apiFetch(`/api/api-keys/${id}`, {
      method: "DELETE",
      credentials: "include",
    });
    await handle(res);
  },

  async getSettings(): Promise<Settings> {
    const res = await apiFetch("/api/settings", { credentials: "include" });
    return handle(res);
  },

  async patchSettings(body: Record<string, unknown>): Promise<Settings> {
    const res = await apiFetch("/api/settings", {
      method: "PATCH",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    });
    return handle(res);
  },

  async changePassword(current: string, next: string): Promise<void> {
    const res = await apiFetch("/api/password", {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ current, new: next }),
    });
    await handle(res);
  },

  async exportInventory(): Promise<Blob> {
    const res = await apiFetch("/api/export", { credentials: "include" });
    if (!res.ok) {
      const data = (await res.json().catch(() => ({}))) as { error?: string };
      throw new Error(data.error || res.statusText);
    }
    return res.blob();
  },

  async importInventory(body: unknown): Promise<void> {
    const res = await apiFetch("/api/import", {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify(body),
    });
    await handle(res);
  },

  async sftpList(connId: string, path: string): Promise<{ path: string; entries: SftpEntry[] }> {
    const res = await apiFetch(`/api/sftp/${connId}/list`, {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ path }),
    });
    return handle(res);
  },

  async sftpMkdir(connId: string, path: string): Promise<void> {
    const res = await apiFetch(`/api/sftp/${connId}/mkdir`, {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ path }),
    });
    await handle(res);
  },

  async sftpRemove(connId: string, path: string): Promise<void> {
    const res = await apiFetch(`/api/sftp/${connId}/remove`, {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ path }),
    });
    await handle(res);
  },

  async sftpRename(connId: string, oldPath: string, newPath: string): Promise<void> {
    const res = await apiFetch(`/api/sftp/${connId}/rename`, {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ old_path: oldPath, new_path: newPath }),
    });
    await handle(res);
  },

  async sftpUpload(
    connId: string,
    path: string,
    file: File,
    onProgress?: (loaded: number, total: number) => void,
  ): Promise<void> {
    const fd = new FormData();
    fd.set("path", path);
    fd.set("file", file);
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open("POST", `/api/sftp/${connId}/upload`);
      xhr.withCredentials = true;
      authXhr(xhr);
      if (onProgress) {
        xhr.upload.onprogress = (e) => {
          if (e.lengthComputable) {
            onProgress(e.loaded, e.total);
          }
        };
      }
      xhr.onload = () => {
        let data: { error?: string } = {};
        try {
          data = JSON.parse(xhr.responseText);
        } catch {
          /* ignore */
        }
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve();
        } else {
          reject(new Error(data.error || xhr.statusText));
        }
      };
      xhr.onerror = () => reject(new Error("Network Error"));
      xhr.send(fd);
    });
  },

  sftpDownloadUrl(connId: string, path: string): string {
    const q = new URLSearchParams({ path });
    return withAuthQuery(`/api/sftp/${connId}/download?${q}`);
  },

  async sharedList(hostId: string, path: string): Promise<{ path: string; entries: SftpEntry[] }> {
    const res = await apiFetch(`/api/shared/${hostId}/list`, {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ path }),
    });
    return handle(res);
  },

  async sharedMkdir(hostId: string, path: string): Promise<void> {
    const res = await apiFetch(`/api/shared/${hostId}/mkdir`, {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ path }),
    });
    await handle(res);
  },

  async sharedRemove(hostId: string, path: string): Promise<void> {
    const res = await apiFetch(`/api/shared/${hostId}/remove`, {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ path }),
    });
    await handle(res);
  },

  async sharedRename(hostId: string, oldPath: string, newPath: string): Promise<void> {
    const res = await apiFetch(`/api/shared/${hostId}/rename`, {
      method: "POST",
      credentials: "include",
      headers: jsonHeaders,
      body: JSON.stringify({ old_path: oldPath, new_path: newPath }),
    });
    await handle(res);
  },

  async sharedUpload(
    hostId: string,
    path: string,
    file: File,
    onProgress?: (loaded: number, total: number) => void,
  ): Promise<void> {
    const fd = new FormData();
    fd.set("path", path);
    fd.set("file", file);
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open("POST", `/api/shared/${hostId}/upload`);
      xhr.withCredentials = true;
      authXhr(xhr);
      if (onProgress) {
        xhr.upload.onprogress = (e) => {
          if (e.lengthComputable) {
            onProgress(e.loaded, e.total);
          }
        };
      }
      xhr.onload = () => {
        let data: { error?: string } = {};
        try {
          data = JSON.parse(xhr.responseText);
        } catch {
          /* ignore */
        }
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve();
        } else {
          reject(new Error(data.error || xhr.statusText));
        }
      };
      xhr.onerror = () => reject(new Error("Network Error"));
      xhr.send(fd);
    });
  },

  sharedDownloadUrl(hostId: string, path: string): string {
    const q = new URLSearchParams({ path });
    return withAuthQuery(`/api/shared/${hostId}/download?${q}`);
  },
};

export interface FolderRow {
  id: string;
  label: string;
  parent_id: string | null;
}

export type HostProtocol = "ssh" | "vnc" | "rdp";

export interface HostRow {
  id: string;
  folder_id: string | null;
  label: string;
  hostname: string;
  port: number;
  protocol: HostProtocol;
  identity_id: string | null;
  jump_host_id: string | null;
  jump_host_label?: string | null;
  identity_label: string;
  identity_auth_type: string;
  folder_label?: string | null;
  last_connected_at?: string | null;
  tags?: string[];
}

export interface IdentityRow {
  id: string;
  label: string;
  auth_type: string;
}

export interface SnippetRow {
  id: string;
  label: string;
  command: string;
}

export interface SftpEntry {
  filename: string;
  st_mode: number;
  st_size: number;
  st_mtime: number;
}

export interface ConnectionAuditRow {
  id: number;
  host_id: string | null;
  host_label: string;
  hostname: string;
  port: number;
  jump_host_id: string | null;
  started_at: string;
  ended_at: string | null;
  duration_seconds: number | null;
}

export interface ApiKeyScopeDef {
  id: string;
  label: string;
  description: string;
}

export interface ApiKeyRow {
  id: string;
  label: string;
  key_prefix: string;
  scopes: string[];
  expires_at: string | null;
  last_used_at: string | null;
  revoked_at: string | null;
  created_at: string;
  expired: boolean;
  active: boolean;
}

export interface CreateApiKeyResponse {
  id: string;
  label: string;
  key_prefix: string;
  scopes: string[];
  expires_at: string | null;
  key: string;
}

export interface Settings {
  listen_addr: string;
  guacd_addr: string;
  terminal_theme: string;
  terminal_font_family: string;
  terminal_font_size: number;
  display_color_depth: number;
  display_width: number;
  display_height: number;
  shared_files_dir: string;
  sync_url: string;
  sync_api_key_set: boolean;
  audit_log_enabled: boolean;
  replica_id: string;
  mode: string;
}

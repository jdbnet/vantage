package httpapi

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/jdbnet/vantage/internal/auth"
	"github.com/jdbnet/vantage/internal/cryptox"
	"github.com/jdbnet/vantage/internal/model"
	"github.com/jdbnet/vantage/internal/sshx"
	"github.com/jdbnet/vantage/internal/store"
)

//go:embed all:dist
var distFS embed.FS

type Deps struct {
	Store           *store.Store
	Box             func() *cryptox.Box
	SetBox          func(*cryptox.Box)
	Jar             *auth.Jar
	Conns           *sshx.Registry
	DataDir         string
	Mode            string
	Version         string
	NeedsSetup      func() bool
	Setup           func(username, password string) error
	Login           func(username, password string) error
	ChangePassword  func(current, newPassword string) error
	OnSettingsSaved func()
}

type Server struct {
	d    Deps
	mux  *http.ServeMux
	upgr websocket.Upgrader
}

func New(d Deps) http.Handler {
	s := &Server{
		d: d,
		upgr: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     func(r *http.Request) bool { return true },
			Subprotocols:    []string{"guacamole"},
		},
	}
	mux := http.NewServeMux()
	s.mux = mux

	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.handleMe)

	mux.HandleFunc("GET /api/identities", s.auth("read:hosts", s.handleListIdentities))
	mux.HandleFunc("POST /api/identities", s.auth("write:hosts", s.handleCreateIdentity))
	mux.HandleFunc("PATCH /api/identities/{id}", s.auth("write:hosts", s.handlePatchIdentity))
	mux.HandleFunc("DELETE /api/identities/{id}", s.auth("write:hosts", s.handleDeleteIdentity))

	mux.HandleFunc("GET /api/snippets", s.auth("read:hosts", s.handleListSnippets))
	mux.HandleFunc("POST /api/snippets", s.auth("write:hosts", s.handleCreateSnippet))
	mux.HandleFunc("PATCH /api/snippets/{id}", s.auth("write:hosts", s.handlePatchSnippet))
	mux.HandleFunc("DELETE /api/snippets/{id}", s.auth("write:hosts", s.handleDeleteSnippet))

	mux.HandleFunc("GET /api/folders", s.auth("read:hosts", s.handleListFolders))
	mux.HandleFunc("POST /api/folders", s.auth("write:hosts", s.handleCreateFolder))
	mux.HandleFunc("PATCH /api/folders/{id}", s.auth("write:hosts", s.handlePatchFolder))
	mux.HandleFunc("DELETE /api/folders/{id}", s.auth("write:hosts", s.handleDeleteFolder))
	mux.HandleFunc("GET /api/browse", s.auth("read:hosts", s.handleBrowse))

	mux.HandleFunc("GET /api/hosts", s.auth("read:hosts", s.handleListHosts))
	mux.HandleFunc("POST /api/hosts", s.auth("write:hosts", s.handleCreateHost))
	mux.HandleFunc("PATCH /api/hosts/{id}", s.auth("write:hosts", s.handlePatchHost))
	mux.HandleFunc("DELETE /api/hosts/{id}", s.auth("write:hosts", s.handleDeleteHost))
	mux.HandleFunc("GET /api/hosts/{id}/ping", s.auth("read:hosts", s.handlePingHost))
	mux.HandleFunc("POST /api/hosts/ping", s.auth("read:hosts", s.handlePingHosts))
	mux.HandleFunc("GET /api/tags", s.auth("read:hosts", s.handleListTags))

	mux.HandleFunc("GET /api/audit/connections", s.auth("read:audit", s.handleAudit))
	mux.HandleFunc("GET /api/api-keys/scopes", s.auth("", s.handleAPIKeyScopes))
	mux.HandleFunc("GET /api/api-keys", s.auth("", s.handleListAPIKeys))
	mux.HandleFunc("POST /api/api-keys", s.auth("", s.handleCreateAPIKey))
	mux.HandleFunc("DELETE /api/api-keys/{id}", s.auth("", s.handleDeleteAPIKey))

	mux.HandleFunc("GET /api/settings", s.auth("", s.handleGetSettings))
	mux.HandleFunc("PATCH /api/settings", s.auth("", s.handlePatchSettings))
	mux.HandleFunc("POST /api/password", s.auth("", s.handleChangePassword))
	mux.HandleFunc("GET /api/export", s.auth("read:hosts", s.handleExport))
	mux.HandleFunc("POST /api/import", s.auth("write:hosts", s.handleImport))

	mux.HandleFunc("GET /api/sync/snapshot", s.auth("sync", s.handleSyncSnapshot))
	mux.HandleFunc("GET /api/sync/changes", s.auth("sync", s.handleSyncChanges))
	mux.HandleFunc("POST /api/sync/push", s.auth("sync", s.handleSyncPush))
	mux.HandleFunc("/ws/sync", s.handleSyncWS)
	mux.HandleFunc("/ws/terminal", s.handleTerminalWS)
	mux.HandleFunc("/ws/guac", s.handleGuacWS)

	mux.HandleFunc("POST /api/sftp/{cid}/list", s.auth("sftp:manage", s.handleSftpList))
	mux.HandleFunc("POST /api/sftp/{cid}/mkdir", s.auth("sftp:manage", s.handleSftpMkdir))
	mux.HandleFunc("POST /api/sftp/{cid}/remove", s.auth("sftp:manage", s.handleSftpRemove))
	mux.HandleFunc("POST /api/sftp/{cid}/rename", s.auth("sftp:manage", s.handleSftpRename))
	mux.HandleFunc("POST /api/sftp/{cid}/upload", s.auth("sftp:manage", s.handleSftpUpload))
	mux.HandleFunc("GET /api/sftp/{cid}/download", s.auth("sftp:manage", s.handleSftpDownload))

	mux.HandleFunc("POST /api/shared/{host_id}/list", s.auth("sftp:manage", s.handleSharedList))
	mux.HandleFunc("POST /api/shared/{host_id}/mkdir", s.auth("sftp:manage", s.handleSharedMkdir))
	mux.HandleFunc("POST /api/shared/{host_id}/remove", s.auth("sftp:manage", s.handleSharedRemove))
	mux.HandleFunc("POST /api/shared/{host_id}/rename", s.auth("sftp:manage", s.handleSharedRename))
	mux.HandleFunc("POST /api/shared/{host_id}/upload", s.auth("sftp:manage", s.handleSharedUpload))
	mux.HandleFunc("GET /api/shared/{host_id}/download", s.auth("sftp:manage", s.handleSharedDownload))

	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		log.Printf("embed dist: %v", err)
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") || strings.HasPrefix(r.URL.Path, "/ws") {
			http.NotFound(w, r)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		f, err := sub.Open(p)
		if err != nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		_ = f.Close()
		fileServer.ServeHTTP(w, r)
	})
	return mux
}

type ctxKey int

const principalKey ctxKey = 1

type principal struct {
	session bool
	scopes  []string
}

func requestToken(r *http.Request) string {
	if t := bearer(r); t != "" {
		return t
	}
	if t := strings.TrimSpace(r.Header.Get("X-Vantage-Session")); t != "" {
		return t
	}
	return r.URL.Query().Get("token")
}

func (s *Server) sessionUser(r *http.Request) (string, bool) {
	if u, err := s.d.Jar.User(r); err == nil {
		return u, true
	}
	tok := requestToken(r)
	if tok == "" {
		return "", false
	}
	u, err := s.d.Jar.Parse(tok)
	if err != nil {
		return "", false
	}
	return u, true
}

func (s *Server) resolveAuth(r *http.Request) *principal {
	if _, ok := s.sessionUser(r); ok {
		return &principal{session: true}
	}
	token := requestToken(r)
	if token == "" {
		return nil
	}
	p, err := s.d.Store.LookupAPIKey(token)
	if err != nil {
		return nil
	}
	return &principal{scopes: p.Scopes}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (s *Server) auth(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := s.resolveAuth(r)
		if p == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !p.session && scope != "" && !store.HasScope(p.scopes, scope) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) requireBox() (*cryptox.Box, error) {
	b := s.d.Box()
	if b == nil {
		return nil, errors.New("vault is locked; sign in first")
	}
	return b, nil
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !s.d.NeedsSetup() {
		writeErr(w, http.StatusConflict, "already set up")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Username) == "" || body.Password == "" {
		writeErr(w, http.StatusBadRequest, "username and password required")
		return
	}
	if err := s.d.Setup(strings.TrimSpace(body.Username), body.Password); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	tok, err := s.d.Jar.Issue(w, strings.TrimSpace(body.Username))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session": tok})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.d.Login(strings.TrimSpace(body.Username), body.Password); err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	tok, err := s.d.Jar.Issue(w, strings.TrimSpace(body.Username))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session": tok})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.d.Jar.Clear(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	st, _ := s.d.Store.LoadSettings()
	_, logged := s.sessionUser(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"logged_in":         logged,
		"needs_setup":       s.d.NeedsSetup(),
		"app_version":       s.d.Version,
		"audit_log_enabled": st.AuditLogEnabled,
		"mode":              s.d.Mode,
	})
}

func (s *Server) handleListIdentities(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListIdentities()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) handleCreateIdentity(w http.ResponseWriter, r *http.Request) {
	box, err := s.requireBox()
	if err != nil {
		writeErr(w, 503, err.Error())
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	id, err := s.createIdentityFromBody(box, body)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) createIdentityFromBody(box *cryptox.Box, body map[string]any) (string, error) {
	label := strings.TrimSpace(asString(body["label"]))
	authType := asString(body["auth_type"])
	user := strings.TrimSpace(firstString(body, "ssh_username", "username"))
	if label == "" || user == "" || (authType != "password" && authType != "publickey") {
		return "", errors.New("label, auth_type, ssh_username required")
	}
	sec := model.IdentitySecret{Username: user, Domain: asString(body["domain"])}
	var passEnc *string
	if authType == "password" {
		sec.Password = asString(body["password"])
		if sec.Password == "" {
			return "", errors.New("password required")
		}
	} else {
		sec.PrivateKey = asString(body["private_key"])
		if sec.PrivateKey == "" {
			return "", errors.New("private_key required")
		}
		if kp := asString(body["key_passphrase"]); kp != "" {
			enc, err := box.Encrypt(kp)
			if err != nil {
				return "", err
			}
			passEnc = &enc
		}
	}
	raw, _ := json.Marshal(sec)
	blob, err := box.Encrypt(string(raw))
	if err != nil {
		return "", err
	}
	return s.d.Store.CreateIdentity(label, model.AuthType(authType), blob, passEnc)
}

func (s *Server) handlePatchIdentity(w http.ResponseWriter, r *http.Request) {
	box, err := s.requireBox()
	if err != nil {
		writeErr(w, 503, err.Error())
		return
	}
	id := r.PathValue("id")
	rec, err := s.d.Store.GetIdentity(id)
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	plain, err := box.Decrypt(rec.Blob)
	if err != nil {
		writeErr(w, 500, "cannot update identity")
		return
	}
	var sec model.IdentitySecret
	_ = json.Unmarshal([]byte(plain), &sec)
	if u := strings.TrimSpace(firstString(body, "ssh_username", "username")); u != "" {
		sec.Username = u
	}
	if p := asString(body["password"]); p != "" {
		sec.Password = p
	}
	if k := asString(body["private_key"]); k != "" {
		sec.PrivateKey = k
	}
	if d := asString(body["domain"]); d != "" {
		sec.Domain = d
	}
	raw, _ := json.Marshal(sec)
	blob, err := box.Encrypt(string(raw))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	label := strings.TrimSpace(asString(body["label"]))
	passSet := false
	var pass *string
	if _, ok := body["key_passphrase"]; ok {
		passSet = true
		if kp := asString(body["key_passphrase"]); kp != "" {
			enc, err := box.Encrypt(kp)
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			pass = &enc
		}
	}
	if err := s.d.Store.UpdateIdentity(id, label, blob, pass, passSet); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleDeleteIdentity(w http.ResponseWriter, r *http.Request) {
	err := s.d.Store.DeleteIdentity(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "not found")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, 409, err.Error())
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleListSnippets(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListSnippets()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) handleCreateSnippet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label   string `json:"label"`
		Command string `json:"command"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Label) == "" || body.Command == "" {
		writeErr(w, 400, "label and command required")
		return
	}
	id, err := s.d.Store.CreateSnippet(strings.TrimSpace(body.Label), body.Command)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) handlePatchSnippet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label   *string `json:"label"`
		Command *string `json:"command"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	if err := s.d.Store.UpdateSnippet(r.PathValue("id"), body.Label, body.Command); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleDeleteSnippet(w http.ResponseWriter, r *http.Request) {
	if err := s.d.Store.DeleteSnippet(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListFolders()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label    string  `json:"label"`
		ParentID *string `json:"parent_id"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Label) == "" {
		writeErr(w, 400, "label required")
		return
	}
	id, err := s.d.Store.CreateFolder(strings.TrimSpace(body.Label), emptyNil(body.ParentID))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) handlePatchFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label    *string `json:"label"`
		ParentID *string `json:"parent_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	label := ""
	if body.Label != nil {
		label = strings.TrimSpace(*body.Label)
	}
	_, parentSet := r.Context().Deadline()
	_ = parentSet
	parentSet = false
	var parent *string
	raw := map[string]json.RawMessage{}
	// re-read is awkward; use body.ParentID presence via pointer from JSON null vs omit
	if body.Label != nil || body.ParentID != nil {
		if body.ParentID != nil {
			parentSet = true
			parent = emptyNil(body.ParentID)
		}
	}
	_ = raw
	if err := s.d.Store.UpdateFolder(r.PathValue("id"), label, parent, parentSet); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	if err := s.d.Store.DeleteFolder(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	fid := r.URL.Query().Get("folder_id")
	var folderID *string
	if fid != "" && fid != "root" {
		folderID = &fid
	}
	res, err := s.d.Store.Browse(folderID, q)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListHosts()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListTags()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	box, err := s.requireBox()
	if err != nil {
		writeErr(w, 503, err.Error())
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	wri, err := s.hostWriteFromBody(box, body, true)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	id, err := s.d.Store.CreateHost(wri)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) handlePatchHost(w http.ResponseWriter, r *http.Request) {
	box, err := s.requireBox()
	if err != nil {
		writeErr(w, 503, err.Error())
		return
	}
	id := r.PathValue("id")
	cur, err := s.d.Store.GetHostRecord(id)
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	merged := map[string]any{
		"label": cur.Host.Label, "hostname": cur.Host.Hostname, "port": cur.Host.Port,
		"protocol": cur.Host.Protocol, "folder_id": cur.Host.FolderID, "identity_id": cur.Host.IdentityID,
		"jump_host_id": cur.Host.JumpHostID,
	}
	for k, v := range body {
		merged[k] = v
	}
	if _, ok := body["use_inline_identity"]; !ok && cur.InlineBlob != nil {
		merged["use_inline_identity"] = true
		merged["_keep_inline"] = true
	}
	wri, err := s.hostWriteFromBody(box, merged, false)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if keep, _ := merged["_keep_inline"].(bool); keep && wri.InlineBlob == nil {
		wri.InlineAuthType = cur.InlineAuthType
		wri.InlineBlob = cur.InlineBlob
		wri.InlinePassphrase = cur.InlinePassphrase
		wri.IdentityID = nil
	}
	if tags, ok := body["tags"]; ok {
		wri.Tags = asStringSlice(tags)
	} else {
		wri.Tags = nil
	}
	if err := s.d.Store.UpdateHost(id, wri); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	if err := s.d.Store.DeleteHost(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) hostWriteFromBody(box *cryptox.Box, body map[string]any, creating bool) (store.HostWrite, error) {
	var w store.HostWrite
	w.Label = strings.TrimSpace(asString(body["label"]))
	w.Hostname = strings.TrimSpace(asString(body["hostname"]))
	if w.Label == "" || w.Hostname == "" {
		return w, errors.New("label, hostname required")
	}
	w.Port = asInt(body["port"], 22)
	proto := asString(body["protocol"])
	if proto == "" {
		proto = "ssh"
	}
	w.Protocol = model.Protocol(proto)
	w.FolderID = asOptString(body["folder_id"])
	w.JumpHostID = asOptString(body["jump_host_id"])
	if tags, ok := body["tags"]; ok {
		w.Tags = asStringSlice(tags)
	}
	useInline, _ := body["use_inline_identity"].(bool)
	if useInline {
		authType := asString(body["auth_type"])
		user := strings.TrimSpace(firstString(body, "ssh_username", "username"))
		if creating {
			if w.Protocol != model.ProtocolSSH {
				authType = "password"
			}
			if authType == "" {
				return w, errors.New("auth_type required for inline identity")
			}
			if user == "" && w.Protocol != model.ProtocolVNC {
				return w, errors.New("username required for inline identity")
			}
		}
		if authType != "" && (user != "" || w.Protocol == model.ProtocolVNC) {
			sec := model.IdentitySecret{Username: user, Domain: asString(body["domain"])}
			if authType == "password" {
				sec.Password = asString(body["password"])
			} else {
				sec.PrivateKey = asString(body["private_key"])
				if kp := asString(body["key_passphrase"]); kp != "" {
					enc, err := box.Encrypt(kp)
					if err != nil {
						return w, err
					}
					w.InlinePassphrase = &enc
				}
			}
			raw, _ := json.Marshal(sec)
			blob, err := box.Encrypt(string(raw))
			if err != nil {
				return w, err
			}
			w.InlineAuthType = &authType
			w.InlineBlob = &blob
		}
		w.IdentityID = nil
	} else {
		w.IdentityID = asOptString(body["identity_id"])
		if creating && w.IdentityID == nil {
			return w, errors.New("identity_id required when not using inline identity")
		}
	}
	return w, nil
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var days *int
	if v := r.URL.Query().Get("days_back"); v != "" {
		n, _ := strconv.Atoi(v)
		days = &n
	}
	items, err := s.d.Store.ListAudit(limit, days)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) handleAPIKeyScopes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"items": store.DefaultScopes})
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListAPIKeys()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label     string   `json:"label"`
		Scopes    []string `json:"scopes"`
		ExpiresAt *string  `json:"expires_at"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Label) == "" || len(body.Scopes) == 0 {
		writeErr(w, 400, "label and scopes required")
		return
	}
	k, secret, err := s.d.Store.CreateAPIKey(strings.TrimSpace(body.Label), body.Scopes, body.ExpiresAt)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{
		"id": k.ID, "label": k.Label, "key_prefix": k.KeyPrefix, "scopes": k.Scopes,
		"expires_at": k.ExpiresAt, "key": secret,
	})
}

func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := s.d.Store.DeleteAPIKey(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.d.Store.LoadSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	st.Mode = s.d.Mode
	st.SharedFilesDir = s.sharedFilesDir(st)
	writeJSON(w, 200, st)
}

func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	patch := map[string]string{}
	for k, v := range body {
		if k == "sync_api_key" && asString(v) == "" {
			continue
		}
		switch t := v.(type) {
		case bool:
			if t {
				patch[k] = "true"
			} else {
				patch[k] = "false"
			}
		case float64:
			patch[k] = strconv.Itoa(int(t))
		default:
			patch[k] = asString(v)
		}
	}
	if err := s.d.Store.SaveSettings(patch); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if s.d.OnSettingsSaved != nil {
		s.d.OnSettingsSaved()
	}
	st, _ := s.d.Store.LoadSettings()
	st.Mode = s.d.Mode
	st.SharedFilesDir = s.sharedFilesDir(st)
	writeJSON(w, 200, st)
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if s.d.ChangePassword == nil {
		writeErr(w, 500, "password change is not available")
		return
	}
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	if err := s.d.ChangePassword(body.Current, body.New); err != nil {
		msg := err.Error()
		if msg == "invalid credentials" {
			writeErr(w, 401, msg)
			return
		}
		if msg == "vault locked" {
			writeErr(w, 503, msg)
			return
		}
		writeErr(w, 400, msg)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(v)
		s := strings.Trim(string(b), `"`)
		if s == "null" {
			return ""
		}
		return s
	}
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := asString(m[k]); s != "" {
			return s
		}
	}
	return ""
}

func asInt(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, err := strconv.Atoi(t)
		if err == nil {
			return n
		}
	}
	return def
}

func asOptString(v any) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(asString(v))
	if s == "" || s == "null" {
		return nil
	}
	return &s
}

func asStringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			out = append(out, asString(x))
		}
		return out
	case []string:
		return t
	}
	return nil
}

func emptyNil(p *string) *string {
	if p == nil {
		return nil
	}
	s := strings.TrimSpace(*p)
	if s == "" {
		return nil
	}
	return &s
}

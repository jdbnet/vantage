package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/jdbnet/vantage/internal/explorer"
	"github.com/jdbnet/vantage/internal/sshx"
	"github.com/jdbnet/vantage/internal/store"
)

func remoteProtocols() []string {
	out := []string{"s3", "sftp", "smb", "webdav", "ftp"}
	if runtime.GOOS != "windows" {
		out = append(out, "nfs")
	}
	return out
}

func (s *Server) handleRemoteProtocols(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"protocols": remoteProtocols()})
}

func (s *Server) handleListRemotes(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListRemotes()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

type remoteBody struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Protocol    string `json:"protocol"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Bucket      string `json:"bucket"`
	Region      string `json:"region"`
	PathStyle   bool   `json:"path_style"`
	Secure      bool   `json:"secure"`
	Username    string `json:"username"`
	SftpKeyAuth bool   `json:"sftp_key_auth"`
	Secret      string `json:"secret"`
	Passphrase  string `json:"passphrase"`
}

func (s *Server) remoteWrite(body remoteBody, existing string, blobSet bool) (store.RemoteWrite, error) {
	label := strings.TrimSpace(body.Label)
	protocol := strings.TrimSpace(body.Protocol)
	if label == "" || protocol == "" {
		return store.RemoteWrite{}, errors.New("label and protocol required")
	}
	ok := false
	for _, p := range remoteProtocols() {
		if p == protocol {
			ok = true
		}
	}
	if !ok {
		return store.RemoteWrite{}, errors.New("unsupported protocol")
	}
	w := store.RemoteWrite{
		Label: label, Protocol: protocol, Host: strings.TrimSpace(body.Host), Port: body.Port,
		Bucket: strings.TrimSpace(body.Bucket), Region: strings.TrimSpace(body.Region),
		PathStyle: body.PathStyle, Secure: body.Secure, Username: strings.TrimSpace(body.Username),
		SftpKeyAuth: body.SftpKeyAuth, BlobSet: blobSet,
	}
	if blobSet {
		box, err := s.requireBox()
		if err != nil {
			return store.RemoteWrite{}, err
		}
		raw, err := json.Marshal(remoteSecret{Secret: body.Secret, Passphrase: body.Passphrase})
		if err != nil {
			return store.RemoteWrite{}, err
		}
		blob, err := box.Encrypt(string(raw))
		if err != nil {
			return store.RemoteWrite{}, err
		}
		w.Blob = blob
	}
	return w, nil
}

func (s *Server) handleCreateRemote(w http.ResponseWriter, r *http.Request) {
	var body remoteBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	if strings.TrimSpace(body.Secret) == "" && body.Protocol != "nfs" {
		writeErr(w, 400, "secret required")
		return
	}
	wr, err := s.remoteWrite(body, "", true)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	id, err := s.d.Store.CreateRemote(wr)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) handleUpdateRemote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body remoteBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	blobSet := strings.TrimSpace(body.Secret) != "" || strings.TrimSpace(body.Passphrase) != ""
	if blobSet && strings.TrimSpace(body.Secret) == "" {
		writeErr(w, 400, "enter the secret again to change the passphrase")
		return
	}
	wr, err := s.remoteWrite(body, id, blobSet)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := s.d.Store.UpdateRemote(id, wr); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 400, err.Error())
		return
	}
	if s.remotes != nil {
		s.remotes.drop(id)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleDeleteRemote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.d.Store.DeleteRemote(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 400, err.Error())
		return
	}
	if s.remotes != nil {
		s.remotes.drop(id)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) configFromBody(body remoteBody) (explorer.Config, error) {
	cfg := explorer.Config{
		ID: body.ID, Label: strings.TrimSpace(body.Label), Protocol: strings.TrimSpace(body.Protocol),
		Host: strings.TrimSpace(body.Host), Port: body.Port, Bucket: strings.TrimSpace(body.Bucket),
		Region: strings.TrimSpace(body.Region), PathStyle: body.PathStyle, Secure: body.Secure,
		Username: strings.TrimSpace(body.Username), SftpKeyAuth: body.SftpKeyAuth,
		Secret: body.Secret, Passphrase: body.Passphrase,
	}
	if strings.TrimSpace(cfg.Secret) == "" && body.ID != "" && s.remotes != nil {
		loaded, err := s.remotes.load(body.ID)
		if err != nil {
			return explorer.Config{}, err
		}
		if cfg.Protocol == "" {
			cfg = loaded
		} else {
			cfg.Secret = loaded.Secret
			cfg.Passphrase = loaded.Passphrase
		}
	}
	if cfg.Protocol == "" {
		return explorer.Config{}, errors.New("protocol required")
	}
	return cfg, nil
}

func (s *Server) handleTestRemote(w http.ResponseWriter, r *http.Request) {
	var body remoteBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	if id := r.PathValue("id"); id != "" {
		body.ID = id
	}
	cfg, err := s.configFromBody(body)
	if err != nil {
		s.writeRemoteErr(w, err)
		return
	}
	exp, err := s.remotes.dial(cfg)
	if err != nil {
		s.writeRemoteErr(w, err)
		return
	}
	defer exp.Disconnect()
	if _, err := exp.Stat(cfg.Bucket); err != nil {
		if _, err2 := exp.ListDir(cfg.Bucket); err2 != nil {
			s.writeRemoteErr(w, err2)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleTrustHostKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hostname    string `json:"hostname"`
		Port        int    `json:"port"`
		KeyType     string `json:"key_type"`
		Fingerprint string `json:"fingerprint"`
		PublicKey   string `json:"public_key"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	if body.Hostname == "" || body.Port == 0 || body.PublicKey == "" || body.Fingerprint == "" {
		writeErr(w, 400, "host key details required")
		return
	}
	if _, err := s.d.Store.UpsertKnownHost(body.Hostname, body.Port, body.KeyType, body.Fingerprint, body.PublicKey); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) writeRemoteErr(w http.ResponseWriter, err error) {
	var hk *sshx.UntrustedHostKeyError
	if errors.As(err, &hk) {
		writeJSON(w, 409, map[string]any{
			"error": hk.Error(),
			"host_key": map[string]any{
				"hostname": hk.Info.Hostname, "port": hk.Info.Port, "key_type": hk.Info.KeyType,
				"fingerprint": hk.Info.Fingerprint, "public_key": hk.PublicKey, "status": hk.Info.Status,
			},
		})
		return
	}
	writeErr(w, 400, err.Error())
}

func fileEntriesJSON(list []explorer.FileEntry) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		mode := 0o100644
		if e.IsDir {
			mode = 0o040755
		}
		var mt int64
		if t, err := time.Parse(time.RFC3339, e.Modified); err == nil {
			mt = t.Unix()
		}
		out = append(out, map[string]any{
			"filename": e.Name, "st_mode": mode, "st_size": e.Size, "st_mtime": mt,
		})
	}
	return out
}

func (s *Server) withRemote(w http.ResponseWriter, id string, fn func(explorer.Explorer) error) {
	if s.remotes == nil {
		writeErr(w, 500, "remotes unavailable")
		return
	}
	err := func() error {
		exp, release, err := s.remotes.acquire(id, false)
		if err != nil {
			return err
		}
		defer release()
		return fn(exp)
	}()
	if err != nil {
		s.remotes.drop(id)
		exp, release, err2 := s.remotes.acquire(id, false)
		if err2 != nil {
			s.writeRemoteErr(w, err)
			return
		}
		defer release()
		if err = fn(exp); err != nil {
			s.writeRemoteErr(w, err)
		}
	}
}

func (s *Server) handleRemoteList(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	_ = readJSON(r, &body)
	id := r.PathValue("id")
	var wrote bool
	s.withRemote(w, id, func(exp explorer.Explorer) error {
		entries, err := exp.ListDir(body.Path)
		if err != nil {
			return err
		}
		shown := body.Path
		if st, err := exp.Stat(body.Path); err == nil && st.Path != "" {
			shown = st.Path
		}
		if shown == "" {
			shown = "/"
		}
		writeJSON(w, 200, map[string]any{"path": shown, "entries": fileEntriesJSON(entries)})
		wrote = true
		return nil
	})
	_ = wrote
}

func (s *Server) handleRemoteMkdir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	s.withRemote(w, r.PathValue("id"), func(exp explorer.Explorer) error {
		if err := exp.MkDir(body.Path); err != nil {
			return err
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return nil
	})
}

func (s *Server) handleRemoteRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	s.withRemote(w, r.PathValue("id"), func(exp explorer.Explorer) error {
		if err := exp.Rename(body.From, body.To); err != nil {
			return err
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return nil
	})
}

func (s *Server) handleRemoteRemove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	s.withRemote(w, r.PathValue("id"), func(exp explorer.Explorer) error {
		if err := exp.Delete(body.Path); err != nil {
			return err
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return nil
	})
}

func (s *Server) handleRemoteStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := r.URL.Query().Get("path")
	if s.remotes == nil {
		writeErr(w, 500, "remotes unavailable")
		return
	}
	exp, release, err := s.remotes.acquire(id, true)
	if err != nil {
		s.writeRemoteErr(w, err)
		return
	}
	defer release()
	serveExplorer(w, r, path.Base(p), exp, p)
}

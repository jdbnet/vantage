package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jdbnet/vantage/internal/model"
	"github.com/jdbnet/vantage/internal/syncx"
)

func (s *Server) sharedFilesDir(st model.Settings) string {
	if s.d.Mode == "desktop" {
		return filepath.Join(s.d.DataDir, "shared")
	}
	dir := strings.TrimSpace(st.SharedFilesDir)
	if dir == "" {
		dir = filepath.Join(s.d.DataDir, "shared")
	}
	return dir
}

// guacdDrivePath is drive-path sent to guacd. It is a path in guacd's filesystem,
// which is not the same as vantaged's unless they share a mount.
func (s *Server) guacdDrivePath(st model.Settings) string {
	if p := strings.TrimSpace(st.GuacdDrivePath); p != "" {
		return p
	}
	abs, err := filepath.Abs(s.sharedFilesDir(st))
	if err != nil {
		return s.sharedFilesDir(st)
	}
	return abs
}

func guacdIsLoopback(addr string) bool {
	host := strings.TrimSpace(addr)
	if host == "" {
		return false
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// rdpDrivePath is the path sent to guacd as drive-path.
// Desktop with sync and a remote guacd uses vantaged's path so the guest sees
// the same folder as the web UI. Local guacd keeps the OS data-dir shared folder.
func (s *Server) rdpDrivePath(st model.Settings) string {
	local := s.sharedFilesDir(st)
	abs, err := filepath.Abs(local)
	if err != nil {
		abs = local
	}
	if s.d.Mode != "desktop" {
		return s.guacdDrivePath(st)
	}
	if strings.TrimSpace(st.SyncURL) == "" || guacdIsLoopback(st.GuacdAddr) {
		return abs
	}
	if s.d.Store == nil {
		return abs
	}
	key, ok, _ := s.d.Store.SyncAPIKey()
	if !ok || key == "" {
		return abs
	}
	remote, err := syncx.FetchRemoteDrivePath(st.SyncURL, key)
	if err != nil || remote == "" {
		log.Printf("rdp drive: vantaged path unavailable (%v); using %s", err, abs)
		return abs
	}
	return remote
}

func repairSharedAccess(root string) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return skipWalkErr(root, p, d, err)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		if rel != "." && ignoredSharedPath(rel) {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d != nil && d.IsDir() {
			_ = os.Chmod(p, 0o777)
		} else if d != nil && d.Type().IsRegular() {
			_ = os.Chmod(p, 0o666)
		}
		return nil
	})
}

func logSharedDrive(local, guacdPath string) {
	fi, err := os.Stat(local)
	if err != nil {
		log.Printf("rdp drive local=%s guacd=%s: %v", local, guacdPath, err)
		return
	}
	perm := fi.Mode().Perm()
	if perm&0o007 != 0o007 {
		log.Printf("rdp drive local=%s perm=%s guacd=%s: directory is not world-writable; guacamole/guacd is UID 1000 and will not be able to list or write files", local, perm, guacdPath)
		return
	}
	log.Printf("rdp drive local=%s perm=%s guacd=%s", local, perm, guacdPath)
}

// ensureSharedDir creates dir so both vantaged (UID 65532) and guacd (often UID 1000) can write.
func ensureSharedDir(dir string) error {
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		if st, stErr := os.Stat(dir); stErr == nil && st.IsDir() {
			return nil
		}
		return err
	}
	return nil
}

func ensureSharedFileMode(path string) error {
	return os.Chmod(path, 0o666)
}

func (s *Server) sharedHostRoot(hostID string) (string, error) {
	if _, err := s.d.Store.GetHost(hostID); err != nil {
		return "", err
	}
	st, _ := s.d.Store.LoadSettings()
	abs, err := filepath.Abs(s.sharedFilesDir(st))
	if err != nil {
		return "", err
	}
	if err := ensureSharedDir(abs); err != nil {
		return "", err
	}
	repairSharedAccess(abs)
	return abs, nil
}

func (s *Server) sharedResolve(hostID, rel string) (string, error) {
	root, err := s.sharedHostRoot(hostID)
	if err != nil {
		return "", err
	}
	return jailJoin(root, rel)
}

func jailJoin(root, rel string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = resolved
	}
	if rel == "" {
		rel = "/"
	}
	clean := path.Clean("/" + strings.TrimPrefix(rel, "/"))
	full := filepath.Join(rootAbs, filepath.FromSlash(strings.TrimPrefix(clean, "/")))
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
		return "", os.ErrInvalid
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		if resolved != rootAbs && !strings.HasPrefix(resolved, rootAbs+string(os.PathSeparator)) {
			return "", os.ErrInvalid
		}
		return resolved, nil
	}
	return abs, nil
}

type sharedFileMeta struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
	Dir   bool   `json:"dir"`
}

func (s *Server) sharedRoot() (string, error) {
	st, _ := s.d.Store.LoadSettings()
	abs, err := filepath.Abs(s.sharedFilesDir(st))
	if err != nil {
		return "", err
	}
	if err := ensureSharedDir(abs); err != nil {
		return "", err
	}
	repairSharedAccess(abs)
	return abs, nil
}

var skippedWalk sync.Map

// ignoredSharedPath is the guacd RDP drive Download folder. It is created as
// 0700 owned by guacd's UID, so vantaged cannot list it. Keep it off file sync.
func ignoredSharedPath(rel string) bool {
	slash := strings.Trim(filepath.ToSlash(rel), "/")
	return slash == "Download" || strings.HasPrefix(slash, "Download/")
}

func skipIgnoredRel(rel string, d os.DirEntry) error {
	if !ignoredSharedPath(rel) {
		return nil
	}
	if d != nil && d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

// skipWalkErr keeps listing the rest of the tree when a path is unreadable.
func skipWalkErr(root, p string, d os.DirEntry, err error) error {
	if p == root {
		return err
	}
	if _, dup := skippedWalk.LoadOrStore(p, struct{}{}); !dup {
		log.Printf("shared files: skip %s: %v", p, err)
	}
	if d != nil && d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

func listSharedTree(root string) ([]sharedFileMeta, error) {
	var out []sharedFileMeta
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return skipWalkErr(root, p, d, err)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			_ = os.Chmod(p, 0o777)
			return nil
		}
		if err := skipIgnoredRel(rel, d); err != nil || ignoredSharedPath(rel) {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if strings.HasSuffix(rel, ".vantage-tmp") {
			return nil
		}
		if !d.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		if d.IsDir() {
			_ = os.Chmod(p, 0o777)
		} else {
			_ = os.Chmod(p, 0o666)
		}
		out = append(out, sharedFileMeta{
			Path:  filepath.ToSlash(rel),
			Size:  info.Size(),
			Mtime: info.ModTime().Unix(),
			Dir:   d.IsDir(),
		})
		return nil
	})
	if out == nil {
		out = []sharedFileMeta{}
	}
	return out, err
}

func (s *Server) handleSharedList(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("host_id")
	var body struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	abs, err := s.sharedResolve(hostID, body.Path)
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	root, err := s.sharedHostRoot(hostID)
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	fis, err := os.ReadDir(abs)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	rel := sharedRel(abs, root)
	entries := make([]map[string]any, 0, len(fis))
	for _, fi := range fis {
		info, err := fi.Info()
		if err != nil {
			continue
		}
		mode := uint32(info.Mode().Perm())
		if info.IsDir() {
			mode |= 0o040000
		} else {
			mode |= 0o100000
		}
		entries = append(entries, map[string]any{
			"filename": fi.Name(),
			"st_mode":  mode,
			"st_size":  info.Size(),
			"st_mtime": info.ModTime().Unix(),
		})
	}
	writeJSON(w, 200, map[string]any{"path": rel, "entries": entries})
}

func (s *Server) handleSharedMkdir(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("host_id")
	var body struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	abs, err := s.sharedResolve(hostID, body.Path)
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	if err := ensureSharedDir(abs); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleSharedRemove(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("host_id")
	var body struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	abs, err := s.sharedResolve(hostID, body.Path)
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	root, err := s.sharedHostRoot(hostID)
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	if abs == root {
		writeErr(w, 400, "cannot delete shared root")
		return
	}
	if err := os.Remove(abs); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleSharedRename(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("host_id")
	var body struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	oldAbs, err := s.sharedResolve(hostID, body.OldPath)
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	newAbs, err := s.sharedResolve(hostID, body.NewPath)
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleSharedUpload(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("host_id")
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file required")
		return
	}
	defer file.Close()
	dest, err := s.sharedResolve(hostID, path.Join(r.FormValue("path"), path.Base(hdr.Filename)))
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	dst, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = ensureSharedFileMode(dest)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleSharedDownload(w http.ResponseWriter, r *http.Request) {
	hostID := r.PathValue("host_id")
	abs, err := s.sharedResolve(hostID, r.URL.Query().Get("path"))
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		writeErr(w, 400, "not a file")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+path.Base(abs)+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, f)
}

func writeSharedErr(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrInvalid) {
		writeErr(w, 400, "invalid path")
		return
	}
	writeErr(w, 404, err.Error())
}

func sharedRel(abs, root string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." {
		return "/"
	}
	return "/" + filepath.ToSlash(rel)
}

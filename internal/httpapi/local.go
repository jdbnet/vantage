package httpapi

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jdbnet/vantage/internal/explorer"
)

func cleanLocalPath(p string) (string, error) {
	return explorer.CleanLocalPath(p)
}

func localIsRoot(p string) bool {
	return explorer.LocalRoot(p)
}

func (s *Server) handleLocalList(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	_ = readJSON(r, &body)
	p, err := cleanLocalPath(body.Path)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	fis, err := os.ReadDir(p)
	if err != nil && strings.TrimSpace(body.Path) == "" && !localIsRoot(p) {
		p = string(os.PathSeparator)
		fis, err = os.ReadDir(p)
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	entries := make([]map[string]any, 0, len(fis))
	for _, de := range fis {
		info, err := de.Info()
		if err != nil {
			continue
		}
		mode := uint32(0o100644)
		if info.IsDir() {
			mode = 0o040755
		}
		entries = append(entries, map[string]any{
			"filename": info.Name(),
			"st_mode":  mode,
			"st_size":  info.Size(),
			"st_mtime": info.ModTime().Unix(),
		})
	}
	writeJSON(w, 200, map[string]any{"path": p, "entries": entries})
}

func (s *Server) handleLocalMkdir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	p, err := cleanLocalPath(body.Path)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := os.Mkdir(p, 0o755); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleLocalRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	oldP, err := cleanLocalPath(body.OldPath)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	newP, err := cleanLocalPath(body.NewPath)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := os.Rename(oldP, newP); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleLocalRemove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	p, err := cleanLocalPath(body.Path)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if localIsRoot(p) {
		writeErr(w, 400, "refusing to delete the filesystem root")
		return
	}
	if err := os.RemoveAll(p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleLocalDownload(w http.ResponseWriter, r *http.Request) {
	p, err := cleanLocalPath(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	f, err := os.Open(p)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(p)+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, f)
}

func (s *Server) handleLocalStream(w http.ResponseWriter, r *http.Request) {
	p, err := cleanLocalPath(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	f, err := os.Open(p)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if info.IsDir() {
		writeErr(w, 400, "not a file")
		return
	}
	serveStream(w, r, filepath.Base(p), info.Size(), f)
}

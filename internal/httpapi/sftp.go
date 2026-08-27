package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/pkg/sftp"
)

func (s *Server) sftpClient(w http.ResponseWriter, r *http.Request) (*sftp.Client, bool) {
	sess := s.d.Conns.Get(r.PathValue("cid"))
	if sess == nil {
		writeErr(w, 404, "unknown connection")
		return nil, false
	}
	c, err := sess.GetSFTP()
	if err != nil {
		writeErr(w, 500, err.Error())
		return nil, false
	}
	return c, true
}

func (s *Server) handleSftpList(w http.ResponseWriter, r *http.Request) {
	c, ok := s.sftpClient(w, r)
	if !ok {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	p, err := sanitizePath(body.Path)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	fis, err := c.ReadDir(p)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	entries := make([]map[string]any, 0, len(fis))
	for _, fi := range fis {
		mode := uint32(fi.Mode())
		if st, ok := fi.Sys().(*sftp.FileStat); ok {
			mode = st.Mode
		}
		entries = append(entries, map[string]any{
			"filename": fi.Name(),
			"st_mode":  mode,
			"st_size":  fi.Size(),
			"st_mtime": fi.ModTime().Unix(),
		})
	}
	writeJSON(w, 200, map[string]any{"path": p, "entries": entries})
}

func (s *Server) handleSftpMkdir(w http.ResponseWriter, r *http.Request) {
	c, ok := s.sftpClient(w, r)
	if !ok {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	p, err := sanitizePath(body.Path)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := c.Mkdir(p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleSftpRemove(w http.ResponseWriter, r *http.Request) {
	c, ok := s.sftpClient(w, r)
	if !ok {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	p, err := sanitizePath(body.Path)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	fi, err := c.Lstat(p)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if fi.IsDir() {
		err = c.RemoveDirectory(p)
	} else {
		err = c.Remove(p)
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleSftpRename(w http.ResponseWriter, r *http.Request) {
	c, ok := s.sftpClient(w, r)
	if !ok {
		return
	}
	var body struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	oldP, err := sanitizePath(body.OldPath)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	newP, err := sanitizePath(body.NewPath)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := c.Rename(oldP, newP); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleSftpUpload(w http.ResponseWriter, r *http.Request) {
	c, ok := s.sftpClient(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	dir := r.FormValue("path")
	pdir, err := sanitizePath(dir)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file required")
		return
	}
	defer file.Close()
	dest := path.Join(pdir, path.Base(hdr.Filename))
	dst, err := c.Create(dest)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleSftpDownload(w http.ResponseWriter, r *http.Request) {
	c, ok := s.sftpClient(w, r)
	if !ok {
		return
	}
	p, err := sanitizePath(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	f, err := c.Open(p)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Disposition", "attachment; filename=\""+path.Base(p)+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, f)
}

func sanitizePath(p string) (string, error) {
	if p == "" {
		p = "/"
	}
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", os.ErrInvalid
	}
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return clean, nil
}

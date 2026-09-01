package httpapi

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxSyncFileBytes = 512 << 20

func (s *Server) handleSyncDrive(w http.ResponseWriter, r *http.Request) {
	st, _ := s.d.Store.LoadSettings()
	writeJSON(w, 200, map[string]any{"drive_path": s.guacdDrivePath(st)})
}

func (s *Server) handleSyncFileList(w http.ResponseWriter, r *http.Request) {
	root, err := s.sharedRoot()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	start := time.Now()
	files, err := listSharedTree(root)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if d := time.Since(start); d > 200*time.Millisecond || len(files) > 200 {
		log.Printf("sync files list count=%d in %s", len(files), d.Round(time.Millisecond))
	}
	writeJSON(w, 200, map[string]any{"files": files})
}

func (s *Server) handleSyncFileGet(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if ignoredSharedPath(rel) {
		writeErr(w, 400, "excluded from sync")
		return
	}
	root, err := s.sharedRoot()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	abs, err := jailJoin(root, rel)
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		writeErr(w, 400, "not a file")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Vantage-Mtime", strconv.FormatInt(st.ModTime().Unix(), 10))
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	_, _ = io.Copy(w, f)
}

func (s *Server) handleSyncFilePut(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if ignoredSharedPath(rel) {
		writeErr(w, 400, "excluded from sync")
		return
	}
	root, err := s.sharedRoot()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	abs, err := jailJoin(root, rel)
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	if r.Header.Get("X-Vantage-Dir") == "1" {
		if err := ensureSharedDir(abs); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if r.ContentLength > maxSyncFileBytes {
		writeErr(w, 400, "file too large")
		return
	}
	if strings.HasSuffix(rel, ".vantage-tmp") {
		writeErr(w, 400, "invalid path")
		return
	}
	if err := ensureSharedDir(filepath.Dir(abs)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	tmp := abs + ".vantage-tmp"
	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	n, err := io.Copy(dst, io.LimitReader(r.Body, maxSyncFileBytes+1))
	_ = dst.Close()
	if err != nil {
		_ = os.Remove(tmp)
		writeErr(w, 500, err.Error())
		return
	}
	if n > maxSyncFileBytes {
		_ = os.Remove(tmp)
		writeErr(w, 400, "file too large")
		return
	}
	_ = ensureSharedFileMode(tmp)
	if ms := r.Header.Get("X-Vantage-Mtime"); ms != "" {
		if unix, err := strconv.ParseInt(ms, 10, 64); err == nil && unix > 0 {
			t := time.Unix(unix, 0)
			_ = os.Chtimes(tmp, t, t)
		}
	}
	if err := replaceFile(tmp, abs); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = ensureSharedFileMode(abs)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleSyncFileDelete(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if ignoredSharedPath(rel) {
		writeErr(w, 400, "excluded from sync")
		return
	}
	root, err := s.sharedRoot()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	abs, err := jailJoin(root, rel)
	if err != nil {
		writeSharedErr(w, err)
		return
	}
	if abs == root {
		writeErr(w, 400, "cannot delete shared root")
		return
	}
	if err := os.RemoveAll(abs); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func replaceFile(tmp, dest string) error {
	if err := os.Rename(tmp, dest); err == nil {
		return nil
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

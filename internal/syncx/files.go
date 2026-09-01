package syncx

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jdbnet/vantage/internal/store"
)

const (
	sharedManifestKey = "shared_files_sync_manifest"
	maxSyncFileBytes  = 512 << 20
	fileQuietAge      = 3 * time.Second
)

type fileMeta struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
	Dir   bool   `json:"dir"`
}

func (c *Client) fileLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	c.syncSharedFiles()
	for {
		select {
		case <-c.stop:
			return
		case <-c.fileKick:
			c.syncSharedFiles()
		case <-ticker.C:
			c.syncSharedFiles()
		}
	}
}

func (c *Client) localSharedDir() string {
	st, err := c.st.LoadSettings()
	if err == nil {
		if d := strings.TrimSpace(st.SharedFilesDir); d != "" {
			return d
		}
	}
	return filepath.Join(c.dataDir, "shared")
}

func (c *Client) syncSharedFiles() {
	settings, err := c.st.LoadSettings()
	if err != nil || settings.SyncURL == "" {
		return
	}
	key, ok, _ := c.st.SyncAPIKey()
	if !ok || key == "" {
		return
	}
	root := c.localSharedDir()
	if err := os.MkdirAll(root, 0o777); err != nil {
		c.setError(fmt.Errorf("shared files: %w", err))
		return
	}
	_ = os.Chmod(root, 0o777)
	base := strings.TrimRight(settings.SyncURL, "/")
	if err := reconcileSharedFiles(c.st, root, base, key); err != nil {
		c.setError(fmt.Errorf("shared files: %w", err))
		log.Printf("sync files: %v", err)
		return
	}
	c.clearFileError()
}

func reconcileSharedFiles(st *store.Store, root, base, key string) error {
	local, err := walkShared(root)
	if err != nil {
		return err
	}
	remote, err := fetchRemoteFiles(base, key)
	if err != nil {
		return err
	}
	prev := loadManifest(st)
	localM := indexFiles(local)
	remoteM := indexFiles(remote)

	paths := map[string]struct{}{}
	for p := range localM {
		paths[p] = struct{}{}
	}
	for p := range remoteM {
		paths[p] = struct{}{}
	}

	now := time.Now()
	var uploads, downloads []fileMeta
	var delLocal, delRemote []string
	for p := range paths {
		L, lok := localM[p]
		R, rok := remoteM[p]
		_, pok := prev[p]
		if (lok && fileUnstable(L, now)) || (rok && fileUnstable(R, now)) {
			continue
		}
		switch {
		case lok && !rok:
			if pok {
				delLocal = append(delLocal, p)
			} else {
				uploads = append(uploads, L)
			}
		case rok && !lok:
			if pok {
				delRemote = append(delRemote, p)
			} else {
				downloads = append(downloads, R)
			}
		case lok && rok:
			if L.Dir && R.Dir {
				continue
			}
			if L.Dir != R.Dir {
				continue
			}
			if !L.Dir && L.Size == R.Size && L.Mtime == R.Mtime {
				continue
			}
			if L.Mtime >= R.Mtime {
				uploads = append(uploads, L)
			} else {
				downloads = append(downloads, R)
			}
		}
	}

	sortFiles(uploads)
	sortFiles(downloads)
	sort.Slice(delRemote, func(i, j int) bool { return len(delRemote[i]) > len(delRemote[j]) })
	sort.Slice(delLocal, func(i, j int) bool { return len(delLocal[i]) > len(delLocal[j]) })

	for _, f := range uploads {
		if err := putRemoteFile(base, key, root, f); err != nil {
			return err
		}
	}
	for _, f := range downloads {
		if err := getRemoteFile(base, key, root, f); err != nil {
			return err
		}
	}
	for _, p := range delRemote {
		if err := deleteRemoteFile(base, key, p); err != nil {
			return err
		}
	}
	for _, p := range delLocal {
		abs := filepath.Join(root, filepath.FromSlash(p))
		_ = os.RemoveAll(abs)
	}

	after, err := walkShared(root)
	if err != nil {
		return err
	}
	return saveManifest(st, manifestAfter(after, prev, time.Now()))
}

func fileUnstable(f fileMeta, now time.Time) bool {
	if f.Dir || fileQuietAge <= 0 {
		return false
	}
	age := now.Sub(time.Unix(f.Mtime, 0))
	return age >= 0 && age < fileQuietAge
}

func manifestAfter(after []fileMeta, prev map[string]fileMeta, now time.Time) []fileMeta {
	out := make([]fileMeta, 0, len(after))
	for _, f := range after {
		if fileUnstable(f, now) {
			if old, ok := prev[f.Path]; ok {
				out = append(out, old)
			}
			continue
		}
		out = append(out, f)
	}
	return out
}

func sortFiles(files []fileMeta) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].Dir != files[j].Dir {
			return files[i].Dir
		}
		return files[i].Path < files[j].Path
	})
}

func indexFiles(files []fileMeta) map[string]fileMeta {
	m := make(map[string]fileMeta, len(files))
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}

func walkShared(root string) ([]fileMeta, error) {
	var out []fileMeta
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return nil
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
		if !d.IsDir() && info.Size() > maxSyncFileBytes {
			log.Printf("sync files: skip %s (too large)", rel)
			return nil
		}
		out = append(out, fileMeta{
			Path:  filepath.ToSlash(rel),
			Size:  info.Size(),
			Mtime: info.ModTime().Unix(),
			Dir:   d.IsDir(),
		})
		return nil
	})
	if out == nil {
		out = []fileMeta{}
	}
	return out, err
}

func loadManifest(st *store.Store) map[string]fileMeta {
	raw, ok, err := st.Meta(sharedManifestKey)
	if err != nil || !ok || raw == "" {
		return map[string]fileMeta{}
	}
	var files []fileMeta
	if json.Unmarshal([]byte(raw), &files) != nil {
		return map[string]fileMeta{}
	}
	return indexFiles(files)
}

func saveManifest(st *store.Store, files []fileMeta) error {
	b, err := json.Marshal(files)
	if err != nil {
		return err
	}
	return st.SetMeta(sharedManifestKey, string(b))
}

func fetchRemoteFiles(base, key string) ([]fileMeta, error) {
	req, err := http.NewRequest(http.MethodGet, base+"/api/sync/files", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := fileHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, httpStatusError("files list", resp.StatusCode)
	}
	var body struct {
		Files []fileMeta `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Files == nil {
		body.Files = []fileMeta{}
	}
	return body.Files, nil
}

func contentURL(base, rel string) string {
	u, _ := url.Parse(base + "/api/sync/files/content")
	q := u.Query()
	q.Set("path", rel)
	u.RawQuery = q.Encode()
	return u.String()
}

func putRemoteFile(base, key, root string, f fileMeta) error {
	if f.Dir {
		req, err := http.NewRequest(http.MethodPut, contentURL(base, f.Path), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-Vantage-Dir", "1")
		resp, err := fileHTTP.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return httpStatusError("files mkdir", resp.StatusCode)
		}
		return nil
	}
	abs := filepath.Join(root, filepath.FromSlash(f.Path))
	fh, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		return err
	}
	live := fileMeta{Path: f.Path, Size: info.Size(), Mtime: info.ModTime().Unix()}
	if info.Size() != f.Size || fileUnstable(live, time.Now()) {
		return nil
	}
	req, err := http.NewRequest(http.MethodPut, contentURL(base, f.Path), fh)
	if err != nil {
		return err
	}
	req.ContentLength = info.Size()
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Vantage-Mtime", strconv.FormatInt(info.ModTime().Unix(), 10))
	resp, err := fileHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return httpStatusError("files put", resp.StatusCode)
	}
	return nil
}

func getRemoteFile(base, key, root string, f fileMeta) error {
	abs := filepath.Join(root, filepath.FromSlash(f.Path))
	if f.Dir {
		return os.MkdirAll(abs, 0o777)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o777); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, contentURL(base, f.Path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := fileHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return httpStatusError("files get", resp.StatusCode)
	}
	tmp := abs + ".vantage-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, io.LimitReader(resp.Body, maxSyncFileBytes+1))
	_ = out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if n != f.Size {
		_ = os.Remove(tmp)
		return nil
	}
	if err := replaceFile(tmp, abs); err != nil {
		return err
	}
	_ = os.Chmod(abs, 0o666)
	mtime := f.Mtime
	if ms := resp.Header.Get("X-Vantage-Mtime"); ms != "" {
		if unix, err := strconv.ParseInt(ms, 10, 64); err == nil && unix > 0 {
			mtime = unix
		}
	}
	if mtime > 0 {
		t := time.Unix(mtime, 0)
		_ = os.Chtimes(abs, t, t)
	}
	return nil
}

func deleteRemoteFile(base, key, rel string) error {
	req, err := http.NewRequest(http.MethodDelete, contentURL(base, rel), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := fileHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		return httpStatusError("files delete", resp.StatusCode)
	}
	return nil
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

var fileHTTP = &http.Client{Timeout: 10 * time.Minute}

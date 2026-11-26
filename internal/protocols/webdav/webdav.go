package webdav

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jdbnet/vantage/internal/explorer"
	"github.com/studio-b12/gowebdav"
)

type WebDAVExplorer struct {
	cfg    explorer.Config
	client *gowebdav.Client
}

func New(c explorer.Config) *WebDAVExplorer {
	return &WebDAVExplorer{cfg: c}
}

func (e *WebDAVExplorer) Connect() error {
	host := e.cfg.Host
	if !strings.HasPrefix(host, "http") {
		if e.cfg.Secure {
			host = "https://" + host
		} else {
			host = "http://" + host
		}
	}
	if e.cfg.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, e.cfg.Port)
	}
	if e.cfg.Bucket != "" {
		host = host + "/" + e.cfg.Bucket
	}

	e.client = gowebdav.NewClient(host, e.cfg.Username, e.cfg.Secret)
	err := e.client.Connect()
	if err != nil {
		return err
	}
	return nil
}

func (e *WebDAVExplorer) Disconnect() error {
	return nil
}

func (e *WebDAVExplorer) ListDir(path string) ([]explorer.FileEntry, error) {
	if path == "" {
		path = "/"
	}
	files, err := e.client.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var entries []explorer.FileEntry
	for _, f := range files {
		entries = append(entries, explorer.FileEntry{
			Name:        f.Name(),
			Path:        strings.TrimSuffix(path, "/") + "/" + f.Name(),
			Size:        f.Size(),
			Modified:    f.ModTime().Format(time.RFC3339),
			IsDir:       f.IsDir(),
			Permissions: f.Mode().String(),
		})
	}
	return entries, nil
}

func (e *WebDAVExplorer) Stat(path string) (explorer.FileEntry, error) {
	f, err := e.client.Stat(path)
	if err != nil {
		return explorer.FileEntry{}, err
	}
	return explorer.FileEntry{
		Name:        f.Name(),
		Path:        path,
		Size:        f.Size(),
		Modified:    f.ModTime().Format(time.RFC3339),
		IsDir:       f.IsDir(),
		Permissions: f.Mode().String(),
	}, nil
}

func (e *WebDAVExplorer) MkDir(path string) error {
	return e.client.MkdirAll(path, 0755)
}

func (e *WebDAVExplorer) Delete(path string) error {
	return e.client.RemoveAll(path)
}

func (e *WebDAVExplorer) Rename(src, dst string) error {
	return e.client.Rename(src, dst, true)
}

func (e *WebDAVExplorer) ReadFile(path string) (io.ReadCloser, error) {
	return e.client.ReadStream(path)
}

func (e *WebDAVExplorer) WriteFile(path string, r io.Reader, size int64) error {
	return e.client.WriteStream(path, r, 0644)
}

func (e *WebDAVExplorer) Checksum(path string) (string, error) {
	r, err := e.ReadFile(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	h := md5.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

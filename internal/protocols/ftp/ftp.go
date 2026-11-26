package ftp

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/jdbnet/vantage/internal/explorer"
	"github.com/jlaffaye/ftp"
)

type FTPExplorer struct {
	cfg    explorer.Config
	client *ftp.ServerConn
}

func New(c explorer.Config) *FTPExplorer {
	return &FTPExplorer{cfg: c}
}

func (e *FTPExplorer) Connect() error {
	port := e.cfg.Port
	if port == 0 {
		port = 21
	}

	addr := fmt.Sprintf("%s:%d", e.cfg.Host, port)
	var c *ftp.ServerConn
	var err error

	if e.cfg.Secure {
		c, err = ftp.Dial(addr, ftp.DialWithExplicitTLS(&tls.Config{
			ServerName: e.cfg.Host,
		}))
	} else {
		c, err = ftp.Dial(addr)
	}

	if err != nil {
		return err
	}

	if err := c.Login(e.cfg.Username, e.cfg.Secret); err != nil {
		c.Quit()
		return err
	}

	e.client = c
	return nil
}

func (e *FTPExplorer) Disconnect() error {
	if e.client != nil {
		return e.client.Quit()
	}
	return nil
}

func (e *FTPExplorer) ListDir(path string) ([]explorer.FileEntry, error) {
	if path == "" {
		path = "."
	}

	entries, err := e.client.List(path)
	if err != nil {
		return nil, err
	}

	var res []explorer.FileEntry
	for _, f := range entries {
		if f.Name == "." || f.Name == ".." {
			continue
		}

		isDir := f.Type == ftp.EntryTypeFolder
		res = append(res, explorer.FileEntry{
			Name:        f.Name,
			Path:        filepath.ToSlash(filepath.Join(path, f.Name)),
			Size:        int64(f.Size),
			Modified:    f.Time.Format(time.RFC3339),
			IsDir:       isDir,
			Permissions: "",
		})
	}
	return res, nil
}

func (e *FTPExplorer) Stat(path string) (explorer.FileEntry, error) {
	if path == "" {
		path = "."
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)

	entries, err := e.client.List(dir)
	if err != nil {
		return explorer.FileEntry{}, err
	}

	for _, f := range entries {
		if f.Name == base {
			return explorer.FileEntry{
				Name:        f.Name,
				Path:        filepath.ToSlash(path),
				Size:        int64(f.Size),
				Modified:    f.Time.Format(time.RFC3339),
				IsDir:       f.Type == ftp.EntryTypeFolder,
				Permissions: "",
			}, nil
		}
	}

	return explorer.FileEntry{}, fmt.Errorf("file not found: %s", path)
}

func (e *FTPExplorer) MkDir(path string) error {
	return e.client.MakeDir(path)
}

func (e *FTPExplorer) removeAll(path string) error {
	entries, err := e.client.List(path)
	if err != nil {
		// If it fails to list, it might be a file
		return e.client.Delete(path)
	}

	// Try treating it as a directory to remove contents
	isDir := false
	for _, f := range entries {
		if f.Name == filepath.Base(path) && f.Type == ftp.EntryTypeFolder {
			isDir = true
			break
		}
	}

	if !isDir && len(entries) == 1 && entries[0].Name == filepath.Base(path) {
		return e.client.Delete(path)
	}

	// Remove contents
	for _, f := range entries {
		if f.Name == "." || f.Name == ".." {
			continue
		}
		subPath := filepath.ToSlash(filepath.Join(path, f.Name))
		if f.Type == ftp.EntryTypeFolder {
			if err := e.removeAll(subPath); err != nil {
				return err
			}
		} else {
			if err := e.client.Delete(subPath); err != nil {
				return err
			}
		}
	}

	return e.client.RemoveDir(path)
}

func (e *FTPExplorer) Delete(path string) error {
	return e.removeAll(path)
}

func (e *FTPExplorer) Rename(src, dst string) error {
	return e.client.Rename(src, dst)
}

func (e *FTPExplorer) ReadFile(path string) (io.ReadCloser, error) {
	return e.client.Retr(path)
}

func (e *FTPExplorer) WriteFile(path string, r io.Reader, size int64) error {
	return e.client.Stor(path, r)
}

func (e *FTPExplorer) Checksum(path string) (string, error) {
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

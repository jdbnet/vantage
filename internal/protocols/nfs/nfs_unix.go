//go:build !windows

package nfs

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"time"

	"github.com/jdbnet/vantage/internal/explorer"
	"github.com/vmware/go-nfs-client/nfs"
	"github.com/vmware/go-nfs-client/nfs/rpc"
)

type NFSExplorer struct {
	cfg    explorer.Config
	mount  *nfs.Mount
	target *nfs.Target
}

func New(c explorer.Config) (*NFSExplorer, error) {
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("NFS not supported on this platform")
	}
	return &NFSExplorer{cfg: c}, nil
}

func (e *NFSExplorer) Connect() error {
	mount, err := nfs.DialMount(e.cfg.Host)
	if err != nil {
		return err
	}
	e.mount = mount

	auth := rpc.AuthNull
	target, err := mount.Mount(e.cfg.Bucket, auth)
	if err != nil {
		mount.Close()
		return err
	}
	e.target = target
	return nil
}

func (e *NFSExplorer) Disconnect() error {
	if e.target != nil {
		e.target.Close()
	}
	if e.mount != nil {
		e.mount.Close()
	}
	return nil
}

func (e *NFSExplorer) ListDir(path string) ([]explorer.FileEntry, error) {
	if path == "" {
		path = "."
	}
	files, err := e.target.ReadDirPlus(path)
	if err != nil {
		return nil, err
	}

	var entries []explorer.FileEntry
	for _, f := range files {
		if f.Name() == "." || f.Name() == ".." {
			continue
		}
		entries = append(entries, explorer.FileEntry{
			Name:        f.Name(),
			Path:        filepath.ToSlash(filepath.Join(path, f.Name())),
			Size:        f.Size(),
			Modified:    f.ModTime().Format(time.RFC3339),
			IsDir:       f.IsDir(),
			Permissions: f.Mode().String(),
		})
	}
	return entries, nil
}

func (e *NFSExplorer) Stat(path string) (explorer.FileEntry, error) {
	if path == "" {
		path = "."
	}
	f, _, err := e.target.Lookup(path)
	if err != nil {
		return explorer.FileEntry{}, err
	}
	return explorer.FileEntry{
		Name:        f.Name(),
		Path:        filepath.ToSlash(path),
		Size:        f.Size(),
		Modified:    f.ModTime().Format(time.RFC3339),
		IsDir:       f.IsDir(),
		Permissions: f.Mode().String(),
	}, nil
}

func (e *NFSExplorer) MkDir(path string) error {
	_, err := e.target.Mkdir(path, 0755)
	return err
}

func (e *NFSExplorer) Delete(path string) error {
	return e.target.RemoveAll(path)
}

func (e *NFSExplorer) Rename(src, dst string) error {
	return fmt.Errorf("rename not fully supported by go-nfs-client wrapper")
}

func (e *NFSExplorer) ReadFile(path string) (io.ReadCloser, error) {
	return e.target.Open(path)
}

func (e *NFSExplorer) WriteFile(path string, r io.Reader, size int64) error {
	f, err := e.target.OpenFile(path, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (e *NFSExplorer) Checksum(path string) (string, error) {
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

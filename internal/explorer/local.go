package explorer

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"time"
)

type localFS struct{}

func NewLocal() Explorer {
	return localFS{}
}

func (localFS) Connect() error    { return nil }
func (localFS) Disconnect() error { return nil }

func entryFromInfo(p string, info os.FileInfo) FileEntry {
	return FileEntry{
		Name:        info.Name(),
		Path:        p,
		Size:        info.Size(),
		Modified:    info.ModTime().UTC().Format(time.RFC3339),
		IsDir:       info.IsDir(),
		Permissions: info.Mode().String(),
	}
}

func (localFS) ListDir(p string) ([]FileEntry, error) {
	p, err := CleanLocalPath(p)
	if err != nil {
		return nil, err
	}
	des, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]FileEntry, 0, len(des))
	for _, d := range des {
		info, err := d.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, entryFromInfo(filepath.Join(p, d.Name()), info))
	}
	return out, nil
}

func (localFS) Stat(p string) (FileEntry, error) {
	p, err := CleanLocalPath(p)
	if err != nil {
		return FileEntry{}, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return FileEntry{}, err
	}
	return entryFromInfo(p, info), nil
}

func (localFS) MkDir(p string) error {
	p, err := CleanLocalPath(p)
	if err != nil {
		return err
	}
	return os.MkdirAll(p, 0o755)
}

func (localFS) Delete(p string) error {
	p, err := CleanLocalPath(p)
	if err != nil {
		return err
	}
	if LocalRoot(p) {
		return os.ErrInvalid
	}
	return os.RemoveAll(p)
}

func (localFS) Rename(src, dst string) error {
	src, err := CleanLocalPath(src)
	if err != nil {
		return err
	}
	dst, err = CleanLocalPath(dst)
	if err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func (localFS) ReadFile(p string) (io.ReadCloser, error) {
	p, err := CleanLocalPath(p)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (localFS) WriteFile(p string, r io.Reader, _ int64) error {
	p, err := CleanLocalPath(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (e localFS) Checksum(p string) (string, error) {
	r, err := e.ReadFile(p)
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

package httpapi

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jdbnet/vantage/internal/explorer"
	"github.com/pkg/sftp"
)

type sessionFS struct {
	get   func() (*sftp.Client, error)
	reset func()
}

func (e sessionFS) client() (*sftp.Client, error) {
	return e.get()
}

func (e sessionFS) once(fn func(*sftp.Client) error) error {
	c, err := e.client()
	if err != nil {
		return err
	}
	err = fn(c)
	if err != nil && isSFTPDead(err) && e.reset != nil {
		e.reset()
		c, err = e.client()
		if err != nil {
			return err
		}
		return fn(c)
	}
	return err
}

func (sessionFS) Connect() error    { return nil }
func (sessionFS) Disconnect() error { return nil }

func (e sessionFS) clean(p string) (string, *sftp.Client, error) {
	c, err := e.client()
	if err != nil {
		return "", nil, err
	}
	p = strings.TrimSpace(p)
	if p == "" {
		wd, err := c.Getwd()
		if err != nil || strings.TrimSpace(wd) == "" {
			return "/", c, nil
		}
		cleaned, err := sanitizePath(wd)
		if err != nil {
			return "/", c, nil
		}
		return cleaned, c, nil
	}
	cleaned, err := sanitizePath(p)
	if err != nil {
		return "", nil, err
	}
	return cleaned, c, nil
}

func sftpEntry(p string, f os.FileInfo) explorer.FileEntry {
	return explorer.FileEntry{
		Name:        f.Name(),
		Path:        p,
		Size:        f.Size(),
		Modified:    f.ModTime().UTC().Format(time.RFC3339),
		IsDir:       f.IsDir(),
		Permissions: f.Mode().String(),
	}
}

func (e sessionFS) ListDir(p string) ([]explorer.FileEntry, error) {
	var out []explorer.FileEntry
	err := e.once(func(c *sftp.Client) error {
		target := p
		if strings.TrimSpace(target) == "" {
			wd, err := c.Getwd()
			if err != nil || strings.TrimSpace(wd) == "" {
				target = "/"
			} else if cleaned, err := sanitizePath(wd); err == nil {
				target = cleaned
			} else {
				target = "/"
			}
		} else {
			cleaned, err := sanitizePath(target)
			if err != nil {
				return err
			}
			target = cleaned
		}
		files, err := c.ReadDir(target)
		if err != nil {
			return err
		}
		out = make([]explorer.FileEntry, 0, len(files))
		for _, f := range files {
			out = append(out, sftpEntry(path.Join(target, f.Name()), f))
		}
		return nil
	})
	return out, err
}

func (e sessionFS) Stat(p string) (explorer.FileEntry, error) {
	var ent explorer.FileEntry
	err := e.once(func(c *sftp.Client) error {
		target := p
		if strings.TrimSpace(target) == "" {
			wd, err := c.Getwd()
			if err != nil || strings.TrimSpace(wd) == "" {
				target = "/"
			} else if cleaned, err := sanitizePath(wd); err == nil {
				target = cleaned
			} else {
				target = "/"
			}
		} else {
			cleaned, err := sanitizePath(target)
			if err != nil {
				return err
			}
			target = cleaned
		}
		f, err := c.Stat(target)
		if err != nil {
			return err
		}
		ent = sftpEntry(target, f)
		return nil
	})
	return ent, err
}

func (e sessionFS) MkDir(p string) error {
	return e.once(func(c *sftp.Client) error {
		cleaned, err := sanitizePath(p)
		if err != nil {
			return err
		}
		return c.MkdirAll(cleaned)
	})
}

func (e sessionFS) Delete(p string) error {
	return e.once(func(c *sftp.Client) error {
		cleaned, err := sanitizePath(p)
		if err != nil {
			return err
		}
		return removeSFTPTree(c, cleaned)
	})
}

func (e sessionFS) Rename(src, dst string) error {
	return e.once(func(c *sftp.Client) error {
		a, err := sanitizePath(src)
		if err != nil {
			return err
		}
		b, err := sanitizePath(dst)
		if err != nil {
			return err
		}
		return c.Rename(a, b)
	})
}

func (e sessionFS) ReadFile(p string) (io.ReadCloser, error) {
	cleaned, c, err := e.clean(p)
	if err != nil {
		return nil, err
	}
	f, err := c.Open(cleaned)
	if err != nil && isSFTPDead(err) && e.reset != nil {
		e.reset()
		cleaned, c, err = e.clean(p)
		if err != nil {
			return nil, err
		}
		return c.Open(cleaned)
	}
	return f, err
}

func (e sessionFS) WriteFile(p string, r io.Reader, _ int64) error {
	return e.once(func(c *sftp.Client) error {
		cleaned, err := sanitizePath(p)
		if err != nil {
			return err
		}
		f, err := c.Create(cleaned)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, r)
		return err
	})
}

func (e sessionFS) Checksum(p string) (string, error) {
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

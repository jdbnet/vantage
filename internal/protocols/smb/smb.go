package smb

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/hirochachacha/go-smb2"
	"github.com/jdbnet/vantage/internal/explorer"
)

type SMBExplorer struct {
	cfg          explorer.Config
	conn         net.Conn
	sess         *smb2.Session
	defaultShare *smb2.Share
	shares       map[string]*smb2.Share
}

func New(c explorer.Config) *SMBExplorer {
	return &SMBExplorer{cfg: c, shares: make(map[string]*smb2.Share)}
}

func (e *SMBExplorer) Connect() error {
	port := e.cfg.Port
	if port == 0 {
		port = 445
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", e.cfg.Host, port))
	if err != nil {
		return err
	}
	e.conn = conn

	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     e.cfg.Username,
			Password: e.cfg.Secret,
		},
	}

	s, err := d.Dial(conn)
	if err != nil {
		conn.Close()
		return err
	}
	e.sess = s

	if e.cfg.Bucket != "" {
		share, err := s.Mount(e.cfg.Bucket)
		if err != nil {
			s.Logoff()
			conn.Close()
			return err
		}
		e.defaultShare = share
	}

	return nil
}

func (e *SMBExplorer) Disconnect() error {
	if e.defaultShare != nil {
		e.defaultShare.Umount()
	}
	for _, s := range e.shares {
		s.Umount()
	}
	if e.sess != nil {
		e.sess.Logoff()
	}
	if e.conn != nil {
		e.conn.Close()
	}
	return nil
}

func (e *SMBExplorer) getShareAndPath(path string) (*smb2.Share, string, string, error) {
	if e.defaultShare != nil {
		if path == "" || path == "/" {
			path = "."
		}
		return e.defaultShare, strings.ReplaceAll(path, "/", "\\"), "", nil
	}

	path = strings.TrimPrefix(filepath.ToSlash(path), "/")
	if path == "" || path == "." {
		return nil, "", "", fmt.Errorf("no share specified")
	}

	parts := strings.SplitN(path, "/", 2)
	sharename := parts[0]
	subpath := "."
	if len(parts) > 1 {
		subpath = parts[1]
	}

	if s, ok := e.shares[sharename]; ok {
		return s, strings.ReplaceAll(subpath, "/", "\\"), sharename, nil
	}

	s, err := e.sess.Mount(sharename)
	if err != nil {
		return nil, "", "", err
	}
	e.shares[sharename] = s
	return s, strings.ReplaceAll(subpath, "/", "\\"), sharename, nil
}

func (e *SMBExplorer) ListDir(path string) ([]explorer.FileEntry, error) {
	if e.defaultShare == nil && (path == "" || path == "/" || path == ".") {
		shares, err := e.sess.ListSharenames()
		if err != nil {
			return nil, err
		}
		var entries []explorer.FileEntry
		for _, s := range shares {
			entries = append(entries, explorer.FileEntry{
				Name:        s,
				Path:        s,
				IsDir:       true,
				Permissions: "share",
			})
		}
		return entries, nil
	}

	share, subpath, shareName, err := e.getShareAndPath(path)
	if err != nil {
		return nil, err
	}

	files, err := share.ReadDir(subpath)
	if err != nil {
		return nil, err
	}

	var entries []explorer.FileEntry
	for _, f := range files {
		fullPath := ""
		if e.defaultShare != nil {
			fullPath = strings.ReplaceAll(filepath.Join(subpath, f.Name()), "\\", "/")
		} else {
			fullPath = filepath.ToSlash(filepath.Join(shareName, subpath, f.Name()))
		}
		entries = append(entries, explorer.FileEntry{
			Name:        f.Name(),
			Path:        fullPath,
			Size:        f.Size(),
			Modified:    f.ModTime().Format(time.RFC3339),
			IsDir:       f.IsDir(),
			Permissions: f.Mode().String(),
		})
	}
	return entries, nil
}

func (e *SMBExplorer) Stat(path string) (explorer.FileEntry, error) {
	if e.defaultShare == nil && (path == "" || path == "/" || path == ".") {
		return explorer.FileEntry{
			Name: "/", Path: "/", IsDir: true, Permissions: "root",
		}, nil
	}

	share, subpath, shareName, err := e.getShareAndPath(path)
	if err != nil {
		return explorer.FileEntry{}, err
	}

	f, err := share.Stat(subpath)
	if err != nil {
		return explorer.FileEntry{}, err
	}

	fullPath := strings.ReplaceAll(subpath, "\\", "/")
	if e.defaultShare == nil {
		fullPath = filepath.ToSlash(filepath.Join(shareName, subpath))
	}
	return explorer.FileEntry{
		Name:        f.Name(),
		Path:        fullPath,
		Size:        f.Size(),
		Modified:    f.ModTime().Format(time.RFC3339),
		IsDir:       f.IsDir(),
		Permissions: f.Mode().String(),
	}, nil
}

func (e *SMBExplorer) MkDir(path string) error {
	share, subpath, _, err := e.getShareAndPath(path)
	if err != nil {
		return err
	}
	return share.Mkdir(subpath, 0755)
}

func (e *SMBExplorer) Delete(path string) error {
	share, subpath, _, err := e.getShareAndPath(path)
	if err != nil {
		return err
	}
	return share.RemoveAll(subpath)
}

func (e *SMBExplorer) Rename(src, dst string) error {
	share1, subpath1, shareName1, err := e.getShareAndPath(src)
	if err != nil {
		return err
	}
	_, subpath2, shareName2, err := e.getShareAndPath(dst)
	if err != nil {
		return err
	}
	if shareName1 != shareName2 {
		return fmt.Errorf("cross-share rename not supported")
	}
	return share1.Rename(subpath1, subpath2)
}

func (e *SMBExplorer) ReadFile(path string) (io.ReadCloser, error) {
	share, subpath, _, err := e.getShareAndPath(path)
	if err != nil {
		return nil, err
	}
	return share.Open(subpath)
}

func (e *SMBExplorer) WriteFile(path string, r io.Reader, size int64) error {
	share, subpath, _, err := e.getShareAndPath(path)
	if err != nil {
		return err
	}
	f, err := share.Create(subpath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (e *SMBExplorer) Checksum(path string) (string, error) {
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

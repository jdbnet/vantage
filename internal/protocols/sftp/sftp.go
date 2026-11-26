package sftp

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/jdbnet/vantage/internal/explorer"
	pkgsftp "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Explorer struct {
	cfg     explorer.Config
	hostKey ssh.HostKeyCallback
	client  *pkgsftp.Client
	conn    *ssh.Client
}

func New(c explorer.Config, hostKey ssh.HostKeyCallback) *Explorer {
	return &Explorer{cfg: c, hostKey: hostKey}
}

func (e *Explorer) Connect() error {
	port := e.cfg.Port
	if port == 0 {
		port = 22
	}
	secret := strings.TrimSpace(e.cfg.Secret)
	secret = strings.ReplaceAll(secret, "\\n", "\n")
	var methods []ssh.AuthMethod
	if strings.Contains(secret, "-----BEGIN") || e.cfg.SftpKeyAuth {
		var signer ssh.Signer
		var err error
		if e.cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(secret), []byte(e.cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(secret))
		}
		if err != nil {
			return fmt.Errorf("failed to parse private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	} else if secret != "" {
		methods = append(methods, ssh.Password(secret))
		methods = append(methods, ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i, q := range questions {
				if strings.Contains(strings.ToLower(q), "password") {
					answers[i] = secret
				}
			}
			return answers, nil
		}))
	}
	if e.hostKey == nil {
		return fmt.Errorf("host key callback required")
	}
	cfg := &ssh.ClientConfig{
		User:            e.cfg.Username,
		Auth:            methods,
		HostKeyCallback: e.hostKey,
		Timeout:         30 * time.Second,
	}
	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", e.cfg.Host, port), cfg)
	if err != nil {
		return err
	}
	client, err := pkgsftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return err
	}
	e.conn = conn
	e.client = client
	return nil
}

func (e *Explorer) Disconnect() error {
	if e.client != nil {
		_ = e.client.Close()
		e.client = nil
	}
	if e.conn != nil {
		_ = e.conn.Close()
		e.conn = nil
	}
	return nil
}

func (e *Explorer) normalize(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "." {
		if b := strings.TrimSpace(e.cfg.Bucket); b != "" {
			return path.Clean(b)
		}
		if e.client != nil {
			if wd, err := e.client.Getwd(); err == nil && strings.TrimSpace(wd) != "" {
				return path.Clean(wd)
			}
		}
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

func (e *Explorer) ListDir(p string) ([]explorer.FileEntry, error) {
	p = e.normalize(p)
	files, err := e.client.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]explorer.FileEntry, 0, len(files))
	for _, f := range files {
		out = append(out, explorer.FileEntry{
			Name:        f.Name(),
			Path:        path.Join(p, f.Name()),
			Size:        f.Size(),
			Modified:    f.ModTime().UTC().Format(time.RFC3339),
			IsDir:       f.IsDir(),
			Permissions: f.Mode().String(),
		})
	}
	return out, nil
}

func (e *Explorer) Stat(p string) (explorer.FileEntry, error) {
	p = e.normalize(p)
	f, err := e.client.Stat(p)
	if err != nil {
		return explorer.FileEntry{}, err
	}
	return explorer.FileEntry{
		Name:        f.Name(),
		Path:        p,
		Size:        f.Size(),
		Modified:    f.ModTime().UTC().Format(time.RFC3339),
		IsDir:       f.IsDir(),
		Permissions: f.Mode().String(),
	}, nil
}

func (e *Explorer) MkDir(p string) error {
	return e.client.MkdirAll(e.normalize(p))
}

func (e *Explorer) removeAll(p string) error {
	fi, err := e.client.Stat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return e.client.Remove(p)
	}
	files, err := e.client.ReadDir(p)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.Name() == "." || f.Name() == ".." {
			continue
		}
		if err := e.removeAll(path.Join(p, f.Name())); err != nil {
			return err
		}
	}
	return e.client.RemoveDirectory(p)
}

func (e *Explorer) Delete(p string) error {
	return e.removeAll(e.normalize(p))
}

func (e *Explorer) Rename(src, dst string) error {
	return e.client.Rename(e.normalize(src), e.normalize(dst))
}

func (e *Explorer) ReadFile(p string) (io.ReadCloser, error) {
	return e.client.Open(e.normalize(p))
}

func (e *Explorer) WriteFile(p string, r io.Reader, _ int64) error {
	f, err := e.client.Create(e.normalize(p))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (e *Explorer) Checksum(p string) (string, error) {
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

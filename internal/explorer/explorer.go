package explorer

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type FileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Modified    string `json:"modified"`
	IsDir       bool   `json:"is_dir"`
	Permissions string `json:"permissions"`
}

type Config struct {
	ID          string
	Label       string
	Protocol    string
	Host        string
	Port        int
	Bucket      string
	Region      string
	PathStyle   bool
	Secure      bool
	Username    string
	SftpKeyAuth bool
	Secret      string
	Passphrase  string
}

type Explorer interface {
	Connect() error
	Disconnect() error
	ListDir(path string) ([]FileEntry, error)
	Stat(path string) (FileEntry, error)
	MkDir(path string) error
	Delete(path string) error
	Rename(src, dst string) error
	ReadFile(path string) (io.ReadCloser, error)
	WriteFile(path string, r io.Reader, size int64) error
	Checksum(path string) (string, error)
}

// Ranger can read a byte range without pulling the whole object.
// S3 uses this. Local files and SFTP expose io.ReaderAt instead.
type Ranger interface {
	ReadRange(path string, start, length int64) (io.ReadCloser, error)
}

// Exclusive reports protocols that cannot safely share one connection.
func Exclusive(protocol string) bool {
	switch protocol {
	case "ftp", "smb", "webdav", "nfs":
		return true
	default:
		return false
	}
}

func CleanLocalPath(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", errors.New("invalid path")
	}
	p = strings.TrimSpace(p)
	if p == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			p = string(os.PathSeparator)
		} else {
			p = home
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if !filepath.IsAbs(abs) {
		return "", errors.New("invalid path")
	}
	return abs, nil
}

func LocalRoot(p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	return abs == string(os.PathSeparator) || abs == filepath.VolumeName(abs)+string(os.PathSeparator)
}

//go:build windows

package nfs

import (
	"fmt"
	"io"

	"github.com/jdbnet/vantage/internal/explorer"
)

type NFSExplorer struct{}

func New(c explorer.Config) (*NFSExplorer, error) {
	return nil, fmt.Errorf("NFS is not supported on Windows")
}

var errNFSWindows = fmt.Errorf("NFS is not supported on Windows")

func (e *NFSExplorer) Connect() error                                    { return errNFSWindows }
func (e *NFSExplorer) Disconnect() error                                 { return nil }
func (e *NFSExplorer) ListDir(path string) ([]explorer.FileEntry, error) { return nil, errNFSWindows }
func (e *NFSExplorer) Stat(path string) (explorer.FileEntry, error) {
	return explorer.FileEntry{}, errNFSWindows
}
func (e *NFSExplorer) MkDir(path string) error                              { return errNFSWindows }
func (e *NFSExplorer) Delete(path string) error                             { return errNFSWindows }
func (e *NFSExplorer) Rename(src, dst string) error                         { return errNFSWindows }
func (e *NFSExplorer) ReadFile(path string) (io.ReadCloser, error)          { return nil, errNFSWindows }
func (e *NFSExplorer) WriteFile(path string, r io.Reader, size int64) error { return errNFSWindows }
func (e *NFSExplorer) Checksum(path string) (string, error)                 { return "", errNFSWindows }

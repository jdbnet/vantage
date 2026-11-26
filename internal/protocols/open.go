package protocols

import (
	"fmt"

	"github.com/jdbnet/vantage/internal/explorer"
	"github.com/jdbnet/vantage/internal/protocols/ftp"
	"github.com/jdbnet/vantage/internal/protocols/nfs"
	"github.com/jdbnet/vantage/internal/protocols/s3"
	sftpx "github.com/jdbnet/vantage/internal/protocols/sftp"
	"github.com/jdbnet/vantage/internal/protocols/smb"
	"github.com/jdbnet/vantage/internal/protocols/webdav"
	"golang.org/x/crypto/ssh"
)

// Open connects a saved remote. hostKey is used for SFTP and ignored otherwise.
func Open(cfg explorer.Config, hostKey ssh.HostKeyCallback) (explorer.Explorer, error) {
	var exp explorer.Explorer
	switch cfg.Protocol {
	case "s3":
		exp = s3.New(cfg)
	case "sftp":
		exp = sftpx.New(cfg, hostKey)
	case "smb":
		exp = smb.New(cfg)
	case "webdav":
		exp = webdav.New(cfg)
	case "ftp":
		exp = ftp.New(cfg)
	case "nfs":
		n, err := nfs.New(cfg)
		if err != nil {
			return nil, err
		}
		exp = n
	default:
		return nil, fmt.Errorf("unknown protocol %s", cfg.Protocol)
	}
	if err := exp.Connect(); err != nil {
		return nil, err
	}
	return exp, nil
}

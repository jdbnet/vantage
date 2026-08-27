package sshx

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/jdbnet/vantage/internal/cryptox"
	"github.com/jdbnet/vantage/internal/model"
	"github.com/jdbnet/vantage/internal/store"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Creds struct {
	User       string
	Password   string
	PrivateKey string
	Passphrase string
	Domain     string
}

type Session struct {
	ID      string
	HostID  string
	Label   string
	Client  *ssh.Client
	Stdin   io.WriteCloser
	Stdout  io.Reader
	Sess    *ssh.Session
	Jumps   []*ssh.Client
	SFTP    *sftp.Client
	mu      sync.Mutex
	AuditID int64
	Started time.Time
}

func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SFTP != nil {
		_ = s.SFTP.Close()
		s.SFTP = nil
	}
	if s.Stdin != nil {
		_ = s.Stdin.Close()
		s.Stdin = nil
	}
	if s.Sess != nil {
		_ = s.Sess.Close()
		s.Sess = nil
	}
	if s.Client != nil {
		_ = s.Client.Close()
		s.Client = nil
	}
	for i := len(s.Jumps) - 1; i >= 0; i-- {
		_ = s.Jumps[i].Close()
	}
	s.Jumps = nil
}

func (s *Session) GetSFTP() (*sftp.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SFTP != nil {
		return s.SFTP, nil
	}
	if s.Client == nil {
		return nil, fmt.Errorf("ssh client closed")
	}
	c, err := sftp.NewClient(s.Client)
	if err != nil {
		return nil, err
	}
	s.SFTP = c
	return c, nil
}

type Registry struct {
	mu sync.Mutex
	m  map[string]*Session
}

func NewRegistry() *Registry {
	return &Registry{m: map[string]*Session{}}
}

func (r *Registry) Put(s *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[s.ID] = s
}

func (r *Registry) Get(id string) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.m[id]
}

func (r *Registry) Pop(id string) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.m[id]
	delete(r.m, id)
	return s
}

func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.m)
}

func ResolveCreds(st *store.Store, box *cryptox.Box, hostID string) (store.HostRecord, Creds, error) {
	rec, err := st.GetHostRecord(hostID)
	if err != nil {
		return rec, Creds{}, err
	}
	var blob, pass string
	if rec.Host.IdentityID != nil {
		ident, err := st.GetIdentity(*rec.Host.IdentityID)
		if err != nil {
			return rec, Creds{}, err
		}
		blob = ident.Blob
		if ident.Passphrase != nil {
			pass = *ident.Passphrase
		}
	} else if rec.InlineBlob != nil {
		blob = *rec.InlineBlob
		if rec.InlinePassphrase != nil {
			pass = *rec.InlinePassphrase
		}
	} else {
		return rec, Creds{}, fmt.Errorf("host has no credentials")
	}
	plain, err := box.Decrypt(blob)
	if err != nil {
		return rec, Creds{}, err
	}
	var sec model.IdentitySecret
	if err := json.Unmarshal([]byte(plain), &sec); err != nil {
		return rec, Creds{}, err
	}
	if pass != "" {
		pp, err := box.Decrypt(pass)
		if err == nil {
			sec.Passphrase = pp
		}
	}
	return rec, Creds{
		User:       sec.Username,
		Password:   sec.Password,
		PrivateKey: sec.PrivateKey,
		Passphrase: sec.Passphrase,
		Domain:     sec.Domain,
	}, nil
}

func Connect(st *store.Store, box *cryptox.Box, hostID string, cols, rows int, prompt *Prompt) (*Session, error) {
	chain, err := jumpChain(st, hostID)
	if err != nil {
		return nil, err
	}
	var jumps []*ssh.Client
	var client *ssh.Client
	for i, hid := range chain {
		rec, creds, err := ResolveCreds(st, box, hid)
		if err != nil {
			closeAll(client, jumps)
			return nil, err
		}
		cfg, err := sshConfig(st, creds, prompt, rec.Host.Hostname, rec.Host.Port)
		if err != nil {
			closeAll(client, jumps)
			return nil, err
		}
		addr := net.JoinHostPort(rec.Host.Hostname, fmt.Sprintf("%d", rec.Host.Port))
		var c *ssh.Client
		if i == 0 {
			c, err = ssh.Dial("tcp", addr, cfg)
		} else {
			nc, e := client.Dial("tcp", addr)
			if e != nil {
				closeAll(client, jumps)
				return nil, e
			}
			conn, chans, reqs, e := ssh.NewClientConn(nc, addr, cfg)
			if e != nil {
				_ = nc.Close()
				closeAll(client, jumps)
				return nil, e
			}
			c = ssh.NewClient(conn, chans, reqs)
			jumps = append(jumps, client)
		}
		if err != nil {
			closeAll(client, jumps)
			return nil, err
		}
		client = c
	}

	session, err := client.NewSession()
	if err != nil {
		closeAll(client, jumps)
		return nil, err
	}
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 40
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = session.Close()
		closeAll(client, jumps)
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		closeAll(client, jumps)
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		closeAll(client, jumps)
		return nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		closeAll(client, jumps)
		return nil, err
	}
	go io.Copy(io.Discard, stderr)
	if err := session.Shell(); err != nil {
		_ = session.Close()
		closeAll(client, jumps)
		return nil, err
	}

	host, err := st.GetHost(hostID)
	if err != nil {
		_ = session.Close()
		closeAll(client, jumps)
		return nil, err
	}
	return &Session{
		HostID:  hostID,
		Label:   host.Label,
		Client:  client,
		Stdin:   stdin,
		Stdout:  stdout,
		Sess:    session,
		Jumps:   jumps,
		Started: time.Now(),
	}, nil
}

func closeAll(client *ssh.Client, jumps []*ssh.Client) {
	if client != nil {
		_ = client.Close()
	}
	for i := len(jumps) - 1; i >= 0; i-- {
		_ = jumps[i].Close()
	}
}

func jumpChain(st *store.Store, hostID string) ([]string, error) {
	var rev []string
	seen := map[string]struct{}{}
	cur := hostID
	for cur != "" {
		if _, ok := seen[cur]; ok {
			return nil, fmt.Errorf("jump host cycle")
		}
		seen[cur] = struct{}{}
		rev = append(rev, cur)
		rec, err := st.GetHostRecord(cur)
		if err != nil {
			return nil, err
		}
		if rec.Host.JumpHostID == nil {
			break
		}
		cur = *rec.Host.JumpHostID
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, nil
}

func Resize(s *Session, cols, rows int) {
	if s != nil && s.Sess != nil {
		_ = s.Sess.WindowChange(rows, cols)
	}
}
